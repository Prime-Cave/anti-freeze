// Command headroom is a menu bar app that warns you before your Mac slows down.
//
//	headroom             run the menu bar app
//	headroom sample      print one JSON snapshot (compare with Activity Monitor)
//	headroom --selftest  start, report the status item to stdout, quit
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
	"github.com/Prime-Cave/headroom/internal/store"
	"github.com/Prime-Cave/headroom/internal/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

var version = "dev"

const dropdownWidth = 360

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sample" {
		os.Exit(runSample())
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
		defer os.RemoveAll(dir)
		dbPath = filepath.Join(dir, "headroom.db")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("Headroom can't open its history at %s: %v", dbPath, err)
	}
	defer st.Close()

	// Collector and machine profile.
	var col collector.Collector
	var profile machine.Profile
	if demo != "" {
		col = collector.NewDemo(demo)
		profile = machine.Profile{Model: "Demo", ModelName: "MacBook Air", Chip: "Apple M1", Arch: "arm64",
			OSVersion: "14.6", MemoryBytes: 8 << 30, DiskBytes: 245_107_195_904, Cores: 8,
			FirstSeen: time.Now().Add(-7 * 24 * time.Hour)}
	} else {
		native := collector.NewNative()
		col = native
		profile = loadProfile(st, native)
	}

	cfg := app.Config{Collector: col, Store: st, Profile: profile, Demo: demo != "", Logger: logger}
	if selftest {
		cfg.Selftest = func(page, text string) { fmt.Printf("--- %s ---\n%s\n", page, text) }
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

	dashboard := wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "dashboard", Title: "Headroom", URL: "/dashboard.html",
		Width: 1120, Height: 760, MinWidth: 900, MinHeight: 600, Hidden: true,
		BackgroundColour:           application.NewRGB(0xF3, 0xF5, 0xF8),
		DefaultContextMenuDisabled: true,
	})
	dashboard.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		dashboard.Hide()
		e.Cancel()
	})

	ui := &wailsUI{app: wapp, tray: tr, dropdown: dropdown, dashboard: dashboard}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wapp.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go svc.Run(ctx, ui)
		if selftest {
			go func() {
				time.Sleep(2 * time.Second)
				application.InvokeSync(func() { fmt.Println(tray.Inspect()) })
				// Render both pages and have them report their text back.
				dropdown.Show()
				dashboard.Show()
				time.Sleep(3 * time.Second)
				js := "window.__headroomReport && window.__headroomReport()"
				dropdown.ExecJS(js)
				dashboard.ExecJS(js)
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
