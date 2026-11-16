package analyzer

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/collector"
	"github.com/Prime-Cave/anti-freeze/internal/machine"
)

// Runway is the data behind the runway bar, in bytes.
type Runway struct {
	MemTotal  uint64 `json:"memTotal"`
	InUse     uint64 `json:"inUse"`    // app memory + wired
	Squeezed  uint64 `json:"squeezed"` // compressed
	Spare     uint64 `json:"spare"`    // total − used
	Overflow  uint64 `json:"overflow"` // swap used ("overflow on disk")
	DiskFree  uint64 `json:"diskFree"` // available for important usage
	DiskTotal uint64 `json:"diskTotal"`
	MemKnown  bool   `json:"memKnown"`
	DiskKnown bool   `json:"diskKnown"`
	SwapKnown bool   `json:"swapKnown"`
}

// Suggestion is a "What would help most" card.
type Suggestion struct {
	Kind      string `json:"kind"` // "quit" or "disk"
	Text      string `json:"text"`
	AppKey    string `json:"appKey,omitempty"`
	AppName   string `json:"appName,omitempty"`
	ShortName string `json:"shortName,omitempty"`
	Frees     uint64 `json:"frees,omitempty"`
	Action    string `json:"action"` // button label, e.g. "Quit Chrome…"
}

// Status is everything the dropdown shows.
type Status struct {
	Time         time.Time          `json:"time"`
	Ready        bool               `json:"ready"` // false until the first samples arrive
	State        State              `json:"state"`
	Label        string             `json:"label"`
	RawState     State              `json:"rawState"`
	Causes       []Cause            `json:"causes"`
	Headroom     Headroom           `json:"headroom"`
	HeadroomText string             `json:"headroomText"`
	Forecast     Forecast           `json:"forecast"`
	SwapTrend    Trend              `json:"swapTrend"`
	DiskTrend    Trend              `json:"diskTrend"`
	Runway       Runway             `json:"runway"`
	CPU          float64            `json:"cpu"`
	Thermal      string             `json:"thermal"`
	Pressure     string             `json:"pressure"`
	TopApps      []AppRow           `json:"topApps"`
	Suggestions  []Suggestion       `json:"suggestions"`
	Unavailable  []string           `json:"unavailable,omitempty"`
	Thresholds   machine.Thresholds `json:"thresholds"`
	Episode      *Episode           `json:"episode,omitempty"`
}

// Analyzer holds all rolling state. Safe for concurrent use.
type Analyzer struct {
	mu         sync.Mutex
	th         machine.Thresholds
	swap, disk *Series
	swapRecent *Series // 3-minute window: "is swap growing right now?"
	hyst       *Hysteresis
	apps       *AppTracker
	episodes   EpisodeTracker
	last       collector.Snapshot
	status     Status
	// DiskHint, if set, adds a sentence to the disk card
	// (e.g. "Downloads holds 6.2 GB."). Set from the last disk scan.
	diskHint string
	// OnEpisode is called (without the lock) when an episode starts or ends.
	OnEpisode func(e Episode, started, ended bool)
}

// New returns an analyzer using the given thresholds.
func New(th machine.Thresholds) *Analyzer {
	return &Analyzer{th: th, swap: NewSeries(), disk: NewSeries(), swapRecent: &Series{Window: RecentWindow},
		hyst: NewHysteresis(), apps: NewAppTracker()}
}

// SetThresholds replaces thresholds (sensitivity change, learning).
func (a *Analyzer) SetThresholds(th machine.Thresholds) {
	a.mu.Lock()
	a.th = th
	a.mu.Unlock()
}

// Thresholds returns the active thresholds.
func (a *Analyzer) Thresholds() machine.Thresholds {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.th
}

// SetDiskHint sets the extra sentence for the disk suggestion card.
func (a *Analyzer) SetDiskHint(s string) {
	a.mu.Lock()
	a.diskHint = s
	a.mu.Unlock()
}

// AddProcesses records a process table (every 10 s).
func (a *Analyzer) AddProcesses(pt collector.ProcessTable) {
	groups := collector.Group(pt)
	a.mu.Lock()
	a.apps.Add(pt, groups)
	a.mu.Unlock()
}

// Apps returns every app row (for the dashboard table).
func (a *Analyzer) Apps() []AppRow {
	a.mu.Lock()
	defer a.mu.Unlock()
	return TopByFootprint(a.apps.Rows(), 1<<30)
}

// Group returns the latest group with a key.
func (a *Analyzer) Group(key string) (collector.AppGroup, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, g := range a.apps.Groups() {
		if g.Key == key {
			return g, true
		}
	}
	return collector.AppGroup{}, false
}

// Activity is the evidence for the quit safety check.
type Activity struct {
	CPU      float64       `json:"cpu"`      // % of one core
	DiskRate float64       `json:"diskRate"` // bytes/s
	Window   time.Duration `json:"window"`
	Children int           `json:"children"` // descendant processes outside the app bundle
}

// Activity measures a group plus its descendants over the last `over`.
func (a *Analyzer) Activity(g collector.AppGroup, over time.Duration) Activity {
	a.mu.Lock()
	defer a.mu.Unlock()
	kids := a.apps.Descendants(g.PIDs)
	inGroup := map[int]bool{}
	for _, p := range g.PIDs {
		inGroup[p] = true
	}
	all := append([]int(nil), g.PIDs...)
	n := 0
	for _, k := range kids {
		if !inGroup[k] {
			all = append(all, k)
			n++
		}
	}
	cpu, disk, w := a.apps.Rates(all, over)
	return Activity{CPU: cpu, DiskRate: disk, Window: w, Children: n}
}

// AddSystem records a system sample and returns the new status.
func (a *Analyzer) AddSystem(s collector.Snapshot) Status {
	a.mu.Lock()
	th := a.th
	a.last = s
	if s.Has("swap") {
		a.swap.Add(s.Time, float64(s.Swap.Used))
		a.swapRecent.Add(s.Time, float64(s.Swap.Used))
	}
	if s.Has("disk") {
		a.disk.Add(s.Time, float64(s.Disk.Available))
	}
	swapTrend := a.swap.Fit(minSlopeBytesPS)
	recent := a.swapRecent.Fit(minSlopeBytesPS)
	diskTrend := a.disk.Fit(minSlopeBytesPS)
	// The 10-minute line lags: once growth stops it keeps pointing up for a
	// while. Only forecast from it while swap is still rising right now.
	if recent.Slope <= minSlopeBytesPS {
		swapTrend.Confident = false
	}
	hr := ComputeHeadroom(s, th)
	fc := Predict(swapTrend, diskTrend, hr.Total, s.Disk.Available, th.DiskFloor)
	raw, causes := Evaluate(Inputs{Snapshot: s, Forecast: fc, SwapSlope: recent, Thresholds: th})
	state := a.hyst.Update(raw, s.Time)
	rows := a.apps.Rows()

	ep, started, ended := a.episodes.Update(s.Time, state, s.Swap.Used, s.Disk.Available, causes, rows)

	st := Status{
		Time: s.Time, Ready: true, State: state, Label: state.Label(), RawState: raw, Causes: causes,
		Headroom: hr, HeadroomText: FormatBytes(hr.Total), Forecast: fc, SwapTrend: swapTrend, DiskTrend: diskTrend,
		CPU: s.CPU.Usage * 100, Thermal: s.Thermal.Label(), Pressure: s.Pressure.String(),
		Unavailable: s.Unavailable, Thresholds: th,
	}
	st.Runway = Runway{
		MemTotal: s.Memory.Total, InUse: s.Memory.AppMemory + s.Memory.Wired, Squeezed: s.Memory.Compressed,
		Spare: s.Memory.Spare(), Overflow: s.Swap.Used, DiskFree: s.Disk.Available, DiskTotal: s.Disk.Total,
		MemKnown: s.Has("memory"), DiskKnown: s.Has("disk"), SwapKnown: s.Has("swap"),
	}
	st.TopApps = TopByFootprint(rows, 4)
	if state >= Tight {
		st.Suggestions = suggestions(s, th, rows, causes, a.diskHint)
	}
	if cur, ok := a.episodes.Current(); ok {
		st.Episode = &cur
	}
	a.status = st
	cb := a.OnEpisode
	a.mu.Unlock()

	if cb != nil && (started || ended) {
		cb(ep, started, ended)
	}
	return st
}

// SetEpisodeID stores the database id of the ongoing episode.
func (a *Analyzer) SetEpisodeID(id int64) {
	a.mu.Lock()
	a.episodes.SetID(id)
	a.mu.Unlock()
}

// Status returns the latest status (Ready=false before the first sample).
func (a *Analyzer) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// growthWorthMentioning is the growth size that earns a sentence.
const growthWorthMentioning = 500 << 20

func suggestions(s collector.Snapshot, th machine.Thresholds, rows []AppRow, causes []Cause, diskHint string) []Suggestion {
	var out []Suggestion
	diskCause := false
	for _, c := range causes {
		if c == CauseDiskReserve || c == CauseDiskFloor {
			diskCause = true
		}
	}
	if c, ok := Culprit(rows); ok {
		short := ShortName(c.Name)
		var text string
		if c.Growth >= growthWorthMentioning {
			text = fmt.Sprintf("%s grew by %s in the last hour. Quitting it frees about %s.",
				c.Name, FormatBytes(uint64(c.Growth)), FormatBytes(c.Footprint))
		} else {
			text = fmt.Sprintf("%s is using the most memory. Quitting it frees about %s.", c.Name, FormatBytes(c.Footprint))
		}
		out = append(out, Suggestion{Kind: "quit", Text: text, AppKey: c.Key, AppName: c.Name, ShortName: short,
			Frees: c.Footprint, Action: "Quit " + short + "…"})
	}
	if diskCause && s.Has("disk") {
		text := fmt.Sprintf("Only %s of disk is left, and overflow needs room to grow.", FormatBytes(s.Disk.Available))
		if diskHint != "" {
			text += " " + diskHint
		}
		card := Suggestion{Kind: "disk", Text: text, Action: "Review large files"}
		// Disk below the hard floor is the more urgent card.
		if containsCause(causes, CauseDiskFloor) {
			out = append([]Suggestion{card}, out...)
		} else {
			out = append(out, card)
		}
	}
	if len(out) > 2 {
		out = out[:2]
	}
	return out
}

func containsCause(cs []Cause, c Cause) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

// ShortName is the name used on buttons: "Google Chrome" → "Chrome".
func ShortName(name string) string {
	for _, p := range []string{"Google ", "Microsoft ", "Adobe "} {
		if strings.HasPrefix(name, p) && len(name) > len(p) {
			return name[len(p):]
		}
	}
	return name
}

// FormatBytes formats sizes for the UI: "2.4 GB", "640 MB".
func FormatBytes(b uint64) string {
	const g = float64(1 << 30)
	switch {
	case float64(b) >= 9.95*g:
		return fmt.Sprintf("%.0f GB", float64(b)/g)
	case float64(b) >= 0.995*g:
		return fmt.Sprintf("%.1f GB", float64(b)/g)
	default:
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	}
}
