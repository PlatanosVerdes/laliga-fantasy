package schedule

import (
	"sort"
	"time"
)

// ShieldsPerRound is how many shields a team gets per matchday. The game calls it "doble
// blindaje": two per matchday, usable from the end of the previous one until this one ends, and
// they do not carry over. The league log agrees: nobody has ever bought a third in one round.
const ShieldsPerRound = 2

// RoundTail is how long after a matchday's last kick-off the game closes it: matchday 8 ends at
// 21:00 on a Monday and its closingWeekDate is 03:00 on the Tuesday.
const RoundTail = 6 * time.Hour

// RoundSpan is how many days a matchday's own block of kick-offs covers. Matches moved out of
// it, earlier or later, are left out of its end: matchday 6 had one on 21/10 and matchday 7 one
// in September, and either would have dragged the whole round with it.
const RoundSpan = 5 * 24 * time.Hour

// Round is the stretch whose shields count against one matchday's two.
type Round struct {
	Week  int       `json:"week"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Contains is whether a shield bought at that instant counts against this round.
func (r Round) Contains(at time.Time) bool {
	return at.After(r.Start) && !at.After(r.End)
}

// ShieldRound is the matchday a shield bought at that instant counts against: the first one
// that has not closed yet. False when the calendar does not reach that far.
func ShieldRound(fixtures []Fixture, at time.Time) (Round, bool) {
	kickoffs := map[int][]time.Time{}
	for _, fixture := range fixtures {
		if kickoff, ok := parse(&fixture.Kickoff); ok && fixture.Week != 0 {
			kickoffs[fixture.Week] = append(kickoffs[fixture.Week], kickoff)
		}
	}
	last := map[int]time.Time{}
	for week, times := range kickoffs {
		sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
		best := 0
		for first := range times {
			count, end := 0, times[first]
			for _, kickoff := range times[first:] {
				if kickoff.Sub(times[first]) <= RoundSpan {
					count, end = count+1, kickoff
				}
			}
			if count > best {
				best, last[week] = count, end
			}
		}
	}
	weeks := make([]int, 0, len(last))
	for week := range last {
		weeks = append(weeks, week)
	}
	sort.Ints(weeks)

	start := time.Time{}
	for _, week := range weeks {
		end := last[week].Add(RoundTail)
		if !at.After(end) {
			return Round{Week: week, Start: start, End: end}, true
		}
		start = end
	}
	return Round{}, false
}
