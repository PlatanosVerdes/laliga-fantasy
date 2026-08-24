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
	"path/filepath"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func path() string { return filepath.Join(config.StateDir, "daily_reward.json") }

// Today is the day a claim belongs to, in the game's own timezone: the counter resets at
// midnight in Spain, not at midnight UTC.
func Today() string {
	location, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		return time.Now().Format("2006-01-02")
	}
	return time.Now().In(location).Format("2006-01-02")
}

func load() map[string]string {
	claimed := map[string]string{}
	body, err := os.ReadFile(path())
	if err != nil {
		return claimed
	}
	if err := json.Unmarshal(body, &claimed); err != nil {
		return map[string]string{}
	}
	return claimed
}

// ClaimedToday says whether this league's reward was already taken today.
func ClaimedToday(leagueID string) bool {
	return load()[leagueID] == Today()
}

// Stamp records the claim. Also called when the API says the reward is already gone, so a
// reward claimed from the phone does not have the cycle asking again all afternoon.
func Stamp(leagueID string) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	claimed := load()
	claimed[leagueID] = Today()
	body, err := json.MarshalIndent(claimed, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), body, 0o600)
}
