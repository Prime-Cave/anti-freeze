package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// A database written by v0.1.0 (schema 1, column "headroom") keeps its
// history when it is upgraded to the "spare_room" columns.
func TestMigrateV1KeepsSpareRoom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anti-freeze.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-time.Minute).UnixMilli()
	for _, q := range []string{
		migrations[0],
		`PRAGMA user_version = 1`,
		fmt.Sprintf(`INSERT INTO samples (ts, mem_total, mem_used, swap_used, disk_free, pressure, cpu, thermal, headroom, state, top_app)
		 VALUES (%d, %d, %d, 0, %d, 1, 5, 0, %d, 0, 'Browser')`, ts, 8*GB, 4*GB, 40*GB, 3*GB),
		fmt.Sprintf(`INSERT INTO rollup_10m (ts, n, headroom, headroom_min) VALUES (%d, 1, %d, %d)`, ts, 3*GB, 2*GB),
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var v int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != SchemaVersion {
		t.Fatalf("user_version = %d (%v), want %d", v, err, SchemaVersion)
	}
	pts, err := s.Timeline(time.UnixMilli(ts).Add(-time.Second), time.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].SpareRoom != 3*GB {
		t.Fatalf("timeline after upgrade = %+v, want one point with 3 GB spare room", pts)
	}
	var lo uint64
	if err := s.db.QueryRow(`SELECT spare_room_min FROM rollup_10m`).Scan(&lo); err != nil || lo != 2*GB {
		t.Fatalf("rollup_10m spare_room_min = %d (%v), want 2 GB", lo, err)
	}
}
