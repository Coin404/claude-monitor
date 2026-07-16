package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/systray"
)

type Status int

const (
	StatusStopped   Status = iota // 灰 — Claude 未运行
	StatusIdle                     // 绿 — 空闲，等待用户
	StatusSubmitted                // 黄 — 用户刚提交 prompt
	StatusWorking                  // 蓝 — 思考/生成中
	StatusToolUse                  // 橙 — 执行工具中
	StatusBlocked                  // 红 — 需要用户操作
)

var pollIntervalMs int32 = 10 // default 10ms, updated atomically

const (
	colorGray   = "#8E8E93"
	colorGreen  = "#34C759"
	colorYellow = "#FFCC00"
	colorBlue   = "#007AFF"
	colorOrange = "#FF9500"
	colorRed    = "#FF3B30"
)

var statusIcons map[Status][]byte

type sessionSlot struct {
	item *systray.MenuItem
	pid  int
}

var (
	selectedPID     int             // 0 = monitor all sessions
	sessionSlots    [10]sessionSlot // pre-allocated menu item slots
	allSessionsItem *systray.MenuItem
	lastNotifyTime  time.Time // debounce notifications
)

// getLogPath returns the path to the log file. It walks up from the
// executable's directory to find go.mod (the project root), then returns
// <project_root>/log/claude-monitor.log. Falls back to /tmp if the
// project root cannot be determined.
func getLogPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "/tmp/claude-monitor.log"
	}
	dir := filepath.Dir(exe)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "log", "claude-monitor.log")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "/tmp/claude-monitor.log"
}

// ensureLogDir creates the log directory if it doesn't exist.
func ensureLogDir() {
	dir := filepath.Dir(getLogPath())
	os.MkdirAll(dir, 0755)
}

// sessionStateSnapshot returns a string summarizing all current session
// states, e.g. "sessions: 12345=green, 12346=yellow".
func sessionStateSnapshot() string {
	sessions := ListClaudeSessions()
	if len(sessions) == 0 {
		return "sessions: none"
	}
	pids := make([]int, 0, len(sessions))
	for pid := range sessions {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, fmt.Sprintf("%d=%s", pid, sessions[pid]))
	}
	return "sessions: " + strings.Join(parts, ", ")
}

func main() {
	statusIcons = map[Status][]byte{
		StatusStopped:   GenerateCircleIcon(colorGray),
		StatusIdle:      GenerateCircleIcon(colorGreen),
		StatusSubmitted: GenerateCircleIcon(colorYellow),
		StatusWorking:   GenerateCircleIcon(colorBlue),
		StatusToolUse:   GenerateCircleIcon(colorOrange),
		StatusBlocked:   GenerateCircleIcon(colorRed),
	}

	if err := WriteHooks(); err != nil {
		fmt.Fprintf(os.Stderr, "claude-monitor: failed to write hooks: %v\n", err)
	}

	systray.Run(onReady, onExit)
}

func onReady() {
	ensureLogDir()

	rewriteItem := systray.AddMenuItem("Re-write Hooks", "重新写入 Claude Code hooks 配置")
	systray.AddSeparator()

	allSessionsItem = systray.AddMenuItem("Monitor All Sessions", "监控所有运行中的会话")
	allSessionsItem.Check()
	systray.AddSeparator()
	for i := range sessionSlots {
		sessionSlots[i].item = systray.AddMenuItem("", "")
		sessionSlots[i].item.Hide()
	}

	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit", "退出 Novascope")

	// Poll interval submenu
	systray.AddSeparator()
	item5ms := systray.AddMenuItem("Poll: 5ms", "轮询间隔 5ms")
	item10ms := systray.AddMenuItem("Poll: 10ms", "轮询间隔 10ms")
	item30ms := systray.AddMenuItem("Poll: 30ms", "轮询间隔 30ms")
	item50ms := systray.AddMenuItem("Poll: 50ms", "轮询间隔 50ms")
	item10ms.Check() // default

	intervalItems := map[int32]*systray.MenuItem{5: item5ms, 10: item10ms, 30: item30ms, 50: item50ms}

	currentStatus := StatusStopped
	systray.SetIcon(statusIcons[currentStatus])

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		systray.Quit()
	}()

	go func() {
		for range rewriteItem.ClickedCh {
			if err := WriteHooks(); err != nil {
				fmt.Fprintf(os.Stderr, "claude-monitor: failed to re-write hooks: %v\n", err)
			}
		}
	}()

	go func() {
		for range quitItem.ClickedCh {
			systray.Quit()
		}
	}()

	// Handle session selection clicks — switch monitoring AND bring window to front
	go func() {
		for range allSessionsItem.ClickedCh {
			selectedPID = 0
			refreshSessionMenu()
		}
	}()
	for i := range sessionSlots {
		i := i
		go func() {
			for range sessionSlots[i].item.ClickedCh {
				pid := sessionSlots[i].pid
				selectedPID = pid
				refreshSessionMenu()
				// Bring the selected session's window to front
				if pid != 0 {
					appName := FindTerminalApp(pid)
					projectPath := sessionWorkPath(pid)
					ActivateTerminal(appName, projectPath)
				}
			}
		}()
	}

	// Handle poll interval changes
	for ms, item := range intervalItems {
		ms := ms
		it := item
		go func() {
			for range it.ClickedCh {
				atomic.StoreInt32(&pollIntervalMs, ms)
				for _, mi := range intervalItems {
					mi.Uncheck()
				}
				it.Check()
			}
		}()
	}

	// Refresh session menu on tray open or every 500ms
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-systray.TrayOpenedCh:
				refreshSessionMenu()
			case <-ticker.C:
				refreshSessionMenu()
			}
		}
	}()

	go func() {
		for {
			time.Sleep(time.Duration(atomic.LoadInt32(&pollIntervalMs)) * time.Millisecond)
			detected, reason := detectStatus()
			if detected != currentStatus {
				// Notify only when entering blocked (red), include session name
				if detected == StatusBlocked {
					pid := selectedPID
					if pid == 0 {
						pid = findBlockedPID()
					}
					project := "Claude"
					if pid != 0 {
						project = sessionWorkDir(pid)
					}
					sendNotification("Novascope", project+" 需要你的确认")
				}
				logStatusChange(currentStatus, detected, reason)
				currentStatus = detected
				systray.SetIcon(statusIcons[currentStatus])
			}
		}
	}()
}

func onExit() {}

func statusLabel(s Status) string {
	switch s {
	case StatusStopped:
		return "stopped(gray)"
	case StatusIdle:
		return "idle(green)"
	case StatusSubmitted:
		return "submitted(yellow)"
	case StatusWorking:
		return "working(blue)"
	case StatusToolUse:
		return "tooluse(orange)"
	case StatusBlocked:
		return "blocked(red)"
	default:
		return "unknown"
	}
}

func logStatusChange(old, new Status, reason string) {
	f, err := os.OpenFile(getLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	now := time.Now().Format("2006-01-02 15:04:05")
	snapshot := sessionStateSnapshot()
	if selectedPID != 0 {
		fmt.Fprintf(f, "%s [PID %d] %s → %s (%s) | %s\n", now, selectedPID, statusLabel(old), statusLabel(new), reason, snapshot)
	} else {
		fmt.Fprintf(f, "%s [all] %s → %s (%s) | %s\n", now, statusLabel(old), statusLabel(new), reason, snapshot)
	}
}

// sendNotification posts a macOS user notification via osascript.
// Notifications are debounced to at most one every 3 seconds.
func sendNotification(title, message string) {
	if time.Since(lastNotifyTime) < 3*time.Second {
		return
	}
	lastNotifyTime = time.Now()
	script := fmt.Sprintf(
		`display notification "%s" with title "%s"`,
		strings.ReplaceAll(message, `"`, `\"`),
		strings.ReplaceAll(title, `"`, `\"`),
	)
	exec.Command("osascript", "-e", script).Start()
}

func refreshSessionMenu() {
	sessions := ListClaudeSessions()

	// Sort PIDs ascending
	pids := make([]int, 0, len(sessions))
	for pid := range sessions {
		pids = append(pids, pid)
	}
	sort.Ints(pids)

	// Fill slots (max 10)
	limit := len(pids)
	if limit > 10 {
		limit = 10
	}

	// Check if selected PID is still alive
	if selectedPID != 0 {
		_, alive := sessions[selectedPID]
		if !alive {
			selectedPID = 0
		}
	}

	for i := 0; i < 10; i++ {
		if i < limit {
			pid := pids[i]
			color := sessions[pid]
			project := sessionWorkDir(pid)
			sessionSlots[i].pid = pid
			sessionSlots[i].item.SetTitle(fmt.Sprintf("%s (PID %d) - %s", project, pid, color))
			if pid == selectedPID {
				sessionSlots[i].item.Check()
			} else {
				sessionSlots[i].item.Uncheck()
			}
			sessionSlots[i].item.Show()
		} else {
			sessionSlots[i].pid = 0
			sessionSlots[i].item.Hide()
		}
	}

	// Update "Monitor All Sessions" check state
	if selectedPID == 0 {
		allSessionsItem.Check()
	} else {
		allSessionsItem.Uncheck()
	}
}

func detectStatus() (Status, string) {
	// If monitoring a specific session, return its status directly
	if selectedPID != 0 {
		color, fresh := ReadSessionState(selectedPID)
		if fresh {
			switch color {
			case "green":
				return StatusIdle, "hook: green"
			case "blue":
				return StatusWorking, "hook: blue"
			case "yellow":
				return StatusSubmitted, "hook: yellow"
			case "orange":
				return StatusToolUse, "hook: orange"
			case "red":
				return StatusBlocked, "hook: red"
			default:
				return StatusIdle, "hook: " + color
			}
		}
		// Session died — auto-reset to monitor-all and fall through
		selectedPID = 0
	}

	runningPIDs := ListClaudeProcesses()
	if len(runningPIDs) == 0 {
		return StatusStopped, "no claude process"
	}

	hookState, _ := ReadHookState(runningPIDs)
	if hookState == "" {
		return StatusIdle, "no hook state"
	}

	switch hookState {
	case "green":
		return StatusIdle, "hook: green"
	case "blue":
		return StatusWorking, "hook: blue"
	case "yellow":
		return StatusSubmitted, "hook: yellow"
	case "orange":
		return StatusToolUse, "hook: orange"
	case "red":
		return StatusBlocked, "hook: red"
	default:
		return StatusIdle, "hook: " + hookState
	}
}
