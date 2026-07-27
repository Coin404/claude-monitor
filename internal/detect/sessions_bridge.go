package detect

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"time"

	"claude-monitor/internal/core"
	"claude-monitor/internal/settings"
)

const sessionsSnapshotPath = "/tmp/claude-monitor-sessions.json"

// SessionInfo holds the display data for one Claude Code session.
type SessionInfo struct {
	PID         int    `json:"pid"`
	Color       string `json:"color"`
	Project     string `json:"project"`
	Terminal    string `json:"terminal"`
	StatusKey   string `json:"statusKey"`
	StatusLabel string `json:"statusLabel"`
	ColorHex    string `json:"colorHex"`
}

// UsageSummary holds today's API usage data.
type UsageSummary struct {
	Balance   *BalanceInfo    `json:"balance,omitempty"`
	Providers []ProviderUsage `json:"providers,omitempty"`
	UpdatedAt string          `json:"updatedAt"`
}

// BalanceInfo holds DeepSeek balance data from official API.
type BalanceInfo struct {
	TotalBalance  float64 `json:"totalBalance"`
	TodaySpending float64 `json:"todaySpending"`
	Currency      string  `json:"currency"`
}

// ProviderUsage holds per-provider token/cost stats from CC Switch proxy.
type ProviderUsage struct {
	Name         string  `json:"name"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	TotalCost    float64 `json:"totalCost"`
	RequestCount int64   `json:"requestCount"`
}

// SessionsSnapshot is the JSON payload written to /tmp/claude-monitor-sessions.json.
type SessionsSnapshot struct {
	Sessions      []SessionInfo `json:"sessions"`
	Timestamp     string        `json:"timestamp"`
	Count         int           `json:"count"`
	Usage         *UsageSummary `json:"usage,omitempty"`
	EarnedToday   float64       `json:"earnedToday"`
	MonthlySalary float64       `json:"monthlySalary"`
}

// WriteSessionsSnapshot collects session data from all running Claude
// processes and writes it atomically to /tmp/claude-monitor-sessions.json.
// The SwiftUI panel reads this file to display session cards.
func WriteSessionsSnapshot() {
	sessions := ListClaudeSessions()
	infos := make([]SessionInfo, 0, len(sessions))

	for pid, color := range sessions {
		status := core.ParseHookColor(color)
		key := core.StatusKey(status)
		info := SessionInfo{
			PID:         pid,
			Color:       color,
			Project:     SessionWorkDir(pid),
			Terminal:    FindTerminalApp(pid),
			StatusKey:   key,
			StatusLabel: core.StatusDisplayNames[key],
			ColorHex:    core.StatusColorMap[key],
		}
		infos = append(infos, info)
	}

	// Sort by PID ascending for stable card order
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].PID < infos[j].PID
	})

	// Aggregate balance from active keys in the settings store
	usage := GetUsageCached()
	if kb := aggregateKeyBalance(); kb != nil {
		if usage == nil {
			usage = &UsageSummary{}
		}
		usage.Balance = kb
		usage.UpdatedAt = time.Now().Format(time.RFC3339)
	}

	// Compute today's earnings from monthly salary
	appSettings := settings.LoadAppSettings()
	earnedToday, _ := settings.CalculateEarnedToday(appSettings.MonthlySalary)

	snap := SessionsSnapshot{
		Sessions:      infos,
		Timestamp:     time.Now().Format(time.RFC3339),
		Count:         len(infos),
		Usage:         usage,
		EarnedToday:   earnedToday,
		MonthlySalary: appSettings.MonthlySalary,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&snap); err != nil {
		return
	}

	// Atomic write: temp file + rename to avoid partial reads
	tmpPath := sessionsSnapshotPath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0644); err != nil {
		return
	}
	if err := os.Rename(tmpPath, sessionsSnapshotPath); err != nil {
		_ = os.Remove(tmpPath)
		return
	}
}

// aggregateKeyBalance sums up balance and spending across all active keys
// from the settings key store.
func aggregateKeyBalance() *BalanceInfo {
	store := settings.LoadKeys()
	var totalBalance, totalSpending float64
	var currency string
	hasData := false

	for _, k := range store.Keys {
		if !k.Active {
			continue
		}
		if k.Error != "" || k.Balance <= 0 {
			continue
		}
		totalBalance += k.Balance
		totalSpending += k.TodaySpending
		currency = k.Currency
		hasData = true
	}

	if !hasData {
		return nil
	}

	return &BalanceInfo{
		TotalBalance:  totalBalance,
		TodaySpending: totalSpending,
		Currency:      currency,
	}
}
