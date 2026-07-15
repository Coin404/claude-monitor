# Claude Monitor

macOS 菜单栏指示灯，通过 Claude Code hooks + CGWindow 对话框检测，实时显示 Claude 工作状态。

设计参考 [claude-traffic-light](https://github.com/TaylorSimery/claude-traffic-light)（Electron 版），使用 Go + systray 实现更轻量的原生菜单栏应用。

## 启动方式

```bash
# 日常启动（双击 Finder 中的 .app 也可以）
open "/Users/xuyi/Desktop/Coin/Personal/Code/Claude-Flash/claude-monitor/Claude Monitor.app"
```

首次启动可能需右键 → 打开（未签名应用）。应用启动后出现在**菜单栏右侧**，无 Dock 图标（`LSUIElement = true`）。

**开机自启动**：系统设置 → 通用 → 登录项与扩展 → 添加 `Claude Monitor.app`。

**重新构建**：
```bash
cd /Users/xuyi/Desktop/Coin/Personal/Code/Claude-Flash/claude-monitor
CGO_ENABLED=1 go build -o "Claude Monitor.app/Contents/MacOS/claude-monitor" .

# 杀掉旧进程，启动新的
pkill -f "Claude Monitor.app" ; sleep 0.3
open "Claude Monitor.app"
```

## 状态定义（6 色）

| 状态 | 图标色 | 含义 | 触发来源 |
|------|--------|------|----------|
| Stopped | 灰 `#8E8E93` | Claude 未运行 | ps 检测无 claude 进程 |
| Idle | 绿 `#34C759` | 空闲，等待用户输入 | Stop hook 或 fallback（无 proxy 连接） |
| Submitted | 黄 `#FFCC00` | 用户刚提交 prompt | UserPromptSubmit hook |
| Working | 蓝 `#007AFF` | 思考/生成中 | fallback（proxy 活跃且无弹窗） |
| ToolUse | 橙 `#FF9500` | 开始输出/SessionStart | SessionStart hook |
| Blocked | 红 `#FF3B30` | 需要用户操作 | PreToolUse(AskUserQuestion) hook 或 CGWindow 检测到弹窗 |

## 实现思路

### 核心设计：两层检测机制

```
第一层（优先）：Claude Code Hooks
  UserPromptSubmit          → echo yellow > /tmp/claude-monitor-state
  SessionStart              → echo orange > /tmp/claude-monitor-state
  PreToolUse(AskUserQuestion) → echo red    > /tmp/claude-monitor-state
  Stop                      → echo green  > /tmp/claude-monitor-state
  状态文件修改时间 < 10s → fresh，直接使用

第二层（Fallback）：系统级检测（hooks 过期 >10s 时触发）
  CheckProxyActivity()  → lsof TCP:15721 检测代理连接
  HasDialogWindow()     → CGWindow API 检测弹窗层窗口
```

### 状态检测流程

```
detectStatus() 每 30ms 执行一次，所有检测实时无缓存：

1. CheckClaudeProcess() == false  →  StatusStopped（灰）
   ps -eo pid,comm=，精确匹配 comm == "claude"，排除自身 PID

2. ReadHookState() 读取 /tmp/claude-monitor-state:
   fresh = 文件修改时间距今 < 10 秒

   2a. fresh（hook 新鲜）:
       "green"  → StatusIdle
       "yellow" → StatusSubmitted
       "orange" → StatusToolUse
       "red"    → StatusBlocked
       "blue"   → StatusWorking
       default  → StatusIdle

   2b. !fresh（hook 过期，fallback）:
       CheckProxyActivity() == false → StatusIdle（绿）
       CheckProxyActivity() == true:
         HasDialogWindow() == true  → StatusBlocked（红: 有弹窗）
         HasDialogWindow() == false → StatusWorking（蓝: 工作中）
```

### 为什么不用 CheckWaitingForInput（stdin TTY + 进程睡眠）

之前 fallback 用「stdin 是 TTY + 进程状态 S (sleeping)」判断是否在等用户输入。但 `S` 状态在等 API 响应时也会出现，导致**误判为红色**。

现在用 `HasDialogWindow()` 通过 CGWindow 枚举真正可见的对话框窗口（layer 1-100），仅匹配 Claude 相关进程（`claude`、`SecurityAgent`、`UserNotificationCenter`），准确率大幅提升。

### 为什么无缓存

所有检测函数（`CheckClaudeProcess`、`CheckProxyActivity`、`HasDialogWindow`）每 30ms 实时执行，不缓存结果。理由：
- 状态变化必须立即反映到指示灯（如 AskUserQuestion 弹窗出现 → 红色，关闭 → 其他色）
- 500ms 缓存延迟在快速切换场景下会导致指示灯闪烁/滞后
- CGWindow 枚举和 ps/lsof 在 30ms 间隔下性能足够

### Hook 设计（4 个事件）

当前仅注册 4 个 hook 事件，PreToolUse 只有 AskUserQuestion matcher（不注册通用 matcher，避免覆盖问题）：

```
UserPromptSubmit          → yellow  (用户提交 prompt)
SessionStart              → orange  (开始输出)
PreToolUse(AskUserQuestion) → red    (需要用户操作)
Stop                      → green   (完成，等待用户)
```

**PreToolUse 不注册通用 matcher**：如果同时有通用 PreToolUse（无 matcher）和专用 PreToolUse（matcher=AskUserQuestion），通用条目会覆盖专用条目导致 AskUserQuestion 颜色错误。

**不用 PostToolUse**：PostToolUse 在 PreToolUse(AskUserQuestion) 之后、用户回答之前就触发，导致红→蓝→红的闪烁。

### Hook 合并策略

`WriteHooks()` 合并而非覆写，参考 traffic-light 实现：
1. 读取现有 `~/.claude/settings.json` 和 `~/.claude/settings.local.json`
2. 在受管事件中识别并移除含 `/tmp/claude-monitor-state` 的旧条目
3. 追加新条目
4. 仅当有变更时才写入磁盘
5. 用户的**其他 hooks 完全不受影响**

## 架构总览

```
┌─────────────────────────────────────────────────────┐
│                    Claude Code                        │
│                                                       │
│  hooks → echo <color> > /tmp/claude-monitor-state    │
│    UserPromptSubmit        → yellow                   │
│    SessionStart            → orange                   │
│    PreToolUse(AskUserQuestion) → red                  │
│    Stop                    → green                    │
└──────────────────────────────┬──────────────────────┘
                               │ write
                               ▼
                  /tmp/claude-monitor-state
                               │
                               │ read (30ms poll, 无缓存)
                               ▼
┌─────────────────────────────────────────────────────┐
│                claude-monitor (Go)                    │
│                                                       │
│  ┌──────────────┐   ┌──────────────┐                 │
│  │ detector.go   │   │   main.go    │                 │
│  │               │   │              │                 │
│  │ ReadHookState │──▶│ detectStatus │                 │
│  │ CheckProcess  │   │   (状态机)    │                 │
│  │ CheckProxy    │   └──────┬───────┘                 │
│  │ WriteHooks    │          │                          │
│  └───────────────┘          ▼                          │
│                      ┌──────────────┐                 │
│                      │  systray     │                 │
│                      │  SetIcon()   │                 │
│                      └──────┬───────┘                 │
│                             │                          │
│  ┌──────────────────┐       │                          │
│  │ a11y_darwin.go    │       │                          │
│  │ + a11y_bridge.m   │       │                          │
│  │ CGWindow 弹窗检测  │       │                          │
│  │ + 辅助功能权限请求  │       │                          │
│  └────────┬──────────┘       │                          │
│           │                  │                          │
│           └────────┬─────────┘                          │
│                    ▼                                    │
│           菜单栏彩色圆点                                 │
└─────────────────────────────────────────────────────┘
```

## 文件说明

| 文件 | 职责 |
|------|------|
| `main.go` | 入口 + 状态机 `detectStatus()` + systray UI |
| `detector.go` | 进程检测 `CheckClaudeProcess()`、状态文件读取 `ReadHookState()`、代理检测 `CheckProxyActivity()`、hooks 写入 `WriteHooks()` |
| `icon.go` | 纯 Go 圆形图标生成（32×32 抗锯齿 PNG） |
| `a11y_bridge.h/m` | ObjC CGWindow API 封装 + 辅助功能权限请求 |
| `a11y_darwin.go` | CGO 桥接层：`HasDialogWindow()`、`RequestAccessibilityPermission()` |
| `CLAUDE.md` | 开发笔记：hook 格式陷阱、调试记录 |

## 与 claude-traffic-light 的对比

| 维度 | claude-traffic-light | claude-monitor |
|------|---------------------|----------------|
| 技术栈 | Electron + React + TypeScript | Go + systray + CGO |
| 包体积 | ~200MB | ~5MB |
| 视觉效果 | 浮动红绿灯窗口 + 呼吸动画 + 音效 | 菜单栏纯色圆点 |
| 统计 | 每日红/绿计数 + 时长 + 周报 | 无 |
| 检测机制 | 文件轮询 + lsof | hooks + CGWindow fallback |
| 轮询间隔 | 300ms | 30ms（实时无缓存） |
| 颜色数 | 3 | 6 |

## 依赖

| 依赖 | 用途 |
|------|------|
| `fyne.io/systray` | 跨平台菜单栏图标库 |
| CoreGraphics.framework | CGWindow 弹窗检测 |
| ApplicationServices.framework | 辅助功能权限请求 |

## 技术方案演进

| 版本 | 方案 | 核心缺陷 |
|------|------|----------|
| v1 | Accessibility API + 窗口标题匹配 | 需辅助功能权限；重签后权限失效 |
| v2 | CGWindow + CPU 快照 | CPU 采样不准；思考阶段无信号 |
| v3 | 翻转文件协议 | 时序问题：回复结束后才切换状态 |
| v4 | Claude Code hooks（6 hook） | PostToolUse 导致红蓝闪烁 |
| v5 | **Claude Code hooks（4 hook）+ CGWindow fallback（当前）** | — |

## 已知限制

1. **强依赖 Claude Code hooks**：如果 Claude Code 未运行或 hook 机制变更，依赖 fallback（仅能区分 Idle/Working/Blocked 三种状态）
2. **状态文件无持久化**：`/tmp/claude-monitor-state` 系统重启后清空
3. **无 idle 超时检测**：Claude 空闲时状态取决于最后一次 hook 写入
4. **无多实例协调**：多个 Claude Code 实例竞争写入同一状态文件
