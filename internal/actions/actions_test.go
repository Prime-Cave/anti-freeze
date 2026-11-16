package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Prime-Cave/headroom/internal/analyzer"
	"github.com/Prime-Cave/headroom/internal/collector"
)

// fakeSys is a fake process table and a fake Trash. It records every call
// and never touches real apps or files.
type fakeSys struct {
	mu         sync.Mutex
	running    map[int]bool
	ignoreQuit map[int]bool // apps that stay open (asking to save)
	terminated []int
	forced     []int
	trashed    []string
	revealed   []string
	opened     []string
}

func newFakeSys(pids ...int) *fakeSys {
	f := &fakeSys{running: map[int]bool{}, ignoreQuit: map[int]bool{}}
	for _, p := range pids {
		f.running[p] = true
	}
	return f
}

func (f *fakeSys) Terminate(pid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminated = append(f.terminated, pid)
	if !f.running[pid] {
		return false
	}
	if !f.ignoreQuit[pid] {
		f.running[pid] = false
	}
	return true
}
func (f *fakeSys) ForceTerminate(pid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forced = append(f.forced, pid)
	ok := f.running[pid]
	f.running[pid] = false
	return ok
}
func (f *fakeSys) IsRunning(pid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[pid]
}
func (f *fakeSys) Trash(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trashed = append(f.trashed, path) // recorded only; the file stays put
	return nil
}
func (f *fakeSys) Reveal(path string) { f.revealed = append(f.revealed, path) }
func (f *fakeSys) Open(url string) bool {
	f.opened = append(f.opened, url)
	return true
}

type fakeSrc struct {
	groups map[string]collector.AppGroup
	act    map[string]analyzer.Activity
}

func (s *fakeSrc) Group(key string) (collector.AppGroup, bool) {
	g, ok := s.groups[key]
	return g, ok
}
func (s *fakeSrc) Activity(g collector.AppGroup, _ time.Duration) analyzer.Activity {
	if a, ok := s.act[g.Key]; ok {
		return a
	}
	return analyzer.Activity{Window: 30 * time.Second}
}

func app(key, name string, pid int) collector.AppGroup {
	return collector.AppGroup{Key: key, Name: name, MainPID: pid, PIDs: []int{pid},
		BundlePath: "/Applications/" + name + ".app", Footprint: 3 << 30, Known: true}
}

type logRec struct{ kind, target, detail string }

func newManager(t *testing.T) (*Manager, *fakeSys, *fakeSrc, *[]logRec) {
	t.Helper()
	sys := newFakeSys(100, 200, 300)
	src := &fakeSrc{groups: map[string]collector.AppGroup{
		"browser": app("browser", "Browser", 100),
		"editor":  app("editor", "Editor", 200),
		"saver":   app("saver", "Saver", 300),
		"finder":  {Key: "finder", Name: "Finder", MainPID: 400, BundlePath: "/System/Library/CoreServices/Finder.app", Protected: true, Reason: "Part of macOS"},
		"ws":      {Key: "ws", Name: "WindowServer", MainPID: 500, BundlePath: "/Applications/WindowServer.app"},
		"tool":    {Key: "tool", Name: "sometool", MainPID: 600},
		"rooty":   {Key: "rooty", Name: "Rooty", MainPID: 700, BundlePath: "/Applications/Rooty.app", Protected: true, Reason: "Owned by the system or another user"},
	}, act: map[string]analyzer.Activity{}}
	sys.ignoreQuit[300] = true
	var logs []logRec
	m := &Manager{Sys: sys, Src: src, Wait: 300 * time.Millisecond, Home: t.TempDir(),
		Log: func(kind, target string, _ uint64, detail string) { logs = append(logs, logRec{kind, target, detail}) }}
	return m, sys, src, &logs
}

func TestQuitNeedsSafetyCheckToken(t *testing.T) {
	m, sys, _, _ := newManager(t)
	for _, tok := range []string{"", "nope", strings.Repeat("0", 32)} {
		if _, err := m.ConfirmQuit(tok); err == nil {
			t.Fatalf("ConfirmQuit(%q) quit without a safety check", tok)
		}
	}
	if len(sys.terminated) != 0 {
		t.Fatalf("terminate was called without a token: %v", sys.terminated)
	}
}

func TestQuitTokenSingleUseAndExpires(t *testing.T) {
	m, sys, _, logs := newManager(t)
	sc, err := m.PrepareQuit("browser")
	if err != nil {
		t.Fatal(err)
	}
	if len(sys.terminated) != 0 {
		t.Fatal("PrepareQuit must not quit anything")
	}
	r, err := m.ConfirmQuit(sc.Token)
	if err != nil || !r.OK {
		t.Fatalf("confirm: %v %+v", err, r)
	}
	if !strings.HasPrefix(r.Message, "Browser quit. 3.0 GB freed") {
		t.Fatalf("message %q", r.Message)
	}
	if _, err := m.ConfirmQuit(sc.Token); err == nil {
		t.Fatal("a token was accepted twice")
	}
	if len(sys.terminated) != 1 || sys.terminated[0] != 100 {
		t.Fatalf("terminated %v", sys.terminated)
	}
	if len(*logs) != 1 || (*logs)[0].kind != "quit" {
		t.Fatalf("not logged: %+v", *logs)
	}

	// Expiry.
	now := time.Now()
	m.Now = func() time.Time { return now }
	sc, _ = m.PrepareQuit("editor")
	m.Now = func() time.Time { return now.Add(TokenTTL + time.Second) }
	if _, err := m.ConfirmQuit(sc.Token); err == nil {
		t.Fatal("an expired token was accepted")
	}
	if len(sys.terminated) != 1 {
		t.Fatal("expired token quit something")
	}
}

func TestProtectedNeverQuit(t *testing.T) {
	m, sys, src, _ := newManager(t)
	for _, key := range []string{"finder", "ws", "tool", "rooty"} {
		if _, err := m.PrepareQuit(key); err == nil {
			t.Errorf("%s: safety check offered a quit", key)
		}
	}
	// An app that becomes protected (or changes pid) after the check is refused.
	sc, err := m.PrepareQuit("editor")
	if err != nil {
		t.Fatal(err)
	}
	g := src.groups["editor"]
	g.Protected, g.Reason = true, "Headroom itself"
	src.groups["editor"] = g
	if _, err := m.ConfirmQuit(sc.Token); err == nil {
		t.Fatal("quit an app that became protected")
	}
	sc, _ = m.PrepareQuit("browser")
	g = src.groups["browser"]
	g.MainPID = 999
	src.groups["browser"] = g
	if _, err := m.ConfirmQuit(sc.Token); err == nil {
		t.Fatal("quit a different process than the one checked")
	}
	if len(sys.terminated)+len(sys.forced) != 0 {
		t.Fatalf("a protected or changed app was quit: %v %v", sys.terminated, sys.forced)
	}
}

func TestBusyEvidence(t *testing.T) {
	m, _, src, _ := newManager(t)
	src.act["browser"] = analyzer.Activity{CPU: 42, Window: 30 * time.Second}
	src.act["editor"] = analyzer.Activity{DiskRate: 2 << 20, Window: 30 * time.Second}
	src.act["saver"] = analyzer.Activity{CPU: 2, Window: 30 * time.Second}
	sc, _ := m.PrepareQuit("browser")
	if !sc.Busy || len(sc.Evidence) == 0 || !strings.Contains(sc.Evidence[0], "42%") {
		t.Fatalf("cpu busy: %+v", sc)
	}
	sc, _ = m.PrepareQuit("editor")
	if !sc.Busy || !strings.Contains(strings.Join(sc.Evidence, " "), "reading or writing") {
		t.Fatalf("disk busy: %+v", sc)
	}
	sc, _ = m.PrepareQuit("saver")
	if sc.Busy {
		t.Fatalf("idle app shown as busy: %+v", sc)
	}
}

func TestQuitStillOpenIsReported(t *testing.T) {
	m, _, _, _ := newManager(t)
	sc, _ := m.PrepareQuit("saver")
	r, err := m.ConfirmQuit(sc.Token)
	if err != nil || r.OK || !strings.Contains(r.Message, "still open") {
		t.Fatalf("%v %+v", err, r)
	}
}

func TestForceQuitNeedsSettingAndSecondConfirmation(t *testing.T) {
	m, sys, _, _ := newManager(t)
	sc, _ := m.PrepareQuit("browser")
	if sc.CanForceQuit {
		t.Fatal("force quit offered while the setting is off")
	}
	if _, err := m.ConfirmForceQuit(sc.Token, "force"); err == nil {
		t.Fatal("force quit while the setting is off")
	}
	allowed := true
	m.ForceQuitAllowed = func() bool { return allowed }
	sc, _ = m.PrepareQuit("browser")
	if !sc.CanForceQuit {
		t.Fatal("force quit not offered with the setting on")
	}
	if _, err := m.ConfirmForceQuit(sc.Token, ""); err == nil {
		t.Fatal("force quit without the second confirmation")
	}
	if _, err := m.ConfirmForceQuit("bogus", "force"); err == nil {
		t.Fatal("force quit without a safety check")
	}
	if len(sys.forced) != 0 {
		t.Fatalf("forced %v", sys.forced)
	}
	sc, _ = m.PrepareQuit("browser")
	r, err := m.ConfirmForceQuit(sc.Token, "force")
	if err != nil || !r.OK || len(sys.forced) != 1 {
		t.Fatalf("%v %+v %v", err, r, sys.forced)
	}
	if len(sys.terminated) != 0 {
		t.Fatal("force quit should not also call terminate")
	}
}

func TestDryRunTouchesNothing(t *testing.T) {
	m, sys, _, logs := newManager(t)
	m.DryRun = true
	m.ForceQuitAllowed = func() bool { return true }
	sc, _ := m.PrepareQuit("browser")
	if r, err := m.ConfirmQuit(sc.Token); err != nil || !strings.Contains(r.Message, "Demo") {
		t.Fatalf("%v %+v", err, r)
	}
	sc, _ = m.PrepareQuit("editor")
	_, _ = m.ConfirmForceQuit(sc.Token, "force")
	rep := m.DiskReport(context.Background(), true)
	files, _ := m.LargeFiles(context.Background(), rep.Items[0].Path)
	plan, err := m.PlanTrash(context.Background(), []string{files[0].Path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ConfirmTrash(plan.Token); err != nil {
		t.Fatal(err)
	}
	if len(sys.terminated)+len(sys.forced)+len(sys.trashed) != 0 {
		t.Fatalf("demo mode touched the system: %+v", sys)
	}
	if len(*logs) != 3 {
		t.Fatalf("demo actions should still be logged: %+v", *logs)
	}
}

// ---- disk -----------------------------------------------------------------------

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiskScanAndTrashNeverDeletes(t *testing.T) {
	m, sys, _, logs := newManager(t)
	home := m.Home
	write(t, filepath.Join(home, "Downloads", "big.zip"), 3<<20)
	write(t, filepath.Join(home, "Downloads", "small.txt"), 10)
	write(t, filepath.Join(home, "Downloads", "folder", "a.bin"), 1<<20)
	write(t, filepath.Join(home, "Library", "Caches", "com.example.app", "c.db"), 2<<20)
	write(t, filepath.Join(home, "Documents", "secret.txt"), 100)

	rep := m.DiskReport(context.Background(), true)
	if rep.Scanned == "" || len(rep.Items) < 3 {
		t.Fatalf("report %+v", rep)
	}
	var dl DiskItem
	for _, it := range rep.Items {
		if it.Kind == "developer" {
			t.Fatalf("missing developer folders should be hidden: %+v", it)
		}
		if it.Kind == "downloads" {
			dl = it
		}
	}
	if !dl.Known || dl.Size < 4<<20 {
		t.Fatalf("downloads %+v", dl)
	}
	files, err := m.LargeFiles(context.Background(), dl.Path)
	if err != nil || len(files) != 3 || files[0].Name != "big.zip" {
		t.Fatalf("%v %+v", err, files)
	}

	plan, err := m.PlanTrash(context.Background(), []string{files[0].Path, files[1].Path})
	if err != nil || plan.Count != 2 || plan.Total < 4<<20 || len(plan.Names) != 2 {
		t.Fatalf("%v %+v", err, plan)
	}
	if len(sys.trashed) != 0 {
		t.Fatal("PlanTrash moved something")
	}
	r, err := m.ConfirmTrash(plan.Token)
	if err != nil || !r.OK || len(sys.trashed) != 2 {
		t.Fatalf("%v %+v %v", err, r, sys.trashed)
	}
	if _, err := m.ConfirmTrash(plan.Token); err == nil {
		t.Fatal("trash token accepted twice")
	}
	// Nothing was deleted from disk (the fake Trash only records).
	for _, f := range files {
		if _, err := os.Stat(f.Path); err != nil {
			t.Fatalf("%s was removed: %v", f.Name, err)
		}
	}
	if (*logs)[len(*logs)-1].kind != "trash" {
		t.Fatal("trash not logged")
	}
}

func TestTrashOnlyInsideAllowedPlaces(t *testing.T) {
	m, sys, _, _ := newManager(t)
	home := m.Home
	write(t, filepath.Join(home, "Downloads", "ok.bin"), 10)
	write(t, filepath.Join(home, "Documents", "secret.txt"), 10)
	write(t, filepath.Join(home, ".Trash", "old.txt"), 10)
	// A link inside Downloads that points at Documents.
	if err := os.Symlink(filepath.Join(home, "Documents"), filepath.Join(home, "Downloads", "docs")); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		filepath.Join(home, "Documents", "secret.txt"),         // outside
		filepath.Join(home, "Downloads"),                       // the folder itself
		filepath.Join(home, "Downloads", "..", "Documents"),    // not clean
		"Downloads/ok.bin",                                     // relative
		filepath.Join(home, "Downloads", "docs", "secret.txt"), // through a link
		filepath.Join(home, ".Trash", "old.txt"),               // Trash is never reviewed
		"/etc/hosts",                                           // system
		filepath.Join(home, "Downloads", "missing.bin"),        // gone
	}
	for _, p := range bad {
		if _, err := m.PlanTrash(context.Background(), []string{p}); err == nil {
			t.Errorf("PlanTrash accepted %s", p)
		} else if !errors.As(err, new(*Error)) {
			t.Errorf("%s: not a user-facing error: %v", p, err)
		}
	}
	// The link itself may be moved (that moves the link, not Documents).
	if _, err := m.PlanTrash(context.Background(), []string{filepath.Join(home, "Downloads", "docs")}); err != nil {
		t.Errorf("link entry itself: %v", err)
	}
	if len(sys.trashed) != 0 {
		t.Fatal("something was moved")
	}
	if err := m.Reveal(filepath.Join(home, "Documents", "secret.txt")); err == nil {
		t.Error("revealed a path outside the allowed places")
	}
	if err := m.Reveal(filepath.Join(home, "Downloads")); err != nil {
		t.Errorf("reveal Downloads: %v", err)
	}
}

func TestTrashRefusesSwappedFile(t *testing.T) {
	m, sys, _, _ := newManager(t)
	p := filepath.Join(m.Home, "Downloads", "file.bin")
	write(t, p, 10)
	plan, err := m.PlanTrash(context.Background(), []string{p})
	if err != nil {
		t.Fatal(err)
	}
	// Replace the file between review and confirmation.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(m.Home, "Downloads", "other.bin"), 10) // keep the inode from being reused at once
	write(t, p, 20)
	if _, err := m.ConfirmTrash(plan.Token); err == nil {
		t.Fatal("moved a file that changed since review")
	}
	if len(sys.trashed) != 0 {
		t.Fatal("something was moved")
	}
}

func TestPermissionDeniedNote(t *testing.T) {
	m, _, _, _ := newManager(t)
	dir := filepath.Join(m.Home, "Downloads")
	write(t, filepath.Join(dir, "x"), 10)
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	rep := m.DiskReport(context.Background(), true)
	for _, it := range rep.Items {
		if it.Kind == "downloads" && (it.Known || it.Note != NeedsAccess || it.CanReview) {
			t.Fatalf("%+v", it)
		}
	}
}
