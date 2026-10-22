package model

import (
	"testing"
	"time"
)

var whistle = time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC)

func kickedOff(ago time.Duration) Fixture {
	return Fixture{Kickoff: whistle.Add(-ago).Format(time.RFC3339)}
}

func TestAbsencesKeepThreeHoursWithNoMatchNear(t *testing.T) {
	fixtures := []Fixture{kickedOff(-24 * time.Hour), kickedOff(48 * time.Hour), {Kickoff: ""}}
	if got := AbsencesTTLAt(fixtures, whistle); got != AbsencesTTL {
		t.Errorf("got %v", got)
	}
	if got := AbsencesTTLAt(nil, whistle); got != AbsencesTTL {
		t.Errorf("no fixtures: got %v", got)
	}
}

func TestAbsencesKeepFifteenMinutesHalfAnHourAfterAMatch(t *testing.T) {
	fixtures := []Fixture{kickedOff(MatchLength + 30*time.Minute)}
	if got := AbsencesTTLAt(fixtures, whistle); got != AfterMatchAbsencesTTL {
		t.Errorf("got %v", got)
	}
}

func TestAbsencesGoBackToThreeHoursPastTheWindow(t *testing.T) {
	fixtures := []Fixture{kickedOff(MatchLength + 2*time.Hour + 5*time.Minute)}
	if got := AbsencesTTLAt(fixtures, whistle); got != AbsencesTTL {
		t.Errorf("got %v", got)
	}
}

func TestAMatchUnderWayAlreadyShortensTheAbsences(t *testing.T) {
	fixtures := []Fixture{kickedOff(40 * time.Minute)}
	if got := AbsencesTTLAt(fixtures, whistle); got != AfterMatchAbsencesTTL {
		t.Errorf("got %v", got)
	}
}

func TestSinceLastMatchIsTheMostRecentFinalWhistle(t *testing.T) {
	fixtures := []Fixture{kickedOff(MatchLength + 3*time.Hour),
		kickedOff(MatchLength + 20*time.Minute), kickedOff(10 * time.Minute)}
	got, ok := SinceLastMatch(fixtures, whistle)
	if !ok || got != 20*time.Minute {
		t.Errorf("got %v %v", got, ok)
	}
	if _, ok := SinceLastMatch([]Fixture{kickedOff(10 * time.Minute)}, whistle); ok {
		t.Error("a match under way has not ended")
	}
}

func TestTheFirstReadOfAbsencesIsNotAChange(t *testing.T) {
	var watch AbsenceWatch
	rows := []map[string]any{{"name": "Vivian", "kind": "lesionado"}}
	if _, _, changed := watch.Changes(rows); changed {
		t.Error("first read counted as a change")
	}
	if _, _, changed := watch.Changes(rows); changed {
		t.Error("same rows counted as a change")
	}
}

func TestAbsenceChangesCountWhoCameAndWent(t *testing.T) {
	var watch AbsenceWatch
	watch.Changes([]map[string]any{
		{"name": "Vivian", "kind": "lesionado", "until": "Octubre"},
		{"name": "Pedri", "kind": "lesionado"},
	})
	added, removed, changed := watch.Changes([]map[string]any{
		{"name": "Vivian", "kind": "lesionado", "until": "Noviembre"},
		{"name": "Bellingham", "kind": "sancionado"},
	})
	if !changed || added != 2 || removed != 2 {
		t.Errorf("added %d removed %d changed %v", added, removed, changed)
	}
}
