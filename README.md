# Claude Monitor

macOS 菜单栏指示灯，通过 Claude Code hooks 实时显示 Claude 工作状态。Go + systray 实现，原生轻量。

设计参考 [claude-traffic-light](https://github.com/TaylorSimery/claude-traffic-light)。

## 启动方式

```bash
open "/Users/xuyi/Desktop/Coin/Personal/Code/Claude-Flash/claude-monitor/Claude Monitor.app"
```

首次启动需右键 → 打开（未签名应用）。应用出现在菜单栏右侧，无 Dock 图标（`LSUIElement = true`）。

**开机自启动**：系统设置 → 通用 → 登录项与扩展 → 添加 `Claude Monitor.app`。

**重新构建**：
```bash
cd /Users/xuyi/Desktop/Coin/Personal/Code/Claude-Flash/claude-monitor
go build -o "Claude Monitor.app/Contents/MacOS/claude-monitor" .
pkill -f "Claude Monitor.app" ; sleep 0.3
open "Claude Monitor.app"
```

## 状态定义（4 色）

| 状态 | 图标色 | 含义 | 触发来源 |
|------|--------|------|----------|
| Stopped | 灰 `#8E8E93` | Claude 未运行 | ps 检测无 claude 进程 |
| Idle | 绿 `#34C759` | 空闲，等待用户输入 | SessionStart / Stop hook，fallback |
| Working | 蓝 `#007AFF` | 思考/生成中 | UserPromptSubmit hook |
| Blocked | 红 `#FF3B30` | 需要用户操作 | PreToolUse(AskUserQuestion) / PermissionRequest |

## 实现思路

### 核心设计

claude-monitor 通过 Claude Code hooks 写入 per-session 状态文件，并轮询读取这些文件来确定指示灯颜色。

每个 Claude Code 进程用自己的 PID 写入独立的状态文件：`/tmp/claude-monitor-state-$PPID`

### Hook 颜色映射

```
SessionStart                  → green  (会话启动，空闲等待)
UserPromptSubmit              → blue   (用户提交 prompt，开始思考)
PreToolUse (AskUserQuestion)  → red    (等待用户回答)
PostToolUse (AskUserQuestion) → blue   (回答完成，回到思考)
PermissionRequest             → red    (等待用户授权)
Stop                          → green  (完成，等待用户)
```

### 多会话支持

- 状态文件名 `/tmp/claude-monitor-state-$PPID`，每个 Claude Code 进程独立写入
- 默认「Monitor All Sessions」模式：聚合所有会话，取优先级最高的颜色（red > blue > green）
- 点击特定会话可单独监控该 PID 的指示灯
- 会话列表每 500ms 刷新，显示项目名（通过 lsof 获取工作目录）

### 轮询间隔

可通过菜单选择 5ms / 10ms / 30ms / 50ms，默认 10ms。

### 状态日志

状态变化记录到 `log/claude-monitor.log`（项目根目录下），包含时间戳、新旧状态、变更原因和当前所有会话快照。

### Hook 合并策略

`WriteHooks()` 合并而非覆写：
1. 读取已有 `~/.claude/settings.json` 和 `~/.claude/settings.local.json`
2. 移除旧版 claude-monitor 条目（含 `/tmp/claude-monitor-state` 路径的）
3. 追加新条目
4. 仅当有变更时才写入
5. 用户的其他 hooks 完全不受影响

## 文件说明

| 文件 | 职责 |
|------|------|
| `main.go` | 入口 + systray UI + 状态检测 `detectStatus()` + 日志 |
| `detector.go` | 进程检测、状态文件读写、hooks 写入 `WriteHooks()` |
| `icon.go` | 纯 Go 圆形图标生成（32x32 抗锯齿 PNG） |
| `scripts/launch.sh` | 启动脚本 |
| `scripts/build_and_launch.sh` | 构建并启动脚本 |
| `CLAUDE.md` | 开发笔记：hook 格式陷阱、状态机细节 |

## 依赖

| 依赖 | 用途 |
|------|------|
| `fyne.io/systray` | 跨平台菜单栏图标库 |

## 已知限制

1. **强依赖 Claude Code hooks**：hook 机制变更会影响状态检测
2. **状态文件在 /tmp**：系统重启后清空
3. **多实例通过 PID 协调**：进程退出后状态文件成为孤儿（下次轮询时自动跳过死进程）
4. **Hooks 需要重启 Claude Code**：修改 settings.json 后需重启 Claude Code 才能生效
