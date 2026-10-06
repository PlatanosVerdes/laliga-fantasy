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
