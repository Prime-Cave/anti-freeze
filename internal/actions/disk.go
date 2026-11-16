package actions

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Prime-Cave/headroom/internal/analyzer"
)

// Disk scanning (section 5.4). Headroom looks only at these places, only when
// asked, and never deletes: it can show an item in Finder or move it to the
// Trash after a confirmation that names every item and the total size.

// DiskItem is one place Headroom measured.
type DiskItem struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      uint64 `json:"size"`
	Known     bool   `json:"known"`
	Note      string `json:"note,omitempty"`
	Kind      string `json:"kind"` // downloads | caches | trash | developer
	CanReveal bool   `json:"canReveal"`
	CanReview bool   `json:"canReview"`
}

// DiskReport is the result of a scan.
type DiskReport struct {
	Scanned string     `json:"scanned"` // RFC 3339, empty if never scanned
	Items   []DiskItem `json:"items"`
	Total   uint64     `json:"total"`
}

// LargeFile is one entry inside a reviewable place.
type LargeFile struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     uint64 `json:"size"`
	Modified string `json:"modified"`
}

// TrashPlan is what the "Move to Trash?" confirmation shows.
type TrashPlan struct {
	Token string   `json:"token"`
	Names []string `json:"names"`
	Total uint64   `json:"total"`
	Count int      `json:"count"`
}

type trashEntry struct {
	path string
	dev  uint64
	ino  uint64
	size uint64
}

type trashTicket struct {
	entries []trashEntry
	total   uint64
	expires time.Time
}

// NeedsAccess is shown when macOS denies Headroom a folder.
const NeedsAccess = "Needs Full Disk Access to measure. Headroom works without it."

// ScanLimit caps how long one place is walked.
const ScanLimit = 20 * time.Second

type place struct {
	name, rel, kind string
	review          bool
}

// The only places Headroom scans. Paths are relative to the home folder.
var places = []place{
	{"Downloads", "Downloads", "downloads", true},
	{"App caches", "Library/Caches", "caches", true},
	{"Trash", ".Trash", "trash", false},
	{"Xcode build data", "Library/Developer/Xcode/DerivedData", "developer", true},
	{"Xcode device support", "Library/Developer/Xcode/iOS DeviceSupport", "developer", true},
	{"Simulator caches", "Library/Developer/CoreSimulator/Caches", "developer", true},
	{"npm cache", ".npm/_cacache", "developer", true},
	{"Gradle caches", ".gradle/caches", "developer", true},
	{"Cargo download cache", ".cargo/registry/cache", "developer", true},
	// Measured and shown only: backups are managed in Finder, never trashed
	// piece by piece. macOS usually blocks this folder without Full Disk
	// Access, and Headroom then shows the NeedsAccess note instead of asking.
	{"iPhone and iPad backups", "Library/Application Support/MobileSync/Backup", "backups", false},
}

// diskState holds the last scan. Embedded in Manager.
type diskState struct {
	diskMu   sync.Mutex
	report   DiskReport
	scanning bool
}

func (m *Manager) home() string {
	if m.Home != "" {
		return m.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

func (m *Manager) placePath(p place) string {
	return filepath.Join(m.home(), filepath.FromSlash(p.rel))
}

// DiskReport returns the last scan, or scans now if scan is true.
func (m *Manager) DiskReport(ctx context.Context, scan bool) DiskReport {
	if m.DryRun {
		return demoReport(m.now())
	}
	m.diskMu.Lock()
	if !scan || m.scanning {
		r := m.report
		m.diskMu.Unlock()
		return r
	}
	m.scanning = true
	m.diskMu.Unlock()

	var r DiskReport
	for _, p := range places {
		path := m.placePath(p)
		fi, err := os.Lstat(path)
		if err != nil || !fi.IsDir() {
			if p.kind == "developer" {
				continue // only show developer leftovers that exist
			}
			if p.kind == "backups" && errors.Is(err, fs.ErrNotExist) {
				continue // no backups on this Mac
			}
			if errors.Is(err, fs.ErrNotExist) {
				r.Items = append(r.Items, DiskItem{Name: p.name, Path: path, Known: true, Kind: p.kind})
				continue
			}
		}
		size, err := measure(ctx, path)
		it := DiskItem{Name: p.name, Path: path, Size: size, Known: err == nil, Kind: p.kind,
			CanReveal: true, CanReview: p.review && err == nil}
		switch {
		case errors.Is(err, fs.ErrPermission):
			it.Note = NeedsAccess
		case errors.Is(err, context.DeadlineExceeded):
			it.Known, it.Note = true, "At least this much. It took too long to count everything."
		case err != nil:
			it.Note = "Couldn't measure this folder."
		}
		if p.kind == "trash" {
			it.Note = strings.TrimSpace(it.Note + " Empty the Trash in Finder to get this space back.")
		}
		if it.Known {
			r.Total += it.Size
		}
		r.Items = append(r.Items, it)
	}
	r.Scanned = m.now().UTC().Format(time.RFC3339)
	m.diskMu.Lock()
	m.report, m.scanning = r, false
	m.diskMu.Unlock()
	return r
}

// measure sums allocated bytes under root without following symlinks.
// A permission error on the root itself fails the measurement; errors deeper
// down are skipped.
func measure(ctx context.Context, root string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, ScanLimit)
	defer cancel()
	if _, err := os.ReadDir(root); err != nil {
		return 0, err
	}
	var total uint64
	n := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		n++
		if n%512 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		total += allocated(path, d)
		return nil
	})
	return total, err
}

func allocated(path string, d fs.DirEntry) uint64 {
	info, err := d.Info()
	if err != nil {
		return 0
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Blocks) * 512
	}
	return uint64(info.Size())
}

// allowedRoot returns the reviewable place that contains path (strictly
// inside it), or "" if path is outside every place Headroom may touch.
func (m *Manager) allowedRoot(path string, review bool) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", &Error{"That isn't a place Headroom looks after."}
	}
	for _, p := range places {
		if review && !p.review {
			continue
		}
		root := m.placePath(p)
		if path == root && !review {
			return root, nil
		}
		if strings.HasPrefix(path, root+string(filepath.Separator)) {
			// No symlinks between the root and the item: the item's folder
			// must resolve to the same place under the (resolved) root, so
			// nothing outside the root can be reached through a link.
			parent := filepath.Dir(path)
			realParent, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", &Error{"That item can't be found any more."}
			}
			realRoot, err := filepath.EvalSymlinks(root)
			if err != nil || realParent != realRoot+strings.TrimPrefix(parent, root) {
				return "", &Error{"That item is reached through a link, so Headroom won't touch it."}
			}
			return root, nil
		}
	}
	return "", &Error{"That isn't a place Headroom looks after."}
}

// LargeFiles lists the biggest items directly inside one reviewable place.
func (m *Manager) LargeFiles(ctx context.Context, placePath string) ([]LargeFile, error) {
	if m.DryRun {
		return demoFiles(placePath), nil
	}
	var root string
	for _, p := range places {
		if p.review && m.placePath(p) == placePath {
			root = placePath
		}
	}
	if root == "" {
		return nil, &Error{"That isn't a place Headroom looks after."}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, &Error{NeedsAccess}
		}
		return nil, &Error{"Couldn't read that folder."}
	}
	var out []LargeFile
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
		if e.Name() == ".DS_Store" || e.Name() == ".localized" {
			continue
		}
		path := filepath.Join(root, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		var size uint64
		if e.IsDir() {
			size, _ = measure(ctx, path)
		} else {
			size = allocated(path, e)
		}
		out = append(out, LargeFile{Path: path, Name: e.Name(), Size: size, Modified: info.ModTime().UTC().Format(time.RFC3339)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	if len(out) > 30 {
		out = out[:30]
	}
	return out, nil
}

// PlanTrash checks the chosen items and returns a one-time token for
// ConfirmTrash, with the names and total size the confirmation must show.
func (m *Manager) PlanTrash(ctx context.Context, paths []string) (TrashPlan, error) {
	if len(paths) == 0 {
		return TrashPlan{}, &Error{"Choose at least one item first."}
	}
	if len(paths) > 200 {
		return TrashPlan{}, &Error{"Choose fewer items at a time (200 at most)."}
	}
	var t trashTicket
	var plan TrashPlan
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		if m.DryRun {
			f, ok := demoFile(p)
			if !ok {
				return TrashPlan{}, &Error{"That isn't a place Headroom looks after."}
			}
			t.entries = append(t.entries, trashEntry{path: p, size: f.Size})
			t.total += f.Size
			plan.Names = append(plan.Names, f.Name)
			continue
		}
		if _, err := m.allowedRoot(p, true); err != nil {
			return TrashPlan{}, err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return TrashPlan{}, &Error{filepath.Base(p) + " can't be found any more."}
		}
		st, _ := fi.Sys().(*syscall.Stat_t)
		var size uint64
		if fi.IsDir() {
			size, _ = measure(ctx, p)
		} else if st != nil {
			size = uint64(st.Blocks) * 512
		}
		e := trashEntry{path: p, size: size}
		if st != nil {
			e.dev, e.ino = uint64(st.Dev), st.Ino
		}
		t.entries = append(t.entries, e)
		t.total += size
		plan.Names = append(plan.Names, filepath.Base(p))
	}
	t.expires = m.now().Add(TokenTTL)
	plan.Token, plan.Total, plan.Count = newToken(), t.total, len(t.entries)
	m.mu.Lock()
	if m.trashs == nil {
		m.trashs = map[string]trashTicket{}
	}
	m.trashs[plan.Token] = t
	m.mu.Unlock()
	return plan, nil
}

// ConfirmTrash moves the planned items to the Trash. Nothing is deleted:
// the user can put items back from the Trash until they empty it.
func (m *Manager) ConfirmTrash(token string) (Result, error) {
	m.mu.Lock()
	t, ok := m.trashs[token]
	delete(m.trashs, token)
	m.mu.Unlock()
	if !ok {
		return Result{}, &Error{"Review the items again before moving them to the Trash."}
	}
	if m.now().After(t.expires) {
		return Result{}, &Error{"That confirmation expired. Review the items again."}
	}
	names := make([]string, 0, len(t.entries))
	for _, e := range t.entries {
		names = append(names, filepath.Base(e.path))
	}
	if m.DryRun {
		m.log("trash", strings.Join(names, ", "), t.total, "Demo mode: nothing was moved")
		return Result{OK: true, Freed: t.total, Message: fmt.Sprintf("Moved %d %s to the Trash. (Demo: nothing was moved.)",
			len(t.entries), plural(len(t.entries), "item", "items"))}, nil
	}
	// Re-check every item is the same file, still inside an allowed place.
	for _, e := range t.entries {
		if _, err := m.allowedRoot(e.path, true); err != nil {
			return Result{}, err
		}
		fi, err := os.Lstat(e.path)
		if err != nil {
			return Result{}, &Error{filepath.Base(e.path) + " can't be found any more. Nothing was moved."}
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && (uint64(st.Dev) != e.dev || st.Ino != e.ino) {
			return Result{}, &Error{filepath.Base(e.path) + " changed since you reviewed it. Nothing was moved."}
		}
	}
	var moved int
	var freed uint64
	var failed []string
	for _, e := range t.entries {
		if err := m.Sys.Trash(e.path); err != nil {
			failed = append(failed, filepath.Base(e.path))
			continue
		}
		moved++
		freed += e.size
	}
	m.log("trash", strings.Join(names, ", "), freed, fmt.Sprintf("Moved %d of %d to the Trash", moved, len(t.entries)))
	m.diskMu.Lock()
	m.report.Scanned = "" // the old numbers are stale
	m.diskMu.Unlock()
	msg := fmt.Sprintf("Moved %d %s to the Trash. Empty the Trash to free %s.", moved, plural(moved, "item", "items"), analyzer.FormatBytes(freed))
	if len(failed) > 0 {
		msg += " Couldn't move: " + strings.Join(failed, ", ") + "."
	}
	return Result{OK: len(failed) == 0, Freed: freed, Message: msg}, nil
}

// Reveal shows an item in Finder. Only places Headroom looks after.
func (m *Manager) Reveal(path string) error {
	if m.DryRun {
		return nil
	}
	if _, err := m.allowedRoot(path, false); err != nil {
		return err
	}
	m.Sys.Reveal(path)
	return nil
}

// OpenTrash opens the Trash in Finder, where the user can empty it.
func (m *Manager) OpenTrash() {
	if m.DryRun {
		return
	}
	m.Sys.Open(filepath.Join(m.home(), ".Trash"))
}

// ---- demo data (synthetic) ---------------------------------------------------

var demoItems = []DiskItem{
	{Name: "Downloads", Path: "/Demo/Downloads", Size: 6_400 << 20, Known: true, Kind: "downloads", CanReveal: true, CanReview: true},
	{Name: "App caches", Path: "/Demo/Library/Caches", Size: 3_100 << 20, Known: true, Kind: "caches", CanReveal: true, CanReview: true},
	{Name: "Trash", Path: "/Demo/.Trash", Size: 1_200 << 20, Known: true, Kind: "trash", CanReveal: true,
		Note: "Empty the Trash in Finder to get this space back."},
	{Name: "Xcode build data", Path: "/Demo/Library/Developer/Xcode/DerivedData", Size: 4_800 << 20, Known: true, Kind: "developer", CanReveal: true, CanReview: true},
}

var demoFileList = map[string][]LargeFile{
	"/Demo/Downloads": {
		{Name: "Design files.zip", Size: 2_300 << 20},
		{Name: "Conference talk.mov", Size: 1_900 << 20},
		{Name: "Installer.dmg", Size: 1_100 << 20},
		{Name: "Photos export", Size: 800 << 20},
	},
	"/Demo/Library/Caches": {
		{Name: "com.example.browser", Size: 1_400 << 20},
		{Name: "com.example.chat", Size: 900 << 20},
		{Name: "com.example.music", Size: 600 << 20},
	},
	"/Demo/Library/Developer/Xcode/DerivedData": {
		{Name: "SampleApp-abcdef", Size: 3_200 << 20},
		{Name: "Widgets-123456", Size: 1_600 << 20},
	},
}

func demoReport(now time.Time) DiskReport {
	r := DiskReport{Scanned: now.UTC().Format(time.RFC3339), Items: demoItems}
	for _, it := range demoItems {
		r.Total += it.Size
	}
	return r
}

func demoFiles(root string) []LargeFile {
	var out []LargeFile
	for _, f := range demoFileList[root] {
		f.Path = root + "/" + f.Name
		f.Modified = "2026-11-01T10:00:00Z"
		out = append(out, f)
	}
	return out
}

func demoFile(path string) (LargeFile, bool) {
	for _, f := range demoFiles(filepath.Dir(path)) {
		if f.Path == path {
			return f, true
		}
	}
	return LargeFile{}, false
}
