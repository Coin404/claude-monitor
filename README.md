# Claude Monitor

macOS 菜单栏指示灯，通过 Claude Code hooks 实时显示 Claude 工作状态。

设计参考 [claude-traffic-light](https://github.com/TaylorSimery/claude-traffic-light)（Electron 版），使用 Go + systray 实现更轻量的原生菜单栏应用。

## 状态定义

| 颜色 | 图标 | 状态常量 | 含义 | 触发条件 |
|------|------|----------|------|----------|
| 灰 `#8E8E93` | ● | `StatusStopped` | Claude 未启动 | `pgrep -f claude` 无结果 |
| 绿 `#34C759` | ● | `StatusActive` | Claude 完成回复，等待用户下一条指令 | Stop hook → 写入 `green` |
| 红 `#FF3B30` | ● | `StatusWaiting` | Claude 工作中（思考/输出中） | Start hook 或 UserPromptSubmit hook → 写入 `yellow` |
| 黄 `#FFCC00` | ● | `StatusBlocked` | 需要用户操作 | PreToolUse(AskUserQuestion) hook → 写入 `red`，或 CGWindow 检测到权限弹窗 |

> **注意**：图标颜色与 hook 写入的颜色关键字并非一一对应，中间经过 `detectStatus()` 映射层。这是为了保持 `Status` 枚举语义清晰（"活跃/等待/阻塞"），同时 hook 写入的颜色遵循 traffic-light 惯例（绿=你可以继续 / 红=需要你操作 / 黄=工作中）。

## 与 claude-traffic-light 的对齐

两个项目使用**完全相同的颜色语义和 hook 事件映射**，可互相替换：

| 事件 | 写入颜色 | claude-traffic-light | claude-monitor |
|------|---------|----------------------|----------------|
| Start | `yellow` | ✓ | ✓ |
| UserPromptSubmit | `yellow` | ✓ | ✓ |
| Stop | `green` | ✓ | ✓ |
| PreToolUse + AskUserQuestion | `red` | ✓ | ✓ |
| Elicitation（清理） | — | ✓ | ✓ |

区别在于架构选择：

| 维度 | claude-traffic-light | claude-monitor |
|------|---------------------|----------------|
| 技术栈 | Electron + React + TypeScript | Go + systray |
| 包体积 | ~200MB（Electron runtime） | ~5MB（Go 原生二进制） |
| 视觉效果 | 浮动红绿灯窗口 + 呼吸动画 + 音效 | 菜单栏纯色圆点 |
| 统计 | 每日红/绿计数 + 红时长 + 周报 | 无 |
| 双模式 | 三灯 / 单灯切换 | 仅单灯 |
| 主题 | 深色 / 浅色 | 跟随系统菜单栏 |
| 更新机制 | 远程 update.json 检查 | 手动构建 |
| 状态轮询 | 300ms | 250ms |

## 架构总览

```
┌──────────────────────────────────────────────────┐
│                  Claude Code                      │
│                                                    │
│  lifecycle events:                                 │
│    Start ─────────────────────┐                    │
│    UserPromptSubmit ──────────┤                    │
│    Stop ──────────────────────┤                    │
│    PreToolUse(AskUserQuestion) ┤                    │
│                               │                    │
│  ~/.claude/settings.json      │                    │
│    hooks.command =            │                    │
│    "echo <color> > <stateFile>"                    │
└───────────────────────────────┼────────────────────┘
                                │ write
                                ▼
                   /tmp/claude-monitor-state
                                │
                                │ read (250ms poll)
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
2. 在 5 个受管事件（Start、Stop、PreToolUse、UserPromptSubmit、Elicitation）中，通过内容匹配识别包含 `/tmp/claude-monitor-state` 路径的旧条目并移除
3. 追加新的 hook 条目
4. 仅当有变更时才写回磁盘（`changed` flag）
5. 返回 error，由调用方决定如何处理

用户的**其他 hooks 完全不受影响**，claude-monitor 可与任意第三方 hook 共存。

### Hook 事件与颜色语义

| 事件 | 写入颜色 | 触发时机 | 设计意图 |
|------|---------|----------|----------|
| `Start` | `yellow` | Claude 开始输出 response | 标记"工作中"，覆盖思考阶段 |
| `UserPromptSubmit` | `yellow` | 用户提交 prompt 后立即触发 | 在 Start 之前提前标记"即将工作" |
| `Stop` | `green` | Claude 完成回复 | traffic-light 惯例：绿色=用户可以继续 |
| `PreToolUse` (matcher: `AskUserQuestion`) | `red` | Claude 需要向用户提问 | traffic-light 惯例：红色=需要用户操作 |

### 旧条目清理机制

清理使用**内容匹配**而非索引匹配：将每个 entry 序列化为 JSON，检查是否包含 `stateFile` 路径。这与 claude-traffic-light 清理 `cc_traffic_light_state` 的策略一致，可安全处理多次安装产生的重复条目。

额外清理 `Elicitation` 事件（旧版 Claude Code 使用的事件名），防止遗留配置污染。

### settings.json 写入格式

```json
{
  "hooks": {
    "Start": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo yellow > /tmp/claude-monitor-state"
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
    ]
  }
}
```

## 状态检测流程

`detectStatus()` 按优先级依次判断：

```
1. CheckClaudeProcess() == false  →  StatusStopped（灰）
   pgrep -f claude，排除自身 PID

2. HasDialogWindow() == true     →  StatusBlocked（黄）
   CGWindow API 检测弹窗层窗口（layer 1-100）

3. ReadHookState() 读取 /tmp/claude-monitor-state:
   "green"  → StatusActive  （绿：等待用户下一条指令）
   "yellow" → StatusWaiting （红：Claude 工作中）
   "red"    → StatusBlocked（黄：需要用户操作）
   default  → StatusWaiting （红：未知状态默认工作中）
```

## 菜单栏菜单

- **Re-write Hooks**：手动重新写入 hooks 配置到 `~/.claude/settings.json`，用于 hooks 被误删或损坏后的恢复（对应 claude-traffic-light 的 "Re-write config"）
- **Quit**：退出应用

## 文件说明

### main.go — 入口 + 状态机 + systray UI
- 定义 `Status` 枚举（Stopped/Active/Waiting/Blocked）和颜色常量
- `main()`: 初始化状态→图标映射表，调用 `WriteHooks()` 写入 hooks，启动 systray
- `onReady()`: 创建菜单项（Re-write Hooks、Quit），信号监听（SIGINT/SIGTERM），启动 250ms 轮询循环
- `detectStatus()`: 优先级级联判断进程→弹窗→hook 状态
- `onExit()`: systray 退出回调（空实现）

### detector.go — 进程检测 + hooks 管理 + 状态文件
- `CheckClaudeProcess()`: 通过 `pgrep -f claude` 检测进程，排除自身 PID
- `ReadHookState()`: 读取 `/tmp/claude-monitor-state`，失败返回空字符串
- `WriteHooks()`: 合并式安装 hooks 到 `~/.claude/settings.json`，带 `changed` flag 避免无效写入，清理 5 个受管事件（含 Elicitation），返回 error

### icon.go — 纯 Go 圆形图标生成
- `GenerateCircleIcon(hexColor)`: 生成 32×32 PNG，带抗锯齿边缘
  - 使用距离函数计算 alpha：dist < -0.5 全不透明，dist > 0.5 全透明，中间线性插值
- `parseHexColor(s)`: 将 `#RRGGBB` 字符串解析为 `color.RGBA`
- 无外部图片依赖，图标完全由代码生成

### a11y_bridge.h / a11y_bridge.m / a11y_darwin.go — CGWindow 弹窗检测
- **ObjC 层** (`a11y_bridge.m`)：调用 `CGWindowListCopyWindowInfo` 获取屏幕上所有窗口，过滤 layer 1-100 的弹窗层窗口，排除 Dock/WindowServer 等基线进程
- **C 头** (`a11y_bridge.h`)：导出 `getWindowOwners()` 返回 `\n` 分隔的进程名
- **Go CGO 层** (`a11y_darwin.go`)：通过 CGO 调用，`HasDialogWindow()` 判断是否有弹窗

### permission.go — AppleScript 权限弹窗检测（备用）
- 通过 `osascript` 遍历所有进程窗口标题
- 匹配关键词：`would like to`、`permission`、`允许`、`访问`
- 3 秒超时
- 当前未被主流程调用，作为 CGWindow 方案的备选

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
| v4 | **Claude Code hooks（当前）** | 依赖 Claude Code 的 hook 机制，hooks 不支持的事件无法感知 |

## 限制与已知问题

1. **强依赖 Claude Code hooks**：如果 Claude Code 未运行或 hook 机制变更，指示灯将停留在灰色（Stopped）
2. **状态文件无持久化**：`/tmp/claude-monitor-state` 在系统重启后清空，首次启动前颜色不确定（代码默认为空→StatusWaiting）
3. **权限弹窗检测有限**：CGWindow 的 layer 过滤区间（1-100）是经验值，某些系统弹窗可能不在此范围
4. **无 idle 超时检测**：如果 Claude 进程存在但长时间无活动，状态取决于最后一次 hook 写入的颜色
5. **无重试机制**：`WriteHooks()` 写入失败仅输出 stderr，不阻塞启动
6. **无多实例协调**：多个 Claude Code 实例同时运行时会竞争写入同一状态文件，最后写入者胜出
