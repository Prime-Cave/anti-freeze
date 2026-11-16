package analyzer

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/collector"
	"github.com/Prime-Cave/anti-freeze/internal/machine"
)

const GB = uint64(1) << 30
const MB = uint64(1) << 20

var t0 = time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)

// 8 GB / 256 GB Mac, the brief's typical struggling machine.
var smallMac = machine.Profile{MemoryBytes: 8 * GB, DiskBytes: 245_107_195_904}

func loadFixture(t *testing.T, name string) collector.Scenario {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var sc collector.Scenario
	if err := json.Unmarshal(b, &sc); err != nil {
		t.Fatal(err)
	}
	return sc
}

// replay feeds a scenario through the analyzer in time order, calling fn
// after each system sample.
func replay(a *Analyzer, sc collector.Scenario, fn func(collector.Snapshot, Status)) {
	ti := 0
	for _, s := range sc.Snaps {
		for ti < len(sc.Tables) && !sc.Tables[ti].Time.After(s.Time) {
			a.AddProcesses(sc.Tables[ti])
			ti++
		}
		st := a.AddSystem(s)
		if fn != nil {
			fn(s, st)
		}
	}
}

// ---- headroom formula -------------------------------------------------------

func TestHeadroomFormula(t *testing.T) {
	th := smallMac.Defaults() // reserve 16 GB, swap budget 4 GB
	s := collector.Snapshot{
		Memory: collector.Memory{Total: 8 * GB, Used: 6 * GB, Free: 512 * MB, Purgeable: 256 * MB},
		Swap:   collector.Swap{Used: 1 * GB},
		Disk:   collector.Disk{Available: 18 * GB},
	}
	h := ComputeHeadroom(s, th)
	// spare = 2 GB = free 512 MB + purgeable 256 MB + reclaimable 1280 MB
	if h.Free != 512*MB || h.Purgeable != 256*MB || h.Reclaimable != 1280*MB {
		t.Fatalf("memory parts wrong: %+v", h)
	}
	// allowance = min(4 GB − 1 GB, 18 GB − 16 GB) = 2 GB
	if h.SwapAllowance != 2*GB {
		t.Fatalf("swap allowance %d MB, want 2048", h.SwapAllowance/MB)
	}
	if h.Total != 4*GB {
		t.Fatalf("total %d MB, want 4096", h.Total/MB)
	}
}

func TestHeadroomSwapCappedByBudget(t *testing.T) {
	th := smallMac.Defaults()
	s := collector.Snapshot{
		Memory: collector.Memory{Total: 8 * GB, Used: 8 * GB},
		Swap:   collector.Swap{Used: 5 * GB}, // past the 4 GB budget
		Disk:   collector.Disk{Available: 100 * GB},
	}
	if h := ComputeHeadroom(s, th); h.Total != 0 {
		t.Fatalf("no room expected, got %+v", h)
	}
}

func TestHeadroomDiskBelowReserveGivesNoSwap(t *testing.T) {
	th := smallMac.Defaults()
	s := collector.Snapshot{
		Memory: collector.Memory{Total: 8 * GB, Used: 7 * GB, Free: GB},
		Disk:   collector.Disk{Available: 9 * GB},
	}
	if h := ComputeHeadroom(s, th); h.SwapAllowance != 0 || h.Total != GB {
		t.Fatalf("got %+v", h)
	}
}

func TestHeadroomUnavailableIsCautious(t *testing.T) {
	th := smallMac.Defaults()
	s := collector.Snapshot{Unavailable: []string{"memory", "disk"}, Swap: collector.Swap{Used: 0}}
	if h := ComputeHeadroom(s, th); h.Total != 0 {
		t.Fatalf("unavailable inputs must not invent room: %+v", h)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[uint64]string{2576980378: "2.4 GB", 640 * MB: "640 MB", 12 * GB: "12 GB", 0: "0 MB"}
	for in, want := range cases {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// ---- states and hysteresis --------------------------------------------------

func TestEvaluateRules(t *testing.T) {
	th := smallMac.Defaults()
	base := collector.Snapshot{Pressure: collector.PressureNormal, Disk: collector.Disk{Available: 60 * GB}}
	cases := []struct {
		name string
		mod  func(*Inputs)
		want State
	}{
		{"calm", func(*Inputs) {}, OK},
		{"pressure warn", func(i *Inputs) { i.Snapshot.Pressure = collector.PressureWarn }, Tight},
		{"pressure critical", func(i *Inputs) { i.Snapshot.Pressure = collector.PressureCritical }, Critical},
		{"disk under reserve", func(i *Inputs) { i.Snapshot.Disk.Available = 12 * GB }, Tight},
		{"disk under floor", func(i *Inputs) { i.Snapshot.Disk.Available = 5 * GB }, Critical},
		{"swap growing fast", func(i *Inputs) { i.SwapSlope = Trend{Confident: true, Slope: th.SwapGrowthTight * 2} }, Tight},
		{"swap growing, not confident", func(i *Inputs) { i.SwapSlope = Trend{Slope: th.SwapGrowthTight * 2} }, OK},
		{"forecast in 3 min", func(i *Inputs) { i.Forecast = Forecast{Confident: true, ETA: 3 * time.Minute} }, Critical},
		{"forecast in 20 min", func(i *Inputs) { i.Forecast = Forecast{Confident: true, ETA: 20 * time.Minute} }, OK},
		{"pressure unavailable", func(i *Inputs) {
			i.Snapshot.Pressure = 0
			i.Snapshot.Unavailable = []string{"pressure"}
		}, OK},
	}
	for _, c := range cases {
		in := Inputs{Snapshot: base, Thresholds: th}
		c.mod(&in)
		if got, causes := Evaluate(in); got != c.want {
			t.Errorf("%s: got %v (%v), want %v", c.name, got, causes, c.want)
		}
	}
}

func TestHysteresisEnterImmediatelyLeaveAfterHold(t *testing.T) {
	h := NewHysteresis()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	if h.Update(Critical, at(0)) != Critical {
		t.Fatal("worse state must apply immediately")
	}
	// OK first seen at 5 s, so it has held 60 s at 65 s.
	for s := 5; s <= 60; s += 5 {
		if got := h.Update(OK, at(s)); got != Critical {
			t.Fatalf("at %ds: dropped to %v before 60 s hold", s, got)
		}
	}
	if got := h.Update(OK, at(65)); got != OK {
		t.Fatalf("after 60 s of OK should be OK, got %v", got)
	}
}

func TestHysteresisFlickerResetsHold(t *testing.T) {
	h := NewHysteresis()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	h.Update(Tight, at(0))
	h.Update(OK, at(10))
	h.Update(Tight, at(40)) // back to tight: hold restarts
	if got := h.Update(OK, at(75)); got != Tight {
		t.Fatalf("hold should restart after flicker, got %v", got)
	}
	if got := h.Update(OK, at(100)); got != Tight {
		t.Fatalf("OK has held only 25 s, still tight expected, got %v", got)
	}
	if got := h.Update(OK, at(135)); got != OK {
		t.Fatalf("OK held 60 s (75–135 s), got %v", got)
	}
}

func TestHysteresisStepsDownToWorstSeen(t *testing.T) {
	h := NewHysteresis()
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	h.Update(Critical, at(0))
	h.Update(OK, at(5))
	h.Update(Tight, at(30))
	if got := h.Update(OK, at(65)); got != Tight {
		t.Fatalf("should step to the worst state seen during the hold (tight), got %v", got)
	}
}

// ---- forecast ---------------------------------------------------------------

func TestForecastConfidentLinearGrowth(t *testing.T) {
	s := NewSeries()
	slope := 100 * float64(MB) / 60 // 100 MB/min
	for i := 0; i <= 60; i++ {      // 5 minutes at 5 s
		s.Add(t0.Add(time.Duration(i)*5*time.Second), float64(GB)+slope*float64(i*5))
	}
	tr := s.Fit(minSlopeBytesPS)
	if !tr.Confident || tr.R2 < 0.99 {
		t.Fatalf("expected confident fit: %+v", tr)
	}
	if d := tr.Slope/slope - 1; d > 0.1 || d < -0.1 {
		t.Fatalf("slope %.0f, want ≈%.0f", tr.Slope, slope)
	}
	f := Predict(tr, Trend{}, 1500*MB, 50*GB, 8*GB)
	if !f.Confident || f.Cause != "swap" || f.Minutes < 14 || f.Minutes > 16 {
		t.Fatalf("1.5 GB at 100 MB/min should be ≈15 min: %+v", f)
	}
	if f.Text != "Slowdowns likely in about 15 minutes." {
		t.Fatalf("text %q", f.Text)
	}
}

func TestForecastNotConfidentWhenNoisyOrShort(t *testing.T) {
	short := NewSeries()
	for i := 0; i < 10; i++ {
		short.Add(t0.Add(time.Duration(i)*5*time.Second), float64(i)*float64(GB))
	}
	if short.Fit(minSlopeBytesPS).Confident {
		t.Fatal("10 samples / 45 s must not be confident")
	}
	noisy := NewSeries()
	noisy.Window = time.Hour
	for i := 0; i < 120; i++ {
		v := float64(GB)
		if i%2 == 0 {
			v += float64(3 * GB)
		}
		noisy.Add(t0.Add(time.Duration(i)*5*time.Second), v+float64(i)*float64(MB)/100)
	}
	if tr := noisy.Fit(minSlopeBytesPS); tr.Confident {
		t.Fatalf("noise must not be confident: %+v", tr)
	}
	f := Predict(Trend{}, Trend{}, GB, 50*GB, 8*GB)
	if f.Confident || !strings.HasPrefix(f.Text, "Steady") {
		t.Fatalf("no trend → Steady, got %+v", f)
	}
}

func TestForecastDiskDecline(t *testing.T) {
	disk := Trend{Confident: true, Slope: -float64(GB) / 600} // losing 1 GB per 10 min
	f := Predict(Trend{}, disk, 10*GB, 10*GB, 8*GB)
	if !f.Confident || f.Cause != "disk" || f.Minutes != 20 {
		t.Fatalf("2 GB above floor at 0.1 GB/min = 20 min: %+v", f)
	}
}

func TestSeriesWindowDropsOldPoints(t *testing.T) {
	s := NewSeries()
	for i := 0; i < 200; i++ { // 1000 s
		s.Add(t0.Add(time.Duration(i)*5*time.Second), 1)
	}
	if s.Len() > 121 {
		t.Fatalf("window should hold ≤ 10 min of points, has %d", s.Len())
	}
}

// ---- culprit attribution ----------------------------------------------------

func TestCulpritSkipsProtectedAndPrefersLargest(t *testing.T) {
	rows := []AppRow{
		{Name: "WindowServer", Footprint: 5 * GB, Protected: true, Known: true},
		{Name: "Chat", Footprint: 2 * GB, Growth: int64(100 * MB), Known: true},
		{Name: "Browser", Footprint: 3 * GB, Growth: int64(2 * GB), Known: true},
		{Name: "Unknown", Footprint: 9 * GB, Known: false},
	}
	c, ok := Culprit(rows)
	if !ok || c.Name != "Browser" {
		t.Fatalf("culprit %+v", c)
	}
	g := TopByGrowth(rows, 3)
	if len(g) != 2 || g[0].Name != "Browser" {
		t.Fatalf("growth ranking %+v", g)
	}
}

func TestShortName(t *testing.T) {
	if ShortName("Google Chrome") != "Chrome" || ShortName("Claude") != "Claude" {
		t.Fatal("short names")
	}
}

// ---- recorded scenarios -----------------------------------------------------

func TestSteadyScenarioStaysOK(t *testing.T) {
	a := New(smallMac.Defaults())
	replay(a, loadFixture(t, "steady.json"), func(s collector.Snapshot, st Status) {
		if st.State != OK {
			t.Fatalf("%s: steady machine went %v (%v)", s.Time, st.State, st.Causes)
		}
		if st.Forecast.Confident {
			t.Fatalf("steady machine got a forecast: %+v", st.Forecast)
		}
		if len(st.Suggestions) != 0 {
			t.Fatal("no suggestions in ok state")
		}
	})
}

func TestRealSlowdownScenario(t *testing.T) {
	a := New(smallMac.Defaults())
	var started, ended []Episode
	a.OnEpisode = func(e Episode, s, en bool) {
		if s {
			started = append(started, e)
		}
		if en {
			ended = append(ended, e)
		}
	}
	firstTight, firstCritical, backToOK := time.Duration(-1), time.Duration(-1), time.Duration(-1)
	var forecastBeforeCritical bool
	var sawSuggestion Suggestion
	transitions := 0
	prev := OK
	replay(a, loadFixture(t, "slowdown.json"), func(s collector.Snapshot, st Status) {
		at := s.Time.Sub(t0)
		if st.State != prev {
			transitions++
			prev = st.State
		}
		if st.State == Tight && firstTight < 0 {
			firstTight = at
		}
		if st.State == Critical && firstCritical < 0 {
			firstCritical = at
		}
		if firstCritical < 0 && st.Forecast.Confident {
			forecastBeforeCritical = true
		}
		if firstCritical >= 0 && st.State == OK && backToOK < 0 {
			backToOK = at
		}
		if len(st.Suggestions) > 0 && sawSuggestion.Kind == "" {
			sawSuggestion = st.Suggestions[0]
		}
	})

	t.Logf("tight at %v, critical at %v, back to ok at %v, %d transitions", firstTight, firstCritical, backToOK, transitions)
	if firstTight < 10*time.Minute || firstTight > 16*time.Minute {
		t.Errorf("tight at %v, want between 10 and 16 min (swap growth / pressure warn)", firstTight)
	}
	if firstCritical < 0 || firstCritical > 26*time.Minute {
		t.Errorf("critical at %v, want by 26 min", firstCritical)
	}
	if !forecastBeforeCritical {
		t.Error("the forecast should warn before the state goes critical")
	}
	if backToOK < 32*time.Minute || backToOK > 36*time.Minute {
		t.Errorf("back to ok at %v: want 60 s hold after the quit at 31 min, but recovery within 5 min", backToOK)
	}
	if transitions > 6 {
		t.Errorf("%d state changes: icon would flicker", transitions)
	}
	if len(started) != 1 || len(ended) != 1 {
		t.Fatalf("want exactly one episode, got %d started / %d ended", len(started), len(ended))
	}
	ep := ended[0]
	if ep.PeakSwap <= ep.StartSwap || len(ep.TopFootprint) == 0 || ep.TopFootprint[0].Name != "Browser" {
		t.Errorf("episode details wrong: %+v", ep)
	}
	if len(ep.TopGrowth) == 0 || ep.TopGrowth[0].Name != "Browser" {
		t.Errorf("episode top growth should be Browser: %+v", ep.TopGrowth)
	}
	if sawSuggestion.Kind != "quit" || sawSuggestion.AppName != "Browser" || sawSuggestion.Action != "Quit Browser…" {
		t.Errorf("suggestion %+v", sawSuggestion)
	}
	if !strings.Contains(sawSuggestion.Text, "Quitting it frees about") {
		t.Errorf("suggestion text %q", sawSuggestion.Text)
	}
}

// ---- episodes and learning --------------------------------------------------

func TestExplain(t *testing.T) {
	eps := []Episode{
		{StartDiskFree: 11 * GB, TopFootprint: []EpisodeApp{{Name: "Google Chrome", Footprint: 3*GB + 200*MB}}},
		{StartDiskFree: 9 * GB, TopFootprint: []EpisodeApp{{Name: "Google Chrome", Footprint: 3*GB + 600*MB}}},
	}
	want := "Two slowdowns. Both started when Google Chrome passed 3 GB while disk was under 11 GB free."
	if got := Explain(eps); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if got := Explain(nil); got != "No slowdowns in this period." {
		t.Errorf("empty: %q", got)
	}
	mixed := []Episode{
		{StartDiskFree: 80 * GB, TopFootprint: []EpisodeApp{{Name: "A", Footprint: GB}}},
		{StartDiskFree: 80 * GB, TopFootprint: []EpisodeApp{{Name: "B", Footprint: GB}}},
		{StartDiskFree: 80 * GB, TopFootprint: []EpisodeApp{{Name: "A", Footprint: GB}}},
	}
	if got := Explain(mixed); got != "Three slowdowns. Each started with a different app using the most memory." {
		t.Errorf("mixed: %q", got)
	}
}

func TestLearnOnlyMoreCautious(t *testing.T) {
	def := smallMac.Defaults()
	eps := []Episode{{StartSwap: 3 * GB, StartDiskFree: 20 * GB}, {StartSwap: 2 * GB, StartDiskFree: 22 * GB}}
	if got := Learn(def, eps, 3*24*time.Hour); got != def {
		t.Error("needs 7 days of history before learning")
	}
	got := Learn(def, eps, 8*24*time.Hour)
	if !got.Learned || got.SwapBudget >= def.SwapBudget || got.DiskReserve <= def.DiskReserve {
		t.Errorf("learned thresholds should be more cautious: def %+v got %+v", def, got)
	}
	// Episodes that began with lots of swap and a roomy disk must not relax anything.
	loose := []Episode{{StartSwap: 7 * GB, StartDiskFree: 200 * GB}, {StartSwap: 6 * GB, StartDiskFree: 180 * GB}}
	if got := Learn(def, loose, 8*24*time.Hour); got.SwapBudget > def.SwapBudget || got.DiskReserve < def.DiskReserve || got.Learned {
		t.Errorf("learning must never be less cautious than defaults: %+v", got)
	}
	// Near-zero swap at the start means swap wasn't the trigger.
	tiny := []Episode{{StartSwap: 100 * MB, StartDiskFree: 60 * GB}, {StartSwap: 50 * MB, StartDiskFree: 60 * GB}}
	if got := Learn(def, tiny, 8*24*time.Hour); got.SwapBudget != def.SwapBudget {
		t.Errorf("tiny swap should not set the budget: %+v", got)
	}
}

func TestActivityCountsDescendants(t *testing.T) {
	a := New(smallMac.Defaults())
	mk := func(sec int, cpu uint64) collector.ProcessTable {
		return collector.ProcessTable{Time: t0.Add(time.Duration(sec) * time.Second), SelfUID: 501, Processes: []collector.Process{
			{PID: 10, UID: 501, Name: "Term", Path: "/Applications/Term.app/Contents/MacOS/Term", Accessible: true},
			{PID: 11, PPID: 10, UID: 501, Name: "zsh", Path: "/bin/zsh", Accessible: true},
			{PID: 12, PPID: 11, UID: 501, Name: "make", Path: "/usr/bin/make", CPUTimeNs: cpu, DiskWrite: cpu, Accessible: true},
		}, Apps: []collector.RunningApp{{PID: 10, Name: "Term", BundlePath: "/Applications/Term.app"}}}
	}
	a.AddProcesses(mk(0, 0))
	a.AddProcesses(mk(30, uint64(15*time.Second))) // 50% of a core
	g, ok := a.Group("/Applications/Term.app")
	if !ok {
		t.Fatal("group missing")
	}
	act := a.Activity(g, 30*time.Second)
	if act.Children != 2 || act.CPU < 49 || act.CPU > 51 {
		t.Fatalf("activity %+v: a build running in the terminal must count", act)
	}
}
