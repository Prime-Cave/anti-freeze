package analyzer

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/machine"
)

// EpisodeApp is an app's footprint and growth when an episode began.
type EpisodeApp struct {
	Name      string `json:"name"`
	Footprint uint64 `json:"footprint"`
	Growth    int64  `json:"growth"`
}

// Episode is one stretch of time in the critical state (section 4.4).
type Episode struct {
	ID            int64        `json:"id"`
	Start         time.Time    `json:"start"`
	End           time.Time    `json:"end"` // zero while ongoing
	StartSwap     uint64       `json:"startSwap"`
	PeakSwap      uint64       `json:"peakSwap"`
	StartDiskFree uint64       `json:"startDiskFree"`
	MinDiskFree   uint64       `json:"minDiskFree"`
	Causes        []Cause      `json:"causes"`
	TopFootprint  []EpisodeApp `json:"topFootprint"`
	TopGrowth     []EpisodeApp `json:"topGrowth"`
}

// Ongoing reports whether the episode has not ended.
func (e Episode) Ongoing() bool { return e.End.IsZero() }

// EpisodeTracker opens an episode when the displayed state reaches critical
// and closes it when it leaves.
type EpisodeTracker struct {
	cur *Episode
}

// Update feeds the displayed state. It returns the episode when one starts
// (started=true) or ends (ended=true), so the caller can persist it.
func (t *EpisodeTracker) Update(now time.Time, state State, swap, diskFree uint64, causes []Cause, rows []AppRow) (ep Episode, started, ended bool) {
	switch {
	case state == Critical && t.cur == nil:
		e := &Episode{Start: now, StartSwap: swap, PeakSwap: swap, StartDiskFree: diskFree, MinDiskFree: diskFree,
			Causes: append([]Cause(nil), causes...)}
		for _, r := range TopByFootprint(rows, 3) {
			e.TopFootprint = append(e.TopFootprint, EpisodeApp{r.Name, r.Footprint, r.Growth})
		}
		for _, r := range TopByGrowth(rows, 3) {
			e.TopGrowth = append(e.TopGrowth, EpisodeApp{r.Name, r.Footprint, r.Growth})
		}
		t.cur = e
		return *e, true, false
	case state == Critical:
		t.cur.PeakSwap = max(t.cur.PeakSwap, swap)
		t.cur.MinDiskFree = min(t.cur.MinDiskFree, diskFree)
		return *t.cur, false, false
	case t.cur != nil:
		t.cur.End = now
		e := *t.cur
		t.cur = nil
		return e, false, true
	}
	return Episode{}, false, false
}

// Current returns the ongoing episode, if any.
func (t *EpisodeTracker) Current() (Episode, bool) {
	if t.cur == nil {
		return Episode{}, false
	}
	return *t.cur, true
}

// SetID records the stored id of the ongoing episode.
func (t *EpisodeTracker) SetID(id int64) {
	if t.cur != nil {
		t.cur.ID = id
	}
}

var counts = []string{"No", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten"}

// Explain writes the dashboard's one-sentence summary of episodes, e.g.
// "Two slowdowns. Both started when Google Chrome passed 3 GB while disk was under 12 GB free."
func Explain(eps []Episode) string {
	n := len(eps)
	if n == 0 {
		return "No slowdowns in this period."
	}
	head := fmt.Sprintf("%d slowdowns.", n)
	if n < len(counts) {
		head = counts[n] + " slowdowns."
	}
	if n == 1 {
		head = "One slowdown."
	}
	who := "It"
	if n == 2 {
		who = "Both"
	} else if n > 2 {
		who = "All"
	}

	// An app that was the biggest user at the start of every episode.
	app, appMin := "", uint64(0)
	for i, e := range eps {
		if len(e.TopFootprint) == 0 {
			app = ""
			break
		}
		top := e.TopFootprint[0]
		if i == 0 {
			app, appMin = top.Name, top.Footprint
		} else if top.Name != app {
			app = ""
			break
		}
		appMin = min(appMin, top.Footprint)
	}
	// The highest disk-free at any start: "disk was under N GB free".
	var diskMax uint64
	for _, e := range eps {
		diskMax = max(diskMax, e.StartDiskFree)
	}
	diskClause := ""
	if diskMax > 0 && diskMax < 40*gib {
		diskClause = fmt.Sprintf(" while disk was under %s free", ceilGB(diskMax))
	}

	switch {
	case app != "":
		return fmt.Sprintf("%s %s started when %s passed %s%s.", head, who, app, floorGB(appMin), diskClause)
	case diskClause != "":
		return fmt.Sprintf("%s %s started%s.", head, who, strings.Replace(diskClause, " while", " when", 1))
	case n == 1:
		return head
	default:
		return head + " Each started with a different app using the most memory."
	}
}

const gib = uint64(1) << 30

// floorGB rounds down to the nearest half GB ("passed 3 GB").
func floorGB(b uint64) string {
	v := math.Floor(float64(b)/float64(gib)*2) / 2
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f GB", v)
	}
	return fmt.Sprintf("%.1f GB", v)
}

// ceilGB rounds up to a whole GB ("under 12 GB free").
func ceilGB(b uint64) string {
	return fmt.Sprintf("%.0f GB", math.Ceil(float64(b)/float64(gib)))
}

// LearnMinHistory is how much history is needed before thresholds adapt.
const LearnMinHistory = 7 * 24 * time.Hour

// Learn adjusts thresholds to where this Mac actually struggled (section
// 4.6): the swap level and disk free at which past episodes began. The
// defaults are a floor — learning only ever makes Anti-Freeze more cautious.
func Learn(def machine.Thresholds, eps []Episode, history time.Duration) machine.Thresholds {
	if history < LearnMinHistory || len(eps) < 2 {
		return def
	}
	swaps := make([]float64, 0, len(eps))
	disks := make([]float64, 0, len(eps))
	for _, e := range eps {
		swaps = append(swaps, float64(e.StartSwap))
		disks = append(disks, float64(e.StartDiskFree))
	}
	out := def
	// Warn a little before the swap level where trouble usually began. Swap
	// near zero at the start means swap wasn't the trigger, so a learned
	// budget is never below a quarter of the default (that would warn on
	// ordinary idle swap).
	if s := uint64(median(swaps) * 0.9); s >= def.SwapBudget/4 && s < def.SwapBudget {
		out.SwapBudget, out.Learned = s, true
	}
	// Keep a little more disk free than where trouble usually began — but
	// only when disk was actually low then (under twice the reserve), and
	// never more than double the default, so a roomy disk can't make the
	// Mac look permanently tight.
	if m := median(disks); m < float64(2*def.DiskReserve) {
		if d := min(uint64(m*1.1), 2*def.DiskReserve); d > def.DiskReserve {
			out.DiskReserve, out.Learned = d, true
		}
	}
	return out
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
