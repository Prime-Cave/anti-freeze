package app

import (
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

// SelftestReport receives the rendered page text during `--selftest` runs and
// prints it. It does nothing in normal use.
func (a *API) SelftestReport(page, text string) {
	if a.s.cfg.Selftest != nil {
		a.s.cfg.Selftest(page, text)
	}
}
