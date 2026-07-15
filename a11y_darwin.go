package main

/*
#cgo LDFLAGS: -framework CoreGraphics
#include "a11y_bridge.h"
#include <stdlib.h>
*/
import "C"
import (
	"strings"
	"time"
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
	dialogCheckMu.Lock()
	defer dialogCheckMu.Unlock()

	if time.Since(lastDialogCheck) < heavyCheckTTL {
		return lastDialogResult
	}

	owners, err := GetWindowOwners()
	result := err == nil && owners != "" && strings.TrimSpace(owners) != ""

	lastDialogCheck = time.Now()
	lastDialogResult = result
	return result
}
