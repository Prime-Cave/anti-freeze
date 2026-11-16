package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/collector"
)

// runSample prints one JSON snapshot: system figures plus the top app groups.
// Used to compare Anti-Freeze's numbers with Activity Monitor.
func runSample() int {
	c := collector.NewNative()
	time.Sleep(time.Second) // give CPU load a one-second window
	snap, err := c.System()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sample:", err)
		return 1
	}
	pt, err := c.Processes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "processes:", err)
		return 1
	}
	groups := collector.Group(pt)
	if len(groups) > 10 {
		groups = groups[:10]
	}
	type groupOut struct {
		Name      string  `json:"name"`
		Count     int     `json:"count"`
		Footprint string  `json:"footprint"`
		Protected bool    `json:"protected"`
		Reason    string  `json:"reason,omitempty"`
		CPUSecs   float64 `json:"cpuSeconds"`
	}
	top := make([]groupOut, 0, len(groups))
	for _, g := range groups {
		top = append(top, groupOut{g.Name, g.Count, gb(g.Footprint), g.Protected, g.Reason, float64(g.CPUTimeNs) / 1e9})
	}
	m := snap.Memory
	out := map[string]any{
		"snapshot": snap,
		"activityMonitorView": map[string]string{
			"physicalMemory": gb(m.Total),
			"memoryUsed":     gb(m.Used),
			"appMemory":      gb(m.AppMemory),
			"wiredMemory":    gb(m.Wired),
			"compressed":     gb(m.Compressed),
			"cachedFiles":    gb(m.Cached),
			"swapUsed":       gb(snap.Swap.Used),
			"diskAvailable":  gb(snap.Disk.Available),
		},
		"processCount": len(pt.Processes),
		"topApps":      top,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return 0
}

func gb(b uint64) string { return fmt.Sprintf("%.2f GB", float64(b)/(1<<30)) }
