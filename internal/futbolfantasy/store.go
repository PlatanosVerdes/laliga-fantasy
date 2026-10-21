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

// Detail is what the panel keeps from a player's futbolfantasy page, with where it came from
// and when. The profitable ceiling is the only part any judgement reads; the season's high and
// low and the injury mark are drawn beside it and decide nothing. A record stored before they
// were kept has them empty, which reads as unknown.
type Detail struct {
	IdealBid  float64   `json:"ideal_bid"`
	URL       string    `json:"url,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
	MaxValue  float64   `json:"max_value,omitempty"`
	MaxDate   string    `json:"max_date,omitempty"`
	MinValue  float64   `json:"min_value,omitempty"`
	MinDate   string    `json:"min_date,omitempty"`
	// Mark is nil when the page said nothing readable about injuries.
	Mark *Mark `json:"mark,omitempty"`
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

// Fresh is whether the page can be used without asking again.
func (c Detail) Fresh(now time.Time) bool {
	return !c.FetchedAt.Before(lastAdjustment(now)) && now.Sub(c.FetchedAt) <= CeilingTTL
}

// Age is how old the page is, in words for the page: "hace 9 h".
func (c Detail) Age(now time.Time) string {
	age := now.Sub(c.FetchedAt)
	switch {
	case age < time.Hour:
		return fmt.Sprintf("hace %d min", max(1, int(age.Minutes())))
	case age < 48*time.Hour:
		return fmt.Sprintf("hace %d h", int(age.Hours()))
	}
	return fmt.Sprintf("hace %d días", int(age.Hours()/24))
}

// The store keeps every page read, on disk, so a restart starts with them instead of asking
// futbolfantasy for a hundred pages again.
var (
	storeMu sync.Mutex
	store   map[string]Detail
)

// StoreFile is where the pages are kept, beside the cache and shared by every profile: a
// player's page is the same whichever league asks. The name is from when it held only ceilings.
func StoreFile() string { return filepath.Join(config.CacheRoot, "ff_ceilings.json") }

// LoadDetails reads the store once, returning how many pages it holds and how many are still
// fresh. Everything else that reads the store loads it first too.
func LoadDetails() (int, int) {
	storeMu.Lock()
	defer storeMu.Unlock()
	loadStore()
	now, fresh := time.Now(), 0
	for _, detail := range store {
		if detail.Fresh(now) {
			fresh++
		}
	}
	return len(store), fresh
}

// loadStore fills the store from disk the first time it is read. Called with storeMu held.
func loadStore() {
	if store != nil {
		return
	}
	store = map[string]Detail{}
	body, err := os.ReadFile(StoreFile())
	if err != nil {
		return
	}
	if err := json.Unmarshal(body, &store); err != nil {
		slog.Warn("stored ceilings unreadable", "file", StoreFile(), "reason", err.Error())
		store = map[string]Detail{}
	}
}

// StoredDetail is the last page read for the player, fresh or not.
func StoredDetail(ffID string) (Detail, bool) {
	storeMu.Lock()
	defer storeMu.Unlock()
	loadStore()
	detail, ok := store[ffID]
	return detail, ok
}

// FetchDetail reads the player's page now, through the throttle, and stores what it says.
func FetchDetail(ffID string) (Detail, error) {
	page, err := PlayerDetail(ffID, 0)
	if err != nil {
		return Detail{}, err
	}
	detail := Detail{FetchedAt: time.Now()}
	if ideal, ok := page["ideal_bid"].(int); ok {
		detail.IdealBid = float64(ideal)
	}
	if url, ok := page["ff_url"].(*string); ok && url != nil {
		detail.URL = *url
	}
	if value, ok := page["max_value"].(*float64); ok && value != nil {
		detail.MaxValue, detail.MaxDate = *value, text(page["max_date"])
	}
	if value, ok := page["min_value"].(*float64); ok && value != nil {
		detail.MinValue, detail.MinDate = *value, text(page["min_date"])
	}
	if mark, ok := page["mark"].(*Mark); ok {
		detail.Mark = mark
	}
	storeMu.Lock()
	loadStore()
	store[ffID] = detail
	blob, err := json.Marshal(store)
	storeMu.Unlock()
	if err == nil {
		if err := os.MkdirAll(config.CacheRoot, 0o700); err == nil {
			err = files.WriteAtomic(StoreFile(), blob, 0o600)
		}
		if err != nil {
			slog.Warn("ceilings not stored", "reason", err.Error())
		}
	}
	return detail, nil
}

// DetailFor is the stored page while it is fresh, and a new read otherwise. A read that fails
// falls back on the stored one, however old, and still returns the error so the caller can say
// how old it is.
func DetailFor(ffID string) (Detail, error) {
	stored, ok := StoredDetail(ffID)
	if ok && stored.Fresh(time.Now()) {
		return stored, nil
	}
	fetched, err := FetchDetail(ffID)
	if err == nil {
		return fetched, nil
	}
	if ok {
		return stored, err
	}
	return Detail{}, err
}

func text(value any) string {
	if found, ok := value.(*string); ok && found != nil {
		return *found
	}
	return ""
}
