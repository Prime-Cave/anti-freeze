package main

import (
	"math"
	"time"

	"github.com/Prime-Cave/headroom/internal/analyzer"
	"github.com/Prime-Cave/headroom/internal/store"
)

// seedDemoHistory fills the throwaway demo database with a synthetic week:
// a daily rhythm, two slowdowns and two logged actions. Nothing here comes
// from a real Mac. Only used with HEADROOM_DEMO.
func seedDemoHistory(st *store.Store, memTotal uint64, now time.Time) {
	const gb = float64(1 << 30)
	start := now.Add(-7 * 24 * time.Hour).Truncate(time.Minute)
	end := now.Add(-25 * time.Minute) // the live demo replay covers the rest
	spikes := []time.Time{now.Add(-4*24*time.Hour - 3*time.Hour), now.Add(-20 * time.Hour)}
	var xs []store.Sample
	for t := start; t.Before(end); t = t.Add(2 * time.Minute) {
		hour := float64(t.Hour()) + float64(t.Minute())/60
		day := 0.5 + 0.5*math.Sin((hour-9)/24*2*math.Pi) // busier in the afternoon
		demand := 4.6*gb + 1.6*gb*day + 0.25*gb*math.Sin(float64(t.Unix())/1700)
		for _, sp := range spikes {
			d := t.Sub(sp).Minutes()
			demand += 5.2 * gb * math.Exp(-(d*d)/(2*22*22))
		}
		used := math.Min(demand, 7.5*gb)
		swap := math.Max(0.5*gb, demand-6.6*gb)
		state := analyzer.OK
		switch {
		case swap > 3.6*gb:
			state = analyzer.Critical
		case swap > 2.2*gb:
			state = analyzer.Tight
		}
		top := "Browser"
		if day < 0.3 {
			top = "Chat"
		}
		xs = append(xs, store.Sample{Time: t, MemTotal: memTotal, MemUsed: uint64(used), MemCompressed: uint64(used * 0.2),
			SwapUsed: uint64(swap), DiskFree: uint64(24*gb - swap*0.5), Pressure: 1 + int(state), CPU: 8 + 25*day,
			Headroom: uint64(math.Max(0, 10*gb-demand)), State: state, TopApp: top})
	}
	_ = st.AddSamples(xs)
	for _, sp := range spikes {
		_, _ = st.SaveEpisode(analyzer.Episode{Start: sp.Add(-18 * time.Minute), End: sp.Add(20 * time.Minute),
			StartSwap: gib(2.4), PeakSwap: gib(5.1), StartDiskFree: gib(22.8), MinDiskFree: gib(21.4),
			Causes:       []analyzer.Cause{analyzer.CausePressureCritical},
			TopFootprint: []analyzer.EpisodeApp{{Name: "Browser", Footprint: gib(3.4), Growth: int64(gib(1.9))}}})
	}
	_ = st.LogAction(store.Action{Time: spikes[0].Add(15 * time.Minute), Kind: "quit", Target: "Browser", Freed: gib(3.4), Detail: "Demo"})
	_ = st.LogAction(store.Action{Time: spikes[1].Add(-3 * time.Hour), Kind: "trash", Target: "Design files.zip, Installer.dmg", Freed: gib(3.3), Detail: "Demo"})
	_ = st.Rollup(now)
}

func gib(x float64) uint64 { return uint64(x * (1 << 30)) }
