# claude-monitor Development Notes

## Project overview
- claude-monitor is a macOS tray icon app that shows Claude Code's real-time status via colored dots
- claude-traffic-light at `/Users/xuyi/Desktop/Coin/Personal/Code/Claude-Flash/claude-traffic-light/electron/main.cjs` is the reference implementation
- Both projects manage Claude Code hooks in `~/.claude/settings.json` and `~/.claude/settings.local.json`

## Hook format (CRITICAL)

Claude Code hooks use a specific nested format. Each event entry MUST have a `hooks` array wrapper:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo orange > /tmp/claude-monitor-state"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "AskUserQuestion",
        "hooks": [
          {
            "type": "command",
            "command": "echo red > /tmp/claude-monitor-state"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo green > /tmp/claude-monitor-state"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo yellow > /tmp/claude-monitor-state"
          }
        ]
      }
    ]
  }
}
```

Key rules:
- **Every entry must have `"hooks": [...]` wrapper** — flat `{"type":"command","command":"..."}` causes "Expected array, but received undefined" error
- **`matcher` is optional** — omit it entirely (don't use empty string) for "match all" entries. Only add `"matcher": "ToolName"` when targeting a specific tool
- **`Start` is deprecated** — use `SessionStart` instead. `Start` causes "Invalid key in record" error on newer Claude Code versions
- **Entire settings.json is skipped if any hook has errors** — one bad hook breaks everything

## Hook execution order and PreToolUse pitfall

**CRITICAL**: When multiple PreToolUse entries match the same tool, the less specific one can overwrite the more specific one:
- Having both a generic PreToolUse (no matcher, matches all tools) AND a specific one (`matcher: "AskUserQuestion"`) causes the generic to overwrite the specific → AskUserQuestion shows wrong color
- **Solution**: Only have the AskUserQuestion matcher in PreToolUse. Do NOT add a generic PreToolUse entry. Follow traffic-light's pattern.

## Valid hook events (current Claude Code)
- `SessionStart` — session begins (NOT `Start`)
- `Stop` — Claude finishes responding
- `UserPromptSubmit` — user submits a prompt
- `PreToolUse` — before a tool executes (with optional matcher)
- `PostToolUse` — after a tool succeeds
- `PostToolUseFailure` — after a tool fails
- `Notification` — Claude sends a notification
- `SessionEnd` — session ends
- `SubagentStart` / `SubagentStop` — subagent lifecycle
- `PreCompact` — before context compaction
- `PermissionRequest` — permission dialog shown

## Color state machine (current)

### Tray icon states
| 颜色 | 状态 | 含义 |
|------|------|------|
| 灰   | 未运行 | Claude Code 没有打开 |
| 绿   | 空闲 | 完成任务/等待用户输入 |
| 蓝   | 思考中 | Claude 正在思考/生成输出 |
| 红   | 等待确认 | 弹出询问权限/确认方案（AskUserQuestion、PermissionRequest）|

### Hook color mapping
```
SessionStart                  → green  (会话启动，空闲等待)
UserPromptSubmit              → blue   (用户提交 prompt，开始思考)
PreToolUse(AskUserQuestion)   → red    (等待用户回答)
PostToolUse(AskUserQuestion)  → blue   (回答完成，继续思考)
PermissionRequest             → red    (等待用户授权)
Stop                          → green  (完成，等待用户)
```

### 为什么没有黄色（工具调用）
通用 PreToolUse/PostToolUse（无 matcher）会与 AskUserQuestion 的红色冲突：
- PreToolUse 无 matcher → yellow 会覆盖 AskUserQuestion → red
- PostToolUse 无 matcher → yellow 会在非 AskUserQuestion 的工具完成后触发，干扰其他状态

因此工具调用期间保持蓝色（思考中），不做区分。但 PostToolUse(AskUserQuestion) 已单独配置，用于在用户回答后立即切回绿色。

## JSON encoding in Go

Use `json.NewEncoder` with `SetEscapeHTML(false)` instead of `json.MarshalIndent` to avoid `>` being escaped to `\u003e` in command strings:

```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false)
enc.SetIndent("", "  ")
enc.Encode(settings)
out := bytes.TrimRight(buf.Bytes(), "\n")
```

## Key files
- `claude-monitor/detector.go` — hook writing, process/proxy/dialog detection, state file reading
- `claude-monitor/main.go` — systray UI, status detection loop, color constants
- `claude-monitor/a11y_darwin.go` — CGWindow-based dialog detection (HasDialogWindow, used in fallback)
- `~/.claude/settings.json` — user settings (hooks written here)
- `~/.claude/settings.local.json` — local settings (hooks also written here)
- `/tmp/claude-monitor-state` — state file updated by hooks, read by detectStatus

## Timing
- `pollInterval = 30ms` — main detection loop
- All detection functions are real-time (no caching) — `CheckClaudeProcess()`, `CheckProxyActivity()`, `HasDialogWindow()` run on every poll tick
- Hook state considered "fresh" if modified within 10 seconds

## Hooks require Claude Code restart
Changes to settings.json hooks only take effect after restarting Claude Code (hooks are loaded at session start).

## Git workflow
- Repo is at `/Users/xuyi/Desktop/Coin/Personal/Code/Claude-Flash/claude-monitor/`
- NOT a git repo at the project root (Claude-Flash), so git operations are within claude-monitor/
