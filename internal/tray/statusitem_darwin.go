//go:build darwin

package tray

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework AppKit
#include <stdbool.h>
#include <stdlib.h>
void htSetStatusIcon(const void *light, int lightLen, const void *dark, int darkLen, bool isTemplate, const char *label);
char *htInspectStatusItem(void);
*/
import "C"

import "unsafe"

// Apply sets the status item's image and accessibility label.
//
// Wails v3 owns the NSStatusItem but cannot switch between template and
// coloured images at runtime, and has no accessibility label API on macOS.
// This bridge finds the status bar button through public AppKit API
// (NSApp.windows → NSStatusBarWindow → NSButton) and sets both.
//
// Must be called on the main thread (use application.InvokeSync).
func Apply(icon Icon, label string) {
	if len(icon.Light) == 0 || len(icon.Dark) == 0 {
		return
	}
	cl := C.CString(label)
	defer C.free(unsafe.Pointer(cl))
	C.htSetStatusIcon(
		unsafe.Pointer(&icon.Light[0]), C.int(len(icon.Light)),
		unsafe.Pointer(&icon.Dark[0]), C.int(len(icon.Dark)),
		C.bool(icon.Template), cl)
}

// Inspect returns a JSON description of the live status item (for --selftest).
// Must be called on the main thread.
func Inspect() string {
	c := C.htInspectStatusItem()
	defer C.free(unsafe.Pointer(c))
	return C.GoString(c)
}
