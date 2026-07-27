# Novascope

macOS 菜单栏指示灯，通过 Claude Code hooks 实时显示 Claude 工作状态。Go + systray 实现，原生轻量。

设计参考 [claude-traffic-light](https://github.com/TaylorSimery/claude-traffic-light)。

## 快速开始

### 启动

```bash
open Novascope.app
```

首次启动需右键 → 打开（未签名应用）。应用出现在菜单栏右侧，无 Dock 图标（`LSUIElement = true`）。

**开机自启动**：系统设置 → 通用 → 登录项与扩展 → 添加 `Novascope.app`。

### 构建

```bash
# 一键构建并启动（推荐）
./scripts/build_and_launch.sh

# 手动构建
go build -o "Novascope.app/Contents/MacOS/claude-monitor" .
swiftc -o "Novascope.app/Contents/MacOS/novascope-panel-helper" helpers/sessions_panel.swift
swiftc -o "Novascope.app/Contents/MacOS/novascope-settings-helper" helpers/settings_window.swift
pkill -f "Novascope.app" ; sleep 0.3
open "Novascope.app"
```

### 分发打包

```bash
./scripts/make_dist.sh   # 输出 dist/Novascope-v1.0.zip（Universal Binary）
```

## 状态定义（6 色）

| 状态 | 图标色 | 含义 | 触发来源 |
|------|--------|------|----------|
| Stopped | 灰 `#8E8E93` | Claude 未运行 | ps 检测无 claude 进程 |
| Idle | 绿 `#34C759` | 空闲，等待用户输入 | SessionStart / Stop hook |
| Submitted | 黄 `#FFCC00` | 用户刚提交 prompt | UserPromptSubmit hook |
| Working | 蓝 `#007AFF` | 思考/生成中 | UserPromptSubmit / PostToolUse hook |
| ToolUse | 橙 `#FF9500` | 执行工具中 | PreToolUse hook（通用） |
| Blocked | 红 `#FF3B30` | 需要用户操作 | PreToolUse(AskUserQuestion) / PermissionRequest |

状态优先级：red > orange > yellow > blue > green（多会话聚合时取最高优先级）。

## 功能特性

### 多会话监控
- 每个 Claude Code 进程维护独立状态文件 `/tmp/claude-monitor-state-$PID`
- 默认「Monitor All Sessions」模式：聚合所有会话，显示最高优先级状态
- 点击菜单中任意会话可单独监控该 PID
- 点击会话项自动将该会话所在窗口带到前台（原生 NSRunningApplication 激活，带 Space 切换动画）

### macOS 系统通知
状态切到红色（需要确认）时发送系统通知，显示项目名。通知去重：3 秒内最多一条。启动后 3 秒内静默（避免初始状态检测误报）。

### Sessions Panel（SwiftUI 原生面板）
- 点击菜单 "Show Sessions Panel" 打开原生 SwiftUI 玻璃拟态窗口
- Go 端每 100ms 写入 `/tmp/claude-monitor-sessions.json`，SwiftUI 端轮询更新
- 会话卡片显示：彩色圆点、项目名、终端应用、状态标签
- 点击卡片激活对应终端/IDE 窗口

### Settings 窗口（API Key 管理）
- 点击菜单 "Settings..." 打开原生 SwiftUI 设置窗口
- 管理 DeepSeek API Key：增删改查、启用/禁用、余额查询
- 可配置余额刷新间隔、轮询间隔、月薪（用于「今日已赚」计算）
- Go 端每 200ms 写入 `/tmp/claude-monitor-keys.json`，SwiftUI 端轮询
- 操作通过 `/tmp/claude-monitor-key-actions.json` 文件桥接回传

### API 用量追踪
- DeepSeek 官方余额 API 查询（需配置 API Key）
- Claude Code JSONL 日志解析（`~/.claude/projects/**/*.jsonl`），按模型统计 token 用量
- CC Switch 代理数据库查询（可选，`~/.cc-switch/cc-switch.db`）
- 用量数据缓存 30 秒

### 今日已赚
- 根据配置的月薪，按工作时间（9:00-18:00，午休 12:00-13:00）实时计算今日收入
- 支持中国法定节假日和调休工作日（内嵌 2026 年节假日数据）
- 每月按 22 个工作日计算

### Hook 合并策略
`WriteHooks()` 合并而非覆写：
1. 读取已有 `~/.claude/settings.json` 和 `~/.claude/settings.local.json`
2. 移除旧版 claude-monitor/novascope 条目
3. 追加新条目
4. 仅当有变更时才写入
5. 用户的其他 hooks 完全不受影响

## 目录结构

```
claude-monitor/
├── main.go                          # 入口：systray UI、状态检测循环、通知、helper 管理
├── go.mod / go.sum                  # Go 模块定义
├── internal/
│   ├── core/
│   │   └── status.go                # 状态枚举、颜色常量、显示名称、解析函数
│   ├── detect/
│   │   ├── hooks.go                 # Hook 写入（合并策略）→ settings.json
│   │   ├── process.go              # Claude 进程列表、阻塞 PID 检测
│   │   ├── state.go                # 会话状态文件读写、会话列表、过期清理
│   │   ├── terminal.go            # 终端应用检测、窗口激活（AppleScript 回退）
│   │   ├── activate_darwin.go     # 原生 NSRunningApplication 窗口激活（CGO + Cocoa）
│   │   ├── sessions_bridge.go     # Sessions JSON 快照写入（Go → SwiftUI 桥接）
│   │   └── usage.go               # API 用量追踪（DeepSeek 余额、JSONL 解析、CC Switch）
│   ├── icon/
│   │   └── icon.go                 # 纯 Go 圆形图标生成（32x32 抗锯齿 PNG）
│   └── settings/
│       ├── keys.go                 # API Key CRUD、余额查询、持久化
│       ├── bridge.go              # Keys JSON 快照 + Action 文件桥接（Go ↔ SwiftUI）
│       ├── earnings.go            # 今日收入计算（工时、节假日）
│       └── holidays/
│           └── holidays-2026.json  # 2026 年中国节假日数据
├── helpers/
│   ├── sessions_panel.swift        # SwiftUI 会话面板（玻璃拟态浮动窗口）
│   └── settings_window.swift       # SwiftUI 设置窗口（API Key 管理）
├── scripts/
│   ├── build_and_launch.sh         # 一键构建 + 启动
│   ├── launch.sh                   # 仅启动（kill 旧进程 + open）
│   ├── install.sh                  # 用户安装脚本（去 quarantine + 启动）
│   └── make_dist.sh               # 分发打包（Universal Binary + zip）
├── Novascope.app/                   # macOS App Bundle
│   └── Contents/
│       ├── MacOS/                   # 主程序 + Swift helper 二进制
│       ├── Resources/               # App 图标
│       └── Info.plist              # LSUIElement=true（无 Dock 图标）
├── assets/                          # 图标源文件
├── icon/                            # 图标资源（icns + png）
├── dist/                            # 分发输出目录
└── docs/                            # 项目文档
```

## 依赖

| 依赖 | 用途 |
|------|------|
| `fyne.io/systray` | 跨平台菜单栏图标库 |
| `github.com/mattn/go-sqlite3` | CC Switch SQLite 数据库读取 |
| CGO + Cocoa framework | 原生窗口激活动画 |
| Swift + SwiftUI | Sessions Panel / Settings 窗口 |

## 已知限制

1. **强依赖 Claude Code hooks**：hook 机制变更会影响状态检测
2. **状态文件在 /tmp**：系统重启后清空
3. **多实例通过 PID 协调**：进程退出后状态文件成为孤儿（下次轮询时自动清理）
4. **Hooks 需重启 Claude Code**：修改 settings.json 后需重启 Claude Code 才能生效
5. **CGO 依赖**：`activate_darwin.go` 需要 macOS 编译环境（CGO_ENABLED=1）
6. **Swift helper 需编译**：Sessions Panel / Settings 功能依赖 `swiftc`，首次点击会自动编译
7. **仅支持 macOS**：CGO + Cocoa + SwiftUI 绑定，不支持其他平台

## 相关文档

- [架构设计](./architecture.md) — 设计模式、模块关系、数据流
- [Hook 机制](./hooks.md) — Claude Code hooks 格式、颜色映射、执行顺序
- [开发指南](./development.md) — 构建、调试、开发注意事项
