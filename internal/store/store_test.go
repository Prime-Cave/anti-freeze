package store

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Prime-Cave/headroom/internal/analyzer"
	"github.com/Prime-Cave/headroom/internal/machine"
)

const GB = uint64(1) << 30

func open(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "Headroom", "headroom.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sample(ts time.Time, used, swap uint64) Sample {
	return Sample{Time: ts, MemTotal: 8 * GB, MemUsed: used, SwapUsed: swap, DiskFree: 40 * GB,
		Pressure: 1, CPU: 10, Headroom: 2 * GB, State: analyzer.OK, TopApp: "Browser"}
}

func count(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOpenCreatesPrivateFiles(t *testing.T) {
	s := open(t)
	fi, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("db mode %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(s.Path()))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %v, want 0700", di.Mode().Perm())
	}
}

func TestMigrationsAreVersionedAndIdempotent(t *testing.T) {
	s := open(t)
	var v int
	_ = s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != SchemaVersion {
		t.Fatalf("user_version %d, want %d", v, SchemaVersion)
	}
	if err := s.AddSample(sample(time.Now(), GB, 0)); err != nil {
		t.Fatal(err)
	}
	path := s.Path()
	_ = s.Close()
	// Reopening must not re-run migrations or lose data.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if count(t, s2, "samples") != 1 {
		t.Fatal("data lost on reopen")
	}
	// Running migrate on an already-migrated db is a no-op.
	if err := migrate(s2.db); err != nil {
		t.Fatal(err)
	}
}

func TestRollupDownsamples(t *testing.T) {
	s := open(t)
	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	// 25 minutes of 5-second samples; memory used ramps 1 GB → 2 GB.
	n := 25 * 12
	for i := 0; i < n; i++ {
		used := GB + GB*uint64(i)/uint64(n)
		if err := s.AddSample(sample(base.Add(time.Duration(i)*5*time.Second), used, uint64(i)*1e6)); err != nil {
			t.Fatal(err)
		}
	}
	now := base.Add(25 * time.Minute)
	if err := s.Rollup(now); err != nil {
		t.Fatal(err)
	}
	if got := count(t, s, "rollup_1m"); got != 25 {
		t.Fatalf("1m rollups %d, want 25", got)
	}
	if got := count(t, s, "rollup_10m"); got != 2 { // 10:00 and 10:10 complete; 10:20 not
		t.Fatalf("10m rollups %d, want 2", got)
	}
	var nSum int
	var maxUsed, maxSwap int64
	_ = s.db.QueryRow(`SELECT n, mem_used_max, swap_used_max FROM rollup_10m ORDER BY ts LIMIT 1`).Scan(&nSum, &maxUsed, &maxSwap)
	if nSum != 120 {
		t.Errorf("first 10m bucket should cover 120 raw samples, got %d", nSum)
	}
	if maxSwap != 119e6 {
		t.Errorf("peaks must survive downsampling: max swap %d", maxSwap)
	}
	// Rollup is idempotent.
	if err := s.Rollup(now); err != nil {
		t.Fatal(err)
	}
	if got := count(t, s, "rollup_1m"); got != 25 {
		t.Fatalf("second rollup duplicated rows: %d", got)
	}
	// Timeline at 1 hour uses raw samples, bucketed.
	pts, err := s.Timeline(base, now, 50)
	if err != nil || len(pts) == 0 || len(pts) > 51 {
		t.Fatalf("timeline: %d points, %v", len(pts), err)
	}
	if pts[0].TopApp != "Browser" {
		t.Errorf("tooltip top app missing: %+v", pts[0])
	}
}

func TestRetention(t *testing.T) {
	s := open(t)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	_ = s.AddSample(sample(now.Add(-25*time.Hour), GB, 0)) // too old for raw
	_ = s.AddSample(sample(now.Add(-time.Hour), GB, 0))
	_, _ = s.db.Exec(`INSERT INTO rollup_1m (ts, n) VALUES (?, 1), (?, 1)`, now.Add(-8*24*time.Hour).UnixMilli(), now.Add(-6*24*time.Hour).UnixMilli())
	_, _ = s.db.Exec(`INSERT INTO rollup_10m (ts, n) VALUES (?, 1), (?, 1)`, now.Add(-91*24*time.Hour).UnixMilli(), now.Add(-89*24*time.Hour).UnixMilli())
	_, _ = s.SaveEpisode(analyzer.Episode{Start: now.Add(-400 * 24 * time.Hour)})
	_, _ = s.SaveEpisode(analyzer.Episode{Start: now.Add(-300 * 24 * time.Hour)})
	if err := s.Prune(now); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"samples": 1, "rollup_1m": 1, "rollup_10m": 1, "episodes": 1} {
		if got := count(t, s, table); got != want {
			t.Errorf("%s: %d rows after prune, want %d", table, got, want)
		}
	}
}

func TestSizeCap(t *testing.T) {
	s := open(t)
	s.MaxBytes = 256 << 10 // 256 KB
	base := time.Now().Add(-20 * time.Hour)
	tx, _ := s.db.Begin()
	for i := 0; i < 20000; i++ {
		x := sample(base.Add(time.Duration(i)*2*time.Second), GB, 0)
		x.TopApp = "An app with a reasonably long name to take up space"
		_, _ = tx.Exec(`INSERT INTO samples (ts, mem_total, mem_used, swap_used, disk_free, pressure, cpu, thermal, headroom, state, top_app) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			x.Time.UnixMilli(), 1, 1, 1, 1, 1, 1.0, 0, 1, 0, x.TopApp)
	}
	_ = tx.Commit()
	if err := s.Prune(time.Now()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	size, _ := s.sizeLocked()
	s.mu.Unlock()
	if size > s.MaxBytes {
		t.Fatalf("db %d bytes, cap %d", size, s.MaxBytes)
	}
	if count(t, s, "samples") == 0 {
		t.Fatal("cap should trim oldest data, not everything")
	}
}

func TestExportCSV(t *testing.T) {
	s := open(t)
	base := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_ = s.AddSample(sample(base.Add(time.Duration(i)*5*time.Second), GB, 0))
	}
	var buf bytes.Buffer
	n, err := s.ExportCSV(&buf)
	if err != nil || n != 3 {
		t.Fatalf("export n=%d err=%v", n, err)
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(recs) != 4 || recs[0][0] != "resolution" || recs[1][1] != "2026-11-09T10:00:00Z" {
		t.Fatalf("csv: %v %v", recs, err)
	}
}

func TestWipeReallyDeletes(t *testing.T) {
	s := open(t)
	_ = s.AddSample(sample(time.Now(), GB, 0))
	_, _ = s.SaveEpisode(analyzer.Episode{Start: time.Now()})
	_ = s.LogAction(Action{Time: time.Now(), Kind: "quit", Target: "Browser"})
	_ = s.SaveProfile(machine.Profile{Model: "Mac0,0"})
	_ = s.SetSetting("sensitivity", "early")

	// Capture the old file's identity so we can prove it was removed, not emptied.
	before, _ := os.Stat(s.Path())
	if err := s.Wipe(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal("fresh database should exist after wipe")
	}
	if os.SameFile(before, after) {
		t.Fatal("wipe must delete the database file, not just its rows")
	}
	for _, table := range []string{"samples", "episodes", "actions", "rollup_1m", "rollup_10m"} {
		if got := count(t, s, table); got != 0 {
			t.Errorf("%s has %d rows after wipe", table, got)
		}
	}
	if _, ok := s.Profile(); ok {
		t.Error("profile should be gone after wipe")
	}
	if s.Settings()["sensitivity"] != "early" {
		t.Error("settings should survive a history wipe")
	}
	// The old content must not be recoverable from the new file.
	raw, _ := os.ReadFile(s.Path())
	if bytes.Contains(raw, []byte("Browser")) {
		t.Error("old data still present in the file bytes")
	}
	// And the file is still private.
	if fi, _ := os.Stat(s.Path()); fi.Mode().Perm() != 0o600 {
		t.Error("recreated db must be 0600")
	}
}

func TestEpisodeRoundTrip(t *testing.T) {
	s := open(t)
	start := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	e := analyzer.Episode{Start: start, StartSwap: 2 * GB, PeakSwap: 2 * GB, StartDiskFree: 11 * GB, MinDiskFree: 11 * GB,
		Causes:       []analyzer.Cause{analyzer.CausePressureCritical},
		TopFootprint: []analyzer.EpisodeApp{{Name: "Browser", Footprint: 3 * GB, Growth: int64(GB)}}}
	id, err := s.SaveEpisode(e)
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	e.ID, e.End, e.PeakSwap = id, start.Add(5*time.Minute), 4*GB
	if _, err := s.SaveEpisode(e); err != nil {
		t.Fatal(err)
	}
	got, err := s.Episodes(start.Add(-time.Hour), start.Add(time.Hour))
	if err != nil || len(got) != 1 {
		t.Fatalf("episodes %v %v", got, err)
	}
	if got[0].PeakSwap != 4*GB || got[0].End.IsZero() || got[0].TopFootprint[0].Name != "Browser" {
		t.Fatalf("round trip %+v", got[0])
	}
}
