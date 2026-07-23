package detect

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	usageCache    *UsageSummary
	usageCacheMu  sync.Mutex
	usageCacheAt  time.Time
	usageCacheTTL = 30 * time.Second
)

// GetUsageCached returns today's usage summary combining:
// 1. DeepSeek official balance API (real money)
// 2. CC Switch proxy data (token counts)
// Results are cached for 30 seconds.
func GetUsageCached() *UsageSummary {
	usageCacheMu.Lock()
	defer usageCacheMu.Unlock()

	if time.Since(usageCacheAt) < usageCacheTTL {
		return usageCache
	}

	summary := &UsageSummary{}
	hasData := false

	// DeepSeek official balance (tracks spending via baseline diff)
	if balance := fetchDeepSeekBalance(); balance != nil {
		summary.Balance = balance
		hasData = true
	}

	// Token counts: self-tracked from JSONL
	if providers := queryJSONLTokens(); len(providers) > 0 {
		summary.Providers = providers
		hasData = true
	}

	if !hasData {
		usageCache = nil
		usageCacheAt = time.Now()
		return nil
	}

	summary.UpdatedAt = time.Now().Format(time.RFC3339)
	usageCache = summary
	usageCacheAt = time.Now()
	return summary
}

// === DeepSeek balance API ===

const balanceBaselinePath = "/tmp/claude-monitor-balance-baseline.json"

type balanceTracker struct {
	Date        string  `json:"date"`
	Baseline    float64 `json:"baseline"`
	LastBalance float64 `json:"lastBalance"`
	TotalTopUps float64 `json:"totalTopUps"`
}

func fetchDeepSeekBalance() *BalanceInfo {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return nil
	}

	req, err := http.NewRequest("GET", "https://api.deepseek.com/user/balance", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil
	}

	var result struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency     string `json:"currency"`
			TotalBalance string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}
	if !result.IsAvailable || len(result.BalanceInfos) == 0 {
		return nil
	}

	current, err := parseFloat(result.BalanceInfos[0].TotalBalance)
	if err != nil {
		return nil
	}

	tracker := loadBalanceTracker()
	today := time.Now().Format("2006-01-02")

	if tracker.Date != today {
		// New day: reset baseline, forget yesterday's top-ups
		tracker = balanceTracker{Date: today, Baseline: current, LastBalance: current, TotalTopUps: 0}
		saveBalanceTracker(tracker)
	}

	// Detect top-up: balance went up compared to last known balance
	if current > tracker.LastBalance+0.001 {
		tracker.TotalTopUps += current - tracker.LastBalance
	}
	tracker.LastBalance = current
	saveBalanceTracker(tracker)

	// Spending = (initial + top-ups) - current, floor at 0
	totalAvailable := tracker.Baseline + tracker.TotalTopUps
	spending := 0.0
	if totalAvailable > current {
		spending = totalAvailable - current
	}

	return &BalanceInfo{
		TotalBalance:  current,
		TodaySpending: spending,
		Currency:      result.BalanceInfos[0].Currency,
	}
}

func loadBalanceTracker() balanceTracker {
	data, err := os.ReadFile(balanceBaselinePath)
	if err != nil {
		return balanceTracker{}
	}
	var t balanceTracker
	json.Unmarshal(data, &t)
	return t
}

func saveBalanceTracker(t balanceTracker) {
	data, _ := json.Marshal(t)
	os.WriteFile(balanceBaselinePath, data, 0644)
}

// === CC Switch proxy token data ===

func ccSwitchDBPath() string {
	if p := os.Getenv("CC_SWITCH_DB_PATH"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cc-switch", "cc-switch.db")
}

func queryCCSwitchTokens() []ProviderUsage {
	dbPath := ccSwitchDBPath()
	if dbPath == "" {
		return nil
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil
	}

	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()

	query := `
		SELECT COALESCE(p.name, r.provider_id) AS provider_name,
		       SUM(r.input_tokens) AS input_tokens,
		       SUM(r.output_tokens) AS output_tokens,
		       SUM(CAST(r.total_cost_usd AS REAL)) AS total_cost,
		       COUNT(*) AS request_count
		FROM proxy_request_logs r
		LEFT JOIN providers p ON p.id = r.provider_id
		WHERE r.created_at >= strftime('%s', date('now', 'localtime'))
		  AND r.provider_id != '_session'
		GROUP BY r.provider_id
		ORDER BY total_cost DESC
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var providers []ProviderUsage
	for rows.Next() {
		var pu ProviderUsage
		if err := rows.Scan(&pu.Name, &pu.InputTokens, &pu.OutputTokens, &pu.TotalCost, &pu.RequestCount); err != nil {
			continue
		}
		providers = append(providers, pu)
	}
	return providers
}

// === Claude Code JSONL self-tracking ===

type jsonlAgg struct {
	InputTokens  int64
	OutputTokens int64
	Requests     int64
}

func queryJSONLTokens() []ProviderUsage {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	projectsDir := filepath.Join(home, ".claude", "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}

	today := time.Now().Format("2006-01-02")
	agg := make(map[string]*jsonlAgg)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projPath := filepath.Join(projectsDir, entry.Name())
		files, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			parseJSONLFile(filepath.Join(projPath, f.Name()), today, agg)
		}
	}

	if len(agg) == 0 {
		return nil
	}

	var providers []ProviderUsage
	for model, a := range agg {
		// Approximate cost at Sonnet pricing
		cost := float64(a.InputTokens)/1e6*3.0 + float64(a.OutputTokens)/1e6*15.0
		providers = append(providers, ProviderUsage{
			Name:         model,
			InputTokens:  a.InputTokens,
			OutputTokens: a.OutputTokens,
			TotalCost:    cost,
			RequestCount: a.Requests,
		})
	}
	return providers
}

func parseJSONLFile(path, today string, agg map[string]*jsonlAgg) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytesContains(line, "assistant") || !bytesContains(line, today) {
			continue
		}

		var record struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			continue
		}
		if record.Type != "assistant" || record.Message.Model == "" {
			continue
		}

		a, ok := agg[record.Message.Model]
		if !ok {
			a = &jsonlAgg{}
			agg[record.Message.Model] = a
		}
		a.InputTokens += int64(record.Message.Usage.InputTokens)
		a.OutputTokens += int64(record.Message.Usage.OutputTokens)
		a.Requests++
	}
}

func bytesContains(b []byte, substr string) bool {
	return strings.Contains(string(b), substr)
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
