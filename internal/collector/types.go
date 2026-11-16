// Package collector samples system performance through public macOS APIs.
//
// Everything sits behind the Collector interface so the analyzer and UI can be
// driven by a Fake that replays recorded (synthetic) samples.
package collector

import "time"

// Memory figures in bytes, from host_statistics64(HOST_VM_INFO64).
//
// Used and AppMemory match Activity Monitor's "Memory Used" and "App Memory":
//
//	AppMemory = internal − purgeable
//	Used      = AppMemory + Wired + Compressed
//	Cached    = external + purgeable          ("Cached Files")
type Memory struct {
	Total       uint64 `json:"total"`
	Free        uint64 `json:"free"`        // free_count − speculative_count
	Active      uint64 `json:"active"`      // active_count
	Inactive    uint64 `json:"inactive"`    // inactive_count
	Wired       uint64 `json:"wired"`       // wire_count
	Compressed  uint64 `json:"compressed"`  // compressor_page_count: RAM the compressor occupies ("Squeezed")
	Purgeable   uint64 `json:"purgeable"`   // purgeable_count
	Speculative uint64 `json:"speculative"` // speculative_count
	External    uint64 `json:"external"`    // file-backed pages
	Internal    uint64 `json:"internal"`    // anonymous pages
	AppMemory   uint64 `json:"appMemory"`
	Used        uint64 `json:"used"`
	Cached      uint64 `json:"cached"`
	PageSize    uint64 `json:"pageSize"`
}

// Swap from sysctl vm.swapusage.
type Swap struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Free      uint64 `json:"free"`
	Encrypted bool   `json:"encrypted"`
}

// Pressure is kern.memorystatus_vm_pressure_level.
type Pressure int

const (
	PressureUnknown  Pressure = 0
	PressureNormal   Pressure = 1
	PressureWarn     Pressure = 2
	PressureCritical Pressure = 4
)

func (p Pressure) String() string {
	switch p {
	case PressureNormal:
		return "normal"
	case PressureWarn:
		return "warn"
	case PressureCritical:
		return "critical"
	}
	return "unknown"
}

// Disk is the boot volume, from NSURLVolumeAvailableCapacityForImportantUsageKey
// (which counts purgeable APFS space as available, unlike statfs).
type Disk struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"` // for important usage
	RawFree   uint64 `json:"rawFree"`   // NSURLVolumeAvailableCapacityKey
}

// Thermal is NSProcessInfo.thermalState.
type Thermal int

const (
	ThermalNominal Thermal = iota
	ThermalFair
	ThermalSerious
	ThermalCritical
)

// Label is the UI word for the thermal state.
func (t Thermal) Label() string {
	switch t {
	case ThermalFair:
		return "Fair"
	case ThermalSerious:
		return "Serious"
	case ThermalCritical:
		return "Critical"
	}
	return "Normal"
}

// CPU is whole-machine load between two host_processor_info samples.
type CPU struct {
	Usage float64 `json:"usage"` // 0..1 across all cores
	Cores int     `json:"cores"`
}

// Snapshot is one system sample. Metrics that could not be read are listed
// in Unavailable (by JSON field name) and must be shown as
// "Not available on this Mac", never as 0.
type Snapshot struct {
	Time        time.Time `json:"time"`
	Memory      Memory    `json:"memory"`
	Swap        Swap      `json:"swap"`
	Pressure    Pressure  `json:"pressure"`
	Disk        Disk      `json:"disk"`
	CPU         CPU       `json:"cpu"`
	Thermal     Thermal   `json:"thermal"`
	Unavailable []string  `json:"unavailable,omitempty"`
}

// Has reports whether a metric was read successfully.
func (s Snapshot) Has(metric string) bool {
	for _, u := range s.Unavailable {
		if u == metric {
			return false
		}
	}
	return true
}

// Process is one process from libproc.
type Process struct {
	PID        int    `json:"pid"`
	PPID       int    `json:"ppid"`
	UID        uint32 `json:"uid"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	Footprint  uint64 `json:"footprint"` // ri_phys_footprint (matches Activity Monitor)
	CPUTimeNs  uint64 `json:"cpuTimeNs"` // user + system, nanoseconds
	DiskRead   uint64 `json:"diskRead"`
	DiskWrite  uint64 `json:"diskWrite"`
	Accessible bool   `json:"accessible"` // false when rusage was denied (other users, root)
}

// RunningApp is an NSRunningApplication entry.
type RunningApp struct {
	PID        int    `json:"pid"`
	Name       string `json:"name"`
	BundleID   string `json:"bundleId"`
	BundlePath string `json:"bundlePath"`
	Regular    bool   `json:"regular"` // activationPolicy == regular (has a Dock icon)
}

// ProcessTable is one process sample.
type ProcessTable struct {
	Time      time.Time    `json:"time"`
	Processes []Process    `json:"processes"`
	Apps      []RunningApp `json:"apps"`
	SelfUID   uint32       `json:"selfUid"`
	SelfPID   int          `json:"selfPid"`
}

// Collector is the sampling interface. Implementations must be safe to call
// from one goroutine at a time.
type Collector interface {
	System() (Snapshot, error)
	Processes() (ProcessTable, error)
}
