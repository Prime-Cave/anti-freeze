//go:build darwin

package macos

/*
#cgo LDFLAGS: -framework ServiceManagement -framework Foundation
#include <stdlib.h>
int hmInBundle(void);
int hmInApplications(void);
int hmLoginStatus(void);
char *hmLoginSet(int enable);
*/
import "C"

import (
	"errors"
	"unsafe"
)

// LoginStatus is the state of Anti-Freeze's login item.
type LoginStatus int

const (
	LoginOff           LoginStatus = 0
	LoginOn            LoginStatus = 1
	LoginNeedsApproval LoginStatus = 2 // the user must allow it in System Settings › Login Items
	LoginUnavailable   LoginStatus = 3 // not running from an .app bundle
)

// InBundle reports whether Anti-Freeze is running from an .app bundle.
func InBundle() bool { return C.hmInBundle() == 1 }

// InApplications reports whether the bundle runs from /Applications or
// ~/Applications (not from the DMG, Downloads or a build folder).
func InApplications() bool { return C.hmInApplications() == 1 }

// LoginItemStatus reads the login item state from SMAppService.
func LoginItemStatus() LoginStatus { return LoginStatus(C.hmLoginStatus()) }

// SetLoginItem registers or unregisters Anti-Freeze as a login item.
func SetLoginItem(enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	if e := C.hmLoginSet(C.int(v)); e != nil {
		defer C.free(unsafe.Pointer(e))
		return errors.New(C.GoString(e))
	}
	return nil
}
