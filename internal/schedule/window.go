package schedule

import (
	"sort"
	"time"
)

// ClauseLead is how long before a matchday begins the game stops accepting clause payments.
// The API answers 030.01.17 — "not allowed to pay the buyout clause if a new fixture is
// starting in less that one day" — and the game's own FAQ says what the other half of it is:
// "the window to purchase players by paying their release clause closes 24 hours before a
// matchday begins and reopens once the matchday starts".
const ClauseLead = 24 * time.Hour

// Window is whether a buyout clause can be paid at all right now, and until when.
//
// It shuts a day before a matchday's first kick-off and opens again at that kick-off, so what
// is closed is the day of build-up and not the matchday: with the football under way a clause
// can be paid, by you and by anybody who wants one of yours, which is also when a shield is
// worth what it costs.
//
// Blocking, OpensAt and ClosesAt are RFC3339 stamps and any of them can be empty: with no
// calendar there is no opinion to give, and saying so is better than guessing an hour.
type Window struct {
	Open     bool   `json:"open"`
	Blocking string `json:"blocking,omitempty"`
	OpensAt  string `json:"opens_at,omitempty"`
	ClosesAt string `json:"closes_at,omitempty"`
}

// Clauses reads the window off the fixture list. Feed it every fixture that is known, past ones
// included: a matchday already under way has to be recognised as under way, and it is its first
// kick-off — which is behind us — that says so.
func Clauses(fixtures []Fixture, now time.Time) Window {
	// The first kick-off of each matchday, because that instant is the whole rule. A fixture
	// with no matchday on it counts as one of its own, which is the cautious reading: better a
	// window that shuts too often than one that promises a payment the game refuses.
	starts := map[int]time.Time{}
	for index, fixture := range fixtures {
		kickoff := fixture.Kickoff
		when, ok := parse(&kickoff)
		if !ok {
			continue
		}
		week := fixture.Week
		if week == 0 {
			week = -index - 1
		}
		if first, seen := starts[week]; !seen || when.Before(first) {
			starts[week] = when
		}
	}

	ahead := make([]time.Time, 0, len(starts))
	for _, when := range starts {
		if when.After(now) {
			ahead = append(ahead, when)
		}
	}
	if len(ahead) == 0 {
		return Window{Open: true}
	}
	sort.Slice(ahead, func(one, two int) bool { return ahead[one].Before(ahead[two]) })

	next := ahead[0]
	if next.Sub(now) > ClauseLead {
		return Window{Open: true, ClosesAt: next.Add(-ClauseLead).Format(time.RFC3339)}
	}
	// The matchday it is waiting for is also the hour it reopens.
	stamp := next.Format(time.RFC3339)
	return Window{Blocking: stamp, OpensAt: stamp}
}
