# Build log: deviations and measurements

A running record of where the build differs from the original brief, and why.

* **Go version floor is 1.26**, not 1.23: required by Wails v3.1.0.
* **Vite 6** (not 7/8): Vite 7+ requires Node 20.19+; 6.4 supports any Node 20.
* **Menu bar icon bridge**: see ARCHITECTURE.md, "Gaps in Wails v3".

## M2: collector accuracy (checked on an Apple Silicon test Mac under memory pressure)

Each figure was compared with a macOS tool reading the same moment. Only the result is recorded here, not the readings.

| Figure | Compared with | Result |
|---|---|---|
| Memory used (app + wired + compressed) | Activity Monitor formula; wired and compressor from `top` | matches within 1% |
| Swap used | `sysctl vm.swapusage` | matches within 3% (sampled seconds apart) |
| Per-process footprint | `footprint -p` | exact, for the processes checked |
| Process CPU time | `ps -o time` | matches after Mach timebase conversion (test: `TestNativeOwnProcessMatchesPS`) |
| Process count | `top` | matches within one process |
| Disk available | `diskutil` container free space | reads higher, as expected: "important usage" counts purgeable APFS space as available |

Differences and fixes found while checking:

* **CPU time units.** `proc_pid_rusage` CPU times are Mach ticks on Apple Silicon (1 tick = 125/3 ns), not nanoseconds. Converted with `mach_timebase_info`.
* **Missing processes.** `PROC_PIDTBSDINFO` is denied for other users' processes. We now fall back to `PROC_PIDT_SHORTBSDINFO`, so they are listed (as Protected) instead of silently dropped.
* **Truncated names.** The kernel truncates process names to 16 characters, so unbundled processes are named from their executable path.
* **Purgeable** is often 0 on recent macOS; the formula still includes it.
* **Process grouping** does not use `responsibility_get_pid_responsible_for_pid`. It is not in a public header, and the brief requires public APIs only. Groups come from the outermost `.app` in the executable path plus `NSRunningApplication`.
* **Extra protection.** Apps under `/System/Library/`, `/usr/`, `/sbin/`, `/bin/` and `/Library/Apple/` are protected too (Control Centre, Notification Centre, Spotlight and so on), on top of the brief's list.

## M5: design tokens and contrast

* Tokens are implemented exactly as specified (`frontend/src/styles/tokens.css`).
* `npm run contrast` checks every text/background pairing the UI uses, in both themes, against 4.5:1. Two pairings from the spec's palette fall short: `ink-3` on `critical-wash` (light, 4.39:1) and `ink-3` on `btn-2-hover` (dark, 4.01:1). The tokens stay as specified; the UI never uses those pairings, and the script lists them as forbidden.
* One colour token was added, built from spec values only: `--on-accent` (text on the primary button: white in light mode, canvas in dark mode, which keeps 4.5:1 on the dark `--ok`).
* The safety check opens in its own 420 px window rather than inside the 360 px dropdown, so the dropdown, notifications and dashboard all use the same dialog.

## M6: actions and notifications

* **Quit needs a fresh safety check.** `PrepareQuit` returns a random single-use token that expires after 2 minutes. `ConfirmQuit` refuses anything else, and checks again that the app has the same process ID and is still not protected. Tested with a fake process table (`internal/actions`).
* **Graceful quit was checked for real** against a throwaway helper app that the integration test builds and launches (`make integration`). No other app was quit while building Anti-Freeze.
* **Force quit** needs the hidden setting turned on, a token, and a second confirmation. Notifications never offer it.
* **Disk actions never delete.** Items can only be revealed in Finder or moved to the Trash (`NSFileManager trashItemAtURL`). Trash requests are limited to items strictly inside Downloads, `~/Library/Caches` and the developer folders. Items reached through a symlink are refused, and the file must be the same one (device + inode) at confirmation as at review. The Trash itself is only measured and opened, never emptied. The real Trash call is opt-in in the integration test (`ANTIFREEZE_TEST_TRASH=1`) and was not run here, to avoid putting a file in your Trash. Logic is covered by unit tests with a fake Trash.
* **Folder access.** Measuring Downloads makes macOS ask once for access to that folder. A refused or protected folder shows "Needs Full Disk Access to measure", and Anti-Freeze never asks for Full Disk Access.
* **Notifications** use `UNUserNotificationCenter` through the Wails notifications service. The service is started directly rather than bound, so the web UI can't send notifications. They only work from the signed `.app` bundle; elsewhere they are off. Interruption level is "active", so Focus modes silence them. The permission prompt appears once, on first launch.
* **No alert for the state at launch.** If the Mac is already tight when Anti-Freeze starts, the icon shows it, but no notification is sent. Alerts are only for changes for the worse.
* **Main-thread fix.** Bringing the app forward (`App.Show`) from a bound call crashed because AppKit needs the main thread. It's now dispatched to the main thread. The selftest opens the dashboard and the safety window to cover this.

## M7: dashboard

* **Timeline chart.** Memory in use (area) with overflow on disk stacked on top (hatched), a dashed red slowdown line (memory plus the overflow budget) and red-wash bands for slowdowns. It has a crosshair tooltip that works with the mouse and the arrow keys, an accessible summary, and a "Show as a table" view.
* **Chart colours** are the spec tokens. A palette validator confirms the two series stay distinct for colour-blind readers (ΔE ≥ 17) and reach 3:1 against the surface in both themes. It also flags the slate `mem-used` as low-chroma. That is intended by the spec, and the overflow series carries a hatch texture as well, so identity never relies on colour alone.
* **Demo history.** `ANTIFREEZE_DEMO` seeds its throwaway database with a synthetic week (a daily rhythm, two slowdowns, two logged actions), so the dashboard can be shown without any real data.
* The `frame-ancestors` directive was dropped from the CSP meta tag, because browsers ignore it in `<meta>`. The windows are never framed anyway.

## M8: login item, packaging, performance

* **Login item** uses `SMAppService.mainApp`, so it only works from the `.app` bundle. The first time Anti-Freeze runs from `/Applications` or `~/Applications` it turns the item on once (the default). A copy opened from the DMG, Downloads or a build folder never registers itself. macOS can report "not found" for a main app that was never registered; inside a bundle that is treated as off. `Anti-Freeze --login-item status|on|off` shows or changes the item from the command line. After that the user's choice in Settings or System Settings is left alone. If macOS wants approval, Settings links to System Settings › Login Items. Demo mode and the selftest never touch it.
* **Quitting Anti-Freeze.** Right-click the menu bar icon for Open dashboard, Settings and Quit Anti-Freeze. Settings also has a Quit Anti-Freeze button.
* **Performance fixes after the first 10-minute run.** The first run missed the targets (main process about 3% of one core, footprint rising past 75 MB). Benchmarks showed sampling itself is cheap (process table about 6 ms, system about 7 ms per call). The cost came from three things, now fixed:
  1. Per-process detail was kept for 65 minutes. Now it is kept for 2 minutes. App-level growth history is kept separately and is unchanged.
  2. The dashboard and safety windows stayed alive while hidden, each with its own web content processes. They are now created when opened and destroyed when closed.
  3. Status events were sent to hidden windows every few seconds. They are now sent only while a panel is open, and a panel fetches the status itself when it opens.
  4. The loop ticked every 500 ms and asked the main thread whether a panel was visible each time. It now sleeps until the next sample is due, so an idle Anti-Freeze wakes about every 5 s. Opening a panel wakes it at once.
  5. Per-call timing showed the disk capacity query ("available for important usage") costs about 8 ms of CPU, almost all of a system sample; memory, swap, pressure, processor and heat together cost under 0.1 ms. That query now runs once a minute. In between, the value follows the volume's free space from `statfs` (microseconds), and a jump of more than 2 GB triggers a fresh query.
* **Result** (10 minutes, Apple Silicon, normal use, panels closed): main process 0.89% of one core and 45 MB peak footprint (42 MB average); web view processes 0.01% and about 12 MB. Before these fixes: 2.9% and 78 MB, still rising.
* **Window lifecycle** is covered by the selftest: it closes and reopens the safety check, closes the dashboard and checks nothing is left visible, then reopens the dashboard straight on Settings. That caught a bug: opening the dashboard directly on Settings showed an empty form.
* **Intel.** The x86_64 half of the universal binary passes the selftest under Rosetta. It has not been run on an Intel Mac.
* **Disk places.** Besides the folders the brief lists, Anti-Freeze also measures the npm, Gradle and Cargo download caches and Xcode device support when they exist. These are common, large and safe to rebuild. iPhone and iPad backups are measured and shown only, never trashed piece by piece. macOS usually blocks that folder, and Anti-Freeze then shows the needs-access note.
* **Screenshots** in the README come from the mock frontend with synthetic data, captured with headless Chrome.
* **Linting.** `staticcheck` runs as part of `golangci-lint` (its default set includes the staticcheck checks), so there is no separate staticcheck binary to keep in step with the Go version.
* **gosec G115** (integer conversion overflow) is excluded in CI. All 52 hits convert kernel counters, sizes and percentages that are bounded by design. The other gosec findings were fixed or annotated with a reason.
