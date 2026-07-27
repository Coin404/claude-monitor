# Hook 机制

## 概述

Novascope 通过 Claude Code 的 hooks 机制获取实时状态。Hook 是 Claude Code 在特定生命周期事件触发时执行的 shell 命令，配置在 `~/.claude/settings.json` 中。

## Hook 格式

Claude Code hooks 使用嵌套数组格式，每个事件条目**必须**有 `hooks` 数组包装：

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo green > /tmp/claude-monitor-state-$PPID"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo orange > /tmp/claude-monitor-state-$PPID"
          }
        ]
      },
      {
        "matcher": "AskUserQuestion",
        "hooks": [
          {
            "type": "command",
            "command": "echo red > /tmp/claude-monitor-state-$PPID"
          }
        ]
      }
    ]
  }
}
```

### 格式规则

- **每个条目必须有 `"hooks": [...]` 包装** — 扁平的 `{"type":"command","command":"..."}` 会导致 "Expected array, but received undefined" 错误
- **`matcher` 是可选的** — 省略表示匹配所有工具；只在针对特定工具时添加 `"matcher": "ToolName"`
- **`Start` 已废弃** — 使用 `SessionStart` 代替，`Start` 在新版 Claude Code 中会报 "Invalid key in record"
- **整个 settings.json 在任何一个 hook 有错误时会被跳过** — 一个坏 hook 会破坏所有功能

## 颜色映射

```
SessionStart                  → green  (会话启动，空闲等待)
UserPromptSubmit              → blue   (用户提交 prompt，开始思考)
PreToolUse (generic)          → orange (执行工具：bash/py/webfetch 等)
PreToolUse (AskUserQuestion)  → red    (覆盖 orange，等待用户回答)
PostToolUse (generic)         → blue   (工具执行完，回到思考/生成)
PostToolUse (AskUserQuestion) → blue   (回答完成，继续思考)
PermissionRequest             → red    (系统权限弹窗，等待用户授权)
Stop                          → green  (完成，等待用户)
```

## 执行顺序与覆盖策略

当多个 PreToolUse/PostToolUse 条目匹配同一工具时，**数组中后面的条目会覆盖前面的**（写入同一状态文件）。利用这一特性：

- 通用条目（无 matcher）放在**前面** → 设置默认工具颜色
- AskUserQuestion 条目放在**后面** → 用特定颜色覆盖

效果：
- 普通工具（bash, webfetch 等）：PreToolUse → orange, PostToolUse → blue
- AskUserQuestion：PreToolUse → orange 然后 red（最终 = red）, PostToolUse → blue

## 状态文件

每个 Claude Code 进程通过 `$PPID`（父进程 PID）写入独立的状态文件：

```
/tmp/claude-monitor-state-$PPID
```

文件内容仅为颜色字符串（如 `green`、`blue`、`red`）。

### 状态文件生命周期

1. **创建**：Claude Code 触发 hook 时写入
2. **读取**：Novascope 主循环每 10ms 轮询
3. **新鲜度**：修改时间 < 10 秒视为 fresh
4. **清理**：
   - 进程退出后成为孤儿文件
   - `CleanupStaleStateFiles()` 在启动和 WriteHooks 时清理死进程的文件
   - 旧版无 PID 后缀的 `/tmp/claude-monitor-state` 也会被清理

## 支持的 Hook 事件

| 事件 | 说明 | Novascope 使用 |
|------|------|:-:|
| `SessionStart` | 会话开始 | ✅ |
| `Stop` | Claude 完成响应 | ✅ |
| `UserPromptSubmit` | 用户提交 prompt | ✅ |
| `PreToolUse` | 工具执行前（可带 matcher） | ✅ |
| `PostToolUse` | 工具执行后 | ✅ |
| `PermissionRequest` | 权限对话框弹出 | ✅ |
| `PostToolUseFailure` | 工具执行失败 | ❌ |
| `Notification` | Claude 发送通知 | ❌ |
| `SessionEnd` | 会话结束 | ❌ |
| `SubagentStart` / `SubagentStop` | 子代理生命周期 | ❌ |
| `PreCompact` | 上下文压缩前 | ❌ |

## Hook 合并策略

`WriteHooks()` 的实现逻辑（`internal/detect/hooks.go`）：

1. 清理死进程的状态文件
2. 读取 `~/.claude/settings.json`（和 `settings.local.json`）
3. 解析现有 hooks 对象
4. 从 managed events 中移除包含 `claude-monitor-state` 的旧条目
5. 追加新的 hook 条目
6. 仅当有变更时才写入磁盘
7. 使用 `json.NewEncoder` + `SetEscapeHTML(false)` 避免 `>` 被转义为 `\u003e`

Managed events 列表：
```go
[]string{"SessionStart", "Stop", "PreToolUse", "PostToolUse", "UserPromptSubmit", "PermissionRequest", "Elicitation"}
```

> `Elicitation` 是旧版事件，仅用于清理旧条目。

## JSON 编码注意事项

Go 的 `json.MarshalIndent` 会将 `>` 转义为 `\u003e`，导致 hook command 中的 `>` 重定向符号被破坏。解决方案：

```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false)
enc.SetIndent("", "  ")
enc.Encode(settings)
out := bytes.TrimRight(buf.Bytes(), "\n")
```

## 重要提醒

- **修改 hooks 后需重启 Claude Code** — hooks 在会话启动时加载
- Novascope 启动时自动写入 hooks，也可通过菜单 "Re-write Hooks" 手动重写
- hooks 同时写入 `settings.json` 和 `settings.local.json`，确保无论 Claude Code 读取哪个文件都能生效
