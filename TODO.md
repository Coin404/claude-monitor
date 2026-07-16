# Novascope TODO

## 近期计划

- [ ] **全局快捷键**：可配置的全局热键，一键把 Claude 窗口带到前台
- [ ] **自定义颜色/图标**：允许用户在 settings 中自定义各状态的颜色和图标样式
- [ ] **工具提示增强**：鼠标悬停时显示当前状态、活跃会话数、各会话详情

- [x] **状态统计面板**：记录 Claude 在各状态的累计时长，菜单中点击"View Stats Chart"打开原生窗口查看 SVG 甜甜圈图。数据持久化到 `log/stats-YYYY-MM-DD.json`

## 中期规划

- [ ] **历史日志可视化**：解析 log 文件，菜单中显示最近状态变化时间线
- [ ] **声音提醒**：状态切到红色/绿色时可选的系统提示音
- [ ] **Do Not Disturb 集成**：检测 macOS 专注模式，自动静音通知
- [ ] **配置文件**：`~/.novascope.json` 支持自定义轮询间隔、通知开关、颜色映射等

## 长期展望

- [ ] **多 AI 工具支持**：扩展到 Cursor、Windsurf、Copilot 等 AI 编码工具的进程检测
- [ ] **HTTP API / WebSocket**：暴露本地 HTTP 接口，允许其他工具（如 Raycast 插件、BetterTouchTool）读取状态
- [ ] **Sparkle 自动更新**：集成 Sparkle 框架，自动检查并推送新版本
- [ ] **状态分享/协作**：团队成员之间共享 Claude 忙碌状态（类似 Slack status sync）

## 已完成

- [x] **品牌重命名 Novascope**：app 名称、菜单项全部更新
- [x] **原生窗口激活**：NSRunningApplication + NSApplicationActivateAllWindows，丝滑 Space 切换动画（无 osascript 延迟和闪烁）
- [x] **多会话独立监控**：per-PID 状态文件，聚合优先级算法
- [x] **点击会话自动聚焦**：菜单点某个会话直接激活对应窗口
- [x] **macOS 系统通知**：状态切到红色时发送通知，含项目名，3 秒去重
- [x] **状态日志**：状态变化记录到 log 文件，含时间戳和会话快照
- [x] **可调轮询间隔**：5/10/30/50ms 四档菜单可选
- [x] **Hook 安全合并**：写入 hooks 时保留用户自定义配置，仅替换 novascope 管理的条目
- [x] **Bash 工具 red/blue 映射**：PreToolUse(Bash)→red, PostToolUse(Bash)→blue
- [x] **6 色状态机**：灰/绿/黄/蓝/橙/红完整覆盖所有 Claude 状态
