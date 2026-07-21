package core

import (
	"os"
	"path/filepath"
)

// Status represents the current Claude Code session state.
type Status int

const (
	StatusStopped   Status = iota // gray — Claude not running
	StatusIdle                    // green — idle, waiting for user
	StatusSubmitted               // yellow — user just submitted prompt
	StatusWorking                 // blue — thinking/generating
	StatusToolUse                 // orange — executing tools
	StatusBlocked                 // red — needs user action
)

// Tray icon colors.
const (
	ColorGray   = "#8E8E93"
	ColorGreen  = "#34C759"
	ColorYellow = "#FFCC00"
	ColorBlue   = "#007AFF"
	ColorOrange = "#FF9500"
	ColorRed    = "#FF3B30"
)

// StatusDisplayNames maps status keys to Chinese display labels.
var StatusDisplayNames = map[string]string{
	"idle":      "空闲",
	"working":   "思考",
	"blocked":   "等待",
	"tooluse":   "工具调用",
	"submitted": "提交中",
}

// StatusColorMap maps status keys to their hex colors.
var StatusColorMap = map[string]string{
	"idle":      ColorGreen,
	"working":   ColorBlue,
	"blocked":   ColorRed,
	"tooluse":   ColorOrange,
	"submitted": ColorYellow,
}

// StatusLabel returns a human-readable label for a Status value.
func StatusLabel(s Status) string {
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

// StatusKey returns the stats key for a Status value.
func StatusKey(s Status) string {
	switch s {
	case StatusIdle:
		return "idle"
	case StatusWorking:
		return "working"
	case StatusBlocked:
		return "blocked"
	case StatusToolUse:
		return "tooluse"
	case StatusSubmitted:
		return "submitted"
	default:
		return ""
	}
}

// ParseHookColor converts a hook color string to a Status value.
func ParseHookColor(color string) Status {
	switch color {
	case "green":
		return StatusIdle
	case "blue":
		return StatusWorking
	case "yellow":
		return StatusSubmitted
	case "orange":
		return StatusToolUse
	case "red":
		return StatusBlocked
	default:
		return StatusIdle
	}
}

// FindProjectRoot walks up from the executable's directory to find go.mod,
// returning the project root path. Falls back to "/tmp" if not found.
func FindProjectRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return "/tmp"
	}
	dir := filepath.Dir(exe)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "/tmp"
}

// ExecutableDir returns the directory containing the running executable.
func ExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "/tmp"
	}
	return filepath.Dir(exe)
}

// AppSupportDir returns the Novascope app support directory
// (~/Library/Application Support/Novascope), creating it if needed.
func AppSupportDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp"
	}
	dir := filepath.Join(home, "Library", "Application Support", "Novascope")
	os.MkdirAll(dir, 0755)
	return dir
}

// LogPath returns the path to the log file.
func LogPath() string {
	return filepath.Join(AppSupportDir(), "claude-monitor.log")
}
