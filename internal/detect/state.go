package detect

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const stateFileBase = "/tmp/claude-monitor-state"
const stateFilePattern = "/tmp/claude-monitor-state-*"

// colorPriority maps hook colors to urgency (higher = more urgent).
var colorPriority = map[string]int{
	"red":    5,
	"orange": 4,
	"yellow": 3,
	"blue":   2,
	"green":  1,
}

// SessionWorkDir returns the working directory of a process.
// Returns the last component (project name) for display, or "?" if
// the directory cannot be determined.
func SessionWorkDir(pid int) string {
	dir := sessionWorkPath(pid)
	if dir == "" {
		return "?"
	}
	return filepath.Base(dir)
}

// sessionWorkPath returns the full working directory path of a process,
// or empty string if the directory cannot be determined.
func sessionWorkPath(pid int) string {
	out, err := exec.Command("lsof", "-p", strconv.Itoa(pid), "-a", "-d", "cwd", "-Fn").Output()
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:]
		}
	}
	return ""
}

// sessionColor reads the state file for a PID and returns its color.
// If the state file is stale (>10s) or missing, returns "green" — an
// idle session is the safe default when we can't determine state.
func sessionColor(pid int) string {
	f := stateFileBase + "-" + strconv.Itoa(pid)
	info, err := os.Stat(f)
	if err != nil {
		return "green"
	}
	if time.Since(info.ModTime()) >= 10*time.Second {
		return "green"
	}
	data, err := os.ReadFile(f)
	if err != nil {
		return "green"
	}
	color := strings.TrimSpace(string(data))
	if color == "" {
		return "green"
	}
	return color
}

// CleanupStaleStateFiles removes state files belonging to dead processes,
// plus the legacy state file without a PID suffix.
func CleanupStaleStateFiles() {
	// Remove legacy state file (without PID suffix) from older versions
	_ = os.Remove(stateFileBase)

	// Collect running PIDs
	running := make(map[int]bool)
	for _, pid := range ListClaudeProcesses() {
		running[pid] = true
	}

	files, err := filepath.Glob(stateFilePattern)
	if err != nil {
		return
	}
	for _, f := range files {
		pidStr := strings.TrimPrefix(filepath.Base(f), "claude-monitor-state-")
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if !running[pid] {
			_ = os.Remove(f)
		}
	}
}

// ReadHookState reads all per-session state files written by Claude Code
// hooks. It filters out files belonging to dead processes and returns the
// most urgent state across all active sessions.
//
// Freshness: a state file is "fresh" if updated within 10 seconds. If all
// state files are stale (>10s) but the processes are still running, the
// most recent stale value is still used — no new hook means no state change.
// The "fresh" flag indicates whether the returned state is from a recent
// (<10s) update.
func ReadHookState(runningPIDs []int) (state string, fresh bool) {
	pidSet := make(map[int]bool, len(runningPIDs))
	for _, pid := range runningPIDs {
		pidSet[pid] = true
	}

	files, err := filepath.Glob(stateFilePattern)
	if err != nil || len(files) == 0 {
		return "", false
	}

	bestState := ""
	bestPriority := 0
	bestFresh := false

	for _, f := range files {
		base := filepath.Base(f)
		pidStr := strings.TrimPrefix(base, "claude-monitor-state-")
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		// Skip state files belonging to dead processes
		if !pidSet[pid] {
			continue
		}
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		fileFresh := time.Since(info.ModTime()) < 10*time.Second
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(data))
		p := colorPriority[s]
		if p > bestPriority {
			bestPriority = p
			bestState = s
			bestFresh = fileFresh
		}
	}

	if bestPriority == 0 {
		return "", false
	}
	return bestState, bestFresh
}

// ListClaudeSessions returns a map of PID → color for all running
// claude sessions. The process list (ps) is the source of truth;
// state files only provide the color, defaulting to "green" when
// unavailable or stale.
func ListClaudeSessions() map[int]string {
	pids := ListClaudeProcesses()
	result := make(map[int]string, len(pids))
	for _, pid := range pids {
		result[pid] = sessionColor(pid)
	}
	return result
}

// ReadSessionState reads the state file for a specific PID and returns
// the color string and whether the state is fresh (updated within 10s).
// If the state file is missing and the process isn't running, it returns
// ("", false). If the file is stale or missing but the process is still
// alive, it returns the last known color (or "green" as safe default).
func ReadSessionState(pid int) (string, bool) {
	f := stateFileBase + "-" + strconv.Itoa(pid)
	info, err := os.Stat(f)
	if err != nil {
		if isProcessRunning(pid) {
			return "green", true
		}
		return "", false
	}
	data, err := os.ReadFile(f)
	if err != nil {
		if isProcessRunning(pid) {
			return "green", true
		}
		return "", false
	}
	color := strings.TrimSpace(string(data))
	if color == "" {
		if isProcessRunning(pid) {
			return "green", true
		}
		return "", false
	}
	fresh := time.Since(info.ModTime()) < 10*time.Second
	if !isProcessRunning(pid) && !fresh {
		return "", false
	}
	// Process is running: trust the last-known color even if stale.
	// No new hook event means the state hasn't changed.
	return color, true
}
