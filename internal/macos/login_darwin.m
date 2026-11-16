// Start at login with SMAppService.mainApp (macOS 13+). Only works when
// running from an .app bundle; there is no LaunchAgent fallback.
#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>
#include <stdlib.h>
#include <string.h>

int hmInBundle(void) {
    NSBundle *b = [NSBundle mainBundle];
    return (b.bundleIdentifier != nil && [b.bundlePath hasSuffix:@".app"]) ? 1 : 0;
}

// 1 if the bundle is in /Applications or ~/Applications.
int hmInApplications(void) {
    if (!hmInBundle()) return 0;
    NSString *p = [NSBundle mainBundle].bundlePath.stringByStandardizingPath;
    NSString *user = [NSHomeDirectory() stringByAppendingPathComponent:@"Applications/"];
    return ([p hasPrefix:@"/Applications/"] || [p hasPrefix:user]) ? 1 : 0;
}

// 0 not registered, 1 enabled, 2 needs approval in System Settings,
// 3 unavailable (not in a bundle). macOS can report "not found" for a main
// app that was never registered; inside a bundle that just means off.
int hmLoginStatus(void) {
    if (!hmInBundle()) return 3;
    switch (SMAppService.mainAppService.status) {
        case SMAppServiceStatusEnabled: return 1;
        case SMAppServiceStatusRequiresApproval: return 2;
        default: return 0;
    }
}

// Returns NULL on success, or a malloc'd error message.
char *hmLoginSet(int enable) {
    if (!hmInBundle()) return strdup("Start at login works when Headroom runs from the Applications folder.");
    NSError *err = nil;
    BOOL ok = enable ? [SMAppService.mainAppService registerAndReturnError:&err]
                     : [SMAppService.mainAppService unregisterAndReturnError:&err];
    if (ok) return NULL;
    return strdup(err.localizedDescription.UTF8String ?: "unknown error");
}
