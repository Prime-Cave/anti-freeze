package app

import (
	"github.com/Prime-Cave/headroom/internal/actions"
	"github.com/Prime-Cave/headroom/internal/analyzer"
	"github.com/Prime-Cave/headroom/internal/store"
)

// API is the only type bound to the frontend. Every exported method here is
// callable from JavaScript, so it exposes exactly what the UI needs and no
// more (the sampling loop and internals stay on Service).
type API struct{ s *Service }

// NewAPI wraps a service for binding.
func NewAPI(s *Service) *API { return &API{s: s} }

func (a *API) Status() analyzer.Status                    { return a.s.Status() }
func (a *API) Machine() MachineInfo                       { return a.s.Machine() }
func (a *API) Apps() []analyzer.AppRow                    { return a.s.Apps() }
func (a *API) Timeline(rangeKey string) (Timeline, error) { return a.s.Timeline(rangeKey) }
func (a *API) History() ([]store.Action, error)           { return a.s.History() }
func (a *API) Settings() Settings                         { return a.s.Settings() }
func (a *API) SetSetting(k, v string) (Settings, error)   { return a.s.SetSetting(k, v) }
func (a *API) OpenDashboard(section string)               { a.s.OpenDashboard(section) }
func (a *API) HideDropdown()                              { a.s.HideDropdown() }
func (a *API) ResizeDropdown(height int)                  { a.s.ResizeDropdown(height) }
func (a *API) PanelOpened()                               { a.s.Wake() }

// Safety check and actions (section 5). Quitting needs a token from
// PrepareQuit; force quit also needs the setting on and a second
// confirmation; files are only ever moved to the Trash.
func (a *API) OpenSafetyCheck(appKey string) error { return a.s.OpenSafetyCheck(appKey) }
func (a *API) PendingSafetyCheck() string          { return a.s.PendingSafetyCheck() }
func (a *API) CloseSafetyCheck()                   { a.s.CloseSafetyCheck() }
func (a *API) ResizeSafety(height int)             { a.s.ResizeSafety(height) }
func (a *API) PrepareQuit(appKey string) (actions.SafetyCheck, error) {
	return a.s.PrepareQuit(appKey)
}
func (a *API) ConfirmQuit(token string) (actions.Result, error) { return a.s.ConfirmQuit(token) }
func (a *API) ConfirmForceQuit(token, confirmation string) (actions.Result, error) {
	return a.s.ConfirmForceQuit(token, confirmation)
}
func (a *API) DiskReport(scan bool) actions.DiskReport { return a.s.DiskReport(scan) }
func (a *API) LargeFiles(placePath string) ([]actions.LargeFile, error) {
	return a.s.LargeFiles(placePath)
}
func (a *API) PlanTrash(paths []string) (actions.TrashPlan, error) { return a.s.PlanTrash(paths) }
func (a *API) ConfirmTrash(token string) (actions.Result, error)   { return a.s.ConfirmTrash(token) }
func (a *API) RevealInFinder(path string) error                    { return a.s.RevealInFinder(path) }
func (a *API) OpenTrash()                                          { a.s.OpenTrash() }
func (a *API) AppIcon(bundlePath string) string                    { return a.s.AppIcon(bundlePath) }

// History export and delete.
func (a *API) ExportHistory() (string, error)          { return a.s.ExportHistory() }
func (a *API) DeleteHistory(confirmation string) error { return a.s.DeleteHistory(confirmation) }

// Notification permission.
func (a *API) NotificationStatus() NotifyStatus   { return a.s.NotificationStatus() }
func (a *API) RequestNotifications() NotifyStatus { return a.s.RequestNotifications() }
func (a *API) OpenNotificationSettings()          { a.s.OpenNotificationSettings() }

// SelftestReport receives the rendered page text during `--selftest` runs and
// prints it. It does nothing in normal use.
func (a *API) SelftestReport(page, text string) {
	if a.s.cfg.Selftest != nil {
		a.s.cfg.Selftest(page, text)
	}
}
