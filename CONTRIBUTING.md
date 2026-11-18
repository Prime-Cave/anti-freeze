# Contributing

Thanks for helping. Anti-Freeze is small on purpose. Changes that keep it small, private and safe are the easiest to accept.

## Build and test

You need macOS 13 or later, Go 1.26+, Node 20+ and the Xcode command line tools (`xcode-select --install`).

```bash
make app          # build/out/Anti-Freeze.app (universal, ad-hoc signed)
make check        # go vet, go test -race, golangci-lint, typecheck, contrast check
make integration  # launches and quits a throwaway helper app through AppKit
make stress       # allocates memory in a child process so you can watch the states change
```

To work on the UI without the Go side, run `npm --prefix frontend run dev` and open `http://127.0.0.1:5173/?state=tight` (also `ok`, `critical`, `loading`, `na`). That uses synthetic mock data.

To run the real app on synthetic data (nothing on your Mac is read, quit or moved):

```bash
ANTIFREEZE_DEMO=tight build/out/Anti-Freeze.app/Contents/MacOS/Anti-Freeze
```

## Rules that every change must keep

* **No network.** Don't import `net`, `net/http`, `crypto/tls` or any client library, and don't load remote content in the UI. `internal/policy` tests fail if you do.
* **Never delete.** Disk actions move items to the Trash, only inside the allowed folders, after confirmation.
* **Never quit without the safety check**, and never force quit unless the user turned it on and confirmed twice. No `kill -9`.
* **No shell commands built from strings.** If a system tool is truly needed, use `exec.Command` with fixed arguments.
* **No data from your own Mac** in fixtures, tests, screenshots or docs. `go run ./scripts/fixtures` makes synthetic samples.
* Plain words in the UI: "memory", "overflow on disk", "to spare". See the copy rules in `docs/ARCHITECTURE.md`.

## Pull requests

* One change per pull request, with a short description of why.
* CI must pass: build, `-race` tests, golangci-lint, govulncheck, gosec, gitleaks, CodeQL and `npm audit`.
* Add or update tests for analyzer, store and actions changes.
