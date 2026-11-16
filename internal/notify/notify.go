// Package notify decides when Anti-Freeze shows a macOS notification and what it
// says (sections 5 and 7.4). The decision logic is pure and tested; sending
// goes through a Sender (UNUserNotificationCenter in the app).
//
// Rules:
//   - At most one notification per state change, and only when things get
//     worse (ok → tight, ok/tight → critical). Getting better is silent.
//   - A cooldown (default 20 minutes) per cause: the same reason never
//     notifies twice inside the cooldown, even across state changes.
//   - Focus modes are respected: notifications use the "active" interruption
//     level, never "time sensitive", so Focus silences them.
//   - Buttons are "Quit [App]…" (opens the safety check, never quits) and
//     "Show details". Force quit is never offered here.
package notify

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/analyzer"
)

// Identifiers shared with the Sender.
const (
	CategoryQuit    = "anti-freeze.quit"    // has "Quit [App]…" and "Show details"
	CategoryDetails = "anti-freeze.details" // "Show details" only
	ActionQuit      = "quit"
	ActionDetails   = "details"
	DefaultCooldown = 20 * time.Minute
	stillGrowing    = 64 << 20 // growth over the last hour worth calling "still growing"
)

// Note is one notification.
type Note struct {
	ID        string
	Title     string
	Body      string
	Category  string
	QuitLabel string // "Quit Chrome…", empty if no app to suggest
	AppKey    string
	Cause     analyzer.Cause
}

// Sender delivers notes. The app implements it with UNUserNotificationCenter.
type Sender interface {
	// Send delivers a note. When n.QuitLabel is set the quit category is
	// re-registered first, so the button names the app ("Quit Chrome…").
	Send(n Note) error
}

// Decider applies the rules. Safe for concurrent use.
type Decider struct {
	mu        sync.Mutex
	Cooldown  time.Duration
	lastState analyzer.State
	seeded    bool
	lastSent  map[analyzer.Cause]time.Time
}

// SetCooldown changes the per-cause cooldown.
func (d *Decider) SetCooldown(c time.Duration) {
	d.mu.Lock()
	d.Cooldown = c
	d.mu.Unlock()
}

// Decide looks at a new status and returns a note to send, if any.
// enabled is the user's notifications setting.
func (d *Decider) Decide(st analyzer.Status, now time.Time, enabled bool) (Note, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !st.Ready {
		return Note{}, false
	}
	prev := d.lastState
	d.lastState = st.State
	if !d.seeded {
		// The state at launch is not a change. (If the Mac is already
		// tight when Anti-Freeze starts, the icon says so; no alert.)
		d.seeded = true
		return Note{}, false
	}
	if !enabled || st.State <= prev || st.State == analyzer.OK {
		return Note{}, false
	}
	cause := primaryCause(st.Causes)
	cool := d.Cooldown
	if cool <= 0 {
		cool = DefaultCooldown
	}
	if d.lastSent == nil {
		d.lastSent = map[analyzer.Cause]time.Time{}
	}
	if t, ok := d.lastSent[cause]; ok && now.Sub(t) < cool {
		return Note{}, false
	}
	d.lastSent[cause] = now
	return Compose(st, cause, now), true
}

// primaryCause picks the most serious cause for the cooldown key.
func primaryCause(cs []analyzer.Cause) analyzer.Cause {
	order := []analyzer.Cause{
		analyzer.CausePressureCritical, analyzer.CauseDiskFloor, analyzer.CauseForecast,
		analyzer.CausePressureWarn, analyzer.CauseSwapGrowth, analyzer.CauseSwapLearned, analyzer.CauseDiskReserve,
	}
	for _, o := range order {
		for _, c := range cs {
			if c == o {
				return o
			}
		}
	}
	if len(cs) > 0 {
		return cs[0]
	}
	return "unknown"
}

// Compose writes the notification copy (section 7.4):
//
//	Your Mac is getting tight
//	Chrome is using 3.1 GB and still growing. Slowdowns likely in about 15 minutes.
//	[Quit Chrome…] [Show details]
func Compose(st analyzer.Status, cause analyzer.Cause, now time.Time) Note {
	n := Note{ID: fmt.Sprintf("anti-freeze-%d", now.Unix()), Cause: cause, Category: CategoryDetails}
	if st.State == analyzer.Critical {
		n.Title = "Slowdown likely"
	} else {
		n.Title = "Your Mac is getting tight"
	}
	var parts []string
	var quit *analyzer.Suggestion
	var disk *analyzer.Suggestion
	for i := range st.Suggestions {
		s := &st.Suggestions[i]
		if s.Kind == "quit" && quit == nil {
			quit = s
		}
		if s.Kind == "disk" && disk == nil {
			disk = s
		}
	}
	diskFirst := cause == analyzer.CauseDiskFloor || cause == analyzer.CauseDiskReserve
	if diskFirst && st.Runway.DiskKnown {
		parts = append(parts, fmt.Sprintf("Only %s of disk is left.", analyzer.FormatBytes(st.Runway.DiskFree)))
	}
	if quit != nil {
		if row, ok := findRow(st.TopApps, quit.AppKey); ok && row.Growth >= stillGrowing {
			parts = append(parts, fmt.Sprintf("%s is using %s and still growing.", quit.ShortName, analyzer.FormatBytes(quit.Frees)))
		} else {
			parts = append(parts, fmt.Sprintf("%s is using the most memory, %s.", quit.ShortName, analyzer.FormatBytes(quit.Frees)))
		}
		n.Category, n.QuitLabel, n.AppKey = CategoryQuit, quit.Action, quit.AppKey
	}
	if st.Forecast.Confident {
		parts = append(parts, st.Forecast.Text)
	} else if len(parts) == 0 {
		parts = append(parts, "Memory is running low. Open Anti-Freeze to see what's using it.")
	}
	n.Body = strings.Join(parts, " ")
	return n
}

func findRow(rows []analyzer.AppRow, key string) (analyzer.AppRow, bool) {
	for _, r := range rows {
		if r.Key == key {
			return r, true
		}
	}
	return analyzer.AppRow{}, false
}
