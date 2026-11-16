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
* **Graceful quit was checked for real** against a throwaway helper app that the integration test builds and launches (`make integration`). No other app was quit while building Headroom.
* **Force quit** needs the hidden setting turned on, a token, and a second confirmation. Notifications never offer it.
* **Disk actions never delete.** Items can only be revealed in Finder or moved to the Trash (`NSFileManager trashItemAtURL`). Trash requests are limited to items strictly inside Downloads, `~/Library/Caches` and the developer folders. Items reached through a symlink are refused, and the file must be the same one (device + inode) at confirmation as at review. The Trash itself is only measured and opened, never emptied. The real Trash call is opt-in in the integration test (`HEADROOM_TEST_TRASH=1`) and was not run here, to avoid putting a file in your Trash. Logic is covered by unit tests with a fake Trash.
* **Folder access.** Measuring Downloads makes macOS ask once for access to that folder. A refused or protected folder shows "Needs Full Disk Access to measure", and Headroom never asks for Full Disk Access.
* **Notifications** use `UNUserNotificationCenter` through the Wails notifications service. The service is started directly rather than bound, so the web UI can't send notifications. They only work from the signed `.app` bundle; elsewhere they are off. Interruption level is "active", so Focus modes silence them. The permission prompt appears once, on first launch.
* **No alert for the state at launch.** If the Mac is already tight when Headroom starts, the icon shows it, but no notification is sent. Alerts are only for changes for the worse.
* **Main-thread fix.** Bringing the app forward (`App.Show`) from a bound call crashed because AppKit needs the main thread. It's now dispatched to the main thread. The selftest opens the dashboard and the safety window to cover this.

## M7: dashboard

* **Timeline chart.** Memory in use (area) with overflow on disk stacked on top (hatched), a dashed red slowdown line (memory plus the overflow budget) and red-wash bands for slowdowns. It has a crosshair tooltip that works with the mouse and the arrow keys, an accessible summary, and a "Show as a table" view.
* **Chart colours** are the spec tokens. A palette validator confirms the two series stay distinct for colour-blind readers (ΔE ≥ 17) and reach 3:1 against the surface in both themes. It also flags the slate `mem-used` as low-chroma. That is intended by the spec, and the overflow series carries a hatch texture as well, so identity never relies on colour alone.
* **Demo history.** `HEADROOM_DEMO` seeds its throwaway database with a synthetic week (a daily rhythm, two slowdowns, two logged actions), so the dashboard can be shown without any real data.
* The `frame-ancestors` directive was dropped from the CSP meta tag, because browsers ignore it in `<meta>`. The windows are never framed anyway.
