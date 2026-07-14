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
// Hooks write color to stateFile on lifecycle events:
//   UserPromptSubmit → green (user submitted, claude starts working)
//   Stop → red (claude finished, waiting for user)
//   PreToolUse+AskUserQuestion → yellow (needs user input)
func WriteHooks() {
	home, _ := os.UserHomeDir()
	settingsPath := home + "/.claude/settings.json"

	// Read existing settings
	var settings map[string]any
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	cmd := func(color string) string {
		return "echo " + color + " > " + stateFile
	}

	hooks := map[string]any{
		"UserPromptSubmit": []any{
			map[string]any{
				"hooks": []any{
					map[string]any{"type": "command", "command": cmd("green")},
				},
			},
		},
		"Stop": []any{
			map[string]any{
				"hooks": []any{
					map[string]any{"type": "command", "command": cmd("red")},
				},
			},
		},
		"PreToolUse": []any{
			map[string]any{
				"matcher": "AskUserQuestion",
				"hooks": []any{
					map[string]any{"type": "command", "command": cmd("yellow")},
				},
			},
		},
	}

	settings["hooks"] = hooks

	out, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(settingsPath, out, 0644)
}
