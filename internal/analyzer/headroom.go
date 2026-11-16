package analyzer

import (
	"github.com/Prime-Cave/anti-freeze/internal/collector"
	"github.com/Prime-Cave/anti-freeze/internal/machine"
)

// Headroom is the "to spare" number and its parts, in bytes.
//
//	anti-freeze = free + reclaimable (cached files) + purgeable + safe swap allowance
//	safe swap allowance = min( SwapBudget − swap used,  disk free − DiskReserve ),  never below 0
//
// free + reclaimable + purgeable is the memory not in use: total − (app +
// wired + compressed), as Activity Monitor computes it. The swap allowance is
// how much more overflow the Mac can take comfortably, limited both by how
// much swap this machine copes with (SwapBudget, half of memory by default,
// lowered by learning) and by the free disk the swap files need.
type Headroom struct {
	Total         uint64 `json:"total"`
	Free          uint64 `json:"free"`
	Reclaimable   uint64 `json:"reclaimable"`
	Purgeable     uint64 `json:"purgeable"`
	SwapAllowance uint64 `json:"swapAllowance"`
}

// ComputeHeadroom applies the formula. Unavailable inputs count as 0 room
// (the cautious reading), and the UI separately says what is unavailable.
func ComputeHeadroom(s collector.Snapshot, th machine.Thresholds) Headroom {
	var h Headroom
	if s.Has("memory") {
		m := s.Memory
		spare := m.Spare()
		h.Free = min(m.Free, spare)
		h.Purgeable = min(m.Purgeable, spare-h.Free)
		h.Reclaimable = spare - h.Free - h.Purgeable
	}
	if s.Has("swap") && s.Has("disk") {
		budget := sub(th.SwapBudget, s.Swap.Used)
		disk := sub(s.Disk.Available, th.DiskReserve)
		h.SwapAllowance = min(budget, disk)
	}
	h.Total = h.Free + h.Reclaimable + h.Purgeable + h.SwapAllowance
	return h
}

func sub(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return 0
}
