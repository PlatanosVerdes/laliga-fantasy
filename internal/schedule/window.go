package schedule

import (
	"sort"
	"time"
)

// ClauseLead is how long before a kick-off the game stops accepting clause payments. The API
// answers 030.01.17 — "not allowed to pay the buyout clause if a new fixture is starting in
// less that one day" — so the rule is the calendar's, not the market's.
const ClauseLead = 24 * time.Hour

// Window is whether a buyout clause can be paid at all right now, and until when.
//
// It shuts a day before the matchday's first kick-off and opens again in the gap between
// matchdays, so a whole weekend is closed for everybody: no rival can raid either, which is
// also what makes a shield spent inside it worth nothing.
//
// Blocking, OpensAt and ClosesAt are RFC3339 stamps and any of them can be empty: with no
// calendar there is no opinion to give, and saying so is better than guessing an hour.
type Window struct {
	Open     bool   `json:"open"`
	Blocking string `json:"blocking,omitempty"`
	OpensAt  string `json:"opens_at,omitempty"`
	ClosesAt string `json:"closes_at,omitempty"`
}

// Clauses reads the window off the fixture list. Feed it every fixture that is known, not just
// this matchday's: when it opens again is the next matchday's business.
func Clauses(fixtures []Fixture, now time.Time) Window {
	kickoffs := make([]time.Time, 0, len(fixtures))
	for _, fixture := range fixtures {
		kickoff := fixture.Kickoff
		if when, ok := parse(&kickoff); ok && when.After(now) {
			kickoffs = append(kickoffs, when)
		}
	}
	if len(kickoffs) == 0 {
		return Window{Open: true}
	}
	sort.Slice(kickoffs, func(one, two int) bool { return kickoffs[one].Before(kickoffs[two]) })

	if next := kickoffs[0]; next.Sub(now) > ClauseLead {
		return Window{Open: true, ClosesAt: next.Add(-ClauseLead).Format(time.RFC3339)}
	}

	window := Window{Blocking: kickoffs[0].Format(time.RFC3339)}
	// The first gap wider than a day: the last kick-off before it is the instant the window
	// opens, because from then on no new fixture starts within the day.
	for index := 0; index+1 < len(kickoffs); index++ {
		if kickoffs[index+1].Sub(kickoffs[index]) > ClauseLead {
			window.OpensAt = kickoffs[index].Format(time.RFC3339)
			break
		}
	}
	return window
}
