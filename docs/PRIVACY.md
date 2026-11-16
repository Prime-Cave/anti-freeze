# Privacy

**Anti-Freeze never sends anything anywhere.** It has no network code at all: no telemetry, analytics, crash reporting, update checks or remote content.

## How that is enforced

* `internal/policy` has tests that fail the build if Anti-Freeze's own packages import `net`, `net/http`, `crypto/tls`, `net/smtp`, `net/rpc` or `os/exec` (the scripts folder is excluded), or if any package under `internal/` pulls in a network client.
* Wails serves the interface from inside the app through a custom URL scheme. No local web server or port is opened. Anti-Freeze is never built with Wails' `server` or `mcp` tags.
* The interface's Content Security Policy blocks everything by default (`default-src 'none'`) and allows scripts, styles, fonts and connections only from the app itself (`'self'`). No remote scripts, images, fonts or connections can load.
* The app has no entitlements (see `build/darwin/Anti-Freeze.entitlements`).
* You can check for yourself while it runs: `lsof -a -i -p "$(pgrep -x Anti-Freeze)"` prints nothing.

## What Anti-Freeze reads

From public macOS interfaces, as you (never as admin):

* memory, compressed memory, swap, memory pressure, disk space, processor load and thermal state;
* the list of running processes with their memory, processor and disk use, and the app each belongs to;
* folder sizes for the places shown under Disk: Downloads, app caches, the Trash, developer caches and iPhone and iPad backups in your home folder. Only sizes are read, never file contents. If macOS blocks a folder (backups usually are), Anti-Freeze says so instead of asking for Full Disk Access.

## What Anti-Freeze stores

One SQLite file: `~/Library/Application Support/Anti-Freeze/anti-freeze.db`. It contains:

| Data | Kept for |
|---|---|
| System samples (memory, swap, disk, processor, heat, top app name) | 24 hours raw, 7 days per minute, then 90 days per 10 minutes (adjustable) |
| Slowdown episodes and their likely cause | 1 year |
| Actions you took (app quit, items moved to Trash, amount freed) | 1 year |
| Your machine profile (model, memory and disk size, macOS version) and settings | until deleted |

The file is capped in size and older data is thinned first. It never contains file contents, window titles, web addresses or anything you type.

## Your controls

* **Settings › Export history** saves a CSV where you choose.
* **Settings › Delete history** really deletes the database file and starts a fresh one. Settings are kept.
* To remove everything: quit Anti-Freeze, delete `Anti-Freeze.app`, and delete `~/Library/Application Support/Anti-Freeze`.

## Permissions

* **Notifications:** asked once, on first launch. Change it any time in System Settings › Notifications.
* **Login item:** the first time Anti-Freeze runs from your Applications folder it adds itself to your login items. Turn it off in Settings or in System Settings › General › Login Items. A copy opened from the DMG or another folder never adds itself.
* Anti-Freeze never asks for Full Disk Access, Accessibility, Screen Recording or admin rights.
