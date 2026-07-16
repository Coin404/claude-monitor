package detect

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// ListClaudeProcesses returns PIDs of all running claude processes
// (excluding self and claude-monitor).
func ListClaudeProcesses() []int {
	selfPID := fmt.Sprint(os.Getpid())
	data, err := exec.Command("ps", "-eo", "pid,comm=").Output()
	if err != nil {
		return nil
	}

	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pidStr, comm := fields[0], fields[1]
		if pidStr == selfPID {
			continue
		}
		if comm == "claude" || (strings.HasPrefix(comm, "claude") && comm != "claude-monitor") {
			pid, err := strconv.Atoi(pidStr)
			if err == nil {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

// isProcessRunning checks whether a process with the given PID is alive.
func isProcessRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

// FirstClaudePID returns the first running claude process PID, or 0.
func FirstClaudePID() int {
	pids := ListClaudeProcesses()
	if len(pids) == 0 {
		return 0
	}
	return pids[0]
}

// FindBlockedPID returns the PID of the session that is currently in
// "red" (blocked) state, or 0 if none is blocked.
func FindBlockedPID() int {
	sessions := ListClaudeSessions()
	for pid, color := range sessions {
		if color == "red" {
			return pid
		}
	}
	return 0
}
