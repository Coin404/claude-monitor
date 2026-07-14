package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

var permissionKeywords = []string{
	"would like to",
	"permission",
	"允许",
	"访问",
}

func CheckPermissionDialog() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Use a fixed AppleScript: get process names first, then access each
	// individually with a try block to handle invalid/missing processes.
	script := `
tell application "System Events"
	set permWindows to {}
	try
		set procNames to name of every process
	on error
		return {}
	end try
	repeat with procName in procNames
		try
			tell process procName
				repeat with w in (every window)
					set wTitle to title of w
					if wTitle is not "" then
						copy wTitle to end of permWindows
					end if
				end repeat
			end tell
		end try
	end repeat
	return permWindows
end tell
`
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}

	output := strings.ToLower(string(out))
	for _, kw := range permissionKeywords {
		if strings.Contains(output, strings.ToLower(kw)) {
			return true, nil
		}
	}

	return false, nil
}
