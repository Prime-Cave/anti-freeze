// Command headroom is a menu bar app that warns you before your Mac slows down.
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Prime-Cave/headroom/frontend"
	"github.com/Prime-Cave/headroom/internal/tray"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func main() {
	selftest := len(os.Args) > 1 && os.Args[1] == "--selftest"
	app := application.New(application.Options{
		Name:        "Headroom",
		Description: "Warns you before your Mac slows down",
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(frontend.Assets())},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	tr := app.SystemTray.New()
	ok := tray.Render(tray.LevelOK, 0.5)
	tr.SetTemplateIcon(ok.Light)

	dropdown := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "dropdown",
		URL:             "/",
		Width:           360,
		Height:          420,
		Frameless:       true,
		AlwaysOnTop:     true,
		Hidden:          true,
		DisableResize:   true,
		HideOnFocusLost: true,
		BackgroundType:  application.BackgroundTypeTransparent,
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTransparent,
		},
	})
	dropdown.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		dropdown.Hide()
		e.Cancel()
	})
	tr.AttachWindow(dropdown).WindowOffset(6)

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		application.InvokeSync(func() { tray.Apply(ok, "Headroom: Plenty of room") })
		if selftest {
			go func() {
				time.Sleep(1500 * time.Millisecond)
				application.InvokeSync(func() { fmt.Println(tray.Inspect()) })
				app.Quit()
			}()
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
