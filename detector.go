package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const stateFile = "/tmp/claude-monitor-state"

// Cache TTL for heavy operations (pgrep, CGWindow, lsof).
// These don't change sub-100ms, so caching reduces CPU and keeps the
// fast path (ReadHookState) responsive.
const heavyCheckTTL = 500 * time.Millisecond

// Process check cache — avoids spawning pgrep on every poll tick.
var (
	processCheckMu    sync.Mutex
	lastProcessCheck  time.Time
	lastProcessResult bool
)

// Proxy activity cache — avoids spawning lsof on every poll tick.
var (
	proxyCheckMu    sync.Mutex
	lastProxyCheck  time.Time
	lastProxyActive bool
	proxyHost       = "127.0.0.1"
	proxyPort       = "15721"
)

// Dialog window cache — avoids CGWindow enumeration on every poll tick.
var (
	dialogCheckMu    sync.Mutex
	lastDialogCheck  time.Time
	lastDialogResult bool
)

// CheckClaudeProcess checks if any claude process is running (excluding self).
// Uses ps instead of pgrep because pgrep may miss processes spawned from
// certain parent processes (e.g. IDEs) on macOS.
// Results are cached for heavyCheckTTL to avoid spawning ps on every poll.
func CheckClaudeProcess() bool {
	processCheckMu.Lock()
	defer processCheckMu.Unlock()

	if time.Since(lastProcessCheck) < heavyCheckTTL {
		return lastProcessResult
	}

	selfPID := fmt.Sprint(os.Getpid())
	data, err := exec.Command("ps", "-eo", "pid,comm=").Output()
	if err != nil {
		lastProcessResult = false
		lastProcessCheck = time.Now()
		return false
	}

	running := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, comm := fields[0], fields[1]
		if pid == selfPID {
			continue
		}
		if comm == "claude" || strings.HasPrefix(comm, "claude") && comm != "claude-monitor" {
			running = true
			break
		}
	}
	lastProcessResult = running
	lastProcessCheck = time.Now()
	return running
}

// ReadHookState reads the state written by Claude Code hooks.
// It returns the state string and whether the state is "fresh" (written
// within the last 10 seconds). A stale state means hooks are likely not
// active in the current session.
func ReadHookState() (state string, fresh bool) {
	info, err := os.Stat(stateFile)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return "", false
	}
	state = strings.TrimSpace(string(data))
	fresh = time.Since(info.ModTime()) < 10*time.Second
	return state, fresh
}

// stdin check cache — avoids spawning ps+lsof on every poll tick.
var (
	stdinCheckMu    sync.Mutex
	lastStdinCheck  time.Time
	lastStdinResult bool
)

// CheckWaitingForInput detects whether any Claude Code process is likely
// waiting for user input (e.g. AskUserQuestion). This is a fallback used
// when hooks aren't active in the current session.
//
// Heuristic: a claude process whose stdin is a TTY and is in a sleeping
// state (S) is likely blocked on a read waiting for user response.
//
// Results are cached for heavyCheckTTL.
func CheckWaitingForInput() bool {
	stdinCheckMu.Lock()
	defer stdinCheckMu.Unlock()

	if time.Since(lastStdinCheck) < heavyCheckTTL {
		return lastStdinResult
	}

	result := checkClaudeWaitingForInput()

	lastStdinCheck = time.Now()
	lastStdinResult = result
	return result
}

// checkClaudeWaitingForInput does the actual ps + lsof work.
// Must be called with stdinCheckMu held.
func checkClaudeWaitingForInput() bool {
	// Find claude PIDs (exact comm match, not claude-monitor)
	data, err := exec.Command("ps", "-eo", "pid,comm=").Output()
	if err != nil {
		return false
	}
	var claudePIDs []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[1] == "claude" {
			claudePIDs = append(claudePIDs, fields[0])
		}
	}

	for _, pid := range claudePIDs {
		// Check if stdin is a TTY (interactive session can ask questions)
		stdin, err := exec.Command("lsof", "-p", pid, "-a", "-d", "0", "-F", "n").Output()
		if err != nil {
			continue
		}
		if !strings.Contains(string(stdin), "/dev/ttys") &&
			!strings.Contains(string(stdin), "/dev/tty") &&
			!strings.Contains(string(stdin), "/dev/pts") {
			continue
		}

		// Check process state: S = sleeping (interruptible), likely
		// blocking on read when combined with TTY stdin.
		state, err := exec.Command("ps", "-p", pid, "-o", "state=").Output()
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(state))
		if strings.HasPrefix(s, "S") {
			return true
		}
	}

	return false
}

// CheckProxyActivity detects whether Claude Code has an active TCP
// connection to the API proxy. This is used as a fallback when hooks
// haven't taken effect yet (e.g. session started before hook installation).
//
// Results are cached for heavyCheckTTL to avoid spawning lsof on every
// poll tick.
func CheckProxyActivity() bool {
	proxyCheckMu.Lock()
	defer proxyCheckMu.Unlock()

	if time.Since(lastProxyCheck) < heavyCheckTTL {
		return lastProxyActive
	}

	// lsof -i TCP:15721 -s TCP:ESTABLISHED -n
	// -i TCP:PORT   → filter by port
	// -s TCP:STATE  → only established connections
	// -n            → no hostname resolution (faster)
	cmd := exec.Command("lsof",
		"-i", "TCP:"+proxyPort,
		"-s", "TCP:ESTABLISHED",
		"-n",
	)
	output, err := cmd.Output()
	active := err == nil && len(output) > 0 && strings.Contains(string(output), proxyHost)

	lastProxyCheck = time.Now()
	lastProxyActive = active

	return active
}

// WriteHooks installs hooks into ~/.claude/settings.json and
// ~/.claude/settings.local.json. It merges with existing hooks instead
// of overwriting them, and only writes to disk if something actually
// changed.
//
// Hook color semantics (6-state model):
//
//	UserPromptSubmit          → yellow (用户提交 prompt)
//	Start                     → blue   (开始输出/思考)
//	PreToolUse (all tools)    → orange (执行工具中)
//	PreToolUse (AskUserQuestion) → red  (需要用户操作)
//	PostToolUse               → blue   (工具执行完，回到工作中)
//	Stop                      → green  (完成，等待用户)
func WriteHooks() error {
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
	managedEvents := []string{"Start", "Stop", "PreToolUse", "PostToolUse", "UserPromptSubmit", "Elicitation"}

	// Remove old claude-monitor entries (containing stateFile path) from managed events
	for _, event := range managedEvents {
		entries, ok := existingHooks[event].([]any)
		if !ok {
			continue
		}
		var filtered []any
		for _, entry := range entries {
			entryJSON, _ := json.Marshal(entry)
			if !strings.Contains(string(entryJSON), stateFile) {
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
		return "echo " + color + " > " + stateFile
	}

	// New hook entries with 6-color semantics.
	// PreToolUse has two entries: one without matcher (orange, all tools)
	// and one with matcher="AskUserQuestion" (red, needs user input).
	// When AskUserQuestion fires, both run — red writes last, taking priority.
	newHooks := map[string][]any{
		"UserPromptSubmit": {map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": cmd("yellow")},
			},
		}},
		"Start": {map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": cmd("blue")},
			},
		}},
		"PreToolUse": {
			map[string]any{
				"hooks": []any{
					map[string]any{"type": "command", "command": cmd("orange")},
				},
			},
			map[string]any{
				"matcher": "AskUserQuestion",
				"hooks": []any{
					map[string]any{"type": "command", "command": cmd("red")},
				},
			},
		},
		"PostToolUse": {map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": cmd("blue")},
			},
		}},
		"Stop": {map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": cmd("green")},
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

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", settingsPath, err)
	}
	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", settingsPath, err)
	}
	return nil
}
