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

// Proxy activity cache — lsof can be slow, so we cache the result briefly.
var (
	proxyCheckMu     sync.Mutex
	lastProxyCheck   time.Time
	lastProxyActive  bool
	proxyCheckTTL    = 500 * time.Millisecond
	proxyHost        = "127.0.0.1"
	proxyPort        = "15721"
)

// CheckClaudeProcess checks if any claude process is running (excluding self).
func CheckClaudeProcess() (bool, error) {
	data, err := exec.Command("pgrep", "-f", "claude").Output()
	if err != nil {
		return false, nil
	}
	selfPID := fmt.Sprint(os.Getpid())
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		pid := strings.TrimSpace(line)
		if pid != "" && pid != selfPID {
			return true, nil
		}
	}
	return false, nil
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

// CheckProxyActivity detects whether Claude Code has an active TCP
// connection to the API proxy. This is used as a fallback when hooks
// haven't taken effect yet (e.g. session started before hook installation).
//
// Results are cached for proxyCheckTTL to avoid spawning lsof on every
// poll tick (50ms).
func CheckProxyActivity() bool {
	proxyCheckMu.Lock()
	defer proxyCheckMu.Unlock()

	if time.Since(lastProxyCheck) < proxyCheckTTL {
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
