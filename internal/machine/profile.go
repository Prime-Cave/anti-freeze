// Package machine detects the Mac Anti-Freeze runs on and derives every
// threshold from it. Nothing about any particular machine is hard-coded.
package machine

import (
	"fmt"
	"strings"
	"time"
)

// Profile is detected on first launch and stored.
type Profile struct {
	Model       string    `json:"model"`       // hw.model, e.g. "Mac14,2"
	ModelName   string    `json:"modelName"`   // friendly, e.g. "MacBook Air"
	Chip        string    `json:"chip"`        // machdep.cpu.brand_string
	Arch        string    `json:"arch"`        // arm64 / amd64
	OSVersion   string    `json:"osVersion"`   // e.g. "14.6.1"
	MemoryBytes uint64    `json:"memoryBytes"` // hw.memsize
	DiskBytes   uint64    `json:"diskBytes"`   // boot volume capacity
	Cores       int       `json:"cores"`
	FirstSeen   time.Time `json:"firstSeen"`
}

const gb = uint64(1) << 30

// Thresholds are the numbers the analyzer judges against.
type Thresholds struct {
	// DiskReserve: free disk below this means "tight", and swap may not use it.
	DiskReserve uint64 `json:"diskReserve"`
	// DiskFloor: free disk below this means "critical".
	DiskFloor uint64 `json:"diskFloor"`
	// SwapBudget: total swap the Mac can carry before overflow itself slows it.
	SwapBudget uint64 `json:"swapBudget"`
	// SwapGrowthTight: swap growing faster than this (bytes/second) is "tight".
	SwapGrowthTight float64 `json:"swapGrowthTight"`
	// ForecastCritical: a forecast slowdown sooner than this is "critical".
	ForecastCritical time.Duration `json:"forecastCritical"`
	// Learned is true when history has made these more cautious than defaults.
	Learned bool `json:"learned"`
}

// Defaults derives the default thresholds from the machine's size.
//
//	DiskReserve      = 10% of disk, at least 8 GB, at most 16 GB
//	DiskFloor        = half the reserve, at least 4 GB
//	SwapBudget       = half of installed memory
//	SwapGrowthTight  = 1% of memory per minute, at least 50 MB/min
//	ForecastCritical = 5 minutes
func (p Profile) Defaults() Thresholds {
	reserve := p.DiskBytes / 10
	reserve = max(reserve, 8*gb)
	reserve = min(reserve, 16*gb)
	floor := max(reserve/2, 4*gb)
	growth := max(float64(p.MemoryBytes)/100/60, 50*1024*1024/60.0)
	return Thresholds{
		DiskReserve:      reserve,
		DiskFloor:        floor,
		SwapBudget:       p.MemoryBytes / 2,
		SwapGrowthTight:  growth,
		ForecastCritical: 5 * time.Minute,
	}
}

// Sensitivity is the user's alert setting.
type Sensitivity string

const (
	Relaxed  Sensitivity = "relaxed"
	Balanced Sensitivity = "balanced"
	Early    Sensitivity = "early"
)

// Apply scales thresholds for a sensitivity. Balanced leaves them unchanged.
func (t Thresholds) Apply(s Sensitivity) Thresholds {
	switch s {
	case Relaxed:
		t.DiskReserve = t.DiskReserve * 3 / 4
		t.SwapGrowthTight *= 1.5
		t.ForecastCritical = 3 * time.Minute
	case Early:
		t.DiskReserve = t.DiskReserve * 5 / 4
		t.SwapGrowthTight *= 0.6
		t.ForecastCritical = 10 * time.Minute
	}
	return t
}

// Describe is the dashboard's machine line, e.g.
// "MacBook Air, 8 GB memory, 256 GB disk. Watching since 9 Nov."
func (p Profile) Describe(now time.Time) string {
	name := p.ModelName
	if name == "" {
		name = "This Mac"
	}
	since := p.FirstSeen.Format("2 Jan")
	if p.FirstSeen.Year() != now.Year() {
		since = p.FirstSeen.Format("2 Jan 2006")
	}
	return fmt.Sprintf("%s, %s memory, %s disk. Watching since %s.",
		name, RoundSize(p.MemoryBytes), RoundSize(p.DiskBytes), since)
}

// RoundSize shows a capacity the way Apple markets it (256 GB, 1 TB).
// Disk capacities are decimal; memory is binary.
func RoundSize(b uint64) string {
	if b == 0 {
		return "unknown"
	}
	g := float64(b) / float64(gb)
	if b%(gb) != 0 { // decimal (disk) sizes
		g = float64(b) / 1e9
	}
	steps := []float64{4, 8, 16, 18, 24, 32, 36, 48, 64, 96, 128, 192, 256, 512, 1000, 2000, 4000, 8000}
	best := steps[0]
	for _, s := range steps {
		if g >= s*0.85 {
			best = s
		}
	}
	if best >= 1000 {
		return fmt.Sprintf("%.0f TB", best/1000)
	}
	return fmt.Sprintf("%.0f GB", best)
}

// FriendlyName maps hw.model to a family name when the registry has none.
func FriendlyName(model string) string {
	switch {
	case strings.HasPrefix(model, "MacBookAir"):
		return "MacBook Air"
	case strings.HasPrefix(model, "MacBookPro"):
		return "MacBook Pro"
	case strings.HasPrefix(model, "MacBook"):
		return "MacBook"
	case strings.HasPrefix(model, "Macmini"):
		return "Mac mini"
	case strings.HasPrefix(model, "MacPro"):
		return "Mac Pro"
	case strings.HasPrefix(model, "iMac"):
		return "iMac"
	}
	return "Mac"
}
