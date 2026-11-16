//go:build darwin && integration

// Run with: go test -tags integration ./internal/macos/
//
// Builds a throwaway helper app in a temp folder, launches it, and quits it
// through the same AppKit path Anti-Freeze uses. It never touches any other app.
// The Trash check is opt-in (ANTIFREEZE_TEST_TRASH=1) because it leaves one
// small temp file in your Trash.
package macos

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const helperSrc = `#import <AppKit/AppKit.h>
int main(void) {
  @autoreleasepool {
    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    [NSApp run];
  }
  return 0;
}`

const helperPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>io.github.prime-cave.anti-freeze.testhelper</string>
<key>CFBundleExecutable</key><string>QuitHelper</string>
<key>CFBundleName</key><string>QuitHelper</string>
<key>LSUIElement</key><true/>
</dict></plist>`

func TestTerminateThrowawayApp(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "QuitHelper.app")
	bin := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "main.m")
	if err := os.WriteFile(src, []byte(helperSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(helperPlist), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bin, "QuitHelper")
	if out, err := exec.Command("/usr/bin/clang", "-fobjc-arc", "-framework", "AppKit", "-o", exe, src).CombinedOutput(); err != nil {
		t.Fatalf("clang: %v\n%s", err, out)
	}
	if out, err := exec.Command("/usr/bin/open", "-n", "-g", app).CombinedOutput(); err != nil {
		t.Fatalf("open: %v\n%s", err, out)
	}
	pid := 0
	for i := 0; i < 50 && pid == 0; i++ {
		time.Sleep(100 * time.Millisecond)
		out, _ := exec.Command("/usr/bin/pgrep", "-f", exe).Output()
		if f := strings.Fields(string(out)); len(f) > 0 {
			pid = atoi(f[0])
		}
	}
	if pid == 0 {
		t.Fatal("helper app did not start")
	}
	var sys System
	for i := 0; i < 30 && !sys.IsRunning(pid); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if !sys.IsRunning(pid) {
		t.Fatal("helper not visible to NSRunningApplication")
	}
	if !sys.Terminate(pid) {
		t.Fatal("terminate was refused")
	}
	for i := 0; i < 50 && sys.IsRunning(pid); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if sys.IsRunning(pid) {
		t.Fatal("helper still running after a graceful quit")
	}
	if png, ok := AppIconPNG("/System/Applications/Calculator.app"); !ok || len(png) < 100 {
		t.Error("no icon for Calculator")
	}
}

func TestTrashTempFile(t *testing.T) {
	if os.Getenv("ANTIFREEZE_TEST_TRASH") != "1" {
		t.Skip("set ANTIFREEZE_TEST_TRASH=1 to move one temp file to your Trash")
	}
	home, _ := os.UserHomeDir()
	dir, err := os.MkdirTemp(home, ".anti-freeze-trash-check-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	f := filepath.Join(dir, "anti-freeze-trash-check.txt")
	if err := os.WriteFile(f, []byte("Anti-Freeze test file. Safe to delete."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (System{}).Trash(f); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("file still in place after Trash")
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
