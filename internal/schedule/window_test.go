package schedule

import (
	"testing"
	"time"
)

// Matchday 4 as the calendar served it, plus the first fixture of the next one. No two
// kick-offs in it are a day apart, so the whole weekend is shut and the first gap is the one
// after the last match.
func jornada() []Fixture {
	return []Fixture{
		{Kickoff: "2026-09-04T21:00:00+02:00"},
		{Kickoff: "2026-09-05T16:15:00+02:00"},
		{Kickoff: "2026-09-05T18:30:00+02:00"},
		{Kickoff: "2026-09-05T21:00:00+02:00"},
		{Kickoff: "2026-09-06T16:15:00+02:00"},
		{Kickoff: "2026-09-06T18:30:00+02:00"},
		{Kickoff: "2026-09-06T21:00:00+02:00"},
		{Kickoff: "2026-09-07T19:00:00+02:00"},
		{Kickoff: "2026-09-07T21:30:00+02:00"},
		{Kickoff: "2026-09-11T21:00:00+02:00"},
	}
}

func at(t *testing.T, stamp string) time.Time {
	t.Helper()
	when, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	return when
}

func TestClauseWindowShutsADayBeforeTheMatchday(t *testing.T) {
	window := Clauses(jornada(), at(t, "2026-09-04T19:39:00+02:00"))
	if window.Open {
		t.Fatal("con partido en menos de un dia la ventana esta cerrada")
	}
	if window.Blocking != "2026-09-04T21:00:00+02:00" {
		t.Errorf("blocking = %q, want el partido que la cierra", window.Blocking)
	}
	// Not when the last match ends: from its kick-off on, no new fixture starts within a day.
	if window.OpensAt != "2026-09-07T21:30:00+02:00" {
		t.Errorf("opens_at = %q, want el ultimo partido de la jornada", window.OpensAt)
	}
}

func TestClauseWindowOpenBetweenMatchdays(t *testing.T) {
	window := Clauses(jornada(), at(t, "2026-09-08T10:00:00+02:00"))
	if !window.Open {
		t.Fatal("entre jornadas se puede pagar")
	}
	// A day before the next kick-off, which is the hour a raid has to beat.
	if window.ClosesAt != "2026-09-10T21:00:00+02:00" {
		t.Errorf("closes_at = %q, want un dia antes del siguiente partido", window.ClosesAt)
	}
}

// Inside the matchday the calendar can no longer see the gap, so the answer is "shut" with no
// hour. Inventing one would be worse: a raid would stand down until a moment that is not real.
func TestClauseWindowSaysNothingItCannotKnow(t *testing.T) {
	window := Clauses([]Fixture{{Kickoff: "2026-09-04T21:00:00+02:00"}},
		at(t, "2026-09-04T19:39:00+02:00"))
	if window.Open || window.OpensAt != "" {
		t.Errorf("sin calendario mas alla no hay hora que dar: %+v", window)
	}
}

func TestClauseWindowWithNoCalendarDoesNotRefuse(t *testing.T) {
	if window := Clauses(nil, time.Now()); !window.Open {
		t.Error("sin fixtures no hay motivo para cerrar nada")
	}
}
