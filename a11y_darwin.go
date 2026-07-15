package main

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include "a11y_bridge.h"
#include <stdlib.h>
*/
import "C"
import (
	"strings"
	"unsafe"
)

func GetWindowOwners() (string, error) {
	cstr := C.getWindowOwners()
	if cstr == nil {
		return "", nil
	}
	defer C.free(unsafe.Pointer(cstr))
	return C.GoString(cstr), nil
}

func HasDialogWindow() bool {
	owners, err := GetWindowOwners()
	return err == nil && owners != "" && strings.TrimSpace(owners) != ""
}

func HasAccessibilityPermission() bool {
	return bool(C.hasAccessibilityPermission())
}

func RequestAccessibilityPermission() {
	C.requestAccessibilityPermission()
}
