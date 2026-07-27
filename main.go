package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/systray"

	"claude-monitor/internal/core"
	"claude-monitor/internal/detect"
	"claude-monitor/internal/icon"
	"claude-monitor/internal/settings"
)

var pollIntervalMs int32 = 10     // default 10ms, updated atomically
var refreshIntervalSec int32 = 30 // default 30s, updated atomically

var statusIcons map[core.Status][]byte

type sessionSlot struct {
	item *systray.MenuItem
	pid  int
}

var (
	selectedPID       int             // 0 = monitor all sessions
	sessionSlots      [10]sessionSlot // pre-allocated menu item slots
	allSessionsItem   *systray.MenuItem
	lastNotifyTime    time.Time // debounce notifications
	startupTime       time.Time // suppresses notification during startup grace period
	panelHelperPID    int       // PID of sessions panel helper (0 = not running)
	settingsHelperPID int       // PID of settings window helper (0 = not running)
)

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
	sessionsPanelItem := systray.AddMenuItem("Show Sessions Panel", "Open sessions overview in a native window")
	settingsWindowItem := systray.AddMenuItem("Settings...", "Manage DeepSeek API keys and settings")
	systray.AddSeparator()

	quitItem := systray.AddMenuItem("Quit", "Quit Novascope")

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

	// Handle sessions panel click
	go func() {
		for range sessionsPanelItem.ClickedCh {
			launchSessionsPanel()
		}
	}()

	// Handle settings window click
	go func() {
		for range settingsWindowItem.ClickedCh {
			launchSettingsWindow()
		}
	}()

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

	// Write sessions snapshot JSON for the SwiftUI panel every 100ms
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			detect.WriteSessionsSnapshot()
		}
	}()

	// === Settings / DeepSeek Key Management ===

	// 1. Startup: migrate DEEPSEEK_API_KEY env var into key store if not already present
	go func() {
		if envKey := os.Getenv("DEEPSEEK_API_KEY"); envKey != "" {
			store := settings.LoadKeys()
			found := false
			for _, e := range store.Keys {
				if e.Key == envKey {
					found = true
					break
				}
			}
			if !found {
				if _, err := settings.AddKey("DEEPSEEK_API_KEY", envKey, "", ""); err == nil {
					_, _ = fmt.Fprintf(os.Stderr, "claude-monitor: migrated DEEPSEEK_API_KEY env var to key store\n")
				}
			}
		}
	}()

	// 2. Load app settings and set initial intervals
	go func() {
		appSettings := settings.LoadAppSettings()
		atomic.StoreInt32(&refreshIntervalSec, int32(appSettings.RefreshIntervalSec))
		atomic.StoreInt32(&pollIntervalMs, int32(appSettings.PollIntervalMs))
	}()

	// 3. Refresh all active key balances on a dynamic interval
	go func() {
		// Initial refresh after a short delay to let import and settings load finish
		time.Sleep(2 * time.Second)
		settings.RefreshAllBalances()

		for {
			time.Sleep(time.Duration(atomic.LoadInt32(&refreshIntervalSec)) * time.Second)
			settings.RefreshAllBalances()
		}
	}()

	// 4. Write keys snapshot JSON every 200ms for the SwiftUI settings window
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			settings.WriteKeysSnapshot(atomic.LoadInt32(&refreshIntervalSec), atomic.LoadInt32(&pollIntervalMs))
		}
	}()

	// 4. Watch for key actions from the SwiftUI settings window
	go func() {
		ch := make(chan settings.KeyAction, 8)
		go settings.WatchKeyActions(ch)
		for action := range ch {
			switch action.Action {
			case "add":
				if action.Key != "" {
					label := action.Label
					if label == "" {
						label = "New Key"
					}
					if _, err := settings.AddKey(label, action.Key, "", ""); err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "claude-monitor: add key: %v\n", err)
					}
				}
			case "delete":
				if action.ID != "" {
					if err := settings.DeleteKey(action.ID); err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "claude-monitor: delete key: %v\n", err)
					}
				}
			case "toggle":
				if action.ID != "" {
					if _, err := settings.ToggleKey(action.ID); err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "claude-monitor: toggle key: %v\n", err)
					} else {
						go settings.RefreshAllBalances()
					}
				}
			case "edit":
				if action.ID != "" && action.Label != "" {
					if err := settings.UpdateKeyLabel(action.ID, action.Label); err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "claude-monitor: edit key: %v\n", err)
					}
				}
			case "refresh":
				settings.RefreshAllBalances()
			case "setInterval":
				if sec, err := strconv.Atoi(action.Key); err == nil && sec > 0 {
					atomic.StoreInt32(&refreshIntervalSec, int32(sec))
					s := settings.LoadAppSettings()
					s.RefreshIntervalSec = sec
					_ = settings.SaveAppSettings(s)
				}
			case "setPollInterval":
				if ms, err := strconv.Atoi(action.Key); err == nil && ms > 0 {
					atomic.StoreInt32(&pollIntervalMs, int32(ms))
					s := settings.LoadAppSettings()
					s.PollIntervalMs = ms
					_ = settings.SaveAppSettings(s)
				}
			case "setSalary":
				if salary, err := strconv.ParseFloat(action.Key, 64); err == nil && salary >= 0 {
					s := settings.LoadAppSettings()
					s.MonthlySalary = salary
					_ = settings.SaveAppSettings(s)
				}
			}
		}
	}()

	// Watch for selected PID changes from the SwiftUI sessions panel.
	// The panel writes /tmp/claude-monitor-selected-pid when a card is clicked.
	const selectedPIDPath = "/tmp/claude-monitor-selected-pid"
	go func() {
		var lastRead int
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			data, err := os.ReadFile(selectedPIDPath)
			if err != nil {
				continue
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || pid == lastRead {
				continue
			}
			lastRead = pid
			selectedPID = pid
			refreshSessionMenu()
		}
	}()

	go func() {
		for {
			time.Sleep(time.Duration(atomic.LoadInt32(&pollIntervalMs)) * time.Millisecond)
			detected, _ := detectStatus()
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
				currentStatus = detected
				systray.SetIcon(statusIcons[currentStatus])
			}
		}
	}()
}

func onExit() {
	// Kill helper processes — use pkill by name for reliability,
	// since PID tracking alone can miss processes on macOS.
	_ = exec.Command("pkill", "-f", "novascope-panel-helper").Run()
	_ = exec.Command("pkill", "-f", "novascope-settings-helper").Run()

	// Also kill by tracked PID as a direct fallback
	if panelHelperPID != 0 {
		_ = syscall.Kill(panelHelperPID, syscall.SIGTERM)
		panelHelperPID = 0
	}
	if settingsHelperPID != 0 {
		_ = syscall.Kill(settingsHelperPID, syscall.SIGTERM)
		settingsHelperPID = 0
	}

	// Clean up temp files
	_ = os.Remove("/tmp/claude-monitor-sessions.json")
	_ = os.Remove("/tmp/claude-monitor-keys.json")
	_ = os.Remove("/tmp/claude-monitor-state")
	_ = os.Remove("/tmp/claude-monitor-skin.json")
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

// panelHelperName is the compiled Swift helper binary for the sessions panel.
const panelHelperName = "novascope-panel-helper"

// appBundleMacOSDir returns the path to Novascope.app/Contents/MacOS/.
func appBundleMacOSDir() (string, error) {
	root := core.FindProjectRoot()
	if root == "/tmp" {
		return "", fmt.Errorf("cannot find project root")
	}
	dir := filepath.Join(root, "Novascope.app", "Contents", "MacOS")
	return dir, nil
}

// ensurePanelHelper compiles the SwiftUI sessions panel helper if needed.
func ensurePanelHelper() (string, error) {
	binDir, err := appBundleMacOSDir()
	if err != nil {
		return "", err
	}
	dest := filepath.Join(binDir, panelHelperName)
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	root := core.FindProjectRoot()
	if root == "/tmp" {
		return "", fmt.Errorf("cannot find source: project root not found (pre-compiled binary not available)")
	}
	src := filepath.Join(root, "helpers", "sessions_panel.swift")
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return "", fmt.Errorf("sessions_panel.swift not found at %s", src)
	}
	cmd := exec.Command("swiftc", "-o", dest, src)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("swiftc: %w\n%s", err, string(output))
	}
	return dest, nil
}

// launchSessionsPanel starts or activates the sessions panel window.
// If the helper is already running, it brings the window to front.
// Otherwise it compiles (if needed) and launches a new instance.
func launchSessionsPanel() {
	// Check if existing helper is still alive
	if panelHelperPID != 0 {
		process, err := os.FindProcess(panelHelperPID)
		if err == nil {
			err = process.Signal(syscall.Signal(0))
		}
		if err == nil {
			// Process exists — kill and restart to bring window to front
			_ = process.Kill()
		}
		panelHelperPID = 0
	}

	helper, err := ensurePanelHelper()
	if err != nil {
		return
	}
	cmd := exec.Command(helper)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return
	}
	panelHelperPID = cmd.Process.Pid
	// Don't Wait() — let the helper run independently
	go func() {
		_ = cmd.Wait()
		panelHelperPID = 0
	}()
}

// === Settings window helper ===

const settingsHelperName = "novascope-settings-helper"

// ensureSettingsHelper compiles the SwiftUI settings window helper if needed.
func ensureSettingsHelper() (string, error) {
	binDir, err := appBundleMacOSDir()
	if err != nil {
		return "", err
	}
	dest := filepath.Join(binDir, settingsHelperName)
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}
	root := core.FindProjectRoot()
	if root == "/tmp" {
		return "", fmt.Errorf("cannot find source: project root not found (pre-compiled binary not available)")
	}
	src := filepath.Join(root, "helpers", "settings_window.swift")
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return "", fmt.Errorf("settings_window.swift not found at %s", src)
	}
	cmd := exec.Command("swiftc", "-o", dest, src)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("swiftc: %w\n%s", err, string(output))
	}
	return dest, nil
}

// launchSettingsWindow starts or activates the settings window.
func launchSettingsWindow() {
	// Check if existing helper is still alive
	if settingsHelperPID != 0 {
		process, err := os.FindProcess(settingsHelperPID)
		if err == nil {
			err = process.Signal(syscall.Signal(0))
		}
		if err == nil {
			// Process exists — kill and restart to bring window to front
			_ = process.Kill()
		}
		settingsHelperPID = 0
	}

	helper, err := ensureSettingsHelper()
	if err != nil {
		return
	}
	cmd := exec.Command(helper)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return
	}
	settingsHelperPID = cmd.Process.Pid
	// Don't Wait() — let the helper run independently
	go func() {
		_ = cmd.Wait()
		settingsHelperPID = 0
	}()
}
