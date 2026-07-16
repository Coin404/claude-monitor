package detect

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

int activateAppSmoothly(const char *processName) {
	// Ensure our own process stays as an accessory so it doesn't
	// leave a ghost Dock icon when we activate another app.
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

	NSString *name = [NSString stringWithUTF8String:processName];
	NSArray<NSRunningApplication *> *apps = [[NSWorkspace sharedWorkspace] runningApplications];
	for (NSRunningApplication *app in apps) {
		if ([app.localizedName isEqualToString:name]) {
			// NSApplicationActivateAllWindows (1) gives the smooth
			// macOS Space transition animation.
			[app activateWithOptions:NSApplicationActivateAllWindows];
			return 1;
		}
	}
	return 0;
}
*/
import "C"
import "unsafe"

func activateAppSmoothly(processName string) bool {
	cName := C.CString(processName)
	defer C.free(unsafe.Pointer(cName))
	return C.activateAppSmoothly(cName) == 1
}
