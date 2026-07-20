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

## Hook execution order and PreToolUse behavior

When multiple PreToolUse/PostToolUse entries match the same tool, **later entries in the array overwrite earlier ones** (they write to the same state file). We use this to our advantage:
- Generic entries (no matcher) are placed **first** → set the default tool color
- AskUserQuestion entries are placed **last** → overwrite with the specific color

This ensures:
- Regular tools (bash, webfetch, etc.): PreToolUse → orange, PostToolUse → blue
- AskUserQuestion: PreToolUse → orange then red (= red), PostToolUse → blue (= blue, same as generic)

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
| 橙   | 工具调用 | 执行 bash/python/webfetch 等工具 |
| 红   | 等待确认 | 弹出询问权限/确认方案（AskUserQuestion、PermissionRequest）|

### Hook color mapping
```
SessionStart                  → green  (会话启动，空闲等待)
UserPromptSubmit              → blue   (用户提交 prompt，开始思考)
PreToolUse(generic)           → orange (执行工具：bash/py/webfetch等)
PreToolUse(AskUserQuestion)   → red    (覆盖 orange，等待用户回答)
PostToolUse(generic)          → blue   (工具执行完，回到思考/生成)
PostToolUse(AskUserQuestion)  → blue   (同上，确认完成继续思考)
PermissionRequest             → red    (等待用户授权)
Stop                          → green  (完成，等待用户)
```

### 为什么 AskUserQuestion 放在数组后面
通用 PreToolUse（无 matcher）和 AskUserQuestion 同时匹配时，后执行的会覆盖先执行的。把 AskUserQuestion 放在数组**后面**，确保它后执行覆盖通用条目：
- PreToolUse: [generic→orange, AskUserQuestion→red] → AskUserQuestion 时最终为 red
- PostToolUse: [generic→blue, AskUserQuestion→blue] → 都是 blue，无需覆盖

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
- `claude-monitor/stats.go` — daily status duration tracking, HTML donut chart generation
- `claude-monitor/stats_window.swift` — standalone Swift helper that renders stats HTML in a native WKWebView window
- `claude-monitor/icon.go` — tray circle icon generation
- `claude-monitor/activate_darwin.go` — native NSRunningApplication window activation
- `~/.claude/settings.json` — user settings (hooks written here)
- `~/.claude/settings.local.json` — local settings (hooks also written here)
- `/tmp/claude-monitor-state-$PID` — per-session state files updated by hooks, read by detectStatus

## Stats tracking
- `StatsTracker` in `stats.go` tracks time spent in each status per day
- `RecordStatusChange(newStatus)` called from main detection loop on every status transition
- Data persisted to `log/stats-YYYY-MM-DD.json` on every change (minimizes data loss)
- `CleanupOldStats(7)` removes files older than 7 days at startup
- "View Stats Chart" menu item opens a native macOS WKWebView window with an SVG donut chart
- The chart window is rendered by a standalone Swift helper (`stats_window.swift`) compiled and invoked as a subprocess — avoids cgo/WKWebView threading issues
- `FlushCurrentSession()` called in `onExit()` to save the final status segment before quit

## Timing
- `pollInterval = 30ms` — main detection loop
- All detection functions are real-time (no caching) — `CheckClaudeProcess()`, `CheckProxyActivity()`, `HasDialogWindow()` run on every poll tick
- Hook state considered "fresh" if modified within 10 seconds

## Hooks require Claude Code restart
Changes to settings.json hooks only take effect after restarting Claude Code (hooks are loaded at session start).

## Git workflow
- Repo is at `/Users/xuyi/Desktop/Coin/Personal/Code/Claude-Flash/claude-monitor/`
- NOT a git repo at the project root (Claude-Flash), so git operations are within claude-monitor/
