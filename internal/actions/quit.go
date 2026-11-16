// Package actions does the few things Headroom changes on the Mac, with the
// safety rules from section 5 built in:
//
//   - An app is never quit on one click. ConfirmQuit only accepts a token from
//     PrepareQuit (the safety check), once, within two minutes.
//   - Quitting is graceful (NSRunningApplication.terminate), so the app can
//     ask to save. Force quit is a separate, hidden-by-default option that
//     needs a second confirmation.
//   - Protected processes are refused here as well as hidden in the UI.
//   - Files are never deleted: only revealed in Finder or moved to the Trash,
//     after a confirmation that names the items and total size.
//   - Every action is logged.
package actions

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Prime-Cave/headroom/internal/analyzer"
	"github.com/Prime-Cave/headroom/internal/collector"
)

// System is the macOS side (internal/macos in the app, a fake in tests).
type System interface {
	Terminate(pid int) bool
	ForceTerminate(pid int) bool
	IsRunning(pid int) bool
	Trash(path string) error
	Reveal(path string)
	Open(url string) bool
}

// Source provides app groups and their recent activity (the analyzer).
type Source interface {
	Group(key string) (collector.AppGroup, bool)
	Activity(g collector.AppGroup, over time.Duration) analyzer.Activity
}

// Logger records actions (the store).
type Logger func(kind, target string, freed uint64, detail string)

// Busy thresholds (section 5.2).
const (
	BusyCPU      = 10.0      // % of one core over the last 30 s
	BusyDiskRate = 512 << 10 // bytes/s read + written
	BusyWindow   = 30 * time.Second
	TokenTTL     = 2 * time.Minute
)

// SafetyCheck is what the "Quit [App]?" dialog shows.
type SafetyCheck struct {
	Token        string   `json:"token"`
	AppName      string   `json:"appName"`
	ShortName    string   `json:"shortName"`
	Busy         bool     `json:"busy"`
	CPU          float64  `json:"cpu"`
	DiskRate     float64  `json:"diskRate"`
	WindowSec    float64  `json:"windowSec"`
	Children     int      `json:"children"`
	Frees        uint64   `json:"frees"`
	Evidence     []string `json:"evidence"`
	CanForceQuit bool     `json:"canForceQuit"`
}

// Result is what a confirmed action reports back (the toast).
type Result struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Freed   uint64 `json:"freed"`
}

// Error is a user-facing error: it says what happened and what to do.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

type quitTicket struct {
	key     string
	name    string
	short   string
	pid     int
	frees   uint64
	expires time.Time
}

// Manager runs actions. Safe for concurrent use.
type Manager struct {
	Sys    System
	Src    Source
	Log    Logger
	DryRun bool             // demo mode: report success, touch nothing
	Now    func() time.Time // for tests
	// ForceQuitAllowed reports whether the user has turned on the hidden
	// force-quit option in Settings.
	ForceQuitAllowed func() bool
	// Wait is how long ConfirmQuit waits to see the app exit.
	Wait time.Duration
	// Home is the user's home folder (a temp folder in tests).
	Home string

	diskState

	mu     sync.Mutex
	quits  map[string]quitTicket
	trashs map[string]trashTicket
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on macOS
	}
	return hex.EncodeToString(b)
}

// PrepareQuit runs the safety check for an app group and returns a one-time
// token that ConfirmQuit requires.
func (m *Manager) PrepareQuit(key string) (SafetyCheck, error) {
	g, ok := m.Src.Group(key)
	if !ok {
		return SafetyCheck{}, &Error{"That app isn't running any more."}
	}
	if err := quittable(g); err != nil {
		return SafetyCheck{}, err
	}
	act := m.Src.Activity(g, BusyWindow)
	short := analyzer.ShortName(g.Name)
	sc := SafetyCheck{
		AppName: g.Name, ShortName: short, CPU: act.CPU, DiskRate: act.DiskRate,
		WindowSec: act.Window.Seconds(), Children: act.Children, Frees: g.Footprint,
	}
	if act.CPU >= BusyCPU {
		sc.Busy = true
		sc.Evidence = append(sc.Evidence, fmt.Sprintf("Processor use is %.0f%% over the last %s.", act.CPU, windowWords(act.Window)))
	}
	if act.DiskRate >= BusyDiskRate {
		sc.Busy = true
		sc.Evidence = append(sc.Evidence, "It is reading or writing files right now.")
	}
	if act.Children > 0 && (act.CPU >= BusyCPU/2 || act.DiskRate >= BusyDiskRate/2) {
		sc.Busy = true
		sc.Evidence = append(sc.Evidence, fmt.Sprintf("%d %s started from it %s working.", act.Children,
			plural(act.Children, "process", "processes"), plural(act.Children, "is", "are")))
	}
	if act.Window < 10*time.Second {
		sc.Evidence = append(sc.Evidence, "Headroom has only just started watching it, so this is a short reading.")
	}
	sc.Token = newToken()
	sc.CanForceQuit = m.ForceQuitAllowed != nil && m.ForceQuitAllowed()
	m.mu.Lock()
	if m.quits == nil {
		m.quits = map[string]quitTicket{}
	}
	m.quits[sc.Token] = quitTicket{key: key, name: g.Name, short: short, pid: g.MainPID, frees: g.Footprint, expires: m.now().Add(TokenTTL)}
	m.mu.Unlock()
	return sc, nil
}

func (m *Manager) takeQuit(token string) (quitTicket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.quits[token]
	delete(m.quits, token) // one use only
	if !ok {
		return t, &Error{"Open the safety check again before quitting."}
	}
	if m.now().After(t.expires) {
		return t, &Error{"The safety check expired. Open it again to see if the app is busy now."}
	}
	return t, nil
}

// recheck makes sure the app hasn't changed (or become protected) since the
// safety check.
func (m *Manager) recheck(t quitTicket) error {
	g, ok := m.Src.Group(t.key)
	if !ok || g.MainPID != t.pid {
		return &Error{t.short + " isn't running any more."}
	}
	return quittable(g)
}

// ConfirmQuit asks the app to quit normally. It requires a token from
// PrepareQuit.
func (m *Manager) ConfirmQuit(token string) (Result, error) {
	t, err := m.takeQuit(token)
	if err != nil {
		return Result{}, err
	}
	if err := m.recheck(t); err != nil {
		return Result{}, err
	}
	if m.DryRun {
		m.log("quit", t.name, t.frees, "Demo mode: nothing was quit")
		return Result{OK: true, Message: fmt.Sprintf("%s quit. %s freed. (Demo: nothing was quit.)", t.short, analyzer.FormatBytes(t.frees)), Freed: t.frees}, nil
	}
	if !m.Sys.Terminate(t.pid) {
		return Result{}, &Error{t.short + " didn't accept the request to quit. Try quitting it from its own menu."}
	}
	if m.waitExit(t.pid) {
		m.log("quit", t.name, t.frees, "Asked to quit normally")
		return Result{OK: true, Message: fmt.Sprintf("%s quit. %s freed.", t.short, analyzer.FormatBytes(t.frees)), Freed: t.frees}, nil
	}
	m.log("quit", t.name, 0, "Asked to quit normally; still open (may be asking to save)")
	return Result{OK: false, Message: t.short + " is still open. It may be asking you to save first."}, nil
}

// ConfirmForceQuit force-quits. It needs the force-quit setting on, a token
// from PrepareQuit, and a second confirmation ("force") from the dialog.
// Notifications never offer it.
func (m *Manager) ConfirmForceQuit(token, confirmation string) (Result, error) {
	if m.ForceQuitAllowed == nil || !m.ForceQuitAllowed() {
		return Result{}, &Error{"Force quit is turned off. Turn it on in Settings first."}
	}
	if confirmation != "force" {
		return Result{}, &Error{"Confirm force quit a second time to continue."}
	}
	t, err := m.takeQuit(token)
	if err != nil {
		return Result{}, err
	}
	if err := m.recheck(t); err != nil {
		return Result{}, err
	}
	if m.DryRun {
		m.log("force-quit", t.name, t.frees, "Demo mode: nothing was quit")
		return Result{OK: true, Message: fmt.Sprintf("%s force quit. %s freed. (Demo: nothing was quit.)", t.short, analyzer.FormatBytes(t.frees)), Freed: t.frees}, nil
	}
	if !m.Sys.ForceTerminate(t.pid) {
		return Result{}, &Error{t.short + " couldn't be force quit. Use Force Quit in the Apple menu."}
	}
	m.waitExit(t.pid)
	m.log("force-quit", t.name, t.frees, "Force quit after a second confirmation")
	return Result{OK: true, Message: fmt.Sprintf("%s force quit. %s freed.", t.short, analyzer.FormatBytes(t.frees)), Freed: t.frees}, nil
}

func (m *Manager) waitExit(pid int) bool {
	wait := m.Wait
	if wait == 0 {
		wait = 5 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		if !m.Sys.IsRunning(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (m *Manager) log(kind, target string, freed uint64, detail string) {
	if m.Log != nil {
		m.Log(kind, target, freed, detail)
	}
}

// protectedNames mirrors the brief's list (section 5.5). The collector marks
// these too; this is the second, independent check at the point of action.
var protectedNames = map[string]bool{
	"Headroom": true, "WindowServer": true, "kernel_task": true, "loginwindow": true,
	"Finder": true, "Dock": true, "SystemUIServer": true, "launchd": true,
}

func quittable(g collector.AppGroup) error {
	switch {
	case g.Protected:
		return &Error{fmt.Sprintf("%s is protected (%s), so Headroom won't quit it.", g.Name, strings.ToLower(g.Reason))}
	case protectedNames[g.Name]:
		return &Error{g.Name + " is part of macOS, so Headroom won't quit it."}
	case g.BundlePath == "" || g.MainPID <= 1:
		return &Error{g.Name + " has no app bundle, so Headroom won't quit it."}
	}
	return nil
}

func windowWords(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s > int(BusyWindow.Seconds()) {
		s = int(BusyWindow.Seconds()) // sampling jitter: "31 seconds" reads oddly
	}
	if s <= 0 {
		return "moment"
	}
	return fmt.Sprintf("%d seconds", s)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
