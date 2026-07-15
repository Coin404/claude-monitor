#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <ApplicationServices/ApplicationServices.h>
#import "a11y_bridge.h"

bool hasAccessibilityPermission(void) {
    NSDictionary *options = @{(__bridge id)kAXTrustedCheckOptionPrompt: @NO};
    return AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)options);
}

void requestAccessibilityPermission(void) {
    NSDictionary *options = @{(__bridge id)kAXTrustedCheckOptionPrompt: @YES};
    AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)options);
}

char* getWindowOwners(void) {
    NSMutableSet *owners = [NSMutableSet set];

    // System processes that always have windows at dialog layers.
    NSSet *baseline = [NSSet setWithObjects:
        @"Window Server", @"控制中心", @"程序坞", @"Dock",
        @"通知中心", @"Dynamic Wallpaper", @"访达", nil];

    // Processes considered Claude-related for dialog detection.
    // "claude" matches the CLI binary; SecurityAgent / UserNotificationCenter
    // catch macOS permission dialogs triggered by any app (including Claude).
    NSSet *claudeRelated = [NSSet setWithObjects:
        @"claude", @"SecurityAgent", @"UserNotificationCenter", nil];

    CFArrayRef windowList = CGWindowListCopyWindowInfo(
        kCGWindowListOptionOnScreenOnly,
        kCGNullWindowID);

    if (!windowList) {
        return strdup("");
    }

    CFIndex count = CFArrayGetCount(windowList);
    for (CFIndex i = 0; i < count; i++) {
        CFDictionaryRef win = (CFDictionaryRef)CFArrayGetValueAtIndex(windowList, i);

        CFStringRef ownerName = CFDictionaryGetValue(win, kCGWindowOwnerName);
        if (!ownerName) continue;

        CFNumberRef layerRef = CFDictionaryGetValue(win, kCGWindowLayer);
        int layer = 0;
        if (layerRef) CFNumberGetValue(layerRef, kCFNumberIntType, &layer);

        NSString *owner = (__bridge NSString *)ownerName;
        if ([baseline containsObject:owner]) continue;

        // Only dialog-layer windows: above normal (0) and below menubar (24+)
        if (layer <= 0 || layer > 100) continue;

        // Only flag dialogs from Claude-related processes to avoid false
        // positives from unrelated apps (IDEs, browsers, etc.).
        NSString *lower = [owner lowercaseString];
        BOOL matched = NO;
        for (NSString *candidate in claudeRelated) {
            if ([lower isEqualToString:candidate]) {
                matched = YES;
                break;
            }
        }
        if (!matched) continue;

        [owners addObject:owner];
    }

    CFRelease(windowList);

    NSString *joined = [[owners allObjects] componentsJoinedByString:@"\n"];
    return strdup([joined UTF8String]);
}
