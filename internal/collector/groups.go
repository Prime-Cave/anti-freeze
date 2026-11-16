package collector

import (
	"path/filepath"
	"sort"
	"strings"
)

// AppGroup rolls helper processes up into the app the user recognises, so
// Chrome's renderers and GPU helper appear as "Google Chrome (41)".
//
// Grouping uses only public information: the outermost ".app" bundle in each
// process's executable path (proc_pidpath), matched to NSRunningApplication
// for the display name and bundle id. (The private
// responsibility_get_pid_responsible_for_pid is deliberately not used.)
type AppGroup struct {
	Key        string `json:"key"` // bundle path, or "proc:<name>" for unbundled processes
	Name       string `json:"name"`
	BundleID   string `json:"bundleId,omitempty"`
	BundlePath string `json:"bundlePath,omitempty"`
	MainPID    int    `json:"mainPid,omitempty"` // NSRunningApplication pid used for quitting
	PIDs       []int  `json:"pids"`
	Count      int    `json:"count"`
	Footprint  uint64 `json:"footprint"`
	CPUTimeNs  uint64 `json:"cpuTimeNs"`
	DiskBytes  uint64 `json:"diskBytes"` // read + written, cumulative
	Protected  bool   `json:"protected"`
	Reason     string `json:"reason,omitempty"` // why it is protected
	Known      bool   `json:"known"`            // false when footprint could not be read
}

// protectedNames are never offered for quitting.
var protectedNames = map[string]bool{
	"WindowServer": true, "kernel_task": true, "loginwindow": true, "Finder": true,
	"Dock": true, "SystemUIServer": true, "launchd": true,
}

// protectedPrefixes are system locations whose apps keep macOS itself running
// (Control Centre, Notification Centre, Spotlight…). Quitting them is never
// the right fix, so they are protected too.
var protectedPrefixes = []string{"/System/Library/", "/usr/", "/sbin/", "/bin/", "/Library/Apple/"}

// OutermostBundle returns the outermost "*.app" directory in path, or "".
func OutermostBundle(path string) string {
	i := strings.Index(path, ".app/")
	if i < 0 {
		if strings.HasSuffix(path, ".app") {
			return path
		}
		return ""
	}
	return path[:i+len(".app")]
}

// Group builds app groups from a process table, largest footprint first.
func Group(t ProcessTable) []AppGroup {
	apps := map[string]RunningApp{}
	for _, a := range t.Apps {
		if a.BundlePath == "" {
			continue
		}
		// Prefer the regular (Dock) app if a bundle has several entries.
		if prev, ok := apps[a.BundlePath]; !ok || (!prev.Regular && a.Regular) {
			apps[a.BundlePath] = a
		}
	}

	groups := map[string]*AppGroup{}
	for _, p := range t.Processes {
		bundle := OutermostBundle(p.Path)
		key := bundle
		if key == "" {
			key = "proc:" + processName(p)
		}
		g := groups[key]
		if g == nil {
			g = &AppGroup{Key: key, BundlePath: bundle, Known: true}
			if bundle != "" {
				if a, ok := apps[bundle]; ok {
					g.Name, g.BundleID, g.MainPID = a.Name, a.BundleID, a.PID
				} else {
					g.Name = strings.TrimSuffix(filepath.Base(bundle), ".app")
				}
			} else {
				g.Name = processName(p)
			}
			groups[key] = g
		}
		g.PIDs = append(g.PIDs, p.PID)
		g.Count++
		if p.Accessible {
			g.Footprint += p.Footprint
			g.CPUTimeNs += p.CPUTimeNs
			g.DiskBytes += p.DiskRead + p.DiskWrite
		} else {
			g.Known = false
		}
		if reason := protectReason(p, bundle, t); reason != "" && g.Reason == "" {
			g.Protected, g.Reason = true, reason
		}
	}

	out := make([]AppGroup, 0, len(groups))
	for _, g := range groups {
		if !g.Protected && g.MainPID == 0 {
			g.Protected, g.Reason = true, "Not a running app"
		}
		if protectedNames[g.Name] {
			g.Protected, g.Reason = true, "Part of macOS"
		}
		sort.Ints(g.PIDs)
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Footprint != out[j].Footprint {
			return out[i].Footprint > out[j].Footprint
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func protectReason(p Process, bundle string, t ProcessTable) string {
	switch {
	case p.PID == t.SelfPID:
		return "Anti-Freeze itself"
	case protectedNames[p.Name]:
		return "Part of macOS"
	case p.UID != t.SelfUID:
		return "Owned by the system or another user"
	case bundle == "":
		return "No app bundle"
	}
	for _, pre := range protectedPrefixes {
		if strings.HasPrefix(p.Path, pre) {
			return "Part of macOS"
		}
	}
	return ""
}

// processName prefers the executable's file name: the kernel truncates
// p_comm to 16 characters ("com.apple.Virtua").
func processName(p Process) string {
	if p.Path != "" {
		return filepath.Base(p.Path)
	}
	return p.Name
}
