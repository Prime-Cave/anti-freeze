package main

import (
	"math"
	"sync"
	"time"

	"github.com/Prime-Cave/anti-freeze/internal/analyzer"
	"github.com/Prime-Cave/anti-freeze/internal/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// wailsUI implements app.UI on top of Wails windows and the tray.
type wailsUI struct {
	app      *application.App
	tray     *application.SystemTray
	dropdown *application.WebviewWindow

	// Created on demand and destroyed on close (which also ends their web
	// content processes), so a closed dashboard costs nothing.
	newDashboard func(section string) *application.WebviewWindow
	newSafety    func() *application.WebviewWindow

	wmu       sync.Mutex
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
	if u.dropdown.IsVisible() {
		return true
	}
	u.wmu.Lock()
	defer u.wmu.Unlock()
	return u.dashboard != nil || u.safety != nil
}

// window returns the dashboard or safety window if it is open.
func (u *wailsUI) window(name string) *application.WebviewWindow {
	u.wmu.Lock()
	defer u.wmu.Unlock()
	if name == "safety" {
		return u.safety
	}
	return u.dashboard
}

// forget drops a window that is closing, so it is created afresh next time.
func (u *wailsUI) forget(w *application.WebviewWindow) {
	u.wmu.Lock()
	defer u.wmu.Unlock()
	if u.dashboard == w {
		u.dashboard = nil
	}
	if u.safety == w {
		u.safety = nil
	}
}

var stateLabels = map[analyzer.State]string{
	analyzer.OK:       "Anti-Freeze: Plenty of room",
	analyzer.Tight:    "Anti-Freeze: Getting tight",
	analyzer.Critical: "Anti-Freeze: Slowdown likely",
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
	u.wmu.Lock()
	w, existed := u.dashboard, u.dashboard != nil
	if !existed {
		w = u.newDashboard(section)
		u.dashboard = w
	}
	u.wmu.Unlock()
	w.Show()
	w.Focus()
	application.InvokeAsync(u.app.Show) // bring the (accessory) app forward; AppKit needs the main thread
	if existed {
		w.EmitEvent("navigate", section)
	}
}

func (u *wailsUI) HideDropdown() { u.dropdown.Hide() }

func (u *wailsUI) Quit() { u.app.Quit() }

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
// A new window asks for the pending app itself when its page loads.
func (u *wailsUI) ShowSafety(appKey string) {
	u.wmu.Lock()
	w, existed := u.safety, u.safety != nil
	if !existed {
		w = u.newSafety()
		u.safety = w
	}
	u.wmu.Unlock()
	w.Center()
	w.Show()
	application.InvokeAsync(u.app.Show)
	w.Focus()
	if existed {
		w.EmitEvent("safety-open", appKey)
	}
}

func (u *wailsUI) HideSafety() {
	if w := u.window("safety"); w != nil {
		w.Close()
	}
}

func (u *wailsUI) ResizeSafety(height int) {
	w := u.window("safety")
	if w == nil {
		return
	}
	if _, h := w.Size(); h != height {
		w.SetSize(safetyWidth, height)
		w.Center()
	}
}

// SaveFileDialog shows the macOS save panel, attached to the dashboard.
func (u *wailsUI) SaveFileDialog(defaultName string) (string, error) {
	d := u.app.Dialog.SaveFile().
		SetFilename(defaultName).
		AddFilter("CSV", "*.csv").
		CanCreateDirectories(true)
	if w := u.window("dashboard"); w != nil {
		d = d.AttachToWindow(w)
	}
	return d.PromptForSingleSelection()
}
