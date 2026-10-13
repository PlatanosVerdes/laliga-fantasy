// Package dismissed remembers the decision cards I said no to, so "Qué hacer ahora" stops
// proposing them.
//
// A card is dismissed as the exact proposal it was: this player, for that one, until that
// deadline. A new offer or a new listing is a new card and shows up again.
package dismissed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// Keep is how long a dismissal is remembered. Most cards carry a deadline in their key and are
// dead long before this; it only bounds the ones that do not.
const Keep = 14 * 24 * time.Hour

func path() string { return filepath.Join(config.StateDir, "dismissed.json") }

// Load is every live dismissal, keyed by card, with when it was made.
func Load() map[string]time.Time {
	out := map[string]time.Time{}
	body, err := os.ReadFile(path())
	if err != nil {
		return out
	}
	var saved map[string]time.Time
	if json.Unmarshal(body, &saved) != nil {
		return out
	}
	cutoff := time.Now().Add(-Keep)
	for key, at := range saved {
		if at.After(cutoff) {
			out[key] = at
		}
	}
	return out
}

// Add dismisses one card.
func Add(key string) error {
	all := Load()
	all[key] = time.Now()
	return save(all)
}

// Clear brings every dismissed card back.
func Clear() error { return save(map[string]time.Time{}) }

func save(all map[string]time.Time) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), blob, 0o600)
}
