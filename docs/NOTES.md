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
