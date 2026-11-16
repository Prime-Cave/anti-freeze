// Command headroom is a menu bar app that warns you before your Mac slows down.
//
//	headroom             run the menu bar app
//	headroom sample      print one JSON snapshot (compare with Activity Monitor)
//	headroom --selftest  start, report the status item to stdout, quit
//	headroom --version   print the version
//	headroom --login-item status|on|off   show or change start at login (bundle only)
//
// HEADROOM_DEMO=ok|tight|critical|recover replays a synthetic slowdown
// instead of reading this Mac, with a throwaway database. Actions in demo
// mode never touch real apps or files.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/Prime-Cave/headroom/frontend"
	"github.com/Prime-Cave/headroom/internal/app"
	"github.com/Prime-Cave/headroom/internal/collector"
	"github.com/Prime-Cave/headroom/internal/machine"
	"github.com/Prime-Cave/headroom/internal/macos"
	"github.com/Prime-Cave/headroom/internal/store"
	"github.com/Prime-Cave/headroom/internal/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var version = "dev"

const (
	dropdownWidth = 360
	safetyWidth   = 420
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("Headroom", version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "sample" {
		os.Exit(runSample())
	}
	if len(os.Args) > 2 && os.Args[1] == "--login-item" {
		os.Exit(runLoginItem(os.Args[2]))
	}
	selftest := len(os.Args) > 1 && os.Args[1] == "--selftest"
	demo := os.Getenv("HEADROOM_DEMO")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Storage. Demo mode never writes to the real history.
	dbPath, err := store.DefaultPath()
	if err != nil {
		log.Fatal(err)
	}
	if demo != "" {
		dir, err := os.MkdirTemp("", "headroom-demo-*")
		if err != nil {
			log.Fatal(err)
		}
		defer func() { _ = os.RemoveAll(dir) }()
		dbPath = filepath.Join(dir, "headroom.db")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("Headroom can't open its history at %s: %v", dbPath, err)
	}
	defer func() { _ = st.Close() }()

	// Collector and machine profile.
	var col collector.Collector
	var profile machine.Profile
	if demo != "" {
		col = collector.NewDemo(demo)
		seedDemoHistory(st, 8<<30, time.Now())
		profile = machine.Profile{Model: "Demo", ModelName: "MacBook Air", Chip: "Apple M1", Arch: "arm64",
			OSVersion: "14.6", MemoryBytes: 8 << 30, DiskBytes: 245_107_195_904, Cores: 8,
			FirstSeen: time.Now().Add(-7 * 24 * time.Hour)}
	} else {
		native := collector.NewNative()
		col = native
		profile = loadProfile(st, native)
	}

	cfg := app.Config{Collector: col, Store: st, Profile: profile, Demo: demo != "", Logger: logger,
		System: macos.System{}, AppIcon: macos.AppIconPNG}
	if selftest {
		cfg.Selftest = func(page, text string) { fmt.Printf("--- %s ---\n%s\n", page, text) }
	} else if demo == "" && macos.InBundle() {
		cfg.LoginItem = loginItem{} // never touched in demo mode or selftests
	}
	svc := app.New(cfg)

	wapp := application.New(application.Options{
		Name:        "Headroom",
		Description: "Warns you before your Mac slows down",
		Services:    []application.Service{application.NewService(app.NewAPI(svc))},
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(frontend.Assets())},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "io.github.prime-cave.headroom"},
	})

	tr := wapp.SystemTray.New()
	initial := tray.Render(tray.LevelOK, 0)
	tr.SetTemplateIcon(initial.Light)

	dropdown := wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "dropdown", URL: "/", Width: dropdownWidth, Height: 520,
		Frameless: true, AlwaysOnTop: true, Hidden: true, DisableResize: true, HideOnFocusLost: true,
		BackgroundType:             application.BackgroundTypeTransparent,
		Mac:                        application.MacWindow{Backdrop: application.MacBackdropTransparent},
		DefaultContextMenuDisabled: true,
	})
	dropdown.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		dropdown.Hide()
		e.Cancel()
	})
	tr.AttachWindow(dropdown).WindowOffset(6)

	// The dashboard and the safety check are created when first needed and
	// destroyed when closed, so no hidden web views run in the background.
	ui := &wailsUI{app: wapp, tray: tr, dropdown: dropdown}
	ui.newDashboard = func(section string) *application.WebviewWindow {
		w := wapp.Window.NewWithOptions(application.WebviewWindowOptions{
			Name: "dashboard", Title: "Headroom", URL: "/dashboard.html#" + section,
			Width: 1120, Height: 760, MinWidth: 900, MinHeight: 600,
			BackgroundColour:           application.NewRGB(0xF3, 0xF5, 0xF8),
			DefaultContextMenuDisabled: true,
		})
		w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) { ui.forget(w) })
		return w
	}
	// The safety check (section 7.5): its own small window so it can appear
	// from a notification without the dropdown. 420 px wide, radius 14.
	ui.newSafety = func() *application.WebviewWindow {
		w := wapp.Window.NewWithOptions(application.WebviewWindowOptions{
			Name: "safety", Title: "Quit app", URL: "/safety.html", Width: safetyWidth, Height: 320,
			Frameless: true, AlwaysOnTop: true, DisableResize: true, InitialPosition: application.WindowCentered,
			BackgroundType:             application.BackgroundTypeTransparent,
			Mac:                        application.MacWindow{Backdrop: application.MacBackdropTransparent},
			DefaultContextMenuDisabled: true,
		})
		w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) { ui.forget(w) })
		return w
	}
	// Right-click menu on the menu bar icon (left click opens the dropdown).
	menu := wapp.NewMenu()
	menu.Add("Open dashboard").OnClick(func(*application.Context) { svc.OpenDashboard("overview") })
	menu.Add("Settings…").OnClick(func(*application.Context) { svc.OpenDashboard("settings") })
	menu.AddSeparator()
	menu.Add("Quit Headroom").OnClick(func(*application.Context) { wapp.Quit() })
	tr.SetMenu(menu)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		// Notifications need the .app bundle; without it they are simply off.
		if n := newNotifier(svc.HandleNotificationAction); n != nil {
			svc.SetNotifier(n)
			if !selftest {
				go svc.AskNotificationsOnce()
			}
		}
		go svc.SetupLoginItemOnce()
		go svc.Run(ctx, ui)
		if selftest {
			go func() {
				time.Sleep(2 * time.Second)
				application.InvokeSync(func() { fmt.Println(tray.Inspect()) })
				fmt.Printf("notifications: %+v\n", svc.NotificationStatus())
				// Render both pages and have them report their text back.
				dropdown.Show()
				ui.ShowDashboard("overview")
				time.Sleep(3 * time.Second)
				js := "window.__headroomReport && window.__headroomReport()"
				dropdown.ExecJS(js)
				if w := ui.window("dashboard"); w != nil {
					w.ExecJS(js)
				}
				// Open the safety check for the suggested app (demo data only:
				// the selftest never confirms a quit).
				for _, sg := range svc.Status().Suggestions {
					if sg.Kind == "quit" {
						if err := svc.OpenSafetyCheck(sg.AppKey); err != nil {
							fmt.Println("safety:", err)
						}
						time.Sleep(2 * time.Second)
						if w := ui.window("safety"); w != nil {
							w.ExecJS(js)
						}
						time.Sleep(time.Second)
						// Keep [App] open closes and destroys the window;
						// opening it again makes a fresh one.
						svc.CloseSafetyCheck()
						time.Sleep(time.Second)
						fmt.Println("lifecycle: safety closed:", ui.window("safety") == nil)
						_ = svc.OpenSafetyCheck(sg.AppKey)
						time.Sleep(2 * time.Second)
						fmt.Println("lifecycle: safety reopened:", ui.window("safety") != nil)
						svc.CloseSafetyCheck()
						break
					}
				}
				// Close the dashboard and dropdown: nothing may stay alive.
				time.Sleep(time.Second)
				if w := ui.window("dashboard"); w != nil {
					w.Close()
				}
				dropdown.Hide()
				time.Sleep(time.Second)
				fmt.Println("lifecycle: dashboard closed:", ui.window("dashboard") == nil, "panels visible:", ui.PanelsVisible())
				// Reopen straight on Settings (section from the URL hash).
				svc.OpenDashboard("settings")
				time.Sleep(3 * time.Second)
				if w := ui.window("dashboard"); w != nil {
					w.ExecJS(js)
				}
				time.Sleep(2 * time.Second)
				wapp.Quit()
			}()
		}
	})

	if err := wapp.Run(); err != nil {
		log.Fatal(err)
	}
}

// loadProfile returns the stored machine profile, detecting (and storing) it
// on first launch. Hardware fields are refreshed if the Mac changed.
func loadProfile(st *store.Store, c collector.Collector) machine.Profile {
	detected := machine.Detect(c)
	if p, ok := st.Profile(); ok {
		detected.FirstSeen = p.FirstSeen
	}
	_ = st.SaveProfile(detected)
	return detected
}

// runLoginItem shows or changes the login item from the command line, for
// troubleshooting (Settings › Start at login does the same).
func runLoginItem(cmd string) int {
	if !macos.InBundle() {
		fmt.Println("Start at login only works when Headroom runs from its .app bundle.")
		return 1
	}
	switch cmd {
	case "on", "off":
		if err := (loginItem{}).Set(cmd == "on"); err != nil {
			fmt.Println("Couldn't change start at login:", err)
			return 1
		}
	case "status":
	default:
		fmt.Println("Use --login-item status, on or off.")
		return 2
	}
	on, approval, _ := loginItem{}.Status()
	switch {
	case on:
		fmt.Println("Start at login: on")
	case approval:
		fmt.Println("Start at login: waiting for approval in System Settings")
	default:
		fmt.Println("Start at login: off")
	}
	return 0
}
