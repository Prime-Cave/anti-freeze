# Anti-Freeze build. Requires: Go 1.26+, Node 20+, Xcode command line tools.
SHELL := /bin/bash
APP_NAME   := Anti-Freeze
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo 0.0.0)
PLIST_VER  := $(shell echo "$(VERSION)" | grep -Eo '^[0-9]+(\.[0-9]+){1,2}' || echo 0.1.0)
OUT        := build/out
APP        := $(OUT)/$(APP_NAME).app
DMG        := $(OUT)/$(APP_NAME).dmg
GO_TAGS    := production
# -trimpath keeps local file paths (and so usernames) out of the binary.
GO_FLAGS   := -trimpath -tags $(GO_TAGS) -ldflags "-s -w -X main.version=$(VERSION)"
MINOS      := 13.0
export MACOSX_DEPLOYMENT_TARGET := $(MINOS)
export CGO_ENABLED := 1
export CGO_CFLAGS  := -mmacosx-version-min=$(MINOS)
export CGO_LDFLAGS := -mmacosx-version-min=$(MINOS)
# Signing identity: ad-hoc by default. Override from the environment only,
# e.g. SIGN_IDENTITY="Developer ID Application: …" make app. Never commit it.
SIGN_IDENTITY ?= -

.PHONY: all frontend build run app dmg test race integration vet lint security check stress clean sample icon

all: app

frontend/node_modules: frontend/package-lock.json
	cd frontend && npm ci --no-fund --no-audit
	@touch $@

frontend: frontend/node_modules
	cd frontend && npm run build
	@touch frontend/dist/.gitkeep

build: frontend
	go build $(GO_FLAGS) -o $(OUT)/anti-freeze ./cmd/anti-freeze

$(OUT)/anti-freeze-arm64: frontend
	GOARCH=arm64 go build $(GO_FLAGS) -o $@ ./cmd/anti-freeze

$(OUT)/anti-freeze-amd64: frontend
	GOARCH=amd64 CGO_CFLAGS="$(CGO_CFLAGS) -arch x86_64" CGO_LDFLAGS="$(CGO_LDFLAGS) -arch x86_64" \
		go build $(GO_FLAGS) -o $@ ./cmd/anti-freeze

# Universal (arm64 + x86_64) app bundle, ad-hoc signed.
app: $(OUT)/anti-freeze-arm64 $(OUT)/anti-freeze-amd64
	rm -rf $(APP)
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	lipo -create -output $(APP)/Contents/MacOS/$(APP_NAME) $(OUT)/anti-freeze-arm64 $(OUT)/anti-freeze-amd64
	sed "s/__VERSION__/$(PLIST_VER)/g" build/darwin/Info.plist > $(APP)/Contents/Info.plist
	go run ./scripts/mkicon -out $(APP)/Contents/Resources/icons.icns
	codesign --force --options runtime $(if $(filter -,$(SIGN_IDENTITY)),--timestamp=none,--timestamp) \
		--entitlements build/darwin/Anti-Freeze.entitlements -s "$(SIGN_IDENTITY)" $(APP)
	codesign --verify --strict $(APP)
	@lipo -info $(APP)/Contents/MacOS/$(APP_NAME)

dmg: app
	rm -f $(DMG)
	./scripts/make-dmg.sh $(APP) $(DMG)

run: app
	open $(APP)

sample: build
	$(OUT)/anti-freeze sample

test:
	go test ./...

race:
	go test -race ./...

# Launches and quits a throwaway helper app through the real AppKit path.
# ANTIFREEZE_TEST_TRASH=1 also moves one temp file to your Trash.
integration:
	go test -tags integration -v ./internal/macos/

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# Same scanners as CI: govulncheck, gosec (G115 excluded, see CI), gitleaks.
security:
	govulncheck ./...
	gosec -quiet -exclude=G115 ./...
	gitleaks dir --redact .
	gitleaks git --redact .

check: vet race lint
	cd frontend && npm run typecheck && npm run contrast

# Allocates memory in a child process so you can watch ok → tight → critical.
# Hard-capped and auto-exits; Ctrl-C stops it at any time.
stress:
	go run ./scripts/stress $(STRESS_ARGS)

clean:
	rm -rf $(OUT) frontend/dist/assets frontend/dist/*.html
