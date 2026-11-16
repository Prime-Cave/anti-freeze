# Setup guide

Two paths: **install the app** (no tools needed) or **build it from source**.

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
