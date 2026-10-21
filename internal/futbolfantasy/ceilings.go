package futbolfantasy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/files"
)

// Ceiling is the one thing the panel reads from a player's futbolfantasy page: the profitable
// ceiling, with where it came from and when.
type Ceiling struct {
	IdealBid  float64   `json:"ideal_bid"`
	URL       string    `json:"url,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

// CeilingTTL is how long a stored ceiling is trusted within a day, FANTASY_CEILING_TTL to change
// it. A ceiling read before the day's price adjustment is stale whatever its age: futbolfantasy
// recomputes it when a price moves, and prices move once a night.
var CeilingTTL = ceilingTTL()

// The site adjusts prices at 00:17 Madrid time and reads them again for a few minutes after.
const adjustedBy = 30 * time.Minute

func ceilingTTL() time.Duration {
	if raw := os.Getenv("FANTASY_CEILING_TTL"); raw != "" {
		if ttl, err := time.ParseDuration(raw); err == nil && ttl > 0 {
			return ttl
		}
		slog.Warn("FANTASY_CEILING_TTL unreadable, using the default", "value", raw)
	}
	return 12 * time.Hour
}

var madrid = func() *time.Location {
	location, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		return time.UTC
	}
	return location
}()

// lastAdjustment is the most recent nightly price adjustment at or before now.
func lastAdjustment(now time.Time) time.Time {
	local := now.In(madrid)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, madrid).Add(adjustedBy)
	if day.After(local) {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

// Fresh is whether the ceiling can be used without asking again.
func (c Ceiling) Fresh(now time.Time) bool {
	return !c.FetchedAt.Before(lastAdjustment(now)) && now.Sub(c.FetchedAt) <= CeilingTTL
}

// Age is how old the ceiling is, in words for the page: "hace 9 h".
func (c Ceiling) Age(now time.Time) string {
	age := now.Sub(c.FetchedAt)
	switch {
	case age < time.Hour:
		return fmt.Sprintf("hace %d min", max(1, int(age.Minutes())))
	case age < 48*time.Hour:
		return fmt.Sprintf("hace %d h", int(age.Hours()))
	}
	return fmt.Sprintf("hace %d días", int(age.Hours()/24))
}

// The store keeps every ceiling read, on disk, so a restart starts with them instead of asking
// futbolfantasy for a hundred pages again.
var (
	ceilingMu sync.Mutex
	ceilings  map[string]Ceiling
)

// CeilingFile is where the ceilings are kept, beside the cache and shared by every profile:
// a player's page is the same whichever league asks.
func CeilingFile() string { return filepath.Join(config.CacheRoot, "ff_ceilings.json") }

// LoadCeilings reads the stored ceilings once, returning how many there are and how many are
// still fresh. Everything else that reads the store loads it first too.
func LoadCeilings() (int, int) {
	ceilingMu.Lock()
	defer ceilingMu.Unlock()
	loadCeilings()
	now, fresh := time.Now(), 0
	for _, ceiling := range ceilings {
		if ceiling.Fresh(now) {
			fresh++
		}
	}
	return len(ceilings), fresh
}

// loadCeilings fills the store from disk the first time it is read. Called with ceilingMu held.
func loadCeilings() {
	if ceilings != nil {
		return
	}
	ceilings = map[string]Ceiling{}
	body, err := os.ReadFile(CeilingFile())
	if err != nil {
		return
	}
	if err := json.Unmarshal(body, &ceilings); err != nil {
		slog.Warn("stored ceilings unreadable", "file", CeilingFile(), "reason", err.Error())
		ceilings = map[string]Ceiling{}
	}
}

// StoredCeiling is the last ceiling read for the player, fresh or not.
func StoredCeiling(ffID string) (Ceiling, bool) {
	ceilingMu.Lock()
	defer ceilingMu.Unlock()
	loadCeilings()
	ceiling, ok := ceilings[ffID]
	return ceiling, ok
}

// FetchCeiling reads the player's page now, through the throttle, and stores what it says.
func FetchCeiling(ffID string) (Ceiling, error) {
	detail, err := PlayerDetail(ffID, 0)
	if err != nil {
		return Ceiling{}, err
	}
	ceiling := Ceiling{FetchedAt: time.Now()}
	if ideal, ok := detail["ideal_bid"].(int); ok {
		ceiling.IdealBid = float64(ideal)
	}
	if url, ok := detail["ff_url"].(*string); ok && url != nil {
		ceiling.URL = *url
	}
	ceilingMu.Lock()
	loadCeilings()
	ceilings[ffID] = ceiling
	blob, err := json.Marshal(ceilings)
	ceilingMu.Unlock()
	if err == nil {
		if err := os.MkdirAll(config.CacheRoot, 0o700); err == nil {
			err = files.WriteAtomic(CeilingFile(), blob, 0o600)
		}
		if err != nil {
			slog.Warn("ceilings not stored", "reason", err.Error())
		}
	}
	return ceiling, nil
}

// CeilingFor is the stored ceiling while it is fresh, and a new read otherwise. A read that
// fails falls back on the stored one, however old, and still returns the error so the caller
// can say how old it is.
func CeilingFor(ffID string) (Ceiling, error) {
	stored, ok := StoredCeiling(ffID)
	if ok && stored.Fresh(time.Now()) {
		return stored, nil
	}
	fetched, err := FetchCeiling(ffID)
	if err == nil {
		return fetched, nil
	}
	if ok {
		return stored, err
	}
	return Ceiling{}, err
}
