package analyzer

import (
	"sort"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/collector"
)

// AppRow is one app in the "Using the most memory" list and the Apps table.
type AppRow struct {
	Key        string  `json:"key"`
	Name       string  `json:"name"`
	BundleID   string  `json:"bundleId,omitempty"`
	BundlePath string  `json:"bundlePath,omitempty"`
	Count      int     `json:"count"`
	Footprint  uint64  `json:"footprint"`
	Known      bool    `json:"known"`
	Growth     int64   `json:"growth"`        // bytes over the last hour (or since history began)
	GrowthSpan float64 `json:"growthSpanSec"` // how much history the growth covers
	CPU        float64 `json:"cpu"`           // % of one core, like Activity Monitor
	DiskRate   float64 `json:"diskRate"`      // bytes/second read + written
	Protected  bool    `json:"protected"`
	Reason     string  `json:"reason,omitempty"`
	MainPID    int     `json:"mainPid,omitempty"`
}

type procSample struct {
	cpu, disk uint64
	name      string
	ppid      int
}

type table struct {
	t     time.Time
	procs map[int]procSample
	foot  map[string]uint64 // group key → footprint
}

// AppTracker keeps the last hour of process tables (footprints per group,
// CPU and disk counters per pid) for growth, rates and the busy check.
type AppTracker struct {
	History time.Duration
	tables  []table
	groups  []collector.AppGroup
}

// NewAppTracker keeps 65 minutes so "last hour" growth always has a baseline.
func NewAppTracker() *AppTracker { return &AppTracker{History: 65 * time.Minute} }

// Add records a process table and its grouping.
func (a *AppTracker) Add(pt collector.ProcessTable, groups []collector.AppGroup) {
	tb := table{t: pt.Time, procs: make(map[int]procSample, len(pt.Processes)), foot: make(map[string]uint64, len(groups))}
	for _, p := range pt.Processes {
		if p.Accessible {
			tb.procs[p.PID] = procSample{cpu: p.CPUTimeNs, disk: p.DiskRead + p.DiskWrite, name: p.Name, ppid: p.PPID}
		} else {
			tb.procs[p.PID] = procSample{name: p.Name, ppid: p.PPID}
		}
	}
	for _, g := range groups {
		tb.foot[g.Key] = g.Footprint
	}
	a.tables = append(a.tables, tb)
	cut := 0
	for cut < len(a.tables) && pt.Time.Sub(a.tables[cut].t) > a.History {
		cut++
	}
	a.tables = a.tables[cut:]
	// Per-process detail is only needed for short rates (30 s); older
	// tables keep just per-app totals for hourly growth. This keeps memory
	// flat instead of holding ~400 copies of the whole process list.
	for i := range a.tables {
		if pt.Time.Sub(a.tables[i].t) > DetailWindow {
			a.tables[i].procs = nil
		}
	}
	a.groups = groups
}

// DetailWindow is how long per-process samples are kept.
const DetailWindow = 2 * time.Minute

// Groups is the latest grouping.
func (a *AppTracker) Groups() []collector.AppGroup { return a.groups }

// Rates returns CPU (% of one core) and disk (bytes/s) for pids between the
// newest table and the newest table at least `over` older. Pids that were
// reused (name changed) or missing at the start are skipped.
func (a *AppTracker) Rates(pids []int, over time.Duration) (cpu, disk float64, window time.Duration) {
	if len(a.tables) < 2 {
		return 0, 0, 0
	}
	newest := a.tables[len(a.tables)-1]
	base := a.tables[0]
	for i := len(a.tables) - 2; i >= 0; i-- {
		if a.tables[i].procs == nil {
			break // older tables have no per-process detail
		}
		base = a.tables[i]
		if newest.t.Sub(base.t) >= over {
			break
		}
	}
	if base.procs == nil {
		return 0, 0, 0
	}
	window = newest.t.Sub(base.t)
	if window <= 0 {
		return 0, 0, 0
	}
	var dc, dd uint64
	for _, pid := range pids {
		n, ok1 := newest.procs[pid]
		b, ok2 := base.procs[pid]
		if !ok1 || !ok2 || n.name != b.name || n.cpu < b.cpu || n.disk < b.disk {
			continue
		}
		dc += n.cpu - b.cpu
		dd += n.disk - b.disk
	}
	secs := window.Seconds()
	return float64(dc) / 1e9 / secs * 100, float64(dd) / secs, window
}

// Descendants returns pids whose parent chain leads to any of roots, in the
// newest table (e.g. a build started from Terminal).
func (a *AppTracker) Descendants(roots []int) []int {
	if len(a.tables) == 0 {
		return nil
	}
	procs := a.tables[len(a.tables)-1].procs
	in := map[int]bool{}
	for _, r := range roots {
		in[r] = true
	}
	var out []int
	for changed := true; changed; {
		changed = false
		for pid, p := range procs {
			if !in[pid] && in[p.ppid] && p.ppid > 1 {
				in[pid] = true
				out = append(out, pid)
				changed = true
			}
		}
	}
	sort.Ints(out)
	return out
}

// growth returns footprint change for a group over the last hour.
func (a *AppTracker) growth(key string, now uint64) (int64, float64) {
	if len(a.tables) < 2 {
		return 0, 0
	}
	newest := a.tables[len(a.tables)-1]
	for _, tb := range a.tables {
		if newest.t.Sub(tb.t) > time.Hour {
			continue
		}
		span := newest.t.Sub(tb.t).Seconds()
		base, ok := tb.foot[key]
		if !ok {
			base = 0 // app started within the window: all of it is growth
		}
		return int64(now) - int64(base), span
	}
	return 0, 0
}

// Rows builds AppRows for every group, with growth and rates (rates over the
// last 30 s of samples).
func (a *AppTracker) Rows() []AppRow {
	rows := make([]AppRow, 0, len(a.groups))
	for _, g := range a.groups {
		r := AppRow{
			Key: g.Key, Name: g.Name, BundleID: g.BundleID, BundlePath: g.BundlePath,
			Count: g.Count, Footprint: g.Footprint, Known: g.Known,
			Protected: g.Protected, Reason: g.Reason, MainPID: g.MainPID,
		}
		r.Growth, r.GrowthSpan = a.growth(g.Key, g.Footprint)
		r.CPU, r.DiskRate, _ = a.Rates(g.PIDs, 30*time.Second)
		rows = append(rows, r)
	}
	return rows
}

// TopByFootprint returns the n largest rows.
func TopByFootprint(rows []AppRow, n int) []AppRow {
	out := append([]AppRow(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Footprint > out[j].Footprint })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// TopByGrowth returns the n fastest-growing rows (growth > 0 only).
func TopByGrowth(rows []AppRow, n int) []AppRow {
	var out []AppRow
	for _, r := range rows {
		if r.Growth > 0 {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Growth > out[j].Growth })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Culprit is the app whose quitting frees the most memory. Protected apps are
// never chosen. Ties prefer the faster-growing app.
func Culprit(rows []AppRow) (AppRow, bool) {
	var best AppRow
	found := false
	for _, r := range rows {
		if r.Protected || !r.Known || r.Footprint == 0 {
			continue
		}
		if !found || r.Footprint > best.Footprint || (r.Footprint == best.Footprint && r.Growth > best.Growth) {
			best, found = r, true
		}
	}
	return best, found
}
