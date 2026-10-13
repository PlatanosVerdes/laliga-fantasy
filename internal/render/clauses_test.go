package render

import (
	"testing"
	"time"
)

// A host in UTC must not fold a Madrid 00:30 into the day before, under the later day's label.
func TestCalendarGroupsByTheStampsOwnDay(t *testing.T) {
	saved := time.Local
	time.Local = time.UTC
	defer func() { time.Local = saved }()
	madrid := time.FixedZone("", 2*3600)
	day := time.Now().In(madrid).AddDate(0, 0, 3)
	late := time.Date(day.Year(), day.Month(), day.Day(), 23, 0, 0, 0, madrid)
	early := late.Add(90 * time.Minute)
	document := Document{Universe: map[string]any{"players": []any{
		map[string]any{"id": "a", "owner": "x", "is_mine": true, "clause_locked": true,
			"clause_locked_until": late.Format(time.RFC3339)},
		map[string]any{"id": "b", "owner": "x", "is_mine": true, "clause_locked": true,
			"clause_locked_until": early.Format(time.RFC3339)},
	}}}
	days := document.calendarBlock().Data.([]CalendarDay)
	if len(days) != 2 {
		t.Fatalf("days = %+v, want two", days)
	}
	for index, want := range []time.Time{late, early} {
		if label := esDay(want.Format(time.RFC3339)); days[index].Label != label ||
			days[index].Mine[0].ID != []string{"a", "b"}[index] {
			t.Errorf("day %d = %+v, want %s", index, days[index], label)
		}
	}
}
