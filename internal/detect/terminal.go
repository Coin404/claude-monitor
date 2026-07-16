package detect

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FindTerminalApp traces the parent process chain of the given PID to
// find the GUI application hosting Claude. It skips shells and system
// processes, returning the first ancestor that looks like a GUI app.
// Returns the process name suitable for AppleScript (e.g. "goland",
// "Terminal", "iTerm2"), or empty string if nothing found.
func FindTerminalApp(pid int) string {
	shells := map[string]bool{
		"bash": true, "zsh": true, "sh": true, "fish": true,
		"dash": true, "tcsh": true, "csh": true, "ksh": true,
	}
	skip := map[string]bool{
		"launchd": true, "login": true, "screen": true, "tmux": true,
	}

	// Move to parent first (skip the claude process itself)
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || ppid <= 1 {
		return ""
	}
	pid = ppid

	for range 10 {
		out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return ""
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) < 2 {
			return ""
		}
		ppid, err := strconv.Atoi(fields[0])
		if err != nil {
			return ""
		}
		comm := filepath.Base(fields[1])

		if !shells[comm] && !skip[comm] {
			return comm
		}
		if ppid <= 1 || ppid == pid {
			return ""
		}
		pid = ppid
	}
	return ""
}

// ActivateTerminal brings the host terminal/IDE application to the
// foreground. It first tries the native NSRunningApplication API which
// provides a smooth macOS Space transition animation. Falls back to
// osascript methods if native activation is unavailable.
func ActivateTerminal(appName string) error {
	// Primary: native NSRunningApplication activation with smooth
	// Space transition animation (no abrupt jump).
	if appName != "" && activateAppSmoothly(appName) {
		return nil
	}

	// Fallback 1: set frontmost via System Events
	tryFrontmost := func(name string) bool {
		script := fmt.Sprintf(
			`tell application "System Events" to set frontmost of process "%s" to true`,
			name,
		)
		return exec.Command("osascript", "-e", script).Run() == nil
	}
	tryActivate := func(name string) bool {
		script := fmt.Sprintf(`tell application "%s" to activate`, name)
		return exec.Command("osascript", "-e", script).Run() == nil
	}

	if appName != "" {
		if tryFrontmost(appName) || tryActivate(appName) {
			return nil
		}
	}

	// Fallback 2: try common terminals
	for _, name := range []string{"Terminal", "iTerm2", "Warp", "kitty", "WezTerm", "Alacritty"} {
		if tryFrontmost(name) || tryActivate(name) {
			return nil
		}
	}
	return fmt.Errorf("could not activate any terminal app")
}
