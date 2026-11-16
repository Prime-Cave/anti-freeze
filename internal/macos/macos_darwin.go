//go:build darwin

// Package macos wraps the few AppKit calls Headroom's actions need. Every
// call here is a public API; none of them deletes anything.
package macos

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>
int hmTerminate(int pid);
int hmForceTerminate(int pid);
int hmIsRunning(int pid);
char *hmTrash(const char *path);
void hmReveal(const char *path);
int hmOpenURL(const char *url);
int hmAppIconPNG(const char *path, unsigned char **out, int *len);
*/
import "C"

import (
	"errors"
	"unsafe"
)

// System is the real AppKit-backed implementation of actions.System.
type System struct{}

// Terminate asks an app to quit normally (NSRunningApplication.terminate).
func (System) Terminate(pid int) bool { return C.hmTerminate(C.int(pid)) == 1 }

// ForceTerminate force-quits an app (NSRunningApplication.forceTerminate).
func (System) ForceTerminate(pid int) bool { return C.hmForceTerminate(C.int(pid)) == 1 }

// IsRunning reports whether the app is still running.
func (System) IsRunning(pid int) bool { return C.hmIsRunning(C.int(pid)) == 1 }

// Trash moves an item to the Trash (NSFileManager.trashItemAtURL).
func (System) Trash(path string) error {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	if e := C.hmTrash(cp); e != nil {
		defer C.free(unsafe.Pointer(e))
		return errors.New(C.GoString(e))
	}
	return nil
}

// Reveal selects an item in Finder.
func (System) Reveal(path string) {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	C.hmReveal(cp)
}

// Open opens a folder path or a System Settings URL.
func (System) Open(url string) bool {
	cu := C.CString(url)
	defer C.free(unsafe.Pointer(cu))
	return C.hmOpenURL(cu) == 1
}

// AppIconPNG returns a small PNG of an app bundle's icon.
func AppIconPNG(bundlePath string) ([]byte, bool) {
	cp := C.CString(bundlePath)
	defer C.free(unsafe.Pointer(cp))
	var out *C.uchar
	var n C.int
	if C.hmAppIconPNG(cp, &out, &n) != 1 {
		return nil, false
	}
	defer C.free(unsafe.Pointer(out))
	return C.GoBytes(unsafe.Pointer(out), n), true
}
