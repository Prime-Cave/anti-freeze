package collector

type pageCounts struct {
	free, active, inactive, wired, compressor, purgeable, speculative, external, internal uint64
	pageSize, memsize                                                                     uint64
}

// memoryFromPages turns raw page counts into the Activity Monitor-compatible
// figures. Pure so it can be unit tested.
func memoryFromPages(p pageCounts) Memory {
	ps := p.pageSize
	sat := func(a, b uint64) uint64 {
		if a > b {
			return a - b
		}
		return 0
	}
	m := Memory{
		Total:       p.memsize,
		Free:        sat(p.free, p.speculative) * ps,
		Active:      p.active * ps,
		Inactive:    p.inactive * ps,
		Wired:       p.wired * ps,
		Compressed:  p.compressor * ps,
		Purgeable:   p.purgeable * ps,
		Speculative: p.speculative * ps,
		External:    p.external * ps,
		Internal:    p.internal * ps,
		PageSize:    ps,
	}
	m.AppMemory = sat(p.internal, p.purgeable) * ps
	m.Used = m.AppMemory + m.Wired + m.Compressed
	if m.Used > m.Total && m.Total > 0 {
		m.Used = m.Total
	}
	m.Cached = (p.external + p.purgeable) * ps
	return m
}

// Spare is memory not in use: Total − Used (free + cached files + purgeable).
func (m Memory) Spare() uint64 {
	if m.Total > m.Used {
		return m.Total - m.Used
	}
	return 0
}
