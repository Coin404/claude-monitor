package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/systray"

	"claude-monitor/internal/core"
	"claude-monitor/internal/detect"
	"claude-monitor/internal/icon"
	"claude-monitor/internal/stats"
)

var pollIntervalMs int32 = 10 // default 10ms, updated atomically

var statusIcons map[core.Status][]byte

type sessionSlot struct {
	item *systray.MenuItem
	pid  int
}

var (
	selectedPID     int             // 0 = monitor all sessions
	sessionSlots    [10]sessionSlot // pre-allocated menu item slots
	allSessionsItem *systray.MenuItem
	lastNotifyTime  time.Time // debounce notifications
	startupTime     time.Time // suppresses notification during startup grace period
	tracker         *stats.Tracker
)

// sessionStateSnapshot returns a string summarizing all current session
// states, e.g. "sessions: 12345=green, 12346=yellow".
func sessionStateSnapshot() string {
	sessions := detect.ListClaudeSessions()
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
	statusIcons = map[core.Status][]byte{
		core.StatusStopped:   icon.GenerateCircleIcon(core.ColorGray),
		core.StatusIdle:      icon.GenerateCircleIcon(core.ColorGreen),
		core.StatusSubmitted: icon.GenerateCircleIcon(core.ColorYellow),
		core.StatusWorking:   icon.GenerateCircleIcon(core.ColorBlue),
		core.StatusToolUse:   icon.GenerateCircleIcon(core.ColorOrange),
		core.StatusBlocked:   icon.GenerateCircleIcon(core.ColorRed),
	}

	if err := detect.WriteHooks(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "claude-monitor: failed to write hooks: %v\n", err)
	}

	systray.Run(onReady, onExit)
}

func onReady() {
	core.EnsureLogDir()
	startupTime = time.Now()

	rewriteItem := systray.AddMenuItem("Re-write Hooks", "Re-write Claude Code hook configuration")
	systray.AddSeparator()

	allSessionsItem = systray.AddMenuItem("Monitor All Sessions", "Monitor all running sessions")
	allSessionsItem.Check()
	systray.AddSeparator()
	for i := range sessionSlots {
		sessionSlots[i].item = systray.AddMenuItem("", "")
		sessionSlots[i].item.Hide()
	}

	systray.AddSeparator()
	tracker = stats.NewTracker()
	stats.CleanupOldStats(7)
	statsChartItem := systray.AddMenuItem("View Stats Chart", "Open statistics chart in a native window")
	systray.AddSeparator()

	quitItem := systray.AddMenuItem("Quit", "Quit Novascope")

	// Poll interval submenu
	systray.AddSeparator()
	item5ms := systray.AddMenuItem("Poll: 5ms", "Poll interval 5ms")
	item10ms := systray.AddMenuItem("Poll: 10ms", "Poll interval 10ms")
	item30ms := systray.AddMenuItem("Poll: 30ms", "Poll interval 30ms")
	item50ms := systray.AddMenuItem("Poll: 50ms", "Poll interval 50ms")
	item10ms.Check() // default

	intervalItems := map[int32]*systray.MenuItem{5: item5ms, 10: item10ms, 30: item30ms, 50: item50ms}

	currentStatus := core.StatusStopped
	systray.SetIcon(statusIcons[currentStatus])

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		systray.Quit()
	}()

	go func() {
		for range rewriteItem.ClickedCh {
			if err := detect.WriteHooks(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "claude-monitor: failed to re-write hooks: %v\n", err)
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
					appName := detect.FindTerminalApp(pid)
					_ = detect.ActivateTerminal(appName)
				}
			}
		}()
	}

	// Handle stats chart click
	go func() {
		for range statsChartItem.ClickedCh {
			ds := tracker.Snapshot()
			_ = stats.OpenStatsInBrowser(ds)
		}
	}()

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
				// Notify only when entering blocked (red), suppress
				// during the first 3s after startup to avoid alerts
				// from the initial state detection.
				if detected == core.StatusBlocked && time.Since(startupTime) >= 3*time.Second {
					pid := selectedPID
					if pid == 0 {
						pid = detect.FindBlockedPID()
					}
					project := "Claude"
					if pid != 0 {
						project = detect.SessionWorkDir(pid)
					}
					sendNotification("Novascope", project+" needs your attention")
				}
				logStatusChange(currentStatus, detected, reason)
				tracker.RecordStatusChange(detected)
				currentStatus = detected
				systray.SetIcon(statusIcons[currentStatus])
			}
		}
	}()
}

func onExit() {
	tracker.FlushCurrentSession()
}

func logStatusChange(old, new core.Status, reason string) {
	f, err := os.OpenFile(core.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	now := time.Now().Format("2006-01-02 15:04:05")
	snapshot := sessionStateSnapshot()
	if selectedPID != 0 {
		_, _ = fmt.Fprintf(f, "%s [PID %d] %s → %s (%s) | %s\n", now, selectedPID, core.StatusLabel(old), core.StatusLabel(new), reason, snapshot)
	} else {
		_, _ = fmt.Fprintf(f, "%s [all] %s → %s (%s) | %s\n", now, core.StatusLabel(old), core.StatusLabel(new), reason, snapshot)
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
	_ = exec.Command("osascript", "-e", script).Start()
}

func refreshSessionMenu() {
	sessions := detect.ListClaudeSessions()

	// Sort PIDs ascending
	pids := make([]int, 0, len(sessions))
	for pid := range sessions {
		pids = append(pids, pid)
	}
	sort.Ints(pids)

	// Fill slots (max 10)
	limit := min(len(pids), 10)

	// Check if selected PID is still alive
	if selectedPID != 0 {
		_, alive := sessions[selectedPID]
		if !alive {
			selectedPID = 0
		}
	}

	for i := range 10 {
		if i < limit {
			pid := pids[i]
			color := sessions[pid]
			project := detect.SessionWorkDir(pid)
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

func detectStatus() (core.Status, string) {
	// If monitoring a specific session, return its status directly
	if selectedPID != 0 {
		color, fresh := detect.ReadSessionState(selectedPID)
		if fresh {
			s := core.ParseHookColor(color)
			return s, "hook: " + color
		}
		// Session died — auto-reset to monitor-all and fall through
		selectedPID = 0
	}

	runningPIDs := detect.ListClaudeProcesses()
	if len(runningPIDs) == 0 {
		return core.StatusStopped, "no claude process"
	}

	hookState, fresh := detect.ReadHookState(runningPIDs)
	_ = fresh
	if hookState == "" {
		return core.StatusIdle, "no hook state"
	}

	s := core.ParseHookColor(hookState)
	return s, "hook: " + hookState
}
