//go:build darwin

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// Graceful quit: the app gets a normal quit request and can ask to save.
// Returns 1 if the request was delivered.
int hmTerminate(int pid) {
    @autoreleasepool {
        NSRunningApplication *a = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        if (a == nil) return 0;
        return [a terminate] ? 1 : 0;
    }
}

// Force quit: only reachable through a separate, twice-confirmed option.
int hmForceTerminate(int pid) {
    @autoreleasepool {
        NSRunningApplication *a = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        if (a == nil) return 0;
        return [a forceTerminate] ? 1 : 0;
    }
}

int hmIsRunning(int pid) {
    @autoreleasepool {
        NSRunningApplication *a = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        return (a != nil && !a.terminated) ? 1 : 0;
    }
}

// Moves one item to the Trash. Never deletes. Returns NULL on success or a
// malloc'd error description.
char *hmTrash(const char *path) {
    @autoreleasepool {
        NSURL *u = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
        NSError *err = nil;
        if ([[NSFileManager defaultManager] trashItemAtURL:u resultingItemURL:nil error:&err]) return NULL;
        return strdup((err.localizedDescription ?: @"The item could not be moved to the Trash.").UTF8String);
    }
}

void hmReveal(const char *path) {
    @autoreleasepool {
        NSURL *u = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
        [[NSWorkspace sharedWorkspace] activateFileViewerSelectingURLs:@[ u ]];
    }
}

// Opens a file URL (a folder in Finder) or an x-apple.systempreferences URL.
int hmOpenURL(const char *url) {
    @autoreleasepool {
        NSString *s = [NSString stringWithUTF8String:url];
        NSURL *u = [s hasPrefix:@"/"] ? [NSURL fileURLWithPath:s] : [NSURL URLWithString:s];
        if (u == nil) return 0;
        return [[NSWorkspace sharedWorkspace] openURL:u] ? 1 : 0;
    }
}

// PNG of an app's icon at 32×32 px. Caller frees *out.
int hmAppIconPNG(const char *path, unsigned char **out, int *len) {
    @autoreleasepool {
        NSImage *icon = [[NSWorkspace sharedWorkspace] iconForFile:[NSString stringWithUTF8String:path]];
        if (icon == nil) return 0;
        NSBitmapImageRep *rep = [[NSBitmapImageRep alloc]
            initWithBitmapDataPlanes:NULL pixelsWide:40 pixelsHigh:40 bitsPerSample:8 samplesPerPixel:4
            hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:0 bitsPerPixel:0];
        if (rep == nil) return 0;
        [NSGraphicsContext saveGraphicsState];
        [NSGraphicsContext setCurrentContext:[NSGraphicsContext graphicsContextWithBitmapImageRep:rep]];
        [icon drawInRect:NSMakeRect(0, 0, 40, 40) fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1.0];
        [NSGraphicsContext restoreGraphicsState];
        NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        if (png == nil) return 0;
        *out = malloc(png.length);
        memcpy(*out, png.bytes, png.length);
        *len = (int)png.length;
        return 1;
    }
}
