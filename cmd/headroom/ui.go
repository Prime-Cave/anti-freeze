package main

import (
	"math"
	"sync"
	"time"

	"github.com/Prime-Cave/headroom/internal/analyzer"
	"github.com/Prime-Cave/headroom/internal/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// wailsUI implements app.UI on top of Wails windows and the tray.
type wailsUI struct {
	app       *application.App
	tray      *application.SystemTray
	dropdown  *application.WebviewWindow
	dashboard *application.WebviewWindow
	safety    *application.WebviewWindow

	mu          sync.Mutex
	lastLevel   tray.Level
	lastFill    int
	lastSimple  bool
	lastApplied time.Time
}

func (u *wailsUI) Emit(event string, data any) { u.app.Event.Emit(event, data) }

func (u *wailsUI) PanelsVisible() bool {
	return u.dropdown.IsVisible() || u.dashboard.IsVisible() || u.safety.IsVisible()
}

var stateLabels = map[analyzer.State]string{
	analyzer.OK:       "Headroom: Plenty of room",
	analyzer.Tight:    "Headroom: Getting tight",
	analyzer.Critical: "Headroom: Slowdown likely",
}

// SetTrayIcon redraws the menu bar icon when the state or fill changes, and at
// least every 30 s so it follows a light/dark menu bar switch.
func (u *wailsUI) SetTrayIcon(state analyzer.State, memFill float64, simple bool) {
	level := tray.Level(state)
	fill := int(math.Round(memFill * 20)) // 5% steps
	u.mu.Lock()
	changed := level != u.lastLevel || fill != u.lastFill || simple != u.lastSimple || time.Since(u.lastApplied) > 30*time.Second
	if changed {
		u.lastLevel, u.lastFill, u.lastSimple, u.lastApplied = level, fill, simple, time.Now()
	}
	u.mu.Unlock()
	if !changed {
		return
	}
	f := float64(fill) / 20
	if simple && level == tray.LevelOK {
		f = 0
	}
	icon := tray.Render(level, f)
	label := stateLabels[state]
	application.InvokeSync(func() { tray.Apply(icon, label) })
}

func (u *wailsUI) ShowDashboard(section string) {
	u.dashboard.Show()
	u.dashboard.Focus()
	application.InvokeAsync(u.app.Show) // bring the (accessory) app forward; AppKit needs the main thread
	u.dashboard.EmitEvent("navigate", section)
}

func (u *wailsUI) HideDropdown() { u.dropdown.Hide() }

func (u *wailsUI) ResizeDropdown(height int) {
	w, h := u.dropdown.Size()
	if h == height && w == dropdownWidth {
		return
	}
	u.dropdown.SetSize(dropdownWidth, height)
	if u.dropdown.IsVisible() {
		_ = u.tray.PositionWindow(u.dropdown, 6)
	}
}

// ShowSafety opens the safety check window centred on screen, in front.
func (u *wailsUI) ShowSafety(appKey string) {
	u.safety.Center()
	u.safety.Show()
	application.InvokeAsync(u.app.Show)
	u.safety.Focus()
	u.safety.EmitEvent("safety-open", appKey)
}

func (u *wailsUI) HideSafety() { u.safety.Hide() }

func (u *wailsUI) ResizeSafety(height int) {
	if _, h := u.safety.Size(); h != height {
		u.safety.SetSize(safetyWidth, height)
		u.safety.Center()
	}
}

// SaveFileDialog shows the macOS save panel, attached to the dashboard.
func (u *wailsUI) SaveFileDialog(defaultName string) (string, error) {
	return u.app.Dialog.SaveFile().
		SetFilename(defaultName).
		AddFilter("CSV", "*.csv").
		CanCreateDirectories(true).
		AttachToWindow(u.dashboard).
		PromptForSingleSelection()
}
