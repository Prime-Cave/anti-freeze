package collector

import (
	"math"
	"time"
)

// Scenario builds synthetic, deterministic sample streams. Nothing here is
// recorded from a real machine. Used by tests (via testdata fixtures), the
// demo mode and README screenshots.
type Scenario struct {
	Snaps  []Snapshot
	Tables []ProcessTable
}

const mib = uint64(1) << 20

// SlowdownScenario models an 8 GB / 256 GB Mac over 40 minutes at 5 s steps:
//
//	 0–10 min  steady: pressure normal, 1 GB swap, browser at 1.2 GB
//	10–25 min  browser grows ~150 MB/min; swap starts climbing; pressure warn at 15 min
//	25–31 min  pressure critical; swap climbs fast; disk free falls with it
//	31–40 min  the browser is quit: swap stops growing, pressure returns to normal
func SlowdownScenario(start time.Time) Scenario {
	const (
		step     = 5 * time.Second
		total    = 40 * time.Minute
		memTotal = 8 << 30
		diskSize = uint64(245_107_195_904)
	)
	var sc Scenario
	swap := float64(1 << 30)
	browser := float64(1200 * mib)
	diskBase := float64(22 << 30)
	cpuBase := uint64(0)
	quitAt := 31 * time.Minute
	for d := time.Duration(0); d <= total; d += step {
		t := start.Add(d)
		m := d.Minutes()
		pressure := PressureNormal
		switch {
		case d >= quitAt:
			browser = 0
		case m >= 10:
			browser += 150 * float64(mib) / 12 // per 5 s
		}
		switch {
		case d >= quitAt:
			// overflow drains slowly
			swap = math.Max(float64(1<<30), swap-20*float64(mib)/12)
		case m >= 25:
			swap += 400 * float64(mib) / 12
			pressure = PressureCritical
		case m >= 12:
			swap += 120 * float64(mib) / 12
			if m >= 15 {
				pressure = PressureWarn
			}
		}
		// small deterministic wobble so the fit isn't perfect
		wobble := 8 * float64(mib) * math.Sin(float64(d/step)*0.7)
		used := uint64(math.Min(float64(memTotal)-200*float64(mib), 5.2*float64(1<<30)+browser*0.6))
		if d >= quitAt {
			used = 4939212390 // ≈4.6 GB
		}
		compressed := uint64(math.Min(float64(used)/3, browser/3+600*float64(mib)))
		wired := uint64(1500 * mib)
		app := used - compressed - wired
		disk := diskBase - (swap - float64(1<<30))
		snap := Snapshot{
			Time: t,
			Memory: Memory{
				Total: memTotal, Used: used, AppMemory: app, Wired: wired, Compressed: compressed,
				Free: (memTotal - used) / 3, Cached: (memTotal - used) * 2 / 3, External: (memTotal - used) * 2 / 3,
				PageSize: 16384,
			},
			Swap:     Swap{Total: uint64(swap) + 1<<30, Used: uint64(swap + wobble), Free: 1 << 30, Encrypted: true},
			Pressure: pressure,
			Disk:     Disk{Total: diskSize, Available: uint64(disk), RawFree: uint64(disk) - 2<<30},
			CPU:      CPU{Usage: 0.22 + 0.1*math.Sin(m), Cores: 8},
			Thermal:  ThermalNominal,
		}
		if pressure == PressureCritical {
			snap.Thermal = ThermalFair
			snap.CPU.Usage = 0.71
		}
		sc.Snaps = append(sc.Snaps, snap)

		if int(d/step)%2 == 0 { // process table every 10 s
			cpuBase += uint64(2 * time.Second) // the browser burns ~20% of a core
			sc.Tables = append(sc.Tables, demoTable(t, uint64(browser), cpuBase))
		}
	}
	return sc
}

// SteadyScenario is a calm Mac: pressure normal, nothing growing.
func SteadyScenario(start time.Time, n int) Scenario {
	var sc Scenario
	for i := 0; i < n; i++ {
		t := start.Add(time.Duration(i) * 5 * time.Second)
		sc.Snaps = append(sc.Snaps, Snapshot{
			Time: t,
			Memory: Memory{Total: 8 << 30, Used: 5 << 30, AppMemory: 3 << 30, Wired: 1 << 30, Compressed: 1 << 30,
				Free: 1 << 30, Cached: 2 << 30, PageSize: 16384},
			Swap:     Swap{Used: 512 * mib, Total: 1 << 30},
			Pressure: PressureNormal,
			Disk:     Disk{Total: 245_107_195_904, Available: 61 << 30},
			CPU:      CPU{Usage: 0.12, Cores: 8},
		})
		if i%2 == 0 {
			sc.Tables = append(sc.Tables, demoTable(t, 1200*mib, uint64(i)*uint64(time.Second)))
		}
	}
	return sc
}

// demoTable is a small synthetic process table. App names are generic.
func demoTable(t time.Time, browser uint64, cpu uint64) ProcessTable {
	const uid = 501
	br := "/Applications/Browser.app"
	pt := ProcessTable{Time: t, SelfUID: uid, SelfPID: 999}
	if browser > 0 {
		helper := br + "/Contents/Frameworks/Browser Helper.app/Contents/MacOS/Browser Helper"
		pt.Processes = append(pt.Processes,
			Process{PID: 100, UID: uid, Name: "Browser", Path: br + "/Contents/MacOS/Browser", Footprint: browser / 4, CPUTimeNs: cpu / 2, Accessible: true})
		for i := 0; i < 11; i++ {
			pt.Processes = append(pt.Processes, Process{PID: 101 + i, PPID: 100, UID: uid, Name: "Browser Helper", Path: helper,
				Footprint: browser * 3 / 4 / 11, CPUTimeNs: cpu / 22, Accessible: true})
		}
		pt.Apps = append(pt.Apps, RunningApp{PID: 100, Name: "Browser", BundleID: "com.example.browser", BundlePath: br, Regular: true})
	}
	add := func(pid int, name, bundle string, fp uint64) {
		pt.Processes = append(pt.Processes, Process{PID: pid, UID: uid, Name: name, Path: bundle + "/Contents/MacOS/" + name, Footprint: fp, Accessible: true})
		pt.Apps = append(pt.Apps, RunningApp{PID: pid, Name: name, BundleID: "com.example." + name, BundlePath: bundle, Regular: true})
	}
	add(200, "Chat", "/Applications/Chat.app", 900*mib)
	add(210, "Editor", "/Applications/Editor.app", 640*mib)
	add(220, "Music", "/Applications/Music.app", 310*mib)
	add(230, "Notes", "/Applications/Notes.app", 180*mib)
	pt.Processes = append(pt.Processes,
		Process{PID: 1, UID: 0, Name: "launchd", Path: "/sbin/launchd"},
		Process{PID: 150, UID: 88, Name: "WindowServer", Path: "/System/Library/PrivateFrameworks/SkyLight.framework/Resources/WindowServer"},
		Process{PID: 300, UID: uid, Name: "Finder", Path: "/System/Library/CoreServices/Finder.app/Contents/MacOS/Finder", Footprint: 150 * mib, Accessible: true},
		Process{PID: 999, UID: uid, Name: "Headroom", Path: "/Applications/Headroom.app/Contents/MacOS/Headroom", Footprint: 45 * mib, Accessible: true},
	)
	pt.Apps = append(pt.Apps,
		RunningApp{PID: 300, Name: "Finder", BundleID: "com.apple.finder", BundlePath: "/System/Library/CoreServices/Finder.app", Regular: true},
		RunningApp{PID: 999, Name: "Headroom", BundlePath: "/Applications/Headroom.app"},
	)
	return pt
}
