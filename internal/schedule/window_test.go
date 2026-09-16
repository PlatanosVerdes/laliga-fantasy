package schedule

import (
	"testing"
	"time"
)

// Matchday 4 as the calendar served it, Friday to Monday, plus the first fixture of matchday 5.
func jornada() []Fixture {
	return []Fixture{
		{Week: 4, Kickoff: "2026-09-04T21:00:00+02:00"},
		{Week: 4, Kickoff: "2026-09-05T16:15:00+02:00"},
		{Week: 4, Kickoff: "2026-09-05T18:30:00+02:00"},
		{Week: 4, Kickoff: "2026-09-05T21:00:00+02:00"},
		{Week: 4, Kickoff: "2026-09-06T16:15:00+02:00"},
		{Week: 4, Kickoff: "2026-09-06T18:30:00+02:00"},
		{Week: 4, Kickoff: "2026-09-06T21:00:00+02:00"},
		{Week: 4, Kickoff: "2026-09-07T19:00:00+02:00"},
		{Week: 4, Kickoff: "2026-09-07T21:30:00+02:00"},
		{Week: 5, Kickoff: "2026-09-11T21:00:00+02:00"},
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
		t.Fatal("con la jornada empezando en menos de un dia la ventana esta cerrada")
	}
	if window.Blocking != "2026-09-04T21:00:00+02:00" {
		t.Errorf("blocking = %q, want el primer partido de la jornada", window.Blocking)
	}
	// The game's own FAQ: it reopens when the matchday starts, not when it ends.
	if window.OpensAt != "2026-09-04T21:00:00+02:00" {
		t.Errorf("opens_at = %q, want el saque inicial de la jornada", window.OpensAt)
	}
}

// The one this got backwards. Saturday, matchday under way: clausulazos are on, and the panel
// spent every weekend saying they were not. Nothing else starts within the day -- the matches
// left are this same matchday's -- so the window stays open until the next one is a day out.
func TestClauseWindowIsOpenOnceTheMatchdayHasStarted(t *testing.T) {
	for _, when := range []string{
		"2026-09-05T12:00:00+02:00", // after Friday's kick-off, before Saturday's
		"2026-09-06T20:00:00+02:00", // between two of its own matches
		"2026-09-07T23:00:00+02:00", // the last one over
	} {
		window := Clauses(jornada(), at(t, when))
		if !window.Open {
			t.Errorf("%s: con la jornada en marcha se pagan clausulas: %+v", when, window)
		}
		if window.ClosesAt != "2026-09-10T21:00:00+02:00" {
			t.Errorf("%s: closes_at = %q, want un dia antes de la jornada siguiente",
				when, window.ClosesAt)
		}
	}
}

func TestClauseWindowOpenBetweenMatchdays(t *testing.T) {
	window := Clauses(jornada(), at(t, "2026-09-08T10:00:00+02:00"))
	if !window.Open {
		t.Fatal("entre jornadas se puede pagar")
	}
	// A day before the next matchday, which is the hour a raid has to beat.
	if window.ClosesAt != "2026-09-10T21:00:00+02:00" {
		t.Errorf("closes_at = %q, want un dia antes de la jornada siguiente", window.ClosesAt)
	}
}

// A fixture with no matchday on it counts as a matchday of its own: a window that shuts too
// often is a nuisance, one that promises a payment the game refuses is a failed clausulazo.
func TestClauseWindowWithoutMatchdaysIsCautious(t *testing.T) {
	window := Clauses([]Fixture{{Kickoff: "2026-09-04T21:00:00+02:00"},
		{Kickoff: "2026-09-05T16:15:00+02:00"}}, at(t, "2026-09-04T19:39:00+02:00"))
	if window.Open {
		t.Errorf("sin jornadas que agrupar, cada partido cierra la suya: %+v", window)
	}
	if window.OpensAt != "2026-09-04T21:00:00+02:00" {
		t.Errorf("opens_at = %q", window.OpensAt)
	}
}

func TestClauseWindowWithNoCalendarDoesNotRefuse(t *testing.T) {
	if window := Clauses(nil, time.Now()); !window.Open {
		t.Error("sin fixtures no hay motivo para cerrar nada")
	}
}
