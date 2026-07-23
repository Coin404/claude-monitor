package detect

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// ExtractDeepSeekAPIKey reads the DeepSeek API key from CC Switch's SQLite DB.
// Returns empty string if not found.
func ExtractDeepSeekAPIKey() string {
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

	var config string
	err = db.QueryRow("SELECT settings_config FROM providers WHERE name='DeepSeek' LIMIT 1").Scan(&config)
	if err != nil {
		return ""
	}

	var settings struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(config), &settings); err != nil {
		return ""
	}
	return settings.Env["ANTHROPIC_AUTH_TOKEN"]
}
