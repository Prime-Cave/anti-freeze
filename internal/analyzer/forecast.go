package analyzer

import (
	"fmt"
	"math"
	"time"
)

// Forecast settings (section 4.3).
const (
	ForecastWindow  = 10 * time.Minute
	RecentWindow    = 3 * time.Minute // for "swap growing fast"
	ewmaAlpha       = 0.3
	minSamples      = 20
	minSpan         = 2 * time.Minute
	minR2           = 0.6
	minSlopeBytesPS = 2 * 1024 * 1024 / 60.0 // 2 MB/min: anything slower is "steady"
	maxHorizon      = 60 * time.Minute       // beyond this we say "Steady"
)

type point struct {
	t time.Time
	v float64
}

// Series is a rolling window of a metric, EWMA-smoothed as it arrives.
type Series struct {
	Window time.Duration
	pts    []point
	ewma   float64
	primed bool
}

// NewSeries returns a series with the forecast window.
func NewSeries() *Series { return &Series{Window: ForecastWindow} }

// Add appends a raw value and drops points older than the window.
func (s *Series) Add(t time.Time, v float64) {
	if !s.primed {
		s.ewma, s.primed = v, true
	} else {
		s.ewma = ewmaAlpha*v + (1-ewmaAlpha)*s.ewma
	}
	s.pts = append(s.pts, point{t, s.ewma})
	cut := 0
	for cut < len(s.pts) && t.Sub(s.pts[cut].t) > s.Window {
		cut++
	}
	if cut > 0 {
		s.pts = append(s.pts[:0], s.pts[cut:]...)
	}
}

// Len is the number of points in the window.
func (s *Series) Len() int { return len(s.pts) }

// Last is the latest smoothed value.
func (s *Series) Last() float64 { return s.ewma }

// Trend is a least-squares line through the window.
type Trend struct {
	Slope     float64 `json:"slope"` // units per second
	R2        float64 `json:"r2"`
	N         int     `json:"n"`
	Span      float64 `json:"spanSeconds"`
	Confident bool    `json:"confident"` // enough samples and span, R² high, slope meaningful
}

// Fit computes the trend. Confident requires at least minSamples points over
// minSpan, R² ≥ minR2 and |slope| ≥ minSlope.
func (s *Series) Fit(minSlope float64) Trend {
	n := len(s.pts)
	tr := Trend{N: n}
	if n < 2 {
		return tr
	}
	t0 := s.pts[0].t
	var sx, sy, sxx, sxy, syy float64
	for _, p := range s.pts {
		x := p.t.Sub(t0).Seconds()
		sx += x
		sy += p.v
		sxx += x * x
		sxy += x * p.v
		syy += p.v * p.v
	}
	fn := float64(n)
	den := fn*sxx - sx*sx
	tr.Span = s.pts[n-1].t.Sub(t0).Seconds()
	if den == 0 {
		return tr
	}
	tr.Slope = (fn*sxy - sx*sy) / den
	vy := fn*syy - sy*sy
	if vy > 0 {
		r := (fn*sxy - sx*sy) / math.Sqrt(den*vy)
		tr.R2 = r * r
	} else {
		tr.R2 = 1 // perfectly flat line: fits, but slope is 0
	}
	tr.Confident = n >= minSamples && tr.Span >= minSpan.Seconds() &&
		tr.R2 >= minR2 && math.Abs(tr.Slope) >= minSlope
	return tr
}

// Forecast is the prediction shown under the big number.
type Forecast struct {
	Confident bool          `json:"confident"`
	ETA       time.Duration `json:"eta"`
	Minutes   int           `json:"minutes"`
	Cause     string        `json:"cause,omitempty"` // "swap" or "disk"
	Text      string        `json:"text"`
}

// Predict combines the swap and disk trends with the current spare room.
//
// Swap growth eats spare room directly: ETA = spare room / swap slope.
// Disk loss eats the space swap needs: ETA = (disk free − floor) / −disk slope.
// The sooner of the two wins. Only confident, rising-pressure trends count,
// and anything beyond an hour reads as "Steady".
func Predict(swap, disk Trend, spareRoom, diskFree, diskFloor uint64) Forecast {
	best := time.Duration(math.MaxInt64)
	cause := ""
	if swap.Confident && swap.Slope > 0 {
		eta := time.Duration(float64(spareRoom) / swap.Slope * float64(time.Second))
		if eta < best {
			best, cause = eta, "swap"
		}
	}
	if disk.Confident && disk.Slope < 0 {
		left := float64(sub(diskFree, diskFloor))
		eta := time.Duration(left / -disk.Slope * float64(time.Second))
		if eta < best {
			best, cause = eta, "disk"
		}
	}
	if cause == "" || best > maxHorizon {
		return Forecast{Text: "Steady. Nothing is filling up fast."}
	}
	mins := int(math.Round(best.Minutes()))
	f := Forecast{Confident: true, ETA: best, Minutes: mins, Cause: cause}
	switch {
	case mins <= 1:
		f.Text = "Slowdowns likely within a minute."
	default:
		f.Text = fmt.Sprintf("Slowdowns likely in about %d minutes.", mins)
	}
	return f
}
