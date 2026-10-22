// Package rewards remembers which day the daily reward was claimed.
//
// The API is the authority on whether today's is still there, and it is asked every time. This
// file is the second lock: if a claim answers 200 but the counter does not move, the cycle would
// keep claiming every two minutes forever. Written down per league, because the reward is per
// league.
package rewards

import (
	"encoding/json"
	"os"
	"sync"
	"time"
	// The zone database, embedded: the runtime image has no /usr/share/zoneinfo, and
	// without it LoadLocation fails and the day silently becomes UTC's, two hours late.
	_ "time/tzdata"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

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
	if store := opened[profile.RewardFile]; store != nil {
		return store
	}
	store := &Store{profile: profile}
	opened[profile.RewardFile] = store
	return store
}

// Today is the day a claim belongs to, in the game's own timezone: the counter resets at
// midnight in Spain, not at midnight UTC.
func Today() string {
	location, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		return time.Now().Format("2006-01-02")
	}
	return time.Now().In(location).Format("2006-01-02")
}

func (s *Store) load() map[string]string {
	claimed := map[string]string{}
	body, err := os.ReadFile(s.profile.RewardFile)
	if err != nil {
		return claimed
	}
	if err := json.Unmarshal(body, &claimed); err != nil {
		return map[string]string{}
	}
	return claimed
}

// ClaimedToday says whether this league's reward was already taken today.
func (s *Store) ClaimedToday(leagueID string) bool {
	return s.load()[leagueID] == Today()
}

// Stamp records the claim. Also called when the API says the reward is already gone, so a
// reward claimed from the phone does not have the cycle asking again all afternoon.
func (s *Store) Stamp(leagueID string) error {
	if err := s.profile.EnsureDirs(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	claimed := s.load()
	claimed[leagueID] = Today()
	body, err := json.MarshalIndent(claimed, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.profile.RewardFile, body, 0o600)
}
