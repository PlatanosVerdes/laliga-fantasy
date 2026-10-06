package schedule

import (
	"strings"
	"testing"
)

// A scheduled shield has to wake the engine at its hour: on 06/10 the one armed for 19:40 waited
// for the next periodic rebuild instead, and the clause stayed payable in between.
func TestDeadlinesWakeForScheduledShield(t *testing.T) {
	now := at(t, "2026-10-06T19:00:00+02:00")
	hour := "2026-10-06T19:40:00+02:00"
	payload := Payload{
		Players:  []Player{{ID: "7", Name: "Angel Perez", IsMine: true}},
		Policies: map[string]Policy{"7": {ShieldAt: &hour}},
	}

	found := Deadlines(payload, now, now)
	if len(found) != 1 || !found[0].At.Equal(at(t, hour)) ||
		!strings.Contains(found[0].Why, "blindar") {
		t.Fatalf("deadlines %+v, esperaba el blindaje de las 19:40", found)
	}

	if past := Deadlines(payload, at(t, "2026-10-06T19:41:00+02:00"), now); len(past) != 0 {
		t.Errorf("un blindaje ya pasado no es un vencimiento: %+v", past)
	}
}

// Matchday 8's shields run from the end of matchday 7 to the end of matchday 8, last kick-off
// plus the tail: the two bought on Tuesday 06/10 are matchday 8's, and so is Friday's. Matchday
// 6's postponed match on 21/10 and matchday 7's brought forward to September must not move the
// boundaries.
func TestShieldRound(t *testing.T) {
	fixtures := []Fixture{
		{Week: 6, Kickoff: "2026-09-26T16:15:00+02:00"},
		{Week: 6, Kickoff: "2026-10-21T19:00:00+02:00"},
		{Week: 7, Kickoff: "2026-09-16T19:00:00+02:00"},
		{Week: 7, Kickoff: "2026-10-03T16:15:00+02:00"},
		{Week: 7, Kickoff: "2026-10-04T16:15:00+02:00"},
		{Week: 7, Kickoff: "2026-10-05T21:00:00+02:00"},
		{Week: 8, Kickoff: "2026-10-09T21:00:00+02:00"},
		{Week: 8, Kickoff: "2026-10-12T21:00:00+02:00"},
	}
	cases := []struct {
		at   string
		week int
	}{
		{"2026-09-30T12:00:00+02:00", 7},
		{"2026-10-05T22:00:00+02:00", 7},
		{"2026-10-06T15:49:00+02:00", 8},
		{"2026-10-09T21:00:00+02:00", 8},
		{"2026-10-13T02:59:00+02:00", 8},
	}
	for _, test := range cases {
		round, ok := ShieldRound(fixtures, at(t, test.at))
		if !ok || round.Week != test.week {
			t.Errorf("%s: jornada %d (%v), esperaba %d", test.at, round.Week, ok, test.week)
		}
	}
	if _, ok := ShieldRound(fixtures, at(t, "2026-10-14T12:00:00+02:00")); ok {
		t.Error("past the calendar there is no round to count against")
	}
}
