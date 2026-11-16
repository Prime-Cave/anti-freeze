// Package store keeps Headroom's history in SQLite at
// ~/Library/Application Support/Headroom/headroom.db (folder 0700, file 0600).
//
// Retention: raw samples 24 h, 1-minute rollups 7 days, 10-minute rollups for
// the user's history setting (default 90 days), episodes and the action log
// 1 year. The file is capped (default 64 MB) by trimming the oldest data.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Prime-Cave/headroom/internal/analyzer"
	"github.com/Prime-Cave/headroom/internal/machine"
	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// Retention defaults (section 6).
const (
	RawRetention     = 24 * time.Hour
	MinuteRetention  = 7 * 24 * time.Hour
	TenMinRetention  = 90 * 24 * time.Hour
	EpisodeRetention = 365 * 24 * time.Hour
	DefaultMaxBytes  = 64 << 20
)

// Store is the database handle. Safe for concurrent use.
type Store struct {
	mu       sync.Mutex
	path     string
	db       *sql.DB
	MaxBytes int64
	// TenMinRetention can be changed by the "history retention" setting.
	TenMinRetention time.Duration
}

// DefaultPath is ~/Library/Application Support/Headroom/headroom.db.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir() // ~/Library/Application Support on macOS
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Headroom", "headroom.db"), nil
}

// Open opens (creating if needed) the database at path with private
// permissions and runs migrations.
func Open(path string) (*Store, error) {
	s := &Store{path: path, MaxBytes: DefaultMaxBytes, TenMinRetention: TenMinRetention}
	if err := s.open(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) open() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	// Create the file ourselves so it is 0600 from the first byte.
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	_ = f.Close()
	if err := os.Chmod(s.path, 0o600); err != nil {
		return err
	}
	dsn := "file:" + s.path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1) // one writer; keeps memory low
	if err := migrate(db); err != nil {
		_ = db.Close()
		return fmt.Errorf("migrate: %w", err)
	}
	s.db = db
	return nil
}

// Path is the database file path.
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// ---- migrations -------------------------------------------------------------

var migrations = []string{
	// 1: initial schema
	`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
	 CREATE TABLE samples (
	   ts INTEGER PRIMARY KEY,           -- unix ms
	   mem_total INTEGER, mem_used INTEGER, mem_compressed INTEGER,
	   swap_used INTEGER, disk_free INTEGER,
	   pressure INTEGER, cpu REAL, thermal INTEGER,
	   headroom INTEGER, state INTEGER, top_app TEXT);
	 CREATE TABLE rollup_1m (
	   ts INTEGER PRIMARY KEY, n INTEGER,
	   mem_total INTEGER, mem_used INTEGER, mem_used_max INTEGER, mem_compressed INTEGER,
	   swap_used INTEGER, swap_used_max INTEGER, disk_free INTEGER, disk_free_min INTEGER,
	   pressure INTEGER, cpu REAL, thermal INTEGER,
	   headroom INTEGER, headroom_min INTEGER, state INTEGER, top_app TEXT);
	 CREATE TABLE rollup_10m AS SELECT * FROM rollup_1m WHERE 0;
	 CREATE UNIQUE INDEX rollup_10m_ts ON rollup_10m(ts);
	 CREATE TABLE episodes (
	   id INTEGER PRIMARY KEY AUTOINCREMENT,
	   start INTEGER NOT NULL, end INTEGER,
	   start_swap INTEGER, peak_swap INTEGER, start_disk INTEGER, min_disk INTEGER,
	   causes TEXT, top_footprint TEXT, top_growth TEXT);
	 CREATE INDEX episodes_start ON episodes(start);
	 CREATE TABLE actions (
	   id INTEGER PRIMARY KEY AUTOINCREMENT,
	   ts INTEGER NOT NULL, kind TEXT NOT NULL, target TEXT, freed INTEGER, detail TEXT);
	 CREATE INDEX actions_ts ON actions(ts);
	 CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
}

// SchemaVersion is the latest migration number.
var SchemaVersion = len(migrations)

func migrate(db *sql.DB) error {
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---- samples ----------------------------------------------------------------

// Sample is one stored point. Bytes unless noted.
type Sample struct {
	Time          time.Time      `json:"time"`
	MemTotal      uint64         `json:"memTotal"`
	MemUsed       uint64         `json:"memUsed"`
	MemCompressed uint64         `json:"memCompressed"`
	SwapUsed      uint64         `json:"swapUsed"`
	DiskFree      uint64         `json:"diskFree"`
	Pressure      int            `json:"pressure"`
	CPU           float64        `json:"cpu"` // percent
	Thermal       int            `json:"thermal"`
	Headroom      uint64         `json:"headroom"`
	State         analyzer.State `json:"state"`
	TopApp        string         `json:"topApp"`
}

// SampleFromStatus builds a stored sample from an analyzer status.
func SampleFromStatus(st analyzer.Status, pressure, thermal int) Sample {
	top := ""
	if len(st.TopApps) > 0 {
		top = st.TopApps[0].Name
	}
	r := st.Runway
	return Sample{
		Time: st.Time, MemTotal: r.MemTotal, MemUsed: r.InUse + r.Squeezed, MemCompressed: r.Squeezed,
		SwapUsed: r.Overflow, DiskFree: r.DiskFree, Pressure: pressure, CPU: st.CPU, Thermal: thermal,
		Headroom: st.Headroom.Total, State: st.State, TopApp: top,
	}
}

// AddSample stores a raw sample.
func (s *Store) AddSample(x Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errClosed
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO samples
	  (ts, mem_total, mem_used, mem_compressed, swap_used, disk_free, pressure, cpu, thermal, headroom, state, top_app)
	  VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.Time.UnixMilli(), int64(x.MemTotal), int64(x.MemUsed), int64(x.MemCompressed), int64(x.SwapUsed),
		int64(x.DiskFree), x.Pressure, x.CPU, x.Thermal, int64(x.Headroom), int(x.State), x.TopApp)
	return err
}

// AddSamples records many samples in one transaction.
func (s *Store) AddSamples(xs []Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO samples
	  (ts, mem_total, mem_used, mem_compressed, swap_used, disk_free, pressure, cpu, thermal, headroom, state, top_app)
	  VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, x := range xs {
		if _, err := stmt.Exec(x.Time.UnixMilli(), int64(x.MemTotal), int64(x.MemUsed), int64(x.MemCompressed), int64(x.SwapUsed),
			int64(x.DiskFree), x.Pressure, x.CPU, x.Thermal, int64(x.Headroom), int(x.State), x.TopApp); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

var errClosed = errors.New("store: closed")

// ---- rollups and retention ----------------------------------------------------

// rollupSQL aggregates rows of `src` into buckets of `ms` milliseconds in
// `dst`, for buckets that are complete (end before `until`) and not yet
// present in `dst`. top_app is the app from the row with the highest memory use.
const rollupSQL = `
INSERT OR IGNORE INTO %[2]s
 (ts, n, mem_total, mem_used, mem_used_max, mem_compressed, swap_used, swap_used_max,
  disk_free, disk_free_min, pressure, cpu, thermal, headroom, headroom_min, state, top_app)
SELECT b, %[4]s, MAX(mem_total), CAST(AVG(mem_used) AS INTEGER), MAX(%[5]s), CAST(AVG(mem_compressed) AS INTEGER),
       CAST(AVG(swap_used) AS INTEGER), MAX(%[6]s), CAST(AVG(disk_free) AS INTEGER), MIN(%[7]s),
       MAX(pressure), AVG(cpu), MAX(thermal), CAST(AVG(headroom) AS INTEGER), MIN(%[8]s), MAX(state),
       (SELECT top_app FROM %[1]s s2 WHERE s2.ts >= b AND s2.ts < b + %[3]d ORDER BY mem_used DESC LIMIT 1)
FROM (SELECT ts / %[3]d * %[3]d AS b, * FROM %[1]s
      WHERE ts >= (SELECT COALESCE(MAX(ts), 0) FROM %[2]s))
WHERE b + %[3]d <= ? AND b NOT IN (SELECT ts FROM %[2]s)
GROUP BY b`

// Rollup builds 1-minute rollups from raw samples and 10-minute rollups from
// 1-minute rollups, for complete buckets before now.
func (s *Store) Rollup(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errClosed
	}
	until := now.UnixMilli()
	q1 := fmt.Sprintf(rollupSQL, "samples", "rollup_1m", 60_000, "COUNT(*)", "mem_used", "swap_used", "disk_free", "headroom")
	if _, err := s.db.Exec(q1, until); err != nil {
		return fmt.Errorf("rollup 1m: %w", err)
	}
	q10 := fmt.Sprintf(rollupSQL, "rollup_1m", "rollup_10m", 600_000, "SUM(n)", "mem_used_max", "swap_used_max", "disk_free_min", "headroom_min")
	if _, err := s.db.Exec(q10, until); err != nil {
		return fmt.Errorf("rollup 10m: %w", err)
	}
	return nil
}

// Prune applies retention and the size cap.
func (s *Store) Prune(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errClosed
	}
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	for _, q := range []struct {
		sql string
		arg int64
	}{
		{`DELETE FROM samples WHERE ts < ?`, ms(RawRetention)},
		{`DELETE FROM rollup_1m WHERE ts < ?`, ms(MinuteRetention)},
		{`DELETE FROM rollup_10m WHERE ts < ?`, ms(s.TenMinRetention)},
		{`DELETE FROM episodes WHERE start < ?`, ms(EpisodeRetention)},
		{`DELETE FROM actions WHERE ts < ?`, ms(EpisodeRetention)},
	} {
		if _, err := s.db.Exec(q.sql, q.arg); err != nil {
			return err
		}
	}
	return s.capSizeLocked()
}

// capSizeLocked trims the oldest data, coarsest-last, until the database
// fits MaxBytes.
func (s *Store) capSizeLocked() error {
	for i := 0; i < 20; i++ {
		size, err := s.sizeLocked()
		if err != nil || size <= s.MaxBytes {
			return err
		}
		// Drop the oldest 20% of raw samples, then 1-minute, then 10-minute rollups.
		trimmed := false
		for _, t := range []string{"samples", "rollup_1m", "rollup_10m"} {
			var n int64
			_ = s.db.QueryRow(`SELECT COUNT(*) FROM ` + t).Scan(&n)
			if n == 0 {
				continue
			}
			if _, err := s.db.Exec(`DELETE FROM `+t+` WHERE ts IN (SELECT ts FROM `+t+` ORDER BY ts LIMIT ?)`, max(n/5, 1)); err != nil {
				return err
			}
			trimmed = true
			break
		}
		if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`VACUUM`); err != nil {
			return err
		}
		if !trimmed {
			return nil
		}
	}
	return nil
}

func (s *Store) sizeLocked() (int64, error) {
	var pages, pageSize int64
	if err := s.db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		return 0, err
	}
	if err := s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, err
	}
	return pages * pageSize, nil
}

// ---- queries ----------------------------------------------------------------

// Point is one timeline point.
type Point struct {
	Time     time.Time `json:"time"`
	MemUsed  uint64    `json:"memUsed"` // in use + squeezed
	MemTotal uint64    `json:"memTotal"`
	SwapUsed uint64    `json:"swapUsed"`
	DiskFree uint64    `json:"diskFree"`
	Headroom uint64    `json:"headroom"`
	State    int       `json:"state"`
	CPU      float64   `json:"cpu"`
	TopApp   string    `json:"topApp"`
}

// Timeline returns at most maxPoints points between from and to, using the
// finest table that still covers the range.
func (s *Store) Timeline(from, to time.Time, maxPoints int) ([]Point, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errClosed
	}
	span := to.Sub(from)
	table := "samples"
	switch {
	case span > 7*24*time.Hour || time.Since(from) > MinuteRetention:
		table = "rollup_10m"
	case span > 2*time.Hour || time.Since(from) > RawRetention:
		table = "rollup_1m"
	}
	bucket := max(span.Milliseconds()/int64(max(maxPoints, 1)), 1)
	q := fmt.Sprintf(`SELECT b, MAX(mem_total), MAX(mem_used), MAX(swap_used), MIN(disk_free),
	   MIN(headroom), MAX(state), AVG(cpu),
	   (SELECT top_app FROM %[1]s s2 WHERE s2.ts >= b AND s2.ts < b + %[2]d ORDER BY mem_used DESC LIMIT 1)
	 FROM (SELECT ts / %[2]d * %[2]d AS b, * FROM %[1]s WHERE ts >= ? AND ts <= ?) GROUP BY b ORDER BY b`, table, bucket)
	rows, err := s.db.Query(q, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var ts, mt, mu, su, df, hr int64
		var state int
		var cpu float64
		var top sql.NullString
		if err := rows.Scan(&ts, &mt, &mu, &su, &df, &hr, &state, &cpu, &top); err != nil {
			return nil, err
		}
		out = append(out, Point{Time: time.UnixMilli(ts), MemTotal: uint64(mt), MemUsed: uint64(mu), SwapUsed: uint64(su),
			DiskFree: uint64(df), Headroom: uint64(hr), State: state, CPU: cpu, TopApp: top.String})
	}
	return out, rows.Err()
}

// HistoryStart is the time of the oldest stored data (zero if none).
func (s *Store) HistoryStart() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return time.Time{}
	}
	var ts sql.NullInt64
	_ = s.db.QueryRow(`SELECT MIN(t) FROM (SELECT MIN(ts) t FROM samples UNION ALL SELECT MIN(ts) FROM rollup_1m UNION ALL SELECT MIN(ts) FROM rollup_10m)`).Scan(&ts)
	if !ts.Valid {
		return time.Time{}
	}
	return time.UnixMilli(ts.Int64)
}

// ---- episodes ---------------------------------------------------------------

// SaveEpisode inserts (ID == 0) or updates an episode and returns its id.
func (s *Store) SaveEpisode(e analyzer.Episode) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return 0, errClosed
	}
	causes, _ := json.Marshal(e.Causes)
	tf, _ := json.Marshal(e.TopFootprint)
	tg, _ := json.Marshal(e.TopGrowth)
	var end any
	if !e.End.IsZero() {
		end = e.End.UnixMilli()
	}
	if e.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO episodes (start, end, start_swap, peak_swap, start_disk, min_disk, causes, top_footprint, top_growth)
		  VALUES (?,?,?,?,?,?,?,?,?)`, e.Start.UnixMilli(), end, int64(e.StartSwap), int64(e.PeakSwap),
			int64(e.StartDiskFree), int64(e.MinDiskFree), string(causes), string(tf), string(tg))
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.db.Exec(`UPDATE episodes SET end=?, peak_swap=?, min_disk=? WHERE id=?`,
		end, int64(e.PeakSwap), int64(e.MinDiskFree), e.ID)
	return e.ID, err
}

// Episodes returns episodes that started in [from, to], oldest first.
func (s *Store) Episodes(from, to time.Time) ([]analyzer.Episode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errClosed
	}
	rows, err := s.db.Query(`SELECT id, start, end, start_swap, peak_swap, start_disk, min_disk, causes, top_footprint, top_growth
	  FROM episodes WHERE start >= ? AND start <= ? ORDER BY start`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []analyzer.Episode
	for rows.Next() {
		var e analyzer.Episode
		var start int64
		var end sql.NullInt64
		var ss, ps, sd, md int64
		var causes, tf, tg string
		if err := rows.Scan(&e.ID, &start, &end, &ss, &ps, &sd, &md, &causes, &tf, &tg); err != nil {
			return nil, err
		}
		e.Start = time.UnixMilli(start)
		if end.Valid {
			e.End = time.UnixMilli(end.Int64)
		}
		e.StartSwap, e.PeakSwap, e.StartDiskFree, e.MinDiskFree = uint64(ss), uint64(ps), uint64(sd), uint64(md)
		_ = json.Unmarshal([]byte(causes), &e.Causes)
		_ = json.Unmarshal([]byte(tf), &e.TopFootprint)
		_ = json.Unmarshal([]byte(tg), &e.TopGrowth)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- action log -------------------------------------------------------------

// Action is one thing Headroom did (section 5.6).
type Action struct {
	ID     int64     `json:"id"`
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`   // "quit", "force-quit", "trash", "reveal", "export", "wipe"
	Target string    `json:"target"` // app name or item names
	Freed  uint64    `json:"freed"`  // bytes, when known
	Detail string    `json:"detail"`
}

// LogAction records an action.
func (s *Store) LogAction(a Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errClosed
	}
	_, err := s.db.Exec(`INSERT INTO actions (ts, kind, target, freed, detail) VALUES (?,?,?,?,?)`,
		a.Time.UnixMilli(), a.Kind, a.Target, int64(a.Freed), a.Detail)
	return err
}

// Actions returns the newest actions first.
func (s *Store) Actions(limit int) ([]Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errClosed
	}
	rows, err := s.db.Query(`SELECT id, ts, kind, target, freed, detail FROM actions ORDER BY ts DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		var a Action
		var ts, freed int64
		var target, detail sql.NullString
		if err := rows.Scan(&a.ID, &ts, &a.Kind, &target, &freed, &detail); err != nil {
			return nil, err
		}
		a.Time, a.Freed, a.Target, a.Detail = time.UnixMilli(ts), uint64(freed), target.String, detail.String
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- meta: profile and settings ---------------------------------------------

// Profile loads the stored machine profile.
func (s *Store) Profile() (machine.Profile, bool) {
	var p machine.Profile
	v, ok := s.getKV("meta", "profile")
	if !ok || json.Unmarshal([]byte(v), &p) != nil {
		return p, false
	}
	return p, true
}

// SaveProfile stores the machine profile.
func (s *Store) SaveProfile(p machine.Profile) error {
	b, _ := json.Marshal(p)
	return s.setKV("meta", "profile", string(b))
}

// Settings returns all settings.
func (s *Store) Settings() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	if s.db == nil {
		return out
	}
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			out[k] = v
		}
	}
	return out
}

// SetSetting stores one setting.
func (s *Store) SetSetting(k, v string) error { return s.setKV("settings", k, v) }

func (s *Store) getKV(table, k string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return "", false
	}
	var v string
	if err := s.db.QueryRow(`SELECT value FROM `+table+` WHERE key = ?`, k).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func (s *Store) setKV(table, k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errClosed
	}
	_, err := s.db.Exec(`INSERT INTO `+table+` (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v)
	return err
}

// ---- wipe -------------------------------------------------------------------

// Wipe deletes the database files from disk (main file, WAL and shared
// memory) and creates a fresh, empty database. Settings are carried over;
// history, episodes, the action log and the machine profile are not.
func (s *Store) Wipe(ctx context.Context) error {
	settings := s.Settings()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(s.path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.open(); err != nil {
		return err
	}
	for k, v := range settings {
		if _, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, k, v); err != nil {
			return err
		}
	}
	return nil
}
