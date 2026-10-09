package schedule

import (
	"testing"
	"time"
)

// NextWake is NextWakeWith at the constants: the harness against Python keeps comparing the same
// decisions whatever the page lets be tuned.
func TestCadenceAtTheDefaultsDecidesAsBefore(t *testing.T) {
	now := at(t, "2026-10-06T19:00:00+02:00")
	kickoff := "2026-10-06T18:30:00+02:00"
	payloads := []Payload{{}, {
		Players:  []Player{{ID: "7", Name: "Angel Perez", IsMine: true, TeamID: "1"}},
		Fixtures: []Fixture{{Kickoff: kickoff, LocalID: "1", VisitorID: "2", State: 1}},
	}}
	defaults := Cadence{Tick: 2 * time.Minute, Live: LiveTick, Ceiling: Ceiling}
	for _, payload := range payloads {
		for _, watched := range []bool{false, true} {
			for _, lastFull := range []time.Time{{}, now.Add(-14 * time.Minute)} {
				old := NextWake(payload, now, 2*time.Minute, lastFull, watched, time.Time{})
				got := NextWakeWith(payload, now, defaults, lastFull, watched, time.Time{})
				if old != got {
					t.Errorf("con los valores de siempre cambia la decision: %+v frente a %+v",
						got, old)
				}
			}
		}
	}
}

func TestEachClockMovesItsOwnWake(t *testing.T) {
	now := at(t, "2026-10-06T19:00:00+02:00")
	got := NextWakeWith(Payload{}, now, Cadence{Tick: time.Minute, Live: LiveTick,
		Ceiling: time.Hour}, now, true, time.Time{})
	if !got.At.Equal(now.Add(time.Minute)) || got.Kind != Probe {
		t.Errorf("el sondeo sigue a su reloj: %+v", got)
	}
	got = NextWakeWith(Payload{}, now, Cadence{Tick: time.Hour, Live: LiveTick,
		Ceiling: 5 * time.Minute}, now, true, time.Time{})
	if !got.At.Equal(now.Add(5*time.Minute)) || got.Kind != Rebuild {
		t.Errorf("la reconstruccion sigue a su reloj: %+v", got)
	}
	live := Payload{
		Players:  []Player{{ID: "7", Name: "Angel Perez", IsMine: true, TeamID: "1"}},
		Fixtures: []Fixture{{Kickoff: "2026-10-06T18:30:00+02:00", LocalID: "1", VisitorID: "2"}},
	}
	got = NextWakeWith(live, now, Cadence{Tick: time.Hour, Live: 45 * time.Second,
		Ceiling: time.Hour}, now, false, time.Time{})
	if !got.At.Equal(now.Add(45 * time.Second)) {
		t.Errorf("con un partido nuestro manda su reloj: %+v", got)
	}
}
