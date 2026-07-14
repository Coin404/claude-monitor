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

// CheckClaudeProcess checks if any claude process is running (excluding self).
// Uses ps instead of pgrep because macOS pgrep has quirks matching short process names.
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
		// Skip header line
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
