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

const doneFile = "/tmp/claude-done"
const questionFile = "/tmp/claude-question"

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

// IsDone checks if claude just finished a response (done file was touched recently).
// Returns true if claude is IDLE (just finished, waiting for user).
func IsDone() bool {
	info, err := os.Stat(doneFile)
	if err != nil {
		// File doesn't exist → claude is working
		return false
	}
	// File was touched within the last second → just finished → idle
	return time.Since(info.ModTime()) < time.Second
}

// IsQuestion checks if claude asked a question and is waiting for user answer.
func IsQuestion() bool {
	_, err := os.Stat(questionFile)
	return err == nil
}
