//go:build darwin

package collector

/*
#cgo CFLAGS: -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework Foundation -framework AppKit
#include <stdlib.h>
#include "native_darwin.h"
*/
import "C"

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
	"unsafe"
)

// Native is the real collector for macOS.
type Native struct {
	mu        sync.Mutex
	lastBusy  uint64
	lastTotal uint64
	pidBuf    []C.int
}

// NewNative returns the macOS collector.
func NewNative() *Native {
	n := &Native{pidBuf: make([]C.int, 8192)}
	// Prime the CPU counters so the first System() call has a baseline.
	var busy, total C.uint64_t
	var ncpu C.int
	if C.ht_cpu_ticks(&busy, &total, &ncpu) == 0 {
		n.lastBusy, n.lastTotal = uint64(busy), uint64(total)
	}
	return n
}

// System takes one system snapshot.
func (n *Native) System() (Snapshot, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := Snapshot{Time: time.Now()}

	var m C.ht_mem
	if C.ht_memory(&m) == 0 {
		s.Memory = memoryFromPages(pageCounts{
			free: uint64(m.free_count), active: uint64(m.active_count), inactive: uint64(m.inactive_count),
			wired: uint64(m.wire_count), compressor: uint64(m.compressor_count), purgeable: uint64(m.purgeable_count),
			speculative: uint64(m.speculative_count), external: uint64(m.external_count), internal: uint64(m.internal_count),
			pageSize: uint64(m.page_size), memsize: uint64(m.memsize),
		})
	} else {
		s.Unavailable = append(s.Unavailable, "memory")
	}

	var sw C.ht_swap
	if C.ht_swap_usage(&sw) == 0 {
		s.Swap = Swap{Total: uint64(sw.total), Used: uint64(sw.used), Free: uint64(sw.avail), Encrypted: sw.encrypted != 0}
	} else {
		s.Unavailable = append(s.Unavailable, "swap")
	}

	var lvl C.int
	if C.ht_pressure(&lvl) == 0 {
		s.Pressure = Pressure(lvl)
	} else {
		s.Unavailable = append(s.Unavailable, "pressure")
	}

	var dt, di, dr C.uint64_t
	if C.ht_disk(&dt, &di, &dr) == 0 {
		s.Disk = Disk{Total: uint64(dt), Available: uint64(di), RawFree: uint64(dr)}
	} else {
		s.Unavailable = append(s.Unavailable, "disk")
	}

	var busy, total C.uint64_t
	var ncpu C.int
	if C.ht_cpu_ticks(&busy, &total, &ncpu) == 0 {
		db, dtot := uint64(busy)-n.lastBusy, uint64(total)-n.lastTotal
		if dtot > 0 && uint64(total) >= n.lastTotal {
			s.CPU.Usage = float64(db) / float64(dtot)
		}
		s.CPU.Cores = int(ncpu)
		n.lastBusy, n.lastTotal = uint64(busy), uint64(total)
	} else {
		s.Unavailable = append(s.Unavailable, "cpu")
	}

	s.Thermal = Thermal(C.ht_thermal())
	return s, nil
}

// Processes lists every process with its footprint, CPU time and disk I/O.
// Processes owned by other users (including root) are listed with
// Accessible=false; their numbers are unknown, not zero.
func (n *Native) Processes() (ProcessTable, error) {
	n.mu.Lock()
	cnt := int(C.ht_list_pids(&n.pidBuf[0], C.int(len(n.pidBuf))))
	pids := make([]int, 0, cnt)
	for i := 0; i < cnt && i < len(n.pidBuf); i++ {
		if p := int(n.pidBuf[i]); p > 0 {
			pids = append(pids, p)
		}
	}
	n.mu.Unlock()
	if cnt <= 0 {
		return ProcessTable{}, errors.New("proc_listallpids failed")
	}

	t := ProcessTable{Time: time.Now(), SelfUID: uint32(os.Getuid()), SelfPID: os.Getpid()}
	t.Processes = make([]Process, 0, len(pids))
	var info C.ht_proc
	for _, pid := range pids {
		if C.ht_proc_info(C.int(pid), &info) != 0 {
			continue
		}
		t.Processes = append(t.Processes, Process{
			PID:        pid,
			PPID:       int(info.ppid),
			UID:        uint32(info.uid),
			Name:       C.GoString(&info.name[0]),
			Path:       C.GoString(&info.path[0]),
			Footprint:  uint64(info.footprint),
			CPUTimeNs:  uint64(info.cpu_ns),
			DiskRead:   uint64(info.rbytes),
			DiskWrite:  uint64(info.wbytes),
			Accessible: info.rusage_ok != 0,
		})
	}

	cs := C.ht_running_apps_json()
	defer C.free(unsafe.Pointer(cs))
	_ = json.Unmarshal([]byte(C.GoString(cs)), &t.Apps)
	return t, nil
}

// SysctlString reads a string sysctl (e.g. hw.model).
func SysctlString(name string) (string, bool) {
	cn := C.CString(name)
	defer C.free(unsafe.Pointer(cn))
	buf := make([]byte, 256)
	if C.ht_sysctl_string(cn, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf))) != 0 {
		return "", false
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), true
}

// SysctlUint reads an integer sysctl (e.g. hw.memsize).
func SysctlUint(name string) (uint64, bool) {
	cn := C.CString(name)
	defer C.free(unsafe.Pointer(cn))
	var v C.uint64_t
	if C.ht_sysctl_u64(cn, &v) != 0 {
		return 0, false
	}
	return uint64(v), true
}

// HostInfo is OS-level machine information from NSProcessInfo.
type HostInfo struct {
	OSVersion      string `json:"osVersion"`
	Cores          int    `json:"cores"`
	PhysicalMemory uint64 `json:"physicalMemory"`
}

// Host returns NSProcessInfo machine details.
func Host() HostInfo {
	cs := C.ht_machine_json()
	defer C.free(unsafe.Pointer(cs))
	var h HostInfo
	_ = json.Unmarshal([]byte(C.GoString(cs)), &h)
	return h
}
