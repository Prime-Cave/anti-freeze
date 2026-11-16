package store

import (
	"encoding/csv"
	"io"
	"strconv"
	"time"
)

// ExportCSV writes every stored sample and rollup as CSV, oldest first, with
// a "resolution" column (raw, 1m, 10m). Sizes are in bytes.
func (s *Store) ExportCSV(w io.Writer) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return 0, errClosed
	}
	cw := csv.NewWriter(w)
	header := []string{"resolution", "time", "memory_total", "memory_used", "memory_squeezed", "overflow_on_disk",
		"disk_free", "pressure_level", "processor_percent", "heat", "to_spare", "state", "top_app"}
	if err := cw.Write(header); err != nil {
		return 0, err
	}
	n := 0
	for _, t := range []struct{ name, table string }{{"10m", "rollup_10m"}, {"1m", "rollup_1m"}, {"raw", "samples"}} {
		rows, err := s.db.Query(`SELECT ts, mem_total, mem_used, mem_compressed, swap_used, disk_free, pressure, cpu, thermal, headroom, state, COALESCE(top_app, '') FROM ` + t.table + ` ORDER BY ts`)
		if err != nil {
			return n, err
		}
		for rows.Next() {
			var ts, mt, mu, mc, su, df, hr int64
			var pr, th, st int
			var cpu float64
			var top string
			if err := rows.Scan(&ts, &mt, &mu, &mc, &su, &df, &pr, &cpu, &th, &hr, &st, &top); err != nil {
				rows.Close()
				return n, err
			}
			rec := []string{t.name, time.UnixMilli(ts).UTC().Format(time.RFC3339), i(mt), i(mu), i(mc), i(su), i(df),
				strconv.Itoa(pr), strconv.FormatFloat(cpu, 'f', 1, 64), strconv.Itoa(th), i(hr), stateName(st), top}
			if err := cw.Write(rec); err != nil {
				rows.Close()
				return n, err
			}
			n++
		}
		rows.Close()
	}
	cw.Flush()
	return n, cw.Error()
}

func i(v int64) string { return strconv.FormatInt(v, 10) }

func stateName(s int) string {
	switch s {
	case 1:
		return "tight"
	case 2:
		return "critical"
	}
	return "ok"
}
