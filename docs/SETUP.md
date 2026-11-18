# Setup guide

Three ways in:

* **A. Install the app.** No tools needed. Pick this if you just want to use Anti-Freeze.
* **B. Build it from source.** Pick this if you want to read or change the code.
* **C. Let an AI coding agent do B for you.** Paste one prompt into your agent.

You need a Mac with macOS 13 Ventura or later (Apple Silicon or Intel). Anti-Freeze doesn't run on Windows or Linux.

Once it builds, [GUIDE.md](GUIDE.md) explains how the code works and which file to open for common changes.

## A. Install the app

1. Download `Anti-Freeze.dmg` from the [latest release](https://github.com/Prime-Cave/anti-freeze/releases/latest).
2. Double-click the DMG and drag **Anti-Freeze** onto **Applications**. Eject the DMG.
3. Open **Anti-Freeze** from Applications. macOS says it can't verify the developer: click **Done**.
4. Open **System Settings › Privacy & Security**, scroll down, click **Open Anyway** next to the Anti-Freeze message, and confirm with your password or Touch ID.
   (macOS 13 and 14 also allow right-click › **Open** › **Open**.)
5. Anti-Freeze appears in the menu bar (top right). The first time it:
   * asks to send notifications: click **Allow** to be warned before slowdowns;
   * adds itself to your login items, so it starts when you log in.
6. Click the icon for the dropdown. Right-click it for **Open dashboard**, **Settings** and **Quit Anti-Freeze**.

To turn off start at login or notifications: right-click the icon › **Settings**.

## B. Build from source

### 1. Install the tools (once)

| Tool | Version | How |
|---|---|---|
| macOS | 13 Ventura or later | |
| Xcode command line tools | any recent | `xcode-select --install` |
| Go | 1.26 or later | `brew install go` or [go.dev/dl](https://go.dev/dl/) |
| Node.js | 20 or later | `brew install node` |

Check them:

```bash
go version
```

```bash
node --version
```

```bash
xcode-select -p
```

### 2. Get the code

```bash
git clone https://github.com/Prime-Cave/anti-freeze.git
```

```bash
cd anti-freeze
```

### 3. Build the app

```bash
make app
```

This installs the frontend packages the first time (`npm ci`), builds the interface, compiles the Apple Silicon and Intel versions, joins them into one universal app, and ad-hoc signs it. Result: `build/out/Anti-Freeze.app`.

### 4. Run it

On synthetic demo data (reads nothing from your Mac, never quits or moves anything):

```bash
ANTIFREEZE_DEMO=tight build/out/Anti-Freeze.app/Contents/MacOS/Anti-Freeze
```

Other demo stages: `ok`, `critical`, `recover`. Press Ctrl-C in the terminal to stop it.

For real, watching your Mac:

```bash
open build/out/Anti-Freeze.app
```

A copy run from `build/out` never adds itself to your login items; only a copy in Applications does that on first launch.

### 5. Make the DMG (optional)

```bash
make dmg
```

Result: `build/out/Anti-Freeze.dmg`. To install your own build, open it and drag Anti-Freeze to Applications.

### 6. Test and check (for changes)

```bash
make test
```

```bash
make check
```

`make check` runs go vet, the race-detector tests, golangci-lint and the frontend type and contrast checks. It needs golangci-lint:

```bash
brew install golangci-lint
```

The same security scanners CI runs:

```bash
brew install gitleaks
```

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

```bash
make security
```

(`go install` puts tools in `$(go env GOPATH)/bin`; add that to your `PATH` if `make` can't find them.)

### 7. Try it under memory pressure (optional)

```bash
make stress
```

This allocates memory in a child process, at most 1.25 × your Mac's memory by default, and stops if free disk drops below 15 GB. Watch the menu bar icon go from ok to tight to critical. It gives the memory back after 2 minutes, or at once with Ctrl-C.

### 8. Work on the interface only (optional)

```bash
npx --prefix frontend vite frontend
```

Open `http://localhost:5173/?state=tight` (also `ok`, `critical`, `loading`, `na`), or `/dashboard.html`, `/safety.html`. It uses mock data, with no Go side needed.

## C. Set it up with an AI coding agent

Any coding agent that can run terminal commands on your Mac will do: Claude Code, Codex, Cursor, GitHub Copilot's agent mode, Aider, and so on. Open it in the folder where you keep code, paste the prompt below, and approve the steps it asks about. It follows part B for you, using only demo data.

The prompt tells the agent to ask before installing anything, never to use `sudo`, and to run the app only on synthetic data. That way, nothing on your Mac gets quit, moved or deleted while it sets things up.

```text
Set up the Anti-Freeze project on this Mac so I can build, run and edit it.
Anti-Freeze is an open-source macOS menu bar app written in Go (with Wails v3)
and a TypeScript/Vite frontend. Repository:
https://github.com/Prime-Cave/anti-freeze

Rules:
- Explain each step in one line before you run it.
- Never use sudo or ask for my password. If something seems to need admin
  rights, stop and tell me what to do myself.
- Ask me before installing or upgrading any tool, and say what and why.
- Don't change files outside the cloned project folder, except installing
  tools I've approved.
- Only run the app in demo mode (ANTIFREEZE_DEMO set). Don't open the real app
  or run "make stress" unless I ask.
- Don't commit, push, or open pull requests.

Steps:
1. Check this is macOS 13 or later (sw_vers). If not, stop and tell me.
2. Check the tools and their versions: Go 1.26 or later (go version),
   Node.js 20 or later (node --version), and the Xcode command line tools
   (xcode-select -p). For anything missing or too old, tell me and suggest
   the fix (Homebrew: brew install go / brew install node; for Xcode tools:
   xcode-select --install, which opens a dialog I have to click through).
   Wait for my OK before installing.
3. Clone the repository into the current folder and cd into it, unless it's
   already cloned there.
4. Read docs/SETUP.md (part B) and follow it. Build with: make app
   This installs the frontend packages with npm ci and creates
   build/out/Anti-Freeze.app. If the build fails, read the error, check the
   Troubleshooting table in docs/SETUP.md, fix what you safely can, and tell
   me what you changed.
5. Run the tests: make test
6. Run the app's built-in self-test on demo data:
   ANTIFREEZE_DEMO=tight build/out/Anti-Freeze.app/Contents/MacOS/Anti-Freeze --selftest
   It should print the dropdown and dashboard text and exit by itself.
7. Optional, ask me first: install golangci-lint (brew install golangci-lint)
   and run "make check", which is the full set of checks CI runs.
8. Finish with a short report:
   - what you installed, with versions
   - whether the build, tests and self-test passed (quote any errors)
   - how I run it on demo data myself:
     ANTIFREEZE_DEMO=tight build/out/Anti-Freeze.app/Contents/MacOS/Anti-Freeze
     (stop it with Ctrl-C)
   - how I work on the interface in a browser:
     npx --prefix frontend vite frontend
     then open http://localhost:5173/?state=tight
   - that docs/GUIDE.md explains how the code fits together.
```

If the agent gets stuck, the [Troubleshooting](#troubleshooting) table below covers the common problems. You can also paste it the exact error message.

## Troubleshooting

| Problem | Fix |
|---|---|
| `make: golangci-lint: command not found` | `brew install golangci-lint` |
| `xcrun: error: invalid active developer path` | `xcode-select --install` |
| "Anti-Freeze can't be opened" / "can't verify the developer" | Use **Open Anyway** in System Settings › Privacy & Security (step A4) |
| "Anti-Freeze is damaged and can't be opened" | The download's quarantine flag tripped Gatekeeper. Run `xattr -dr com.apple.quarantine /Applications/Anti-Freeze.app`, then open it again |
| Start at login says it needs approval | System Settings › General › Login Items › allow Anti-Freeze |
| No notifications | System Settings › Notifications › Anti-Freeze › Allow notifications |
| Check or change start at login from Terminal | `/Applications/Anti-Freeze.app/Contents/MacOS/Anti-Freeze --login-item status` (also `on`, `off`) |

## Uninstall

1. Right-click the menu bar icon › **Quit Anti-Freeze**.
2. Drag Anti-Freeze from Applications to the Trash.
3. Delete `~/Library/Application Support/Anti-Freeze` (your history).
