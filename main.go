package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"fyne.io/systray"
)

type Status int

const (
	StatusStopped Status = iota
	StatusActive
	StatusWaiting
	StatusBlocked
)

const pollInterval = 250 * time.Millisecond

const (
	colorGreen  = "#34C759"
	colorGray   = "#8E8E93"
	colorRed    = "#FF3B30"
	colorYellow = "#FFCC00"
)

var statusIcons map[Status][]byte

func main() {
	statusIcons = map[Status][]byte{
		StatusStopped: GenerateCircleIcon(colorGray),
		StatusActive:  GenerateCircleIcon(colorGreen),
		StatusWaiting: GenerateCircleIcon(colorRed),
		StatusBlocked: GenerateCircleIcon(colorYellow),
	}

	WriteHooks()

	systray.Run(onReady, onExit)
}

func onReady() {
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
	running, _ := CheckClaudeProcess()
	if !running {
		return StatusStopped
	}

	if HasDialogWindow() {
		return StatusBlocked
	}

	switch ReadHookState() {
	case "green":
		return StatusActive
	case "yellow":
		return StatusBlocked
	case "red":
		return StatusWaiting
	default:
		return StatusWaiting
	}
}
