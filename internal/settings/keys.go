package settings

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"claude-monitor/internal/core"
)

// === Data model ===

// KeyEntry represents a single DeepSeek API key.
type KeyEntry struct {
	ID                 string  `json:"id"`
	Label              string  `json:"label"`
	Key                string  `json:"key"`
	Active             bool    `json:"active"`
	Balance            float64 `json:"balance,omitempty"`
	TodaySpending      float64 `json:"todaySpending,omitempty"`
	Currency           string  `json:"currency,omitempty"`
	Error              string  `json:"error,omitempty"`
	CreatedAt          string  `json:"createdAt"`
	Model              string  `json:"model,omitempty"`
	CCSwitchProviderID string  `json:"ccSwitchProviderId,omitempty"`
}

// KeyStore holds all managed API keys.
type KeyStore struct {
	Keys []KeyEntry `json:"keys"`
}

// === Persistence ===

var (
	storeMu sync.Mutex
)

func keysFilePath() string {
	return filepath.Join(core.AppSupportDir(), "keys.json")
}

func settingsFilePath() string {
	return filepath.Join(core.AppSupportDir(), "novascope-settings.json")
}

// === App settings ===

// AppSettings holds application-level settings.
type AppSettings struct {
	RefreshIntervalSec int     `json:"refreshIntervalSec"`
	PollIntervalMs     int     `json:"pollIntervalMs"`
	MonthlySalary      float64 `json:"monthlySalary"`
}

// LoadAppSettings reads app settings from disk. Returns defaults if the file
// doesn't exist or is corrupted.
func LoadAppSettings() AppSettings {
	data, err := os.ReadFile(settingsFilePath())
	if err != nil {
		return AppSettings{RefreshIntervalSec: 30, PollIntervalMs: 10}
	}
	var s AppSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return AppSettings{RefreshIntervalSec: 30, PollIntervalMs: 10}
	}
	if s.RefreshIntervalSec <= 0 {
		s.RefreshIntervalSec = 30
	}
	if s.PollIntervalMs <= 0 {
		s.PollIntervalMs = 10
	}
	return s
}

// SaveAppSettings writes app settings to disk atomically.
func SaveAppSettings(s AppSettings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := settingsFilePath()
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// LoadKeys reads the key store from disk. Returns an empty store if the file
// doesn't exist or is corrupted.
func LoadKeys() KeyStore {
	storeMu.Lock()
	defer storeMu.Unlock()

	data, err := os.ReadFile(keysFilePath())
	if err != nil {
		return KeyStore{}
	}
	var store KeyStore
	if err := json.Unmarshal(data, &store); err != nil {
		return KeyStore{}
	}
	if store.Keys == nil {
		store.Keys = []KeyEntry{}
	}
	return store
}

// SaveKeys writes the key store to disk atomically (tmp + rename).
func SaveKeys(store KeyStore) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	path := keysFilePath()
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// === CRUD ===

// generateID creates a random UUID v4 string using crypto/rand (zero dependencies).
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// AddKey adds a new API key. Deduplicates by key value. If label is empty,
// generates a default label ("Key 1", "Key 2", etc).
// model and providerID are optional (used for CC Switch imports).
func AddKey(label, key, model, providerID string) (KeyEntry, error) {
	store := LoadKeys()

	// Dedup check
	for _, e := range store.Keys {
		if e.Key == key {
			return KeyEntry{}, fmt.Errorf("duplicate key")
		}
	}

	if label == "" {
		label = fmt.Sprintf("Key %d", len(store.Keys)+1)
	}

	entry := KeyEntry{
		ID:                 generateID(),
		Label:              label,
		Key:                key,
		Active:             true,
		CreatedAt:          time.Now().Format(time.RFC3339),
		Model:              model,
		CCSwitchProviderID: providerID,
	}

	store.Keys = append(store.Keys, entry)
	if err := SaveKeys(store); err != nil {
		return KeyEntry{}, err
	}
	return entry, nil
}

// DeleteKey removes a key by ID and cleans up its balance baseline file.
func DeleteKey(id string) error {
	store := LoadKeys()
	found := false
	filtered := make([]KeyEntry, 0, len(store.Keys))
	for _, e := range store.Keys {
		if e.ID == id {
			found = true
			// Clean up baseline file
			_ = os.Remove(balanceBaselinePath(id))
			continue
		}
		filtered = append(filtered, e)
	}
	if !found {
		return fmt.Errorf("key not found: %s", id)
	}
	store.Keys = filtered
	return SaveKeys(store)
}

// UpdateKeyLabel updates the label for a key by ID.
func UpdateKeyLabel(id, label string) error {
	store := LoadKeys()
	for i := range store.Keys {
		if store.Keys[i].ID == id {
			store.Keys[i].Label = label
			return SaveKeys(store)
		}
	}
	return fmt.Errorf("key not found: %s", id)
}

// ToggleKey activates a key by ID and deactivates all others.
// If the key is already active, it's a no-op (at least one key must remain active).
func ToggleKey(id string) (KeyEntry, error) {
	store := LoadKeys()
	var target KeyEntry
	found := false
	for i := range store.Keys {
		if store.Keys[i].ID == id {
			target = store.Keys[i]
			found = true
			break
		}
	}
	if !found {
		return KeyEntry{}, fmt.Errorf("key not found: %s", id)
	}

	// If already active, no-op (single-key activation: must have at least one active)
	if target.Active {
		return target, nil
	}

	// Activate target, deactivate all others
	for i := range store.Keys {
		store.Keys[i].Active = store.Keys[i].ID == id
	}
	if err := SaveKeys(store); err != nil {
		return KeyEntry{}, err
	}
	for _, k := range store.Keys {
		if k.ID == id {
			return k, nil
		}
	}
	return KeyEntry{}, fmt.Errorf("key not found after save: %s", id)
}

// MaskKey returns a masked version of the API key (e.g. "sk-...XXXX").
func MaskKey(key string) string {
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:3] + "..." + key[len(key)-4:]
}

// === Balance API ===

const balanceBaselinePrefix = "claude-monitor-balance-baseline-"

func balanceBaselinePath(keyID string) string {
	return filepath.Join(core.AppSupportDir(), balanceBaselinePrefix+keyID+".json")
}

type balanceTracker struct {
	Date        string  `json:"date"`
	Baseline    float64 `json:"baseline"`
	LastBalance float64 `json:"lastBalance"`
	TotalTopUps float64 `json:"totalTopUps"`
}

// FetchBalanceForKey fetches the DeepSeek balance for a specific key entry.
// Returns the balance and today's spending. Error field is set on the entry
// pointer if the API call fails.
func FetchBalanceForKey(entry *KeyEntry) {
	req, err := http.NewRequest("GET", "https://api.deepseek.com/user/balance", nil)
	if err != nil {
		entry.Error = "request failed"
		return
	}
	req.Header.Set("Authorization", "Bearer "+entry.Key)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		entry.Error = "timeout"
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		entry.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return
	}

	var result struct {
		IsAvailable  bool `json:"is_available"`
		BalanceInfos []struct {
			Currency     string `json:"currency"`
			TotalBalance string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		entry.Error = "parse error"
		return
	}
	if !result.IsAvailable || len(result.BalanceInfos) == 0 {
		entry.Error = "unavailable"
		return
	}

	current, err := parseFloat(result.BalanceInfos[0].TotalBalance)
	if err != nil {
		entry.Error = "parse balance"
		return
	}

	tracker := loadBalanceTracker(entry.ID)
	today := time.Now().Format("2006-01-02")

	if tracker.Date != today {
		tracker = balanceTracker{Date: today, Baseline: current, LastBalance: current, TotalTopUps: 0}
		saveBalanceTracker(entry.ID, tracker)
	}

	if current > tracker.LastBalance+0.001 {
		tracker.TotalTopUps += current - tracker.LastBalance
	}
	tracker.LastBalance = current
	saveBalanceTracker(entry.ID, tracker)

	totalAvailable := tracker.Baseline + tracker.TotalTopUps
	spending := 0.0
	if totalAvailable > current {
		spending = totalAvailable - current
	}

	entry.Balance = current
	entry.TodaySpending = spending
	entry.Currency = result.BalanceInfos[0].Currency
	entry.Error = ""
}

// RefreshAllBalances fetches balances for all keys concurrently.
// Each fetch has a 5-second timeout via HTTP client. Results are written
// back to the key store (balance + error fields), which is then saved.
func RefreshAllBalances() {
	store := LoadKeys()
	if len(store.Keys) == 0 {
		return
	}

	var wg sync.WaitGroup
	for i := range store.Keys {
		wg.Add(1)
		go func(entry *KeyEntry) {
			defer wg.Done()
			FetchBalanceForKey(entry)
		}(&store.Keys[i])
	}
	wg.Wait()

	_ = SaveKeys(store)
}

// === Balance tracker persistence ===

func loadBalanceTracker(keyID string) balanceTracker {
	data, err := os.ReadFile(balanceBaselinePath(keyID))
	if err != nil {
		return balanceTracker{}
	}
	var t balanceTracker
	json.Unmarshal(data, &t)
	return t
}

func saveBalanceTracker(keyID string, t balanceTracker) {
	data, _ := json.Marshal(t)
	os.WriteFile(balanceBaselinePath(keyID), data, 0644)
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
