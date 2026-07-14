# Claude Monitor

macOS 菜单栏指示灯，实时显示 Claude Code CLI 的运行状态。

## 状态定义

| 颜色 | 含义 | 触发条件 |
|------|------|----------|
| 灰 `#8E8E93` | 未启动 | 无 claude 进程 |
| 红 `#FF3B30` | 等待输入 | done 文件 mtime < 1 秒（刚结束回复） |
| 绿 `#34C759` | 工作中 | 默认（done 文件不存在或过期 > 1 秒） |
| 黄 `#FFCC00` | 需要操作 | question 文件存在 或 检测到权限弹窗 |

## 检测优先级

```
1. 无 claude 进程           → 灰 (StatusStopped)
2. 权限弹窗 (CGWindow)       → 黄 (StatusBlocked)
3. question 文件存在         → 黄 (StatusBlocked)
4. done 文件 mtime < 1s     → 红 (StatusWaiting)
5. 其他                     → 绿 (StatusActive)
```

## 信号文件协议

Claude（AI 侧）在每个回复中管理 `/tmp` 目录下的文件来通知 monitor 自身状态。

### 回复结束时（最后一步）
```bash
touch /tmp/claude-done
rm -f /tmp/claude-question
```

### 使用 AskUserQuestion 时
```bash
touch /tmp/claude-question
```

### 回复开始时
不需要操作。done 文件已过期 > 1 秒，monitor 自动判定为绿色。

### 为什么用翻转逻辑？

monitor 无法知道 Claude 何时开始思考。如果回复开始时才 `touch` busy 文件，
思考阶段的 2-3 秒内就会误显红色。改由回复结束 `touch` done 文件，用 mtime
判断是否"刚结束"，思考阶段自动绿色。

## 权限弹窗检测

`a11y_bridge.m` 使用 CGWindow API（`CGWindowListCopyWindowInfo`）扫描所有
窗口，找出 dialog 层级 (layer 1-100) 且不属于基线进程的窗口。

- **无需系统权限**：CGWindow API 不需要辅助功能或屏幕录制权限
- **基线进程**（始终忽略）：Window Server、控制中心、程序坞/Dock、通知中心、Dynamic Wallpaper、访达、GoLand
- **检测原理**：权限弹窗出现时，system dialog 窗口进入 dialog 层级范围

## 项目结构

```
claude-monitor/
├── main.go              # 入口 + 状态机 + systray UI
├── detector.go          # claude 进程检测 + 信号文件读取
├── a11y_bridge.h        # CGWindow 桥接头文件
├── a11y_bridge.m        # CGWindow 桥接实现（ObjC）
├── a11y_darwin.go        # CGWindow 桥接（Go CGO）
├── icon.go              # 彩圈图标生成
├── permission.go        # osascript 方案（已废弃，保留备用）
├── Claude Monitor.app/   # macOS app bundle
├── go.mod / go.sum      # Go 依赖
└── README.md            # 本文档
```

## 构建与启动

```bash
CGO_ENABLED=1 go build -o "Claude Monitor.app/Contents/MacOS/claude-monitor" .
codesign --force --deep --sign - "Claude Monitor.app"
open "Claude Monitor.app"
```

## 技术方案演进

### 版本 1：Accessibility API（废弃）
- `AXUIElementCopyAttributeValue` 读取所有进程窗口标题
- 匹配关键词：`would like to`、`permission`、`允许`、`访问`
- **问题**：需要辅助功能权限，每次 `codesign` 后 TCC 权限失效；`activationPolicy` 过滤跳过了命令行进程

### 版本 2：CGWindow API + CPU 检测（废弃）
- CGWindow API 检测 dialog 层级窗口（不需要权限）
- CPU 使用率判断 claude 是否活跃（> 0.5% = 工作中）
- **问题**：CPU 快照不准，瞬态波动不可靠；思考阶段无法立即更新

### 版本 3：信号文件协议（当前）
- 翻转逻辑：回复结束 `touch /tmp/claude-done`，mtime 判断"刚结束"
- 权限弹窗仍用 CGWindow API
- 轮询间隔 20ms
- **优势**：思考阶段自动绿色，无需权限，签名无关
