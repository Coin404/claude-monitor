package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const stateFileBase = "/tmp/claude-monitor-state"
const stateFilePattern = "/tmp/claude-monitor-state-*"

// ListClaudeProcesses returns PIDs of all running claude processes
// (excluding self and claude-monitor).
func ListClaudeProcesses() []int {
	selfPID := fmt.Sprint(os.Getpid())
	data, err := exec.Command("ps", "-eo", "pid,comm=").Output()
	if err != nil {
		return nil
	}

	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pidStr, comm := fields[0], fields[1]
		if pidStr == selfPID {
			continue
		}
		if comm == "claude" || (strings.HasPrefix(comm, "claude") && comm != "claude-monitor") {
			pid, err := strconv.Atoi(pidStr)
			if err == nil {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

// isProcessRunning checks whether a process with the given PID is alive.
func isProcessRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

// CleanupStaleStateFiles removes state files belonging to dead processes,
// plus the legacy state file without a PID suffix.
func CleanupStaleStateFiles() {
	// Remove legacy state file (without PID suffix) from older versions
	os.Remove(stateFileBase)

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
			os.Remove(f)
		}
	}
}

// colorPriority maps hook colors to urgency (higher = more urgent).
var colorPriority = map[string]int{
	"red":    5,
	"orange": 4,
	"yellow": 3,
	"blue":   2,
	"green":  1,
}

// sessionWorkDir returns the working directory of a process.
// Returns the last component (project name) for display, or "?" if
// the directory cannot be determined.
func sessionWorkDir(pid int) string {
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
	for _, line := range strings.Split(string(out), "\n") {
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

// WriteHooks installs hooks into ~/.claude/settings.json and
// ~/.claude/settings.local.json. It merges with existing hooks instead
// of overwriting them, and only writes to disk if something actually
// changed.
//
// Hook color semantics:
//
//	SessionStart                  → green  (会话启动，空闲等待)
//	UserPromptSubmit              → blue   (用户提交 prompt，开始思考)
//	PreToolUse (AskUserQuestion)  → red    (需要用户回答)
//	PostToolUse (AskUserQuestion) → blue   (回答完成，继续思考)
//	PreToolUse (Bash)             → red    (需要用户授权命令)
//	PostToolUse (Bash)            → blue   (授权完成，继续思考)
//	PostToolUse (generic)          → blue   (授权完成后恢复，无 matcher)
//	PermissionRequest             → red    (系统权限弹窗)
//	Stop                          → green  (完成，等待用户)
func WriteHooks() error {
	// Clean up stale state files from dead processes first
	CleanupStaleStateFiles()

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}

	// Write to both settings.json and settings.local.json so hooks
	// are present regardless of which file Claude Code reads.
	for _, name := range []string{"settings.json", "settings.local.json"} {
		if err := writeHooksTo(home + "/.claude/" + name); err != nil {
			return err
		}
	}
	return nil
}

func writeHooksTo(settingsPath string) error {
	// Read existing settings
	var settings map[string]any
	data, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", settingsPath, err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parsing %s: %w", settingsPath, err)
		}
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	// Get existing hooks or initialize
	existingHooks, _ := settings["hooks"].(map[string]any)
	if existingHooks == nil {
		existingHooks = make(map[string]any)
	}

	changed := false

	// Events managed by claude-monitor (including deprecated Elicitation
	// from older versions, matching claude-traffic-light cleanup behavior).
	managedEvents := []string{"SessionStart", "Stop", "PreToolUse", "PostToolUse", "UserPromptSubmit", "PermissionRequest", "Elicitation"}

	// Remove old claude-monitor entries (containing stateFile path) from managed events
	for _, event := range managedEvents {
		entries, ok := existingHooks[event].([]any)
		if !ok {
			continue
		}
		var filtered []any
		for _, entry := range entries {
			entryJSON, _ := json.Marshal(entry)
			if !strings.Contains(string(entryJSON), stateFileBase) {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) != len(entries) {
			changed = true
		}
		if len(filtered) > 0 {
			existingHooks[event] = filtered
		} else {
			delete(existingHooks, event)
		}
	}

	cmd := func(color string) string {
		return "echo " + color + " > " + stateFileBase + "-$PPID"
	}

	// New hook entries.
	// Color semantics (CLI Claude Code):
	//   SessionStart                  → green  (会话启动，空闲等待)
	//   UserPromptSubmit              → blue   (用户提交 prompt，开始思考)
	//   PreToolUse (AskUserQuestion)  → red    (需要用户回答)
	//   PostToolUse (AskUserQuestion) → blue   (回答完成，继续思考)
	//   PreToolUse (Bash)             → red    (需要用户授权命令)
	//   PostToolUse (Bash)            → blue   (授权完成，继续思考)
	//   PostToolUse (generic)          → blue   (授权完成后恢复，无 matcher)
	//   PermissionRequest             → red    (系统权限弹窗)
	//   Stop                          → green  (完成，等待用户)
	newHooks := map[string][]any{
		"UserPromptSubmit": {map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": cmd("blue"),
				},
			},
		}},
		"SessionStart": {map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": cmd("green"),
				},
			},
		}},
		"PreToolUse": {
			map[string]any{
				"matcher": "AskUserQuestion",
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": cmd("red"),
					},
				},
			},
			map[string]any{
				"matcher": "Bash",
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": cmd("red"),
					},
				},
			},
		},
		"PostToolUse": {
			map[string]any{
				"matcher": "AskUserQuestion",
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": cmd("blue"),
					},
				},
			},
			map[string]any{
				"matcher": "Bash",
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": cmd("blue"),
					},
				},
			},
			map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": cmd("blue"),
					},
				},
			},
		},
		"PermissionRequest": {map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": cmd("red"),
				},
			},
		}},
		"Stop": {map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": cmd("green"),
				},
			},
		}},
	}

	// Append new entries to each event
	for event, entries := range newHooks {
		existing, _ := existingHooks[event].([]any)
		existingHooks[event] = append(existing, entries...)
		changed = true
	}

	if !changed {
		return nil
	}

	settings["hooks"] = existingHooks

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return fmt.Errorf("marshaling %s: %w", settingsPath, err)
	}
	// Encode adds a trailing newline; trim it for clean files
	out := bytes.TrimRight(buf.Bytes(), "\n")
	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", settingsPath, err)
	}
	return nil
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

	for depth := 0; depth < 10; depth++ {
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

// ActivateTerminal uses osascript to bring the host application to the
// foreground. If projectDir is not empty, it first tries to use
// "open -a AppName projectDir" which is the most reliable way to
// focus the correct window in multi-window IDEs.
func ActivateTerminal(appName, projectDir string) error {
	// Best effort: use "open -a" with project directory to focus the
	// specific project window in multi-window IDEs
	if projectDir != "" && appName != "" {
		_, err := exec.Command("open", "-a", appName, projectDir).Output()
		if err == nil {
			return nil
		}
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

// FirstClaudePID returns the first running claude process PID, or 0.
func FirstClaudePID() int {
	pids := ListClaudeProcesses()
	if len(pids) == 0 {
		return 0
	}
	return pids[0]
}

// findBlockedPID returns the PID of the session that is currently in
// "red" (blocked) state, or 0 if none is blocked.
func findBlockedPID() int {
	sessions := ListClaudeSessions()
	for pid, color := range sessions {
		if color == "red" {
			return pid
		}
	}
	return 0
}
