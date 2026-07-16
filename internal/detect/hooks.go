package detect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

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
