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

const (
	pollInterval   = 1 * time.Second
	debounceRounds = 1
)

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
		pendingStatus := currentStatus
		pendingCount := 0

		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for range ticker.C {
			detectedStatus := detectStatus()

			if detectedStatus == pendingStatus {
				pendingCount++
			} else {
				pendingStatus = detectedStatus
				pendingCount = 1
			}

			if pendingCount >= debounceRounds && pendingStatus != currentStatus {
				currentStatus = pendingStatus
				systray.SetIcon(statusIcons[currentStatus])
			}
		}
	}()
}

func onExit() {}

func detectStatus() Status {
	proc, err := CheckClaudeProcess()
	if err != nil || proc == nil {
		return StatusStopped
	}

	// Permission dialog blocks progress → yellow
	if HasDialogWindow() {
		return StatusBlocked
	}

	// I asked a question, waiting for user response → yellow
	if IsQuestion() {
		return StatusBlocked
	}

	// Busy file exists → I'm working → green
	if IsBusy() {
		return StatusActive
	}

	// Claude running but idle, waiting for next prompt → red
	return StatusWaiting
}
