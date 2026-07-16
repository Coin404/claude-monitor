package stats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"claude-monitor/internal/core"
)

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

// DayStats holds accumulated time for each status in a single day.
type DayStats struct {
	Date     string             `json:"date"`
	Total    float64            `json:"total"`
	Statuses map[string]float64 `json:"statuses"`
}

// Tracker persists per-day status durations to disk and provides
// menu summaries and HTML reports.
type Tracker struct {
	mu             sync.Mutex
	day            string
	currentStatus  core.Status
	lastChangeTime time.Time
	today          *DayStats
}

// ---------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------

func statsDir() string {
	return filepath.Dir(core.LogPath())
}

func statsPathForDay(day string) string {
	return filepath.Join(statsDir(), "stats-"+day+".json")
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewTracker creates a tracker, loading today's data from disk if it
// exists. lastChangeTime is set to now so that the first status change
// only accumulates a tiny (near-zero) duration.
func NewTracker() *Tracker {
	core.EnsureLogDir()
	today := time.Now().Format("2006-01-02")
	st := &Tracker{
		day:            today,
		currentStatus:  core.StatusStopped,
		lastChangeTime: time.Now(),
		today:          loadDayStats(today),
	}
	return st
}

// Snapshot returns a deep copy of today's stats (thread-safe).
func (st *Tracker) Snapshot() *DayStats {
	st.mu.Lock()
	defer st.mu.Unlock()
	ds := *st.today
	ds.Statuses = make(map[string]float64, len(st.today.Statuses))
	maps.Copy(ds.Statuses, st.today.Statuses)
	return &ds
}

// ---------------------------------------------------------------------------
// Persistence helpers (caller must hold st.mu)
// ---------------------------------------------------------------------------

func loadDayStats(day string) *DayStats {
	path := statsPathForDay(day)
	data, err := os.ReadFile(path)
	if err != nil {
		return newDayStats(day)
	}
	var ds DayStats
	if err := json.Unmarshal(data, &ds); err != nil {
		return newDayStats(day)
	}
	if ds.Statuses == nil {
		ds.Statuses = make(map[string]float64)
	}
	return &ds
}

func newDayStats(day string) *DayStats {
	return &DayStats{
		Date:     day,
		Statuses: make(map[string]float64),
	}
}

func (st *Tracker) saveStatsLocked() {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(st.today); err != nil {
		return
	}
	_ = os.WriteFile(statsPathForDay(st.day), buf.Bytes(), 0644)
}

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

// RecordStatusChange accumulates the elapsed time for the previous status
// and records the new status. Called from the detection loop whenever the
// status changes.
func (st *Tracker) RecordStatusChange(newStatus core.Status) {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	today := now.Format("2006-01-02")

	// Day rollover: save yesterday, start fresh
	if today != st.day {
		st.saveStatsLocked()
		st.today = newDayStats(today)
		st.day = today
		st.lastChangeTime = now
	}

	elapsed := now.Sub(st.lastChangeTime).Seconds()

	// Accumulate to the previous status (skip Stopped — it is not
	// meaningful "active" time).
	if elapsed > 0 && st.currentStatus != core.StatusStopped {
		key := core.StatusKey(st.currentStatus)
		if key != "" {
			st.today.Statuses[key] += elapsed
			st.today.Total += elapsed
		}
	}

	st.currentStatus = newStatus
	st.lastChangeTime = now
	st.saveStatsLocked()
}

// FlushCurrentSession saves the current in-progress segment to disk.
// Called on quit so we don't lose the last status segment.
func (st *Tracker) FlushCurrentSession() {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(st.lastChangeTime).Seconds()
	if elapsed > 0 && st.currentStatus != core.StatusStopped {
		key := core.StatusKey(st.currentStatus)
		if key != "" {
			st.today.Statuses[key] += elapsed
			st.today.Total += elapsed
		}
	}
	st.lastChangeTime = now
	st.saveStatsLocked()
}

// ---------------------------------------------------------------------------
// Cleanup
// ---------------------------------------------------------------------------

// CleanupOldStats removes stats files older than maxDays.
func CleanupOldStats(maxDays int) {
	dir := statsDir()
	files, err := filepath.Glob(filepath.Join(dir, "stats-*.json"))
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -maxDays)
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(f)
		}
	}
}

// ---------------------------------------------------------------------------
// HTML report
// ---------------------------------------------------------------------------

// GenerateHTMLReport returns a self-contained HTML page with an SVG donut
// chart visualising the given DayStats.
func GenerateHTMLReport(ds *DayStats) string {
	segments := buildSegments(ds)
	return renderHTML(segments, ds)
}

// statsHelperName is the compiled Swift helper binary name.
const statsHelperName = "novascope-stats-helper"

// findHelperPath returns the path to the compiled stats helper binary.
func findHelperPath() string {
	root := core.FindProjectRoot()
	if root == "/tmp" {
		return ""
	}
	return filepath.Join(root, "Novascope.app", "Contents", "MacOS", statsHelperName)
}

// ensureStatsHelper compiles the Swift helper if it doesn't exist yet.
func ensureStatsHelper() (string, error) {
	dest := findHelperPath()
	if dest == "" {
		return "", fmt.Errorf("cannot determine helper path")
	}
	if _, err := os.Stat(dest); err == nil {
		return dest, nil // already compiled
	}
	root := core.FindProjectRoot()
	src := filepath.Join(root, "helpers", "stats_window.swift")
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return "", fmt.Errorf("stats_window.swift not found at %s", src)
	}
	cmd := exec.Command("swiftc", "-o", dest, src)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("swiftc: %w\n%s", err, string(output))
	}
	return dest, nil
}

// OpenStatsInBrowser shows the stats report in a native macOS WKWebView window
// via a standalone Swift helper (no browser needed, avoids cgo complexity).
func OpenStatsInBrowser(ds *DayStats) error {
	html := GenerateHTMLReport(ds)
	f, err := os.CreateTemp("", "novascope-stats-*.html")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	path := f.Name()
	if _, err := f.WriteString(html); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("writing html: %w", err)
	}
	_ = f.Close()

	helper, err := ensureStatsHelper()
	if err != nil {
		return fmt.Errorf("building stats helper: %w", err)
	}
	return exec.Command(helper, path).Start()
}

// ---------------------------------------------------------------------------
// Donut chart helpers
// ---------------------------------------------------------------------------

type donutSegment struct {
	Key     string // status key for gradient ref ("idle", "working", …)
	Color   string // hex color for legend dot
	Label   string
	Seconds float64
	Percent float64
	Offset  float64 // cumulative offset for stroke-dashoffset
	DashLen float64 // stroke-dasharray visible length
}

func buildSegments(ds *DayStats) []donutSegment {
	total := ds.Total
	if total < 1 {
		return nil
	}

	// Stable order: idle, working, blocked, tooluse, submitted
	order := []string{"idle", "working", "blocked", "tooluse", "submitted"}

	// Donut geometry
	const r = 90
	circumference := 2 * math.Pi * r
	gap := 2.5 // ~1.8 degrees — visual separation between segments

	// Start at 12 o'clock, with half-gap offset so the first/last gap is centered
	offset := -circumference/4 - gap/2

	var segments []donutSegment
	for _, key := range order {
		sec := ds.Statuses[key]
		if sec < 1 {
			continue
		}
		pct := sec / total
		dashLen := max(pct*circumference-gap, 2) // minimum visible segment
		seg := donutSegment{
			Key:     key,
			Color:   core.StatusColorMap[key],
			Label:   core.StatusDisplayNames[key],
			Seconds: sec,
			Percent: pct * 100,
			Offset:  offset,
			DashLen: dashLen,
		}
		segments = append(segments, seg)
		offset -= pct * circumference
	}
	return segments
}

// ---------------------------------------------------------------------------
// HTML template (self-contained, no external dependencies)
// ---------------------------------------------------------------------------

func renderHTML(segments []donutSegment, ds *DayStats) string {
	var b strings.Builder

	// —— Gradient colour stops (base → deeper) for each status ——
	gradStops := map[string][2]string{
		"idle":      {"#34C759", "#28A745"},
		"working":   {"#007AFF", "#0056B3"},
		"blocked":   {"#FF3B30", "#CC2F26"},
		"tooluse":   {"#FF9500", "#E68600"},
		"submitted": {"#FFCC00", "#E6B800"},
	}

	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Novascope Stats – `)
	b.WriteString(ds.Date)
	b.WriteString(`</title>
<style>
  :root {
    --bg: #ffffff;
    --text: #1d1d1f;
    --text-secondary: #86868b;
    --card-bg: #f5f5f7;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #1c1c1e;
      --text: #f5f5f7;
      --text-secondary: #98989d;
      --card-bg: #2c2c2e;
    }
  }
  * { margin:0; padding:0; box-sizing:border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "SF Pro Display", sans-serif;
    background: var(--bg);
    color: var(--text);
    display: flex; justify-content: center; align-items: center;
    min-height: 100vh;
    transition: background 0.3s, color 0.3s;
    -webkit-font-smoothing: antialiased;
  }
  .card {
    background: var(--card-bg);
    border-radius: 20px;
    padding: 32px 48px 40px;
    max-width: 520px;
    width: 100%;
    text-align: center;
    transition: background 0.3s;
  }
  h1 { font-size: 24px; font-weight: 700; letter-spacing: -0.3px; margin-bottom: 4px; }
  .date { color: var(--text-secondary); font-size: 14px; margin-bottom: 24px; }
  .chart-wrap { position:relative; width:300px; height:300px; margin:0 auto 24px; }
  svg { width:100%; height:100%; overflow:visible; }
  .segment {
    transition: stroke-width 0.25s ease, opacity 0.25s ease, filter 0.25s ease;
    cursor: pointer;
  }
  .segment:hover {
    stroke-width: 48;
    opacity: 0.9;
    filter: url(#donut-shadow);
  }
  .center-label { font-size: 15px; fill: var(--text-secondary); }
  .center-value { font-size: 24px; font-weight: 700; fill: var(--text); }
  .legend {
    display: flex; flex-wrap: wrap; justify-content: center;
    gap: 10px 22px;
  }
  .legend-item {
    display: flex; align-items: center; gap: 8px;
    font-size: 14px; color: var(--text-secondary);
  }
  .legend-dot {
    width: 10px; height: 10px; border-radius: 3px; flex-shrink: 0;
  }
  .legend-time { font-weight: 600; color: var(--text); }
  @keyframes pop-in {
    0% { transform: scale(0.85); opacity: 0; }
    60% { transform: scale(1.03); }
    100% { transform: scale(1); opacity: 1; }
  }
  .chart-wrap { animation: pop-in 0.5s ease-out both; }
</style>
</head>
<body>
<div class="card">
  <h1>Today's Stats</h1>
  <div class="date">` + ds.Date + `</div>
`)

	if len(segments) == 0 {
		b.WriteString(`  <p style="color:var(--text-secondary); padding:60px 0; font-size:15px">No data yet</p>`)
	} else {
		circumference := 2 * math.Pi * 90.0
		b.WriteString(`  <div class="chart-wrap">
    <svg viewBox="0 0 300 300">
      <defs>
        <filter id="donut-shadow" x="-30%" y="-30%" width="160%" height="160%">
          <feDropShadow dx="0" dy="4" stdDeviation="6" flood-color="#000" flood-opacity="0.25"/>
        </filter>
`)
		// Gradient definitions
		for key, stops := range gradStops {
			b.WriteString(fmt.Sprintf(`        <linearGradient id="g-%s" x1="0%%" y1="0%%" x2="100%%" y2="100%%">
          <stop offset="0%%" stop-color="%s"/>
          <stop offset="100%%" stop-color="%s"/>
        </linearGradient>
`, key, stops[0], stops[1]))
		}
		b.WriteString(`      </defs>
`)
		// Donut segments — shadow only applied on hover via CSS
		for _, seg := range segments {
			b.WriteString(fmt.Sprintf(
				`      <circle cx="150" cy="150" r="90" fill="none"
           stroke="url(#g-%s)" stroke-width="42" stroke-linecap="round"
           stroke-dasharray="%.2f %.2f"
           stroke-dashoffset="%.2f"
           class="segment">
        <title>%s %s</title>
      </circle>
`, seg.Key, seg.DashLen, circumference-seg.DashLen, seg.Offset,
				seg.Label, formatDuration(seg.Seconds)))
		}
		// Center text
		b.WriteString(fmt.Sprintf(`      <text x="150" y="142" text-anchor="middle" class="center-label">Total</text>
      <text x="150" y="172" text-anchor="middle" class="center-value">%s</text>
`, formatDuration(ds.Total)))
		b.WriteString(`    </svg>
  </div>
`)
	}

	// Legend
	if len(segments) > 0 {
		b.WriteString(`  <div class="legend">
`)
		for _, seg := range segments {
			b.WriteString(fmt.Sprintf(`    <div class="legend-item">
      <span class="legend-dot" style="background:%s"></span>
      <span>%s</span>
      <span class="legend-time">%s</span>
    </div>
`, seg.Color, seg.Label, formatDuration(seg.Seconds)))
		}
		b.WriteString(`  </div>
`)
	}

	b.WriteString(`</div>
</body>
</html>`)
	return b.String()
}

// ---------------------------------------------------------------------------
// Duration formatting
// ---------------------------------------------------------------------------

// formatDuration formats seconds into a compact human-readable form:
//
//	>= 3600 → "1.2h"
//	>= 60   → "15m"
//	< 60    → "42s"
func formatDuration(sec float64) string {
	s := math.Round(sec)
	if s < 60 {
		return fmt.Sprintf("%ds", int(s))
	}
	if s < 3600 {
		return fmt.Sprintf("%dm", int(s/60))
	}
	h := float64(s) / 3600.0
	return fmt.Sprintf("%.1fh", h)
}
