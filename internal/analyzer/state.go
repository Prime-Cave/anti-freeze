// Package analyzer turns samples into what the user sees: a state, a
// "to spare" number, a forecast, the app causing it, and slowdown episodes.
package analyzer

import (
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/collector"
	"github.com/Prime-Cave/anti-freeze/internal/machine"
)

// State is the overall verdict.
type State int

const (
	OK State = iota
	Tight
	Critical
)

func (s State) String() string {
	switch s {
	case Tight:
		return "tight"
	case Critical:
		return "critical"
	}
	return "ok"
}

// Label is the UI title for a state.
func (s State) Label() string {
	switch s {
	case Tight:
		return "Getting tight"
	case Critical:
		return "Slowdown likely"
	}
	return "Plenty of room"
}

// MarshalText makes states readable in JSON.
func (s State) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText parses a state name.
func (s *State) UnmarshalText(b []byte) error {
	switch string(b) {
	case "tight":
		*s = Tight
	case "critical":
		*s = Critical
	default:
		*s = OK
	}
	return nil
}

// Cause names one reason for a state. Notifications cool down per cause.
type Cause string

const (
	CausePressureWarn     Cause = "pressure-warn"
	CausePressureCritical Cause = "pressure-critical"
	CauseSwapGrowth       Cause = "swap-growth"
	CauseSwapLearned      Cause = "swap-learned"
	CauseDiskReserve      Cause = "disk-reserve"
	CauseDiskFloor        Cause = "disk-floor"
	CauseForecast         Cause = "forecast"
)

// Inputs is what the state rules look at.
type Inputs struct {
	Snapshot   collector.Snapshot
	Forecast   Forecast
	SwapSlope  Trend // swap used, bytes/second
	Thresholds machine.Thresholds
}

// Evaluate applies the rules from the brief (section 4.1):
//
//	critical: pressure critical, OR forecast slowdown within ForecastCritical,
//	          OR disk free below the hard floor
//	tight:    pressure warn, OR swap growing fast, OR disk free below the
//	          reserve, OR swap past a level where this Mac struggled before
//	ok:       otherwise
//
// It returns the raw (un-smoothed) state and every cause that applied.
func Evaluate(in Inputs) (State, []Cause) {
	s, th := in.Snapshot, in.Thresholds
	var causes []Cause
	state := OK
	raise := func(to State, c Cause) {
		causes = append(causes, c)
		if to > state {
			state = to
		}
	}

	if s.Has("pressure") {
		switch s.Pressure {
		case collector.PressureCritical:
			raise(Critical, CausePressureCritical)
		case collector.PressureWarn:
			raise(Tight, CausePressureWarn)
		}
	}
	if s.Has("disk") {
		switch {
		case s.Disk.Available < th.DiskFloor:
			raise(Critical, CauseDiskFloor)
		case s.Disk.Available < th.DiskReserve:
			raise(Tight, CauseDiskReserve)
		}
	}
	if in.Forecast.Confident && in.Forecast.ETA <= th.ForecastCritical {
		raise(Critical, CauseForecast)
	}
	if in.SwapSlope.Confident && in.SwapSlope.Slope >= th.SwapGrowthTight {
		raise(Tight, CauseSwapGrowth)
	}
	if th.Learned && s.Has("swap") && th.SwapBudget > 0 && s.Swap.Used >= th.SwapBudget {
		raise(Tight, CauseSwapLearned)
	}
	return state, causes
}

// Hysteresis stops the icon flickering: a worse state applies at once; a
// better state applies only after it has held for Hold (default 60 s). When
// it does, the state steps down to the worst state seen during that hold.
type Hysteresis struct {
	Hold       time.Duration
	current    State
	improving  bool
	since      time.Time
	worstSince State
}

// NewHysteresis returns a filter with the brief's 60-second hold.
func NewHysteresis() *Hysteresis { return &Hysteresis{Hold: 60 * time.Second} }

// Current is the displayed state.
func (h *Hysteresis) Current() State { return h.current }

// Update feeds a raw state and returns the displayed state.
func (h *Hysteresis) Update(raw State, now time.Time) State {
	switch {
	case raw >= h.current:
		h.current = raw
		h.improving = false
	case !h.improving:
		h.improving, h.since, h.worstSince = true, now, raw
	default:
		if raw > h.worstSince {
			h.worstSince = raw
		}
		if now.Sub(h.since) >= h.Hold {
			h.current = h.worstSince
			h.improving = false
			if raw < h.current { // keep improving from here
				h.improving, h.since, h.worstSince = true, now, raw
			}
		}
	}
	return h.current
}
