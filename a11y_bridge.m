#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import "a11y_bridge.h"

char* getWindowOwners(void) {
    NSMutableSet *owners = [NSMutableSet set];

    // Baseline processes that always have non-dialog windows at these layers
    NSSet *baseline = [NSSet setWithObjects:
        @"Window Server", @"控制中心", @"程序坞", @"Dock", nil];

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

        // Only dialog-level windows (above normal, below menubar)
        if (layer <= 0 || layer > 100) continue;

        NSString *owner = (__bridge NSString *)ownerName;
        if ([baseline containsObject:owner]) continue;

        [owners addObject:owner];
    }

    CFRelease(windowList);

    NSString *joined = [[owners allObjects] componentsJoinedByString:@"\n"];
    return strdup([joined UTF8String]);
}
