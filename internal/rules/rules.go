// Package rules is the house rules: what a league has agreed among its people that no API
// enforces. Kept per league, because the next league will have different ones.
//
// Only one kind of rule can change what the tool proposes — a hold period after buying —
// and it is the only one modelled. The rest are notes: printed where they can be read,
// never interpreted, because a rule about who brings breakfast is not a rule about football.
package rules

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

type League struct {
	Name string `json:"name,omitempty"`
	// HoldDays is how long a signing must be kept before selling him. Zero means no rule.
	HoldDays int `json:"hold_days,omitempty"`
	// HoldExceptions is the agreed escape hatch, in the league's own words. It is shown
	// beside the refusal so the person knows whether to go ask.
	HoldExceptions string `json:"hold_exceptions,omitempty"`
	// MaxDebtPct is the share of the squad's value, in percent, that market bids may spend
	// beyond the cash. Never for clauses or clause raises. Zero is cash only.
	MaxDebtPct float64 `json:"max_debt_pct,omitempty"`
	// Notes are the rest of the pact: prizes, forfeits, votes. Displayed, never acted on.
	Notes []string `json:"notes,omitempty"`
}

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
	if store := opened[profile.RulesFile]; store != nil {
		return store
	}
	store := &Store{profile: profile}
	opened[profile.RulesFile] = store
	return store
}

func (s *Store) Load() (map[string]League, error) {
	body, err := os.ReadFile(s.profile.RulesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]League{}, nil
		}
		return nil, err
	}
	var out map[string]League
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]League{}, nil
	}
	return out, nil
}

func (s *Store) Save(leagues map[string]League) error {
	if err := s.profile.EnsureDirs(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	blob, err := json.MarshalIndent(leagues, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.profile.RulesFile, blob, 0o600)
}

// For returns the rules of one league, empty when it has none.
func (s *Store) For(leagueID string) League {
	leagues, err := s.Load()
	if err != nil {
		return League{}
	}
	return leagues[leagueID]
}

// HeldUntil is when a player bought at this instant becomes sellable, or nil when the league
// has no hold rule.
func (l League) HeldUntil(bought time.Time) *time.Time {
	if l.HoldDays <= 0 {
		return nil
	}
	until := bought.AddDate(0, 0, l.HoldDays)
	return &until
}
