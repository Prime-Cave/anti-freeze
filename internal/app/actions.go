package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/actions"
	"github.com/Prime-Cave/anti-freeze/internal/analyzer"
	"github.com/Prime-Cave/anti-freeze/internal/notify"
	"github.com/Prime-Cave/anti-freeze/internal/store"
)

// Notifier delivers notifications (UNUserNotificationCenter in the app).
type Notifier interface {
	notify.Sender
	Authorized() (authorized, determined bool)
	RequestAuthorization() bool
}

// NotifyStatus is shown in Settings and as the permission-denied note.
type NotifyStatus struct {
	Authorized bool `json:"authorized"`
	Determined bool `json:"determined"`
	Available  bool `json:"available"`
}

const (
	notificationSettingsURL = "x-apple.systempreferences:com.apple.preference.notifications"
	loginItemsSettingsURL   = "x-apple.systempreferences:com.apple.LoginItems-Settings.extension"
)

func (s *Service) setupActions() {
	s.act = &actions.Manager{
		Sys: s.cfg.System, Src: s.an, DryRun: s.cfg.Demo,
		ForceQuitAllowed: func() bool { return s.Settings().ShowForceQuit },
		Log: func(kind, target string, freed uint64, detail string) {
			if err := s.cfg.Store.LogAction(store.Action{Time: time.Now(), Kind: kind, Target: target, Freed: freed, Detail: detail}); err != nil {
				s.log.Warn("log action", "err", err)
			}
		},
	}
	s.decider = &notify.Decider{Cooldown: time.Duration(s.settings.CooldownMinutes) * time.Minute}
	s.OnSample(s.maybeNotify)
}

func (s *Service) maybeNotify(st analyzer.Status) {
	set := s.Settings()
	s.decider.SetCooldown(time.Duration(set.CooldownMinutes) * time.Minute)
	n, ok := s.decider.Decide(st, time.Now(), set.Notifications)
	nt := s.getNotifier()
	if !ok || nt == nil {
		return
	}
	if err := nt.Send(n); err != nil {
		s.log.Warn("notify", "err", err)
	}
}

// SetNotifier attaches the notifier once notifications are available.
func (s *Service) SetNotifier(n Notifier) {
	s.mu.Lock()
	s.notifier = n
	s.mu.Unlock()
}

func (s *Service) getNotifier() Notifier {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notifier
}

// HandleNotificationAction is called when the user clicks a notification
// button. "Quit [App]…" only opens the safety check; it never quits.
func (s *Service) HandleNotificationAction(action, appKey string) {
	if action == notify.ActionQuit && appKey != "" {
		if err := s.OpenSafetyCheck(appKey); err == nil {
			return
		}
	}
	s.OpenDashboard("overview")
}

// ---- safety check and quit -------------------------------------------------------

// OpenSafetyCheck shows the safety check window for an app.
func (s *Service) OpenSafetyCheck(appKey string) error {
	g, ok := s.an.Group(appKey)
	if !ok {
		return errInvalid("That app isn't running any more.")
	}
	if g.Protected {
		return errInvalid(g.Name + " is protected, so Anti-Freeze won't quit it.")
	}
	s.mu.Lock()
	s.pendingSafety = appKey
	s.mu.Unlock()
	if s.ui != nil {
		s.ui.HideDropdown()
		s.ui.ShowSafety(appKey)
	}
	return nil
}

// PendingSafetyCheck returns the app the safety window should check.
func (s *Service) PendingSafetyCheck() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingSafety
}

// CloseSafetyCheck hides the safety window ("Keep [App] open" or Escape).
func (s *Service) CloseSafetyCheck() {
	s.mu.Lock()
	s.pendingSafety = ""
	s.mu.Unlock()
	if s.ui != nil {
		s.ui.HideSafety()
	}
}

// ResizeSafety fits the safety window to its content.
func (s *Service) ResizeSafety(height int) {
	if s.ui != nil && height >= 120 && height <= 700 {
		s.ui.ResizeSafety(height)
	}
}

// PrepareQuit runs the busy check and returns a one-time token.
func (s *Service) PrepareQuit(appKey string) (actions.SafetyCheck, error) {
	return s.act.PrepareQuit(appKey)
}

// ConfirmQuit quits the checked app gracefully.
func (s *Service) ConfirmQuit(token string) (actions.Result, error) {
	r, err := s.act.ConfirmQuit(token)
	s.afterAction(r, err)
	return r, err
}

// ConfirmForceQuit force-quits after the second confirmation.
func (s *Service) ConfirmForceQuit(token, confirmation string) (actions.Result, error) {
	r, err := s.act.ConfirmForceQuit(token, confirmation)
	s.afterAction(r, err)
	return r, err
}

func (s *Service) afterAction(r actions.Result, err error) {
	if err != nil || s.ui == nil {
		return
	}
	s.ui.Emit("toast", r.Message)
	s.ui.Emit("history-changed", nil)
	s.forceProcesses.Store(true)
	s.Wake()
}

// ---- disk -------------------------------------------------------------------------

// DiskReport returns the last disk scan, or scans now.
func (s *Service) DiskReport(scan bool) actions.DiskReport {
	r := s.act.DiskReport(context.Background(), scan)
	s.an.SetDiskHint(diskHint(r))
	return r
}

func diskHint(r actions.DiskReport) string {
	var best actions.DiskItem
	for _, it := range r.Items {
		if it.Known && it.Kind != "trash" && it.Size > best.Size {
			best = it
		}
	}
	if best.Size < 1<<30 {
		return ""
	}
	return fmt.Sprintf("%s holds %s.", best.Name, analyzer.FormatBytes(best.Size))
}

// LargeFiles lists the biggest items in one place from the disk report.
func (s *Service) LargeFiles(placePath string) ([]actions.LargeFile, error) {
	return s.act.LargeFiles(context.Background(), placePath)
}

// PlanTrash returns what a Move to Trash would move, with a one-time token.
func (s *Service) PlanTrash(paths []string) (actions.TrashPlan, error) {
	return s.act.PlanTrash(context.Background(), paths)
}

// ConfirmTrash moves the planned items to the Trash.
func (s *Service) ConfirmTrash(token string) (actions.Result, error) {
	r, err := s.act.ConfirmTrash(token)
	s.afterAction(r, err)
	return r, err
}

// RevealInFinder shows an item from the disk report in Finder.
func (s *Service) RevealInFinder(path string) error { return s.act.Reveal(path) }

// OpenTrash opens the Trash in Finder.
func (s *Service) OpenTrash() { s.act.OpenTrash() }

// ---- app icons --------------------------------------------------------------------

// AppIcon returns a PNG data URL for a running app's icon. Only bundles of
// apps Anti-Freeze currently sees are accepted.
func (s *Service) AppIcon(bundlePath string) string {
	if s.cfg.AppIcon == nil || !strings.HasSuffix(bundlePath, ".app") {
		return ""
	}
	s.mu.Lock()
	if v, ok := s.iconCache[bundlePath]; ok {
		s.mu.Unlock()
		return v
	}
	s.mu.Unlock()
	known := false
	for _, a := range s.an.Apps() {
		if a.BundlePath == bundlePath {
			known = true
			break
		}
	}
	if !known {
		return ""
	}
	png, ok := s.cfg.AppIcon(bundlePath)
	v := ""
	if ok {
		v = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	}
	s.mu.Lock()
	if len(s.iconCache) > 200 {
		s.iconCache = map[string]string{}
	}
	s.iconCache[bundlePath] = v
	s.mu.Unlock()
	return v
}

// ---- history export and delete ----------------------------------------------------

// ExportHistory asks where to save and writes the history as CSV. Returns
// the saved path, or "" if the user cancelled.
func (s *Service) ExportHistory() (string, error) {
	if s.ui == nil {
		return "", errInvalid("Open the dashboard to export history.")
	}
	name := "Anti-Freeze history " + time.Now().Format("2006-01-02") + ".csv"
	path, err := s.ui.SaveFileDialog(name)
	if err != nil || path == "" {
		return "", nil // cancelled
	}
	if !strings.HasSuffix(strings.ToLower(path), ".csv") {
		path += ".csv"
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", errInvalid("Couldn't save there. Choose another folder.")
	}
	n, err := s.cfg.Store.ExportCSV(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", errInvalid("Couldn't write the file. Try again, or choose another folder.")
	}
	_ = s.cfg.Store.LogAction(store.Action{Time: time.Now(), Kind: "export", Target: filepath.Base(path),
		Detail: fmt.Sprintf("Exported %d rows", n)})
	return path, nil
}

// DeleteHistory really deletes the database (samples, episodes, actions)
// and starts a fresh one. Settings are kept. confirmation must be "delete".
func (s *Service) DeleteHistory(confirmation string) error {
	if confirmation != "delete" {
		return errInvalid("Confirm to delete all history.")
	}
	if err := s.cfg.Store.Wipe(context.Background()); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return errInvalid("Couldn't delete the history file. Quit Anti-Freeze and remove ~/Library/Application Support/Anti-Freeze.")
	}
	if s.ui != nil {
		s.ui.Emit("history-changed", nil)
	}
	return nil
}

// ---- notification permission ------------------------------------------------------

// NotificationStatus reports whether macOS lets Anti-Freeze notify.
func (s *Service) NotificationStatus() NotifyStatus {
	nt := s.getNotifier()
	if nt == nil {
		return NotifyStatus{}
	}
	a, d := nt.Authorized()
	return NotifyStatus{Authorized: a, Determined: d, Available: true}
}

// RequestNotifications asks macOS for permission (shows the system prompt
// the first time only).
func (s *Service) RequestNotifications() NotifyStatus {
	nt := s.getNotifier()
	if nt == nil {
		return NotifyStatus{}
	}
	nt.RequestAuthorization()
	return s.NotificationStatus()
}

// LoginItem is Anti-Freeze's login item (SMAppService.mainApp in the app).
type LoginItem interface {
	// Status reports whether the item is on, waiting for approval in
	// System Settings, and whether login items are available at all
	// (only from an .app bundle).
	Status() (on, needsApproval, available bool)
	Set(on bool) error
	// Installed reports whether Anti-Freeze runs from an Applications folder.
	// Only an installed copy turns itself on at first launch.
	Installed() bool
}

// SetupLoginItemOnce turns start at login on the first time Anti-Freeze runs
// from an Applications folder (the default the brief asks for). A copy
// opened from the DMG or a build folder never registers itself. After that, the user's
// choice in Settings or in System Settings is left alone.
func (s *Service) SetupLoginItemOnce() {
	li := s.cfg.LoginItem
	if li == nil || s.cfg.Demo || !li.Installed() {
		return
	}
	if _, _, ok := li.Status(); !ok {
		return
	}
	if s.cfg.Store.Settings()["loginSetUp"] == "true" {
		return
	}
	_ = s.cfg.Store.SetSetting("loginSetUp", "true")
	// The stored preference (default on), not Settings(), which reports the
	// live login item state and so reads "off" before the first registration.
	s.mu.Lock()
	want := s.settings.StartAtLogin
	s.mu.Unlock()
	if want {
		if err := li.Set(true); err != nil {
			s.log.Warn("login item", "err", err)
		}
	}
}

// AskNotificationsOnce shows the macOS permission prompt the first time
// Anti-Freeze runs (never again: after that, Settings links to System Settings).
func (s *Service) AskNotificationsOnce() {
	if s.cfg.Demo || s.getNotifier() == nil || !s.Settings().Notifications {
		return
	}
	if s.cfg.Store.Settings()["notifyAsked"] == "true" {
		return
	}
	_ = s.cfg.Store.SetSetting("notifyAsked", "true")
	s.RequestNotifications()
}

// QuitAntiFreeze quits Anti-Freeze itself (Settings › Quit Anti-Freeze).
func (s *Service) QuitAntiFreeze() {
	if s.ui != nil {
		s.ui.Quit()
	}
}

// OpenLoginItemsSettings opens System Settings › General › Login Items.
func (s *Service) OpenLoginItemsSettings() {
	if s.cfg.System != nil {
		s.cfg.System.Open(loginItemsSettingsURL)
	}
}

// OpenNotificationSettings opens System Settings › Notifications.
func (s *Service) OpenNotificationSettings() {
	if s.cfg.System != nil {
		s.cfg.System.Open(notificationSettingsURL)
	}
}
