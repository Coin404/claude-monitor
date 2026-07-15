# Claude Monitor

macOS 菜单栏指示灯，通过 Claude Code hooks 实时显示 Claude 工作状态。

设计参考 [claude-traffic-light](https://github.com/TaylorSimery/claude-traffic-light)（Electron 版），使用 Go + systray 实现更轻量的原生菜单栏应用。

## 状态定义（6 色）

| 状态 | 图标色 | 含义 | Hook 触发 |
|------|--------|------|-----------|
| Stopped | 灰 `#8E8E93` | Claude 未运行 | pgrep 无结果 |
| Idle | 绿 `#34C759` | 空闲，等待用户 | Stop |
| Submitted | 黄 `#FFCC00` | 用户刚提交 prompt | UserPromptSubmit |
| Working | 蓝 `#007AFF` | 思考/生成中 | Start, PostToolUse |
| ToolUse | 橙 `#FF9500` | 执行工具中 | PreToolUse (所有工具) |
| Blocked | 红 `#FF3B30` | 需要用户操作 | PreToolUse(AskUserQuestion) 或权限弹窗 |

颜色直接映射：hook 写入什么颜色 → 图标显示什么颜色，无中间转换层。

## Hook 设计（6 个事件）

```
UserPromptSubmit          → echo yellow  > stateFile  (用户提交)
Start                     → echo blue    > stateFile  (开始输出)
PreToolUse (所有工具)      → echo orange  > stateFile  (执行工具)
PreToolUse (AskUserQuestion) → echo red   > stateFile  (需要用户输入)
PostToolUse               → echo blue    > stateFile  (工具执行完，回到工作中)
Stop                      → echo green   > stateFile  (完成)
```

PreToolUse 有两条 entry：一条无 matcher（匹配所有工具），一条有 matcher="AskUserQuestion"。AskUserQuestion 触发时两者都会执行，最终文件内容是 red（后写入覆盖）。

## 与 claude-traffic-light 的对比

| 维度 | claude-traffic-light | claude-monitor |
|------|---------------------|----------------|
| 技术栈 | Electron + React + TypeScript | Go + systray |
| 包体积 | ~200MB（Electron runtime） | ~5MB（Go 原生二进制） |
| 视觉效果 | 浮动红绿灯窗口 + 呼吸动画 + 音效 | 菜单栏纯色圆点 |
| 统计 | 每日红/绿计数 + 红时长 + 周报 | 无 |
| 双模式 | 三灯 / 单灯切换 | 仅单灯 |
| 主题 | 深色 / 浅色 | 跟随系统菜单栏 |
| 更新机制 | 远程 update.json 检查 | 手动构建 |
| 状态轮询 | 300ms | 50ms |
| 颜色数 | 3 | 6 |

## 架构总览

```
┌──────────────────────────────────────────────────┐
│                  Claude Code                      │
│                                                    │
│  lifecycle events:                                 │
│    UserPromptSubmit ─────────────┤                 │
│    Start ────────────────────────┤                 │
│    PreToolUse ───────────────────┤                 │
│    PreToolUse(AskUserQuestion) ──┤                 │
│    PostToolUse ──────────────────┤                 │
│    Stop ─────────────────────────┤                 │
│                                  │                 │
│  ~/.claude/settings.json         │                 │
│    hooks.command =               │                 │
│    "echo <color> > <stateFile>"  │                 │
└──────────────────────────────────┼─────────────────┘
                                   │ write
                                   ▼
                      /tmp/claude-monitor-state
                                   │
                                   │ read (50ms poll)
                                   ▼
┌──────────────────────────────────────────────────┐
│               claude-monitor (Go)                 │
│                                                    │
│  ┌─────────────┐   ┌──────────────┐               │
│  │ detector.go  │   │   main.go    │               │
│  │              │   │              │               │
│  │ ReadHookState│──▶│ detectStatus │               │
│  │ CheckProcess │   │   (状态机)    │               │
│  │ WriteHooks   │   └──────┬───────┘               │
│  └──────────────┘          │                       │
│                            ▼                       │
│                    ┌──────────────┐               │
│                    │  systray     │               │
│                    │  SetIcon()   │               │
│                    └──────┬───────┘               │
│                           │                       │
│  ┌────────────────┐       │                       │
│  │ a11y_darwin.go  │       │                       │
│  │ + a11y_bridge.m │       │                       │
│  │ CGWindow 检测    │       │                       │
│  └────────────────┘       │                       │
│         │                 │                       │
│         └──────┬──────────┘                       │
│                ▼                                   │
│         菜单栏彩色圆点                              │
└──────────────────────────────────────────────────┘
```

## Hook 系统（最关键组件）

### 合并策略

`WriteHooks()` 采用**合并而非覆写**策略，完全参考 claude-traffic-light 的 `setupClaudeHooks()` 实现：

1. 读取现有 `~/.claude/settings.json`
2. 在 6 个受管事件（Start、Stop、PreToolUse、PostToolUse、UserPromptSubmit、Elicitation）中，通过内容匹配识别包含 `/tmp/claude-monitor-state` 路径的旧条目并移除
3. 追加新的 hook 条目
4. 仅当有变更时才写回磁盘（`changed` flag）
5. 返回 error，由调用方决定如何处理

用户的**其他 hooks 完全不受影响**，claude-monitor 可与任意第三方 hook 共存。

### Hook 事件与颜色语义

| 事件 | 写入颜色 | 触发时机 | 设计意图 |
|------|---------|----------|----------|
| `UserPromptSubmit` | `yellow` | 用户提交 prompt 后立即触发 | 提前标记"已提交" |
| `Start` | `blue` | Claude 开始输出 response | 标记"思考/生成中" |
| `PreToolUse` | `orange` | 任何工具调用前 | 标记"执行工具中" |
| `PreToolUse` (matcher: `AskUserQuestion`) | `red` | Claude 需要向用户提问 | 标记"需要用户操作" |
| `PostToolUse` | `blue` | 工具执行完毕后 | 回到"思考/生成中" |
| `Stop` | `green` | Claude 完成回复 | 标记"空闲，等待用户" |

### 旧条目清理机制

清理使用**内容匹配**而非索引匹配：将每个 entry 序列化为 JSON，检查是否包含 `stateFile` 路径。这与 claude-traffic-light 清理 `cc_traffic_light_state` 的策略一致，可安全处理多次安装产生的重复条目。

额外清理 `Elicitation` 事件（旧版 Claude Code 使用的事件名），防止遗留配置污染。

### settings.json 写入格式

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo yellow > /tmp/claude-monitor-state"
          }
        ]
      }
    ],
    "Start": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo blue > /tmp/claude-monitor-state"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo orange > /tmp/claude-monitor-state"
          }
        ]
      },
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
    "PostToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo blue > /tmp/claude-monitor-state"
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
    ]
  }
}
```

## 状态检测流程

`detectStatus()` 按优先级依次判断：

```
1. CheckClaudeProcess() == false  →  StatusStopped（灰）
   ps -eo pid,comm=，精确匹配 comm == "claude"，排除自身 PID

2. HasDialogWindow() == true     →  StatusBlocked（红）
   CGWindow API 检测弹窗层窗口（layer 1-100），仅匹配 Claude 相关进程
   （claude、SecurityAgent、UserNotificationCenter）以避免误报

3. ReadHookState() 读取 /tmp/claude-monitor-state，返回 (state, fresh):
   fresh = 文件修改时间 < 10 秒

   3a. !fresh（状态文件过期或不存在）:
       CheckProxyActivity() == false →  StatusIdle   （绿：fallback）
       CheckProxyActivity() == true:
         CheckWaitingForInput() == true  →  StatusBlocked（红：等待用户回复）
         CheckWaitingForInput() == false →  StatusWorking（蓝：工作中）
       CheckProxyActivity 通过 lsof 检测 127.0.0.1:15721 的 TCP 连接（500ms 缓存）
       CheckWaitingForInput 通过 ps + lsof 检测 Claude 进程是否在 TTY 上等待 stdin（500ms 缓存）

   3b. fresh（状态文件新鲜）:
       "green"  → StatusIdle      （绿：等待用户下一条指令）
       "yellow" → StatusSubmitted （黄：用户刚提交 prompt）
       "blue"   → StatusWorking   （蓝：思考/生成中）
       "orange" → StatusToolUse   （橙：执行工具中）
       "red"    → StatusBlocked   （红：需要用户操作）
       default  → StatusIdle      （绿：未知状态默认空闲）
```

**Fallback 机制说明**：当 hooks 尚未在当前 session 生效时（如 claude-monitor 晚于 Claude Code 启动），状态文件处于过期状态。此时：
1. 通过 lsof 检测 API 代理连接判断 Idle/Working
2. 通过检测 Claude 进程是否在 TTY 上等待 stdin（sleeping + TTY stdin）推断 Blocked 状态（AskUserQuestion 等待用户回复）
3. 覆盖率：Idle、Working、Blocked；无法区分 Submitted 和 ToolUse

## 菜单栏菜单

- **Re-write Hooks**：手动重新写入 hooks 配置到 `~/.claude/settings.json`，用于 hooks 被误删或损坏后的恢复
- **Quit**：退出应用

## 文件说明

### main.go — 入口 + 状态机 + systray UI
- 定义 `Status` 枚举（Stopped/Idle/Submitted/Working/ToolUse/Blocked）和 6 个颜色常量
- `main()`: 初始化状态→图标映射表，调用 `WriteHooks()` 写入 hooks，启动 systray
- `onReady()`: 创建菜单项（Re-write Hooks、Quit），信号监听（SIGINT/SIGTERM），启动 50ms 轮询循环
- `detectStatus()`: 优先级级联判断进程→弹窗→hook 状态
- `onExit()`: systray 退出回调（空实现）

### detector.go — 进程检测 + hooks 管理 + 状态文件 + 代理检测 + stdin 检测
- `CheckClaudeProcess()`: 通过 `ps` 检测进程（使用 comm 精确匹配 `claude`，比 pgrep 更可靠），结果缓存 500ms
- `ReadHookState()`: 读取 `/tmp/claude-monitor-state`，返回状态字符串和 freshness 标记（修改时间 < 10s 视为新鲜）
- `CheckProxyActivity()`: 通过 `lsof -i TCP:15721` 检测 API 代理连接，结果缓存 500ms，作为 hooks 未生效时的 fallback
- `CheckWaitingForInput()`: 检测 Claude 进程是否在 TTY 上等待 stdin（sleeping + TTY stdin），用于在 hooks 未生效时推断 Blocked 状态
- `WriteHooks()`: 合并式安装 hooks 到 `~/.claude/settings.json` 和 `~/.claude/settings.local.json`，带 `changed` flag 避免无效写入，清理 6 个受管事件（含 Elicitation），返回 error

### icon.go — 纯 Go 圆形图标生成
- `GenerateCircleIcon(hexColor)`: 生成 32×32 PNG，带抗锯齿边缘
  - 使用距离函数计算 alpha：dist < -0.5 全不透明，dist > 0.5 全透明，中间线性插值
- `parseHexColor(s)`: 将 `#RRGGBB` 字符串解析为 `color.RGBA`
- 无外部图片依赖，图标完全由代码生成

### a11y_bridge.h / a11y_bridge.m / a11y_darwin.go — CGWindow 弹窗检测
- **ObjC 层** (`a11y_bridge.m`)：调用 `CGWindowListCopyWindowInfo` 获取屏幕上所有窗口，过滤 layer 1-100 的弹窗层窗口，排除 Dock/WindowServer 等基线进程，仅匹配 Claude 相关进程（claude、SecurityAgent、UserNotificationCenter）以避免其他应用弹窗误报
- **C 头** (`a11y_bridge.h`)：导出 `getWindowOwners()` 返回 `\n` 分隔的进程名
- **Go CGO 层** (`a11y_darwin.go`)：通过 CGO 调用，`HasDialogWindow()` 判断是否有弹窗

### Claude Monitor.app — macOS App Bundle
- `Info.plist`: `LSUIElement = true`（无 Dock 图标，仅菜单栏）
- `CFBundleIdentifier`: `com.claude.monitor`

## 依赖

| 依赖 | 版本 | 用途 |
|------|------|------|
| `fyne.io/systray` | v1.12.2 | 跨平台 systray 库，提供菜单栏图标和菜单 |
| `github.com/godbus/dbus/v5` | v5.1.0 | D-Bus 通信（systray 的 Linux 依赖） |
| `golang.org/x/sys` | v0.15.0 | 系统调用封装 |
| CoreGraphics.framework | macOS 内置 | CGWindow API（CGO 链接） |

## 构建与签名

```bash
# 构建
CGO_ENABLED=1 go build -o "Claude Monitor.app/Contents/MacOS/claude-monitor" .

# Ad-hoc 签名（开发用）
codesign --force --deep --sign - "Claude Monitor.app"

# 启动
open "Claude Monitor.app"
```

> 必须启用 CGO（`CGO_ENABLED=1`），因为 CGWindow 桥接代码依赖 CGO 链接 CoreGraphics framework。

## 技术方案演进

| 版本 | 方案 | 核心缺陷 |
|------|------|----------|
| v1 | Accessibility API + 窗口标题匹配 | 需要辅助功能权限；重签后权限失效；`activationPolicy` 过滤跳过 CLI 进程 |
| v2 | CGWindow API + CPU 快照 | CPU 采样不准；思考阶段（无工具调用）无信号 |
| v3 | 翻转文件协议 | 时序问题：回复结束后 1 秒才切换状态，存在误报窗口 |
| v4 | Claude Code hooks（3 色） | 无法区分"思考中"和"执行工具中" |
| v5 | **Claude Code hooks（6 色，当前）** | 依赖 Claude Code 的 hook 机制 |

## 限制与已知问题

1. **强依赖 Claude Code hooks**：如果 Claude Code 未运行或 hook 机制变更，指示灯将停留在灰色（Stopped）
2. **状态文件无持久化**：`/tmp/claude-monitor-state` 在系统重启后清空，首次启动前颜色不确定（代码默认为空→StatusIdle）
3. **权限弹窗检测有限**：CGWindow 的 layer 过滤区间（1-100）是经验值，某些系统弹窗可能不在此范围
4. **无 idle 超时检测**：如果 Claude 进程存在但长时间无活动，状态取决于最后一次 hook 写入的颜色
5. **无重试机制**：`WriteHooks()` 写入失败仅输出 stderr，不阻塞启动
6. **无多实例协调**：多个 Claude Code 实例同时运行时会竞争写入同一状态文件，最后写入者胜出
