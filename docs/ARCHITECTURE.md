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
* **Production asset serving.** Wails v3 serves the UI through a `WKURLSchemeHandler` on the `wails://` scheme. No TCP listener exists unless the app is built with the `server` or `mcp` build tags, which Headroom never uses. The `internal/policy` tests (run by `make check` and CI) fail if a networking package is imported. That no socket is open at runtime was checked by hand with `lsof` during the build; `docs/PRIVACY.md` shows how to check it yourself.

## Build

`make app` builds the frontend, compiles arm64 and amd64 binaries with `-trimpath` (so no local paths end up in the binary), `lipo`s them into a universal binary, assembles `Headroom.app` (`LSUIElement`, minimum macOS 13) and ad-hoc signs it with the hardened runtime.

## Packages

```
cmd/headroom/        main: wires everything; `headroom sample` prints one JSON snapshot
internal/machine/    machine profile (detected on first run) and the thresholds derived from it
internal/collector/  cgo samplers + Collector interface + Fake (replay) + synthetic scenarios
internal/analyzer/   state, headroom, forecast, attribution, episodes, learning
internal/store/      SQLite: migrations, rollups, retention, export, wipe
internal/actions/    quit (graceful), reveal in Finder, move to Trash, open Settings panes
internal/notify/     notifications with action buttons, cooldowns
internal/app/        Wails service bound to the frontend; sampling loop; events
internal/tray/       menu bar icon rendering + status item bridge
frontend/            dropdown (index.html) and dashboard (dashboard.html)
```

Every collector sits behind `collector.Collector`. Tests and demo mode use `collector.Fake`, which replays samples from `testdata/*.json`. Those files are synthetic: `go run ./scripts/fixtures` generates them, and nothing in them was recorded from a real Mac.

## Thresholds (all derived from the machine profile)

| Threshold | Default | Meaning |
|---|---|---|
| Disk reserve | 10% of disk, min 8 GB, max 16 GB | Disk free below this → *tight*; swap may not use it |
| Disk floor | half the reserve, min 4 GB | Disk free below this → *critical* |
| Swap budget | half of memory | Total overflow the Mac carries comfortably |
| Swap growth | 1% of memory per minute, min 50 MB/min | Faster (over the last 3 min, confident fit) → *tight* |
| Forecast critical | 5 min | Forecast slowdown sooner than this → *critical* |

The brief suggested a 10%, min 8 GB reserve. We cap it at 16 GB: without the cap a 1 TB disk would read "tight" with 90 GB free, and a 256 GB Mac with 20 GB free (which is fine) would never read "ok".

Alert sensitivity scales these. *Relaxed*: reserve ×0.75, growth ×1.5, forecast 3 min. *Early*: reserve ×1.25, growth ×0.6, forecast 10 min.

## States

| State | When |
|---|---|
| ok, "Plenty of room" | none of the below |
| tight, "Getting tight" | pressure *warn*, OR swap growing faster than the growth threshold, OR disk free < reserve, OR (after learning) swap ≥ the learned budget |
| critical, "Slowdown likely" | pressure *critical*, OR the forecast says a slowdown within 5 min, OR disk free < floor |

**Hysteresis.** A worse state applies immediately. A better state applies only after it has held for 60 s. When it does, the state steps down to the worst state seen during that minute.

## The "to spare" number

```
headroom            = free + reclaimable + purgeable + safe swap allowance
free+reclaimable+purgeable = memory total − (app memory + wired + compressed)     ← Activity Monitor's "not used"
safe swap allowance = max(0, min( swap budget − swap used,
                                  disk free (important usage) − disk reserve ))
```

"Reclaimable" is cached files (file-backed pages macOS drops instantly). The swap allowance has two limits. One is how much more overflow this Mac can take before overflow itself slows it down (the swap budget). The other is the disk room the swap files need. Unavailable inputs count as zero room; the UI says "Not available on this Mac" rather than showing a fake 0. Tests: `internal/analyzer/analyzer_test.go` (`TestHeadroom*`).

## Forecast

1. Swap used and disk free go into rolling 10-minute windows, EWMA-smoothed (α = 0.3).
2. A least-squares line is fitted to each. The trend counts as **confident** only with ≥ 20 samples spanning ≥ 2 minutes, R² ≥ 0.6, and a slope of at least 2 MB/min.
3. Swap growth eats headroom directly: `ETA = headroom ÷ swap slope`. Disk loss eats the room swap needs: `ETA = (disk free − floor) ÷ disk loss rate`. The sooner of the two wins.
4. The 10-minute line lags (it keeps pointing up for minutes after growth stops). So a swap forecast also needs the 3-minute line to still be rising.
5. Nothing confident, or an ETA over 60 minutes, reads "Steady. Nothing is filling up fast."

## Culprit attribution

Processes are grouped by app (helpers roll up into their app). For each app we keep footprints for the last 65 minutes and per-process CPU and disk counters. "Growth" is the footprint now minus the footprint an hour ago; an app that started within the hour counts all of its footprint as growth. The suggestion names the **non-protected app whose quitting frees the most** (largest footprint), mentions its growth when that exceeds 500 MB, and says "about" because shared memory makes the freed amount approximate.

## Slowdown episodes and learning

An episode opens when the displayed state reaches critical and closes when it leaves. It records start and end time, swap at start and at peak, disk free at start and at its lowest, and the top 3 apps by footprint and by growth. `analyzer.Explain` writes the dashboard sentence ("Two slowdowns. Both started when … passed 3 GB while disk was under 12 GB free.").

After 7 days of history and at least 2 episodes, `analyzer.Learn` moves thresholds toward where this Mac struggled:

* **Swap budget** becomes 90% of the median swap at episode start. It never goes above the default, and never below a quarter of the default (swap near zero at the start means swap wasn't the trigger).
* **Disk reserve** becomes 110% of the median disk free at episode start, but only when disk was actually low then. It never goes below the default or above twice the default.

Learning only ever makes Headroom more cautious.
