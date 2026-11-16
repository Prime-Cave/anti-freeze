//go:build darwin

#import <AppKit/AppKit.h>
#include <string.h>
#include <stdbool.h>

static NSButton *htFindButton(NSView *v) {
    if (v == nil) return nil;
    if ([v isKindOfClass:[NSButton class]]) return (NSButton *)v;
    for (NSView *sub in v.subviews) {
        NSButton *b = htFindButton(sub);
        if (b != nil) return b;
    }
    return nil;
}

static NSButton *htStatusButton(void) {
    for (NSWindow *w in [NSApp windows]) {
        if ([NSStringFromClass([w class]) isEqualToString:@"NSStatusBarWindow"]) {
            NSButton *b = htFindButton(w.contentView);
            if (b != nil) return b;
        }
    }
    return nil;
}

void htSetStatusIcon(const void *light, int lightLen, const void *dark, int darkLen, bool isTemplate, const char *label) {
    NSButton *button = htStatusButton();
    if (button == nil) return;

    NSAppearanceName match = [button.effectiveAppearance
        bestMatchFromAppearancesWithNames:@[ NSAppearanceNameAqua, NSAppearanceNameDarkAqua ]];
    BOOL isDark = [match isEqualToString:NSAppearanceNameDarkAqua];

    NSData *data = isDark ? [NSData dataWithBytes:dark length:(NSUInteger)darkLen]
                          : [NSData dataWithBytes:light length:(NSUInteger)lightLen];
    NSImage *image = [[NSImage alloc] initWithData:data];
    if (image == nil) return;
    image.size = NSMakeSize(20, 16);
    image.template = isTemplate ? YES : NO;

    NSString *text = [NSString stringWithUTF8String:label];
    image.accessibilityDescription = text;
    button.image = image;
    button.imagePosition = NSImageOnly;
    button.toolTip = text;
    [button setAccessibilityLabel:text];
}

// htInspectStatusItem reports what VoiceOver and the menu bar see, for the
// app's --selftest mode. Returns a malloc'd C string the caller frees.
char *htInspectStatusItem(void) {
    NSButton *button = htStatusButton();
    if (button == nil) return strdup("{\"found\":false}");
    NSImage *img = button.image;
    NSString *json = [NSString stringWithFormat:
        @"{\"found\":true,\"label\":\"%@\",\"template\":%s,\"width\":%.0f,\"height\":%.0f}",
        [button accessibilityLabel] ?: @"",
        img.isTemplate ? "true" : "false", img.size.width, img.size.height];
    return strdup(json.UTF8String);
}
