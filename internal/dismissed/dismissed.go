// Package dismissed remembers the decision cards I said no to, so "Qué hacer ahora" stops
// proposing them.
//
// A card is dismissed as the exact proposal it was: this player, for that one, until that
// deadline. A new offer or a new listing is a new card and shows up again.
package dismissed

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/files"
)

// Keep is how long a dismissal is remembered. Most cards carry a deadline in their key and are
// dead long before this; it only bounds the ones that do not.
const Keep = 14 * 24 * time.Hour

type Store struct {
	profile config.Profile
	mu      sync.Mutex
}

var (
	openMu sync.Mutex
	opened = map[string]*Store{}
)

// One store per file, so everyone writing it shares its lock.
func Open(profile config.Profile) *Store {
	openMu.Lock()
	defer openMu.Unlock()
	if store := opened[profile.DismissedFile]; store != nil {
		return store
	}
	store := &Store{profile: profile}
	opened[profile.DismissedFile] = store
	return store
}

// Load is every live dismissal, keyed by card, with when it was made.
func (s *Store) Load() map[string]time.Time {
	out := map[string]time.Time{}
	body, err := os.ReadFile(s.profile.DismissedFile)
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
func (s *Store) Add(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.Load()
	all[key] = time.Now()
	return s.save(all)
}

// Clear brings every dismissed card back.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(map[string]time.Time{})
}

func (s *Store) save(all map[string]time.Time) error {
	if err := s.profile.EnsureDirs(); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return files.WriteAtomic(s.profile.DismissedFile, blob, 0o600)
}
