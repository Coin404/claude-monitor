# 开发指南

## 环境要求

- macOS（CGO + Cocoa + SwiftUI 依赖）
- Go 1.26+（`CGO_ENABLED=1`）
- Xcode Command Line Tools（`swiftc` 编译 Swift helper）
- Claude Code（hook 来源）

## 构建

### 一键构建并启动

```bash
./scripts/build_and_launch.sh
```

该脚本执行：
1. 清理旧二进制
2. `go build` 主程序
3. `swiftc` 编译 sessions_panel.swift → novascope-panel-helper
4. `swiftc` 编译 settings_window.swift → novascope-settings-helper
5. kill 旧进程
6. `open Novascope.app`

### 仅构建主程序

```bash
go build -o "Novascope.app/Contents/MacOS/claude-monitor" .
```

### 分发打包（Universal Binary）

```bash
./scripts/make_dist.sh
```

输出 `dist/Novascope-v1.0.zip`，包含 arm64 + amd64 通用二进制。

## 开发调试

### 直接运行（不通过 App Bundle）

```bash
go run .
```

注意：直接运行时 `FindProjectRoot()` 会从可执行文件路径向上查找 `go.mod`，确保在项目根目录执行。

### 查看日志

应用日志输出到 stderr（`fmt.Fprintf(os.Stderr, ...)`）。

### 查看状态文件

```bash
# 列出所有会话状态文件
ls /tmp/claude-monitor-state-*

# 查看某会话状态
cat /tmp/claude-monitor-state-12345

# 查看 sessions 快照
cat /tmp/claude-monitor-sessions.json | python3 -m json.tool

# 查看 keys 快照
cat /tmp/claude-monitor-keys.json | python3 -m json.tool
```

### 手动触发状态变化

```bash
# 模拟 hook 写入（替换为实际 Claude PID）
echo red > /tmp/claude-monitor-state-12345
```

## 代码规范

### Go 代码

- 模块名：`claude-monitor`
- 包结构：`internal/` 下按职责分包（core、detect、icon、settings）
- JSON 序列化：使用 `json.NewEncoder` + `SetEscapeHTML(false)`，避免 HTML 转义
- 文件写入：所有对外 JSON 使用原子写入（tmp + rename）
- 并发：使用 `atomic` 操作共享配置（pollIntervalMs、refreshIntervalSec）
- 互斥锁：KeyStore 读写使用 `sync.Mutex`

### Swift Helper

- 独立编译，不依赖 Go 代码
- 通过 JSON 文件与 Go 通信（无 XPC/socket）
- 使用 SwiftUI + AppKit 混合（`.hudWindow` 材质、`.floating` 层级）
- 无 Dock 图标（`.accessory` activation policy）

## 关键实现细节

### 进程检测

`ListClaudeProcesses()` 使用 `ps -eo pid,comm=` 扫描，匹配规则：
- 进程名为 `claude` 或以 `claude` 开头
- 排除自身 PID
- 排除 `claude-monitor`

### 终端应用查找

`FindTerminalApp(pid)` 沿进程父链向上追溯（最多 10 层），跳过 shell（bash/zsh/sh/fish 等）和系统进程（launchd/login/tmux 等），返回第一个 GUI 应用名。

### 窗口激活

优先级：
1. **NSRunningApplication**（CGO）— 平滑 Space 切换动画
2. **System Events set frontmost**（AppleScript）
3. **tell application activate**（AppleScript）
4. **遍历常见终端**（Terminal/iTerm2/Warp/kitty/WezTerm/Alacritty）

### 余额追踪

使用「基线差值法」计算当日消费：
- 每天首次查询记录 baseline
- 检测充值（余额上升）并累加 topUps
- spending = (baseline + topUps) - currentBalance

### 今日收入

```
dailySalary = monthlySalary / 22
earnedToday = dailySalary × (已工作秒数 / 28800)
```

工作时间：9:00-12:00 + 13:00-18:00（8 小时 = 28800 秒），支持节假日/调休。

## Git 工作流

- 仓库位于 `claude-monitor/` 目录
- 非 monorepo（不是 Claude-Flash 根目录的子模块）

## TODO

- [ ] **全局快捷键**：可配置的全局热键，一键把 Claude 窗口带到前台
