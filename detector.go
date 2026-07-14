package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type ProcessInfo struct {
	PID  int
	Name string
}

const questionFile = "/tmp/claude-question"

// Track cumulative CPU time across polls
var lastCPUTicks uint64
var lastCheckPID int
var lastActiveTime time.Time

// CheckClaudeProcess checks if any claude process is running (excluding self).
func CheckClaudeProcess() (*ProcessInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ps", "ax", "-o", "pid,comm")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	selfPID := os.Getpid()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "PID") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		pidStr := strings.TrimSpace(parts[0])
		comm := strings.TrimSpace(parts[1])

		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if pid == selfPID {
			continue
		}
		if strings.Contains(comm, "claude") {
			return &ProcessInfo{PID: pid, Name: comm}, nil
		}
	}

	return nil, nil
}

// getCPUTicks returns cumulative CPU time in ticks (hundredths of a second).
func getCPUTicks(pid int) (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// ps -o time gives cumulative CPU time as mm:ss.hh
	cmd := exec.CommandContext(ctx, "ps", "-o", "time", "-p", strconv.Itoa(pid))
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, nil
	}
	cpuTime := strings.TrimSpace(lines[1])

	// Parse mm:ss.hh or mm:ss
	parts := strings.Split(cpuTime, ":")
	if len(parts) < 2 {
		return 0, nil
	}

	minutes, _ := strconv.ParseUint(parts[0], 10, 64)

	secParts := strings.Split(parts[1], ".")
	seconds, _ := strconv.ParseUint(secParts[0], 10, 64)
	hundredths := uint64(0)
	if len(secParts) > 1 {
		hundredths, _ = strconv.ParseUint(secParts[1], 10, 64)
	}

	return minutes*60*100 + seconds*100 + hundredths, nil
}

// IsActive returns true if the process has consumed CPU time recently.
func (p *ProcessInfo) IsActive() bool {
	ticks, err := getCPUTicks(p.PID)
	if err != nil || ticks == 0 {
		return false
	}

	// Reset tracking if PID changed (new claude process)
	if p.PID != lastCheckPID {
		lastCPUTicks = ticks
		lastCheckPID = p.PID
		lastActiveTime = time.Time{}
		return false
	}

	if ticks > lastCPUTicks {
		lastCPUTicks = ticks
		lastActiveTime = time.Now()
		return true
	}

	lastCPUTicks = ticks

	// Cooldown: stay active for 2s after last CPU activity
	if !lastActiveTime.IsZero() && time.Since(lastActiveTime) < 2*time.Second {
		return true
	}

	return false
}

// IsQuestion checks if claude asked a question and is waiting for user answer.
func IsQuestion() bool {
	_, err := os.Stat(questionFile)
	return err == nil
}
