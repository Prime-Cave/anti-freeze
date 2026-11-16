package machine

import (
	"testing"
	"time"
)

func TestDefaultsScaleWithMachine(t *testing.T) {
	small := Profile{MemoryBytes: 8 * gb, DiskBytes: 256e9}.Defaults()
	if small.DiskReserve != 16*gb { // 25.6 GB capped at 16
		t.Errorf("256 GB disk reserve = %d", small.DiskReserve/gb)
	}
	if small.DiskFloor != 8*gb || small.SwapBudget != 4*gb {
		t.Errorf("small: %+v", small)
	}
	tiny := Profile{MemoryBytes: 8 * gb, DiskBytes: 64e9}.Defaults()
	if tiny.DiskReserve != 8*gb || tiny.DiskFloor != 4*gb {
		t.Errorf("tiny: %+v", tiny)
	}
	big := Profile{MemoryBytes: 64 * gb, DiskBytes: 2000e9}.Defaults()
	if big.SwapBudget != 32*gb || big.SwapGrowthTight <= small.SwapGrowthTight {
		t.Errorf("big: %+v", big)
	}
}

func TestSensitivity(t *testing.T) {
	d := Profile{MemoryBytes: 8 * gb, DiskBytes: 256e9}.Defaults()
	if e := d.Apply(Early); e.ForecastCritical <= d.ForecastCritical || e.DiskReserve <= d.DiskReserve {
		t.Errorf("early should be more cautious: %+v", e)
	}
	if r := d.Apply(Relaxed); r.ForecastCritical >= d.ForecastCritical {
		t.Errorf("relaxed should be less cautious: %+v", r)
	}
	if d.Apply(Balanced) != d {
		t.Error("balanced changes nothing")
	}
}

func TestDescribe(t *testing.T) {
	p := Profile{ModelName: "MacBook Air", MemoryBytes: 8 * gb, DiskBytes: 245107195904,
		FirstSeen: time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)}
	got := p.Describe(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	want := "MacBook Air, 8 GB memory, 256 GB disk. Watching since 9 Nov."
	if got != want {
		t.Errorf("got %q", got)
	}
}

func TestFriendlyName(t *testing.T) {
	if FriendlyName("MacBookAir10,1") != "MacBook Air" || FriendlyName("Mac14,2") != "Mac" {
		t.Error("friendly names")
	}
}
