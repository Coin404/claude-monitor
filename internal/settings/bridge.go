package settings

import (
	"encoding/json"
	"os"
	"time"
)

const (
	keysSnapshotPath = "/tmp/claude-monitor-keys.json"
	keyActionsPath   = "/tmp/claude-monitor-key-actions.json"
)

// === Bridge data structures ===

// KeyDisplayInfo is the per-key display payload sent to SwiftUI.
type KeyDisplayInfo struct {
	ID            string  `json:"id"`
	Label         string  `json:"label"`
	MaskedKey     string  `json:"maskedKey"`
	Active        bool    `json:"active"`
	Balance       float64 `json:"balance"`
	TodaySpending float64 `json:"todaySpending"`
	Currency      string  `json:"currency"`
	Error         string  `json:"error"`
	CreatedAt     string  `json:"createdAt"`
	Model         string  `json:"model"`
}

// KeysSnapshot is the JSON payload written to /tmp/claude-monitor-keys.json.
type KeysSnapshot struct {
	Keys               []KeyDisplayInfo `json:"keys"`
	Count              int              `json:"count"`
	Timestamp          string           `json:"timestamp"`
	RefreshIntervalSec int32            `json:"refreshIntervalSec"`
	PollIntervalMs     int32            `json:"pollIntervalMs"`
}

// KeyAction represents an action requested by the SwiftUI settings window.
type KeyAction struct {
	Action string `json:"action"` // "add", "delete", "toggle", "edit", "refresh"
	ID     string `json:"id,omitempty"`
	Label  string `json:"label,omitempty"`
	Key    string `json:"key,omitempty"`
}

// WriteKeysSnapshot writes all keys (with masked key values and balance data)
// to /tmp/claude-monitor-keys.json for the SwiftUI settings window to consume.
func WriteKeysSnapshot(refreshSec int32, pollMs int32) {
	store := LoadKeys()
	infos := make([]KeyDisplayInfo, 0, len(store.Keys))
	for _, e := range store.Keys {
		infos = append(infos, KeyDisplayInfo{
			ID:            e.ID,
			Label:         e.Label,
			MaskedKey:     MaskKey(e.Key),
			Active:        e.Active,
			Balance:       e.Balance,
			TodaySpending: e.TodaySpending,
			Currency:      e.Currency,
			Error:         e.Error,
			CreatedAt:     e.CreatedAt,
			Model:         e.Model,
		})
	}

	snap := KeysSnapshot{
		Keys:               infos,
		Count:              len(infos),
		Timestamp:          time.Now().Format(time.RFC3339),
		RefreshIntervalSec: refreshSec,
		PollIntervalMs:     pollMs,
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return
	}

	tmpPath := keysSnapshotPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return
	}
	if err := os.Rename(tmpPath, keysSnapshotPath); err != nil {
		_ = os.Remove(tmpPath)
	}
}

// WatchKeyActions polls /tmp/claude-monitor-key-actions.json for changes.
// When a new action file is detected (via ModTime change), it reads, parses,
// sends the action to the channel, and deletes the file. Polls every 500ms.
func WatchKeyActions(ch chan<- KeyAction) {
	var lastMod time.Time

	for {
		time.Sleep(500 * time.Millisecond)

		info, err := os.Stat(keyActionsPath)
		if err != nil {
			continue
		}

		mod := info.ModTime()
		if mod.Equal(lastMod) {
			continue
		}
		lastMod = mod

		data, err := os.ReadFile(keyActionsPath)
		if err != nil {
			continue
		}

		var action KeyAction
		if err := json.Unmarshal(data, &action); err != nil {
			continue
		}

		// Delete the action file after reading
		_ = os.Remove(keyActionsPath)

		ch <- action
	}
}
