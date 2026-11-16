# Architecture

## Stack decisions

| Area | Choice | Why |
|---|---|---|
| Language | Go (module requires **1.26+**) | The brief asked for 1.23+. Wails v3.1.0 declares `go 1.26`, so that is the real floor. |
| UI shell | **Wails v3.1.0** (stable, released 1 Nov 2026) | v3 has a first-class system tray with an *attached window* (`SystemTray.AttachWindow`), which is exactly a menu bar icon + dropdown. It also ships a notification service backed by `UNUserNotificationCenter` and a login-item API backed by `SMAppService.mainApp`. v2 would have needed our own `NSStatusItem` + `NSPopover` bridge. Electron was ruled out. |
| Frontend | TypeScript + Vite 6, plain CSS, no framework | Two small pages (dropdown, dashboard). Vite 6 rather than 7/8 so it builds on Node 20.12+. |
| System data | cgo against public macOS APIs | See "Collectors" below. |
| Storage | SQLite via `modernc.org/sqlite` (pure Go) | No extra C toolchain needs; WAL mode. |

### Gaps in Wails v3 we bridge ourselves

Checked by reading the v3.1.0 source, not assumed:

* **Menu bar icon state.** Wails sets an icon once and cannot switch a status item between a *template* image (monochrome, follows the menu bar) and a *coloured* image at runtime. It also has no tooltip or accessibility label on macOS (`setTooltip` is a no-op).
  `internal/tray` renders the icon in pure Go and a ~50-line Objective-C bridge finds the status bar button through public AppKit API (`NSApp.windows` → `NSStatusBarWindow` → `NSButton`). It then sets the image, the template flag, the light/dark variant and the accessibility label.
* **Production asset serving.** Wails v3 serves the UI through a `WKURLSchemeHandler` on the `wails://` scheme. No TCP listener exists unless the app is built with the `server` or `mcp` build tags, which Headroom never uses. `make check` and CI verify no socket is open (see `docs/PRIVACY.md`).

## Build

`make app` builds the frontend, compiles arm64 and amd64 binaries with `-trimpath` (so no local paths end up in the binary), `lipo`s them into a universal binary, assembles `Headroom.app` (`LSUIElement`, minimum macOS 13) and ad-hoc signs it with the hardened runtime.
