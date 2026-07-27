# Novascope

macOS 菜单栏指示灯，通过 Claude Code hooks 实时显示 Claude 工作状态。Go + systray 实现，原生轻量。

## 快速开始

```bash
# 构建并启动
./scripts/build_and_launch.sh

# 或直接启动已构建的 App
open Novascope.app
```

## 文档

所有项目文档位于 [`docs/`](./docs/) 目录：

- **[docs/README.md](./docs/README.md)** — 项目概览、功能特性、目录结构、依赖
- **[docs/architecture.md](./docs/architecture.md)** — 架构设计、设计模式、数据流、时序参数
- **[docs/hooks.md](./docs/hooks.md)** — Claude Code Hook 格式、颜色映射、合并策略
- **[docs/development.md](./docs/development.md)** — 开发环境、构建命令、调试方法、代码规范
# Novascope

macOS 菜单栏指示灯，通过 Claude Code hooks 实时显示 Claude 工作状态。Go + systray 实现，原生轻量。

设计参考 [claude-traffic-light](https://github.com/TaylorSimery/claude-traffic-light)。

## 启动方式

```bash
open "/Users/xuyi/Desktop/Coin/Personal/Code/Claude-Flash/claude-monitor/Novascope.app"
```

首次启动需右键 → 打开（未签名应用）。应用出现在菜单栏右侧，无 Dock 图标（`LSUIElement = true`）。

**开机自启动**：系统设置 → 通用 → 登录项与扩展 → 添加 `Novascope.app`。

**重新构建**：
```bash
cd /Users/xuyi/Desktop/Coin/Personal/Code/Claude-Flash/claude-monitor
go build -o "Novascope.app/Contents/MacOS/claude-monitor" .
cp stats_window.swift Novascope.app/Contents/MacOS/
swiftc -o Novascope.app/Contents/MacOS/novascope-stats-helper Novascope.app/Contents/MacOS/stats_window.swift
pkill -f "Novascope.app" ; sleep 0.3
open "Novascope.app"
```

## 状态定义（6 色）

| 状态 | 图标色 | 含义 | 触发来源 |
|------|--------|------|----------|
| Stopped | 灰 `#8E8E93` | Claude 未运行 | ps 检测无 claude 进程 |
| Idle | 绿 `#34C759` | 空闲，等待用户输入 | SessionStart / Stop hook |
| Submitted | 黄 `#FFCC00` | 用户刚提交 prompt | UserPromptSubmit hook |
| Working | 蓝 `#007AFF` | 思考/生成中 | UserPromptSubmit / PostToolUse hook |
| ToolUse | 橙 `#FF9500` | 执行工具中 | PreToolUse hook（通用） |
| Blocked | 红 `#FF3B30` | 需要用户操作 | PreToolUse(AskUserQuestion/Bash) / PermissionRequest |

状态优先级：red > orange > yellow > blue > green（多会话聚合时取最高优先级）。

## 功能特性

### 多会话监控
- 每个 Claude Code 进程维护独立状态文件 `/tmp/claude-monitor-state-$PID`
- 默认「Monitor All Sessions」模式：聚合所有会话，显示最高优先级状态
- 点击菜单中任意会话可单独监控该 PID
- 点击会话项自动将该会话所在窗口带到前台（原生 NSRunningApplication 激活，带 Space 切换动画）

### macOS 系统通知
状态切到红色（需要确认）时发送系统通知，显示项目名。通知去重：3 秒内最多一条。

### 轮询间隔
菜单可选 5ms / 10ms / 30ms / 50ms，默认 10ms。

### 状态日志
状态变化记录到 `log/claude-monitor.log`（项目根目录），含时间戳、新旧状态、变更原因和所有会话快照。

### 统计图表 (Stats Chart)
- 追踪每天 Claude 在各状态的累计时长（思考/空闲/等待/工具调用/提交中）
- 数据持久化到 `log/stats-YYYY-MM-DD.json`，每次状态变化时写入
- 点击菜单 "View Stats Chart" 打开原生 macOS 窗口，展示 SVG 甜甜圈图
- 图表含渐变色分段、圆角段尾、悬停阴影放大效果、暗色模式自适应
- 图表窗口由独立 Swift helper (`stats_window.swift`) 渲染，通过 WKWebView 展示，避免 cgo/Cocoa 线程冲突

### Hook 合并策略
`WriteHooks()` 合并而非覆写：
1. 读取已有 `~/.claude/settings.json` 和 `~/.claude/settings.local.json`
2. 移除旧版 claude-monitor/novascope 条目
3. 追加新条目
4. 仅当有变更时才写入
5. 用户的其他 hooks 完全不受影响

## Hook 颜色映射

```
SessionStart                  → green  (会话启动，空闲等待)
UserPromptSubmit              → blue   (用户提交 prompt，开始思考)
PreToolUse (AskUserQuestion)  → red    (等待用户回答)
PostToolUse (AskUserQuestion) → blue   (回答完成，继续思考)
PreToolUse (Bash)             → red    (等待用户授权命令)
PostToolUse (Bash)            → blue   (授权完成，继续思考)
PostToolUse (generic)         → blue   (通用工具完成，恢复思考)
PermissionRequest             → red    (系统权限弹窗)
Stop                          → green  (完成，等待用户)
```

## 文件说明

| 文件 | 职责 |
|------|------|
| `main.go` | 入口 + systray UI + 状态检测 `detectStatus()` + 通知 + 日志 |
| `detector.go` | 进程检测、状态文件读写、hooks 写入 `WriteHooks()`、终端查找 |
| `stats.go` | 日状态时长统计、JSON 持久化、HTML/SVG 甜甜圈图生成 |
| `stats_window.swift` | 独立 Swift 程序，用 WKWebView 在原生窗口中渲染统计图表 |
| `activate_darwin.go` | 原生 NSRunningApplication 窗口激活（CGO + Cocoa） |
| `icon.go` | 纯 Go 圆形图标生成（32x32 抗锯齿 PNG） |
| `scripts/` | 启动脚本、构建脚本 |
| `icon/` | app bundle 图标资源 |

## 依赖

| 依赖 | 用途 |
|------|------|
| `fyne.io/systray` | 跨平台菜单栏图标库 |
| CGO + Cocoa framework | 原生窗口激活动画 |
| Swift + WebKit framework | 统计图表原生窗口渲染 |

## 已知限制

1. **强依赖 Claude Code hooks**：hook 机制变更会影响状态检测
2. **状态文件在 /tmp**：系统重启后清空
3. **多实例通过 PID 协调**：进程退出后状态文件成为孤儿（下次轮询时自动跳过死进程）
4. **Hooks 需重启 Claude Code**：修改 settings.json 后需重启 Claude Code 才能生效
5. **CGO 依赖**：`activate_darwin.go` 需要 macOS 编译环境（CGO_ENABLED=1）
6. **Swift helper 需编译**：统计图表功能依赖 `swiftc` 编译 `stats_window.swift`，首次点击会自动编译
