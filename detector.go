package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const stateFile = "/tmp/claude-monitor-state"

var (
	proxyHost = "127.0.0.1"
	proxyPort = "15721"
)

// CheckClaudeProcess checks if any claude process is running (excluding self
// and claude-monitor). Uses ps for broad process discovery.
func CheckClaudeProcess() bool {
	selfPID := fmt.Sprint(os.Getpid())
	data, err := exec.Command("ps", "-eo", "pid,comm=").Output()
	if err != nil {
		return false
	}

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
			return true
		}
	}
	return false
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
func CheckProxyActivity() bool {
	cmd := exec.Command("lsof",
		"-i", "TCP:"+proxyPort,
		"-s", "TCP:ESTABLISHED",
		"-n",
	)
	output, err := cmd.Output()
	return err == nil && len(output) > 0 && strings.Contains(string(output), proxyHost)
}

// WriteHooks installs hooks into ~/.claude/settings.json and
// ~/.claude/settings.local.json. It merges with existing hooks instead
// of overwriting them, and only writes to disk if something actually
// changed.
//
// Hook color semantics:
//
//	UserPromptSubmit          → yellow (用户提交 prompt)
//	SessionStart              → orange (开始输出)
//	PreToolUse (AskUserQuestion) → red  (需要用户操作)
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
	managedEvents := []string{"SessionStart", "Stop", "PreToolUse", "UserPromptSubmit", "Elicitation"}

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

	// New hook entries matching traffic-light's hook model.
	// PreToolUse only matches AskUserQuestion (red); normal tool execution
	// does not change state — the previous color (orange/yellow) persists.
	newHooks := map[string][]any{
		"UserPromptSubmit": {map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": cmd("yellow"),
				},
			},
		}},
		"SessionStart": {map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": cmd("orange"),
				},
			},
		}},
		"PreToolUse": {map[string]any{
			"matcher": "AskUserQuestion",
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
