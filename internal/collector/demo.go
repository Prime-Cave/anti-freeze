package collector

import (
	"sync"
	"time"
)

// Demo replays the synthetic slowdown scenario in real time, starting at a
// chosen stage. It powers ANTIFREEZE_DEMO (for screenshots and trying the UI
// without stressing a real Mac). It never reads real processes.
type Demo struct {
	mu      sync.Mutex
	sc      Scenario
	startAt int       // scenario index matching `began`
	began   time.Time // wall-clock time of startAt
	now     func() time.Time
}

// DemoStages maps a stage name to minutes into the scenario.
var DemoStages = map[string]time.Duration{
	"ok":       5 * time.Minute,
	"tight":    19 * time.Minute,
	"critical": 27 * time.Minute,
	"recover":  31 * time.Minute,
}

const demoStep = 5 * time.Second

// NewDemo starts the scenario at stage ("ok", "tight", "critical", "recover").
func NewDemo(stage string) *Demo {
	off, ok := DemoStages[stage]
	if !ok {
		off = DemoStages["ok"]
	}
	now := time.Now
	d := &Demo{now: now, began: now(), startAt: int(off / demoStep)}
	d.sc = SlowdownScenario(d.began.Add(-off))
	return d
}

func (d *Demo) index() int {
	i := d.startAt + int(d.now().Sub(d.began)/demoStep)
	return min(i, len(d.sc.Snaps)-1)
}

// Warmup returns the scenario up to the start stage, timestamped so the last
// sample is "now", so trends and growth have history from the first second.
func (d *Demo) Warmup() ([]Snapshot, []ProcessTable) {
	d.mu.Lock()
	defer d.mu.Unlock()
	end := d.sc.Snaps[d.startAt].Time
	snaps := append([]Snapshot(nil), d.sc.Snaps[:d.startAt]...)
	var tables []ProcessTable
	for _, t := range d.sc.Tables {
		if t.Time.Before(end) {
			tables = append(tables, t)
		}
	}
	return snaps, tables
}

// System returns the scenario sample for the current wall-clock time.
func (d *Demo) System() (Snapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.sc.Snaps[d.index()]
	s.Time = d.now()
	return s, nil
}

// Processes returns the process table for the current wall-clock time.
func (d *Demo) Processes() (ProcessTable, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	want := d.sc.Snaps[d.index()].Time
	t := d.sc.Tables[0]
	for _, x := range d.sc.Tables {
		if x.Time.After(want) {
			break
		}
		t = x
	}
	t.Time = d.now()
	return t, nil
}
