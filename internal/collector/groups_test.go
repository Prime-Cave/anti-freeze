package collector

import "testing"

const GB = 1 << 30

func synthTable() ProcessTable {
	chrome := "/Applications/Google Chrome.app"
	helper := chrome + "/Contents/Frameworks/Google Chrome Framework.framework/Helpers/Google Chrome Helper (Renderer).app/Contents/MacOS/Google Chrome Helper (Renderer)"
	t := ProcessTable{SelfUID: 501, SelfPID: 900}
	t.Processes = []Process{
		{PID: 100, UID: 501, Name: "Google Chrome", Path: chrome + "/Contents/MacOS/Google Chrome", Footprint: 1 * GB, Accessible: true},
		{PID: 101, UID: 501, Name: "Google Chrome He", Path: helper, Footprint: 2 * GB, Accessible: true},
		{PID: 102, UID: 501, Name: "Google Chrome He", Path: helper, Footprint: GB / 2, Accessible: true},
		{PID: 1, UID: 0, Name: "launchd", Path: "/sbin/launchd"},
		{PID: 150, UID: 88, Name: "WindowServer", Path: "/System/Library/PrivateFrameworks/SkyLight.framework/Resources/WindowServer"},
		{PID: 200, UID: 501, Name: "Finder", Path: "/System/Library/CoreServices/Finder.app/Contents/MacOS/Finder", Footprint: GB / 4, Accessible: true},
		{PID: 300, UID: 501, Name: "node", Path: "/usr/local/bin/node", Footprint: GB, Accessible: true},
		{PID: 900, UID: 501, Name: "Headroom", Path: "/Applications/Headroom.app/Contents/MacOS/Headroom", Footprint: 50 << 20, Accessible: true},
		{PID: 400, UID: 502, Name: "Notes", Path: "/Applications/Notes.app/Contents/MacOS/Notes", Footprint: GB, Accessible: false},
		{PID: 500, UID: 501, Name: "com.apple.Virtua", Path: "/System/Library/Frameworks/Virtualization.framework/Versions/A/XPCServices/com.apple.Virtualization.VirtualMachine.xpc/Contents/MacOS/com.apple.Virtualization.VirtualMachine", Footprint: GB, Accessible: true},
	}
	t.Apps = []RunningApp{
		{PID: 100, Name: "Google Chrome", BundleID: "com.google.Chrome", BundlePath: chrome, Regular: true},
		{PID: 200, Name: "Finder", BundleID: "com.apple.finder", BundlePath: "/System/Library/CoreServices/Finder.app", Regular: true},
		{PID: 900, Name: "Headroom", BundlePath: "/Applications/Headroom.app"},
	}
	return t
}

func TestOutermostBundle(t *testing.T) {
	cases := map[string]string{
		"/Applications/A.app/Contents/Frameworks/B.app/Contents/MacOS/B": "/Applications/A.app",
		"/usr/bin/top":        "",
		"/Applications/X.app": "/Applications/X.app",
		"":                    "",
	}
	for in, want := range cases {
		if got := OutermostBundle(in); got != want {
			t.Errorf("OutermostBundle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGroupRollsUpHelpers(t *testing.T) {
	gs := Group(synthTable())
	byName := map[string]AppGroup{}
	for _, g := range gs {
		byName[g.Name] = g
	}
	c, ok := byName["Google Chrome"]
	if !ok {
		t.Fatalf("no Chrome group in %+v", gs)
	}
	if c.Count != 3 || c.Footprint != GB*7/2 || c.MainPID != 100 || c.Protected {
		t.Fatalf("chrome group wrong: %+v", c)
	}
	if gs[0].Name != "Google Chrome" {
		t.Fatalf("largest group should sort first, got %s", gs[0].Name)
	}
	if _, ok := byName["com.apple.Virtualization.VirtualMachine"]; !ok {
		t.Fatal("unbundled process should be named from its full executable name")
	}
}

func TestGroupProtectedList(t *testing.T) {
	gs := Group(synthTable())
	want := map[string]bool{
		"launchd": true, "WindowServer": true, "Finder": true, "node": true,
		"Headroom": true, "Notes": true, "com.apple.Virtualization.VirtualMachine": true,
		"Google Chrome": false,
	}
	for _, g := range gs {
		w, ok := want[g.Name]
		if !ok {
			continue
		}
		if g.Protected != w {
			t.Errorf("%s protected=%v (%s), want %v", g.Name, g.Protected, g.Reason, w)
		}
		if g.Protected && g.Reason == "" {
			t.Errorf("%s protected without a reason", g.Name)
		}
	}
}

func TestGroupUnknownFootprintNotZero(t *testing.T) {
	for _, g := range Group(synthTable()) {
		if g.Name == "Notes" && g.Known {
			t.Fatal("inaccessible process must mark the group as unknown, not 0")
		}
	}
}

func TestMemoryFromPages(t *testing.T) {
	ps := uint64(16384)
	m := memoryFromPages(pageCounts{
		free: 1000, speculative: 200, active: 3000, inactive: 2000, wired: 1500,
		compressor: 500, purgeable: 100, external: 900, internal: 4000,
		pageSize: ps, memsize: 16 * GB,
	})
	if m.Free != 800*ps {
		t.Errorf("free %d", m.Free)
	}
	if m.AppMemory != 3900*ps {
		t.Errorf("app memory %d", m.AppMemory)
	}
	if m.Used != (3900+1500+500)*ps {
		t.Errorf("used %d", m.Used)
	}
	if m.Cached != 1000*ps {
		t.Errorf("cached %d", m.Cached)
	}
	if m.Spare() != m.Total-m.Used {
		t.Error("spare")
	}
}
