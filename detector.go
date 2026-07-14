package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const stateFile = "/tmp/claude-monitor-state"

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
func ReadHookState() string {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// WriteHooks installs hooks into ~/.claude/settings.json.
// It merges with existing hooks instead of overwriting them, and only
// writes to disk if something actually changed.
//
// Hook color semantics (aligned with claude-traffic-light conventions):
//
//	Start             → yellow (Claude begins output)
//	UserPromptSubmit  → yellow (user submitted, about to begin)
//	Stop              → green  (Claude finished, user can continue)
//	PreToolUse+AskUserQuestion → red (needs user input)
func WriteHooks() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}
	settingsPath := home + "/.claude/settings.json"

	// Read existing settings
	var settings map[string]any
	data, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading settings.json: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parsing settings.json: %w", err)
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
	managedEvents := []string{"Start", "Stop", "PreToolUse", "UserPromptSubmit", "Elicitation"}

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

	// New hook entries with correct color semantics
	newHooks := map[string][]any{
		"Start": {map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": cmd("yellow")},
			},
		}},
		"UserPromptSubmit": {map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": cmd("yellow")},
			},
		}},
		"Stop": {map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": cmd("green")},
			},
		}},
		"PreToolUse": {map[string]any{
			"matcher": "AskUserQuestion",
			"hooks": []any{
				map[string]any{"type": "command", "command": cmd("red")},
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
		return fmt.Errorf("marshaling settings.json: %w", err)
	}
	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return fmt.Errorf("writing settings.json: %w", err)
	}
	return nil
}
