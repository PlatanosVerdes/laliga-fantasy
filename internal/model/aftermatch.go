package model

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
)

const (
	// AbsencesTTL is how long the injury pages keep while no match is near.
	AbsencesTTL = 3 * time.Hour
	// AfterMatchAbsencesTTL is how long they keep from kick-off until AfterMatchWindow past
	// the final whistle, when the knocks of the day get published.
	AfterMatchAbsencesTTL = 15 * time.Minute
	AfterMatchWindow      = 2 * time.Hour
	// MatchLength stands in for the final whistle, which the calendar does not give: ninety
	// minutes, halftime and added time.
	MatchLength = 115 * time.Minute
)

// AbsencesTTLAt is how long the absences pages keep at `now`. A match under way counts as
// recent too: an injury in the first half can be up before the second ends.
func AbsencesTTLAt(fixtures []Fixture, now time.Time) time.Duration {
	for _, fixture := range fixtures {
		kickoff, err := parseTime(fixture.Kickoff)
		if err != nil || kickoff.After(now) {
			continue
		}
		if now.Before(kickoff.Add(MatchLength + AfterMatchWindow)) {
			return AfterMatchAbsencesTTL
		}
	}
	return AbsencesTTL
}

// SinceLastMatch is how long ago the most recent match ended, false when none has.
func SinceLastMatch(fixtures []Fixture, now time.Time) (time.Duration, bool) {
	var latest time.Time
	for _, fixture := range fixtures {
		kickoff, err := parseTime(fixture.Kickoff)
		if err != nil {
			continue
		}
		if end := kickoff.Add(MatchLength); !end.After(now) && end.After(latest) {
			latest = end
		}
	}
	if latest.IsZero() {
		return 0, false
	}
	return now.Sub(latest), true
}

// recentFixtures is this matchday and the one before: the last match of a matchday can still
// be in its window when the API has already moved on to the next.
func recentFixtures(client *api.Client, teams []api.Team) []Fixture {
	week, err := client.CurrentWeek(30 * time.Minute)
	if err != nil {
		return nil
	}
	out := LoadFixtures(client, week, teams)
	if week.WeekNumber > 1 {
		previous := week
		previous.WeekNumber--
		out = append(out, LoadFixtures(client, previous, teams)...)
	}
	return out
}

// AbsenceWatch remembers the last absences read, to log when they change and how long after
// a match that was.
type AbsenceWatch struct {
	mu   sync.Mutex
	seen map[string]bool
}

var absenceWatch AbsenceWatch

// Changes compares rows with the previous read and keeps them as the new one. A row edited in
// place counts as one removed and one added. The first read is never a change.
func (w *AbsenceWatch) Changes(rows []map[string]any) (added, removed int, changed bool) {
	current := make(map[string]bool, len(rows))
	for _, row := range rows {
		key, err := json.Marshal(row)
		if err != nil {
			continue
		}
		current[string(key)] = true
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	previous := w.seen
	w.seen = current
	if previous == nil {
		return 0, 0, false
	}
	for key := range current {
		if !previous[key] {
			added++
		}
	}
	for key := range previous {
		if !current[key] {
			removed++
		}
	}
	return added, removed, added+removed > 0
}

// Observe logs one line when the absences differ from the last read.
func (w *AbsenceWatch) Observe(rows []map[string]any, fixtures []Fixture, ttl time.Duration,
	now time.Time) {
	added, removed, changed := w.Changes(rows)
	if !changed {
		return
	}
	since := "none"
	if elapsed, ok := SinceLastMatch(fixtures, now); ok {
		since = elapsed.Round(time.Minute).String()
	}
	slog.Info("absences changed", "since_match", since, "added", added, "removed", removed,
		"total", len(rows), "ttl", ttl.String())
}
