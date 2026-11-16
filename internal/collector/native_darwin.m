//go:build darwin

#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#include "native_darwin.h"
#include <string.h>
#include <sys/sysctl.h>

int ht_disk(uint64_t *total, uint64_t *important, uint64_t *raw) {
    @autoreleasepool {
        NSURL *root = [NSURL fileURLWithPath:@"/" isDirectory:YES];
        NSError *err = nil;
        NSDictionary *v = [root resourceValuesForKeys:@[
            NSURLVolumeTotalCapacityKey,
            NSURLVolumeAvailableCapacityForImportantUsageKey,
            NSURLVolumeAvailableCapacityKey
        ] error:&err];
        if (v == nil) return -1;
        NSNumber *t = v[NSURLVolumeTotalCapacityKey];
        NSNumber *i = v[NSURLVolumeAvailableCapacityForImportantUsageKey];
        NSNumber *r = v[NSURLVolumeAvailableCapacityKey];
        if (t == nil || i == nil) return -2;
        *total = t.unsignedLongLongValue;
        *important = i.unsignedLongLongValue;
        *raw = r ? r.unsignedLongLongValue : 0;
        return 0;
    }
}

int ht_thermal(void) {
    return (int)[[NSProcessInfo processInfo] thermalState];
}

static char *ht_json(id obj) {
    NSData *d = [NSJSONSerialization dataWithJSONObject:obj options:0 error:nil];
    if (d == nil) return strdup("null");
    char *s = malloc(d.length + 1);
    memcpy(s, d.bytes, d.length);
    s[d.length] = 0;
    return s;
}

char *ht_running_apps_json(void) {
    @autoreleasepool {
        NSMutableArray *out = [NSMutableArray array];
        for (NSRunningApplication *a in [[NSWorkspace sharedWorkspace] runningApplications]) {
            [out addObject:@{
                @"pid" : @(a.processIdentifier),
                @"name" : a.localizedName ?: @"",
                @"bundleId" : a.bundleIdentifier ?: @"",
                @"bundlePath" : a.bundleURL.path ?: @"",
                @"regular" : @(a.activationPolicy == NSApplicationActivationPolicyRegular),
            }];
        }
        return ht_json(out);
    }
}

char *ht_machine_json(void) {
    @autoreleasepool {
        NSProcessInfo *pi = [NSProcessInfo processInfo];
        NSOperatingSystemVersion v = pi.operatingSystemVersion;
        return ht_json(@{
            @"osVersion" : [NSString stringWithFormat:@"%ld.%ld.%ld", (long)v.majorVersion,
                                                      (long)v.minorVersion, (long)v.patchVersion],
            @"cores" : @(pi.processorCount),
            @"physicalMemory" : @(pi.physicalMemory),
        });
    }
}
