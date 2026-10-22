package model

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
)

const (
	AbsencesTTL           = 3 * time.Hour
	AfterMatchAbsencesTTL = 15 * time.Minute
	AfterMatchWindow      = 2 * time.Hour
	// The calendar has no final whistle: 90 minutes, half-time and added time.
	MatchLength = 115 * time.Minute
)

// A match under way counts too: a first-half injury can be up before the final whistle.
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

// The previous matchday too: the API moves on while its last match is still in the window.
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

type AbsenceWatch struct {
	mu   sync.Mutex
	seen map[string]bool
}

var absenceWatch AbsenceWatch

// A row edited in place counts as one removed and one added; the first read is never a change.
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
