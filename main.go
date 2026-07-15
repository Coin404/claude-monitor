package main

import (
	"fmt"
	"os"
	"os/signal"
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

const pollInterval = 30 * time.Millisecond

const (
	colorGray    = "#8E8E93"
	colorGreen   = "#34C759"
	colorYellow  = "#FFCC00"
	colorBlue    = "#007AFF"
	colorOrange  = "#FF9500"
	colorRed     = "#FF3B30"
)

var statusIcons map[Status][]byte

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
	rewriteItem := systray.AddMenuItem("Re-write Hooks", "重新写入 Claude Code hooks 配置")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit", "退出 Claude Monitor")

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
		<-quitItem.ClickedCh
		systray.Quit()
	}()

	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for range ticker.C {
			detected := detectStatus()
			if detected != currentStatus {
				currentStatus = detected
				systray.SetIcon(statusIcons[currentStatus])
			}
		}
	}()
}

func onExit() {}

func detectStatus() Status {
	running := CheckClaudeProcess()
	if !running {
		return StatusStopped
	}

	if HasDialogWindow() {
		return StatusBlocked
	}

	hookState, fresh := ReadHookState()
	// If hooks aren't active (state file stale or empty), fall back to
	// proxy activity detection + stdin check to distinguish states.
	if !fresh {
		if CheckProxyActivity() {
			// Proxy active: Claude Code is running. Check whether it's
			// waiting for user input (AskUserQuestion) vs working.
			if CheckWaitingForInput() {
				return StatusBlocked
			}
			return StatusWorking
		}
		return StatusIdle
	}

	switch hookState {
	case "green":
		return StatusIdle
	case "yellow":
		return StatusSubmitted
	case "blue":
		return StatusWorking
	case "orange":
		return StatusToolUse
	case "red":
		return StatusBlocked
	default:
		return StatusIdle
	}
}
