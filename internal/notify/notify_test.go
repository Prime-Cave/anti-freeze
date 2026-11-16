package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/analyzer"
)

func status(state analyzer.State, causes ...analyzer.Cause) analyzer.Status {
	return analyzer.Status{Ready: true, State: state, Causes: causes,
		TopApps: []analyzer.AppRow{{Key: "chrome", Name: "Google Chrome", Footprint: 3_330_000_000, Growth: 1 << 30}},
		Suggestions: []analyzer.Suggestion{{Kind: "quit", AppKey: "chrome", AppName: "Google Chrome", ShortName: "Chrome",
			Frees: 3_330_000_000, Action: "Quit Chrome…"}},
		Forecast: analyzer.Forecast{Confident: true, Minutes: 15, Text: "Slowdowns likely in about 15 minutes."},
	}
}

func TestOnePerStateChangeAndCooldown(t *testing.T) {
	d := &Decider{}
	t0 := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	type step struct {
		min   int
		st    analyzer.Status
		wants bool
	}
	steps := []step{
		{0, status(analyzer.OK), false},                                      // launch
		{1, status(analyzer.Tight, analyzer.CausePressureWarn), true},        // ok → tight
		{2, status(analyzer.Tight, analyzer.CausePressureWarn), false},       // same state
		{3, status(analyzer.Tight, analyzer.CauseSwapGrowth), false},         // same state, new cause
		{5, status(analyzer.Critical, analyzer.CausePressureCritical), true}, // tight → critical
		{8, status(analyzer.OK), false},                                      // better: silent
		{10, status(analyzer.Tight, analyzer.CausePressureWarn), false},      // cooldown for pressure-warn (9 min)
		{12, status(analyzer.OK), false},
		{22, status(analyzer.Tight, analyzer.CausePressureWarn), true}, // 21 min later
		{23, status(analyzer.OK), false},
		{24, status(analyzer.Tight, analyzer.CauseDiskReserve), true}, // different cause
	}
	for _, s := range steps {
		_, got := d.Decide(s.st, at(s.min), true)
		if got != s.wants {
			t.Errorf("minute %d (%v %v): notify=%v, want %v", s.min, s.st.State, s.st.Causes, got, s.wants)
		}
	}
}

func TestNoAlertAtLaunchOrWhenDisabled(t *testing.T) {
	d := &Decider{}
	now := time.Now()
	if _, ok := d.Decide(status(analyzer.Critical, analyzer.CausePressureCritical), now, true); ok {
		t.Fatal("alerted for the state at launch")
	}
	d = &Decider{}
	d.Decide(status(analyzer.OK), now, false)
	if _, ok := d.Decide(status(analyzer.Tight, analyzer.CausePressureWarn), now, false); ok {
		t.Fatal("alerted while notifications are off")
	}
	if _, ok := d.Decide(analyzer.Status{}, now, true); ok {
		t.Fatal("alerted before data was ready")
	}
}

func TestCopy(t *testing.T) {
	n := Compose(status(analyzer.Tight, analyzer.CausePressureWarn), analyzer.CausePressureWarn, time.Now())
	if n.Title != "Your Mac is getting tight" {
		t.Errorf("title %q", n.Title)
	}
	want := "Chrome is using 3.1 GB and still growing. Slowdowns likely in about 15 minutes."
	if n.Body != want {
		t.Errorf("body %q\nwant %q", n.Body, want)
	}
	if n.Category != CategoryQuit || n.QuitLabel != "Quit Chrome…" || n.AppKey != "chrome" {
		t.Errorf("%+v", n)
	}
	n = Compose(status(analyzer.Critical, analyzer.CausePressureCritical), analyzer.CausePressureCritical, time.Now())
	if n.Title != "Slowdown likely" {
		t.Errorf("critical title %q", n.Title)
	}
	// No app to suggest: details only, never a quit button.
	st := status(analyzer.Tight, analyzer.CauseDiskReserve)
	st.Suggestions, st.Forecast = nil, analyzer.Forecast{}
	st.Runway = analyzer.Runway{DiskKnown: true, DiskFree: 9 << 30}
	n = Compose(st, analyzer.CauseDiskReserve, time.Now())
	if n.Category != CategoryDetails || n.QuitLabel != "" || !strings.HasPrefix(n.Body, "Only 9.0 GB of disk is left.") {
		t.Errorf("%+v", n)
	}
	for _, s := range []string{n.Title, n.Body} {
		if strings.Contains(strings.ToLower(s), "force") || strings.Contains(strings.ToLower(s), "sorry") {
			t.Errorf("copy rule broken: %q", s)
		}
	}
}
