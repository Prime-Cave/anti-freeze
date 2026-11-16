//go:build darwin

#include "native_darwin.h"
#include <errno.h>
#include <libproc.h>
#include <mach/mach.h>
#include <mach/mach_time.h>
#include <string.h>
#include <sys/proc_info.h>
#include <sys/resource.h>
#include <sys/sysctl.h>

int ht_memory(ht_mem *out) {
    memset(out, 0, sizeof(*out));
    vm_statistics64_data_t vm;
    mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
    mach_port_t host = mach_host_self();
    kern_return_t kr = host_statistics64(host, HOST_VM_INFO64, (host_info64_t)&vm, &count);
    if (kr != KERN_SUCCESS) {
        mach_port_deallocate(mach_task_self(), host);
        return -1;
    }
    vm_size_t ps = 0;
    if (host_page_size(host, &ps) != KERN_SUCCESS || ps == 0) ps = vm_kernel_page_size;
    mach_port_deallocate(mach_task_self(), host);

    out->free_count = vm.free_count;
    out->active_count = vm.active_count;
    out->inactive_count = vm.inactive_count;
    out->wire_count = vm.wire_count;
    out->compressor_count = vm.compressor_page_count;
    out->purgeable_count = vm.purgeable_count;
    out->speculative_count = vm.speculative_count;
    out->external_count = vm.external_page_count;
    out->internal_count = vm.internal_page_count;
    out->page_size = ps;

    uint64_t memsize = 0;
    size_t len = sizeof(memsize);
    if (sysctlbyname("hw.memsize", &memsize, &len, NULL, 0) != 0) return -2;
    out->memsize = memsize;
    return 0;
}

int ht_swap_usage(ht_swap *out) {
    struct xsw_usage sw;
    size_t len = sizeof(sw);
    if (sysctlbyname("vm.swapusage", &sw, &len, NULL, 0) != 0) return -1;
    out->total = sw.xsu_total;
    out->used = sw.xsu_used;
    out->avail = sw.xsu_avail;
    out->encrypted = sw.xsu_encrypted ? 1 : 0;
    return 0;
}

int ht_pressure(int *level) {
    int v = 0;
    size_t len = sizeof(v);
    if (sysctlbyname("kern.memorystatus_vm_pressure_level", &v, &len, NULL, 0) != 0) return -1;
    *level = v;
    return 0;
}

// Sums tick counters across CPUs. The caller diffs two samples.
int ht_cpu_ticks(uint64_t *busy, uint64_t *total, int *ncpu) {
    natural_t n = 0;
    processor_info_array_t info = NULL;
    mach_msg_type_number_t count = 0;
    mach_port_t host = mach_host_self();
    kern_return_t kr = host_processor_info(host, PROCESSOR_CPU_LOAD_INFO, &n, &info, &count);
    mach_port_deallocate(mach_task_self(), host);
    if (kr != KERN_SUCCESS) return -1;
    uint64_t b = 0, t = 0;
    for (natural_t i = 0; i < n; i++) {
        uint64_t user = info[CPU_STATE_MAX * i + CPU_STATE_USER];
        uint64_t sys = info[CPU_STATE_MAX * i + CPU_STATE_SYSTEM];
        uint64_t nice = info[CPU_STATE_MAX * i + CPU_STATE_NICE];
        uint64_t idle = info[CPU_STATE_MAX * i + CPU_STATE_IDLE];
        b += user + sys + nice;
        t += user + sys + nice + idle;
    }
    vm_deallocate(mach_task_self(), (vm_address_t)info, (vm_size_t)count * sizeof(integer_t));
    *busy = b;
    *total = t;
    *ncpu = (int)n;
    return 0;
}

int ht_sysctl_string(const char *name, char *buf, size_t len) {
    size_t l = len;
    if (sysctlbyname(name, buf, &l, NULL, 0) != 0) return -1;
    if (l >= len) l = len - 1;
    buf[l] = 0;
    return 0;
}

int ht_sysctl_u64(const char *name, uint64_t *out) {
    uint64_t v = 0;
    size_t l = sizeof(v);
    if (sysctlbyname(name, &v, &l, NULL, 0) != 0) {
        uint32_t v32 = 0;
        l = sizeof(v32);
        if (sysctlbyname(name, &v32, &l, NULL, 0) != 0) return -1;
        v = v32;
    }
    *out = v;
    return 0;
}

int ht_list_pids(int *buf, int cap) {
    int n = proc_listallpids(buf, cap * (int)sizeof(int));
    return n; // number of pids, or <= 0 on error
}

// rusage CPU times are in Mach absolute-time units, not nanoseconds, on
// Apple Silicon (1 tick = 125/3 ns). Convert with the timebase.
static mach_timebase_info_data_t tb;

int ht_proc_info(int pid, ht_proc *out) {
    memset(out, 0, sizeof(*out));
    out->pid = pid;
    if (tb.denom == 0) mach_timebase_info(&tb);

    struct proc_bsdinfo bsd;
    int n = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &bsd, sizeof(bsd));
    if (n == (int)sizeof(bsd)) {
        out->ppid = (int)bsd.pbi_ppid;
        out->uid = bsd.pbi_uid;
        const char *nm = bsd.pbi_name[0] ? bsd.pbi_name : bsd.pbi_comm;
        strlcpy(out->name, nm, sizeof(out->name));
    } else {
        // Full BSD info is denied for other users' processes; the short form
        // is still readable and is enough to list and protect them.
        struct proc_bsdshortinfo sh;
        n = proc_pidinfo(pid, PROC_PIDT_SHORTBSDINFO, 0, &sh, sizeof(sh));
        if (n != (int)sizeof(sh)) return -1; // process gone
        out->ppid = (int)sh.pbsi_ppid;
        out->uid = sh.pbsi_uid;
        strlcpy(out->name, sh.pbsi_comm, sizeof(out->name));
    }

    if (proc_pidpath(pid, out->path, sizeof(out->path)) <= 0) out->path[0] = 0;

    struct rusage_info_v4 ri;
    if (proc_pid_rusage(pid, RUSAGE_INFO_V4, (rusage_info_t *)&ri) == 0) {
        out->rusage_ok = 1;
        out->footprint = ri.ri_phys_footprint;
        uint64_t ticks = ri.ri_user_time + ri.ri_system_time;
        out->cpu_ns = ticks * tb.numer / tb.denom;
        out->rbytes = ri.ri_diskio_bytesread;
        out->wbytes = ri.ri_diskio_byteswritten;
    }
    return 0;
}
