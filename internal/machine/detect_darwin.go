//go:build darwin

package machine

/*
#cgo CFLAGS: -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <string.h>

// Reads IODeviceTree:/product "product-name" (Apple Silicon), e.g.
// "MacBook Air (M2, 2022)". Returns 0 on success.
static int ht_product_name(char *buf, int len) {
    io_registry_entry_t e = IORegistryEntryFromPath(kIOMainPortDefault, "IODeviceTree:/product");
    if (e == MACH_PORT_NULL) return -1;
    CFTypeRef v = IORegistryEntryCreateCFProperty(e, CFSTR("product-name"), kCFAllocatorDefault, 0);
    IOObjectRelease(e);
    if (v == NULL) return -1;
    int rc = -1;
    if (CFGetTypeID(v) == CFDataGetTypeID()) {
        CFIndex n = CFDataGetLength((CFDataRef)v);
        if (n > len - 1) n = len - 1;
        memcpy(buf, CFDataGetBytePtr((CFDataRef)v), n);
        buf[n] = 0;
        rc = 0;
    }
    CFRelease(v);
    return rc;
}
*/
import "C"

import (
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/Prime-Cave/headroom/internal/collector"
)

// Detect reads the current machine's profile from public APIs.
func Detect(c collector.Collector) Profile {
	p := Profile{Arch: runtime.GOARCH, FirstSeen: time.Now()}
	p.Model, _ = collector.SysctlString("hw.model")
	p.Chip, _ = collector.SysctlString("machdep.cpu.brand_string")
	p.MemoryBytes, _ = collector.SysctlUint("hw.memsize")
	h := collector.Host()
	p.OSVersion, p.Cores = h.OSVersion, h.Cores

	buf := make([]byte, 128)
	if C.ht_product_name((*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))) == 0 {
		name := C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
		// "MacBook Air (M2, 2022)" → "MacBook Air"
		if i := strings.Index(name, " ("); i > 0 {
			name = name[:i]
		}
		p.ModelName = name
	}
	if p.ModelName == "" {
		p.ModelName = FriendlyName(p.Model)
	}
	if s, err := c.System(); err == nil {
		p.DiskBytes = s.Disk.Total
	}
	return p
}
