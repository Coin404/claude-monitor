# Claude Monitor

macOS 菜单栏指示灯，通过 Claude Code hooks 实时显示运行状态。

## 状态定义

| 颜色 | 含义 | Hook 事件 |
|------|------|-----------|
| 灰 | 未启动 | 无 claude 进程 |
| 绿 | 工作中 | `UserPromptSubmit`（用户提交后立即触发，含思考阶段） |
| 红 | 等待输入 | `Stop`（Claude 完成回复） |
| 黄 | 需要操作 | `PreToolUse` + `AskUserQuestion` 或 CGWindow 检测到权限弹窗 |

## 工作原理

```
Claude Code hooks (settings.json)
    │
    ├── UserPromptSubmit → echo green > /tmp/claude-monitor-state
    ├── Stop → echo red > /tmp/claude-monitor-state
    └── PreToolUse(AskUserQuestion) → echo yellow > /tmp/claude-monitor-state
                                    │
                                    ▼
                          /tmp/claude-monitor-state
                                    │
                                    ▼
                          Go monitor (250ms poll)
                                    │
                                    ▼
                              菜单栏图标
```

Hook 事件由 Claude Code 原生生命周期触发，异步精确，无需手动管理文件。

## 项目结构

```
claude-monitor/
├── main.go              # 入口 + 状态机 + systray UI
├── detector.go          # 进程检测 + hooks 安装 + 状态文件读取
├── a11y_bridge.h        # CGWindow 桥接头
├── a11y_bridge.m        # CGWindow 桥接（ObjC，权限弹窗检测）
├── a11y_darwin.go        # CGWindow 桥接（Go CGO）
├── icon.go              # 彩圈图标
├── Claude Monitor.app/   # macOS app bundle
├── go.mod / go.sum
└── README.md
```

## 构建与启动

```bash
CGO_ENABLED=1 go build -o "Claude Monitor.app/Contents/MacOS/claude-monitor" .
codesign --force --deep --sign - "Claude Monitor.app"
open "Claude Monitor.app"
```

首次启动自动写入 hooks 到 `~/.claude/settings.json`。

## 技术方案演进

| 版本 | 方案 | 问题 |
|------|------|------|
| v1 | Accessibility API + 窗口标题匹配 | 需要辅助功能权限，重签失效，activationPolicy 过滤跳过 CLI 进程 |
| v2 | CGWindow API + CPU 检测 | CPU 快照不准，思考阶段无信号 |
| v3 | 翻转文件协议 | 回复结束 1 秒后误显绿 |
| v4 | Claude Code hooks（当前） | UserPromptSubmit 在思考前触发，时序完美 |
