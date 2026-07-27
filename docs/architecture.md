# 架构设计

## 整体架构

Novascope 采用 **Go 主进程 + Swift Helper 子进程** 的混合架构，通过临时 JSON 文件进行进程间通信（IPC）。

```
┌─────────────────────────────────────────────────────────┐
│                    macOS Menu Bar                         │
│                   (fyne.io/systray)                       │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────┐
│                   main.go (Go 主进程)                     │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐  ┌────────┐ │
│  │状态检测循环│  │会话菜单刷新│  │通知管理    │  │Helper  │ │
│  │(10ms poll)│  │(500ms)   │  │(debounce) │  │管理    │ │
│  └─────┬────┘  └─────┬────┘  └───────────┘  └───┬────┘ │
│        │              │                           │      │
│  ┌─────▼──────────────▼───────────────────────────▼───┐ │
│  │              internal/ 包                            │ │
│  │  core/    detect/    icon/    settings/             │ │
│  └────────────────────────────────────────────────────┘ │
└────────────────────────┬────────────────────────────────┘
                         │ JSON 文件桥接
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
   /tmp/sessions.json  /tmp/keys.json  /tmp/key-actions.json
          │              │              │
          ▼              ▼              ▼
┌──────────────┐  ┌──────────────────────────┐
│ Sessions Panel│  │    Settings Window        │
│ (SwiftUI)     │  │    (SwiftUI)              │
│ panel-helper  │  │    settings-helper        │
└──────────────┘  └──────────────────────────┘
```

## 设计模式

### 1. 文件桥接模式（File Bridge IPC）

Go 与 Swift 之间不使用 XPC 或 socket，而是通过 `/tmp` 下的 JSON 文件进行单向/双向通信：

| 文件 | 方向 | 写入频率 | 用途 |
|------|------|----------|------|
| `/tmp/claude-monitor-sessions.json` | Go → SwiftUI | 100ms | 会话列表快照 |
| `/tmp/claude-monitor-keys.json` | Go → SwiftUI | 200ms | API Key 列表快照 |
| `/tmp/claude-monitor-key-actions.json` | SwiftUI → Go | 事件触发 | 用户操作指令 |
| `/tmp/claude-monitor-selected-pid` | SwiftUI → Go | 事件触发 | 面板选中 PID |

**设计理由**：
- 避免 CGO/Swift 互调的复杂性
- SwiftUI helper 可独立编译、独立运行
- 原子写入（tmp + rename）保证读取一致性
- Helper 崩溃不影响主进程

### 2. 状态机模式（Status State Machine）

```
         SessionStart/Stop
              ┌──────┐
              │ Idle │ (green)
              └──┬───┘
                 │ UserPromptSubmit
              ┌──▼───────┐
              │ Submitted │ (yellow)
              └──┬───────┘
                 │ (立即转为 Working)
              ┌──▼────┐
         ┌───►│Working│ (blue)◄───┐
         │    └──┬────┘           │
         │       │ PreToolUse     │ PostToolUse
         │    ┌──▼────┐           │
         │    │ToolUse│ (orange)──┘
         │    └──┬────┘
         │       │ PreToolUse(AskUserQuestion) / PermissionRequest
         │    ┌──▼────┐
         └────│Blocked│ (red)
              └───────┘
```

状态优先级（多会话聚合）：`red(5) > orange(4) > yellow(3) > blue(2) > green(1)`

### 3. 进程检测模式（Process-based Detection）

- **进程列表**：`ps -eo pid,comm=` 扫描所有 `claude*` 进程
- **状态来源**：每个 Claude 进程通过 hook 写入 `/tmp/claude-monitor-state-$PPID`
- **新鲜度判断**：状态文件修改时间 < 10 秒视为 fresh
- **孤儿清理**：启动时 + 每次 WriteHooks 时清理死进程的状态文件

### 4. Helper 管理模式（Lazy Compile + Singleton）

Swift helper 遵循统一的生命周期：
1. **懒编译**：首次使用时检查二进制是否存在，不存在则 `swiftc` 编译
2. **单例行为**：点击菜单时若 helper 已运行，kill 后重启（确保窗口前置）
3. **退出清理**：`onExit()` 中 `pkill` + PID kill 双重保证

### 5. 原子写入模式

所有 JSON 桥接文件使用 `write-to-tmp + rename` 模式：
```go
tmpPath := path + ".tmp"
os.WriteFile(tmpPath, data, 0644)
os.Rename(tmpPath, path)  // 原子操作，读者不会看到半写状态
```

## 模块职责

### `internal/core` — 核心定义
- Status 枚举（6 种状态）
- 颜色常量（hex）
- 状态解析 / 显示名称映射
- 项目根目录查找、App Support 目录管理

### `internal/detect` — 检测引擎
- **hooks.go**：向 `~/.claude/settings.json` 写入 hook 配置（合并策略）
- **process.go**：`ps` 命令扫描 Claude 进程
- **state.go**：读取 `/tmp/claude-monitor-state-*` 状态文件，聚合最高优先级
- **terminal.go**：进程父链追溯找到宿主 GUI 应用
- **activate_darwin.go**：CGO 调用 NSRunningApplication 平滑激活
- **sessions_bridge.go**：组装会话快照 JSON（含用量、收入数据）
- **usage.go**：DeepSeek 余额 API + JSONL token 统计 + CC Switch 查询

### `internal/icon` — 图标生成
- 纯 Go 生成 32x32 抗锯齿圆形 PNG（无外部资源依赖）

### `internal/settings` — 设置管理
- **keys.go**：API Key CRUD + 余额查询 + 持久化（`~/Library/Application Support/Novascope/`）
- **bridge.go**：Keys 快照写入 + Action 文件监听（Go ↔ SwiftUI 双向桥接）
- **earnings.go**：今日收入计算（工时模型 + 节假日判断）

### `helpers/` — Swift 原生 UI
- **sessions_panel.swift**：SwiftUI 玻璃拟态浮动面板，轮询 sessions.json
- **settings_window.swift**：SwiftUI 设置窗口，轮询 keys.json，写入 key-actions.json

## 数据流

### 状态检测主循环

```
Claude Code 触发 Hook
    → echo "color" > /tmp/claude-monitor-state-$PPID
        → Go 主循环 (每 10ms)
            → ListClaudeProcesses() [ps]
            → ReadHookState() [读状态文件，取最高优先级]
            → ParseHookColor() [color → Status]
            → systray.SetIcon() [更新菜单栏图标]
            → sendNotification() [红色时触发]
```

### Sessions Panel 数据流

```
Go (每 100ms):
    ListClaudeSessions() → SessionInfo[]
    + GetUsageCached() → 用量数据
    + CalculateEarnedToday() → 收入
    → WriteSessionsSnapshot() → /tmp/claude-monitor-sessions.json

SwiftUI Panel (轮询):
    读取 sessions.json → 更新会话卡片 UI
    用户点击卡片 → 写入 /tmp/claude-monitor-selected-pid
                 → NSWorkspace.activate() 激活终端
```

### Settings 数据流

```
Go (每 200ms):
    LoadKeys() → KeyDisplayInfo[]
    → WriteKeysSnapshot() → /tmp/claude-monitor-keys.json

Go (每 N 秒，可配置):
    RefreshAllBalances() → 并发查询 DeepSeek API → SaveKeys()

SwiftUI Settings (轮询):
    读取 keys.json → 更新 Key 列表 UI
    用户操作 → 写入 /tmp/claude-monitor-key-actions.json

Go (每 500ms 监听):
    WatchKeyActions() → 读取 action → 执行 CRUD → 删除 action 文件
```

## 持久化存储

| 路径 | 内容 | 生命周期 |
|------|------|----------|
| `~/Library/Application Support/Novascope/keys.json` | API Key 存储 | 持久 |
| `~/Library/Application Support/Novascope/novascope-settings.json` | 应用设置 | 持久 |
| `~/Library/Application Support/Novascope/claude-monitor-balance-baseline-*.json` | 余额基线 | 每日重置 |
| `~/.claude/settings.json` | Claude Code hooks | 持久（合并写入） |
| `~/.claude/settings.local.json` | Claude Code hooks（备份） | 持久（合并写入） |
| `/tmp/claude-monitor-state-$PID` | 会话实时状态 | 进程生命周期 |
| `/tmp/claude-monitor-sessions.json` | 会话快照（IPC） | 运行时 |
| `/tmp/claude-monitor-keys.json` | Key 快照（IPC） | 运行时 |

## 时序参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| pollIntervalMs | 10ms | 主状态检测循环（可配置：5/10/30/50） |
| sessions snapshot | 100ms | Sessions JSON 写入频率 |
| keys snapshot | 200ms | Keys JSON 写入频率 |
| refreshIntervalSec | 30s | 余额 API 刷新间隔（可配置） |
| session menu refresh | 500ms | 菜单会话列表刷新 |
| key actions watch | 500ms | Action 文件轮询 |
| selected PID watch | 500ms | 面板选中 PID 轮询 |
| state freshness | 10s | 状态文件新鲜度阈值 |
| notification debounce | 3s | 通知去重窗口 |
| startup grace | 3s | 启动静默期（抑制通知） |
| usage cache TTL | 30s | 用量数据缓存时间 |
