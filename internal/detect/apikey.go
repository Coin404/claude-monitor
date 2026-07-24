package detect

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DeepSeekProvider holds a single DeepSeek provider record from CC Switch.
type DeepSeekProvider struct {
	ID   string
	Name string
	Key  string
}

// ExtractAllDeepSeekAPIKeys reads all DeepSeek provider API keys from
// CC Switch's SQLite DB. Returns a slice of providers (may be empty).
func ExtractAllDeepSeekAPIKeys() []DeepSeekProvider {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dbPath := filepath.Join(home, ".cc-switch", "cc-switch.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil
	}

	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, name, settings_config FROM providers WHERE name LIKE '%DeepSeek%'")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var providers []DeepSeekProvider
	for rows.Next() {
		var id, name, config string
		if err := rows.Scan(&id, &name, &config); err != nil {
			continue
		}

		var settings struct {
			Env map[string]string `json:"env"`
		}
		if err := json.Unmarshal([]byte(config), &settings); err != nil {
			continue
		}
		key := settings.Env["ANTHROPIC_AUTH_TOKEN"]
		if key == "" {
			continue
		}
		providers = append(providers, DeepSeekProvider{
			ID:   id,
			Name: name,
			Key:  key,
		})
	}
	return providers
}

// ExtractDeepSeekAPIKey reads the first DeepSeek API key from CC Switch's
// SQLite DB. Returns empty string if not found.
func ExtractDeepSeekAPIKey() string {
	providers := ExtractAllDeepSeekAPIKeys()
	if len(providers) == 0 {
		return ""
	}
	return providers[0].Key
}

// ExtractCurrentDeepSeekAPIKey returns the API key of the currently active
// DeepSeek provider in CC Switch (is_current=1). Returns empty string if not found.
//
// Deprecated: Use WatchCCSwitchSettings for runtime detection instead.
// This function queries the SQLite DB directly which CC Switch may not update
// in real-time. Kept for backward compatibility.
func ExtractCurrentDeepSeekAPIKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dbPath := filepath.Join(home, ".cc-switch", "cc-switch.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return ""
	}

	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close()

	rows, err := db.Query("SELECT settings_config FROM providers WHERE name LIKE '%DeepSeek%' AND is_current=1 LIMIT 1")
	if err != nil {
		return ""
	}
	defer rows.Close()

	if !rows.Next() {
		return ""
	}

	var config string
	if err := rows.Scan(&config); err != nil {
		return ""
	}

	var cfg struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return ""
	}

	return cfg.Env["ANTHROPIC_AUTH_TOKEN"]
}

// === CC Switch settings.json watch ===

// CCSwitchChange represents a detected change in CC Switch's active provider.
type CCSwitchChange struct {
	CurrentProviderID string
}

// CCSwitchSettings is the JSON structure of ~/.cc-switch/settings.json.
type CCSwitchSettings struct {
	CurrentProviderClaude string `json:"currentProviderClaude"`
}

// WatchCCSwitchSettings polls ~/.cc-switch/settings.json every 2 seconds and
// sends a CCSwitchChange on the channel whenever currentProviderClaude changes.
// This replaces the old DB-based polling approach — it only reads a simple JSON
// file (no SQLite dependency for runtime detection).
func WatchCCSwitchSettings(ch chan<- CCSwitchChange) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	settingsPath := filepath.Join(home, ".cc-switch", "settings.json")

	var lastMod time.Time
	var lastProviderID string

	for {
		time.Sleep(2 * time.Second)

		info, err := os.Stat(settingsPath)
		if err != nil {
			continue
		}

		mod := info.ModTime()
		if mod.Equal(lastMod) {
			continue
		}
		lastMod = mod

		data, err := os.ReadFile(settingsPath)
		if err != nil {
			continue
		}

		var s CCSwitchSettings
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}

		if s.CurrentProviderClaude == "" || s.CurrentProviderClaude == lastProviderID {
			continue
		}
		lastProviderID = s.CurrentProviderClaude

		ch <- CCSwitchChange{CurrentProviderID: s.CurrentProviderClaude}
	}
}
