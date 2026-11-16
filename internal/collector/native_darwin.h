// Native samplers. Public macOS APIs only; no private frameworks, no root.
#ifndef ANTIFREEZE_NATIVE_H
#define ANTIFREEZE_NATIVE_H
#include <stdint.h>
#include <stddef.h>

typedef struct {
    uint64_t free_count, active_count, inactive_count, wire_count, compressor_count,
        purgeable_count, speculative_count, external_count, internal_count, page_size, memsize;
} ht_mem;

typedef struct {
    uint64_t total, used, avail;
    int encrypted;
} ht_swap;

typedef struct {
    int pid, ppid;
    uint32_t uid;
    int rusage_ok;
    uint64_t footprint, cpu_ns, rbytes, wbytes;
    char name[256];
    char path[4096];
} ht_proc;

int ht_memory(ht_mem *out);
int ht_swap_usage(ht_swap *out);
int ht_pressure(int *level);
int ht_cpu_ticks(uint64_t *busy, uint64_t *total, int *ncpu);
int ht_sysctl_string(const char *name, char *buf, size_t len);
int ht_sysctl_u64(const char *name, uint64_t *out);
int ht_list_pids(int *buf, int cap);
int ht_proc_info(int pid, ht_proc *out);

// Objective-C (native_darwin.m)
int ht_disk(uint64_t *total, uint64_t *important, uint64_t *raw);
int ht_thermal(void);
char *ht_running_apps_json(void); // caller frees
char *ht_machine_json(void);      // caller frees

#endif
