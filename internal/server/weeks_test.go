package server

import "testing"

func week(number, points, minutes float64) map[string]any {
	return map[string]any{"weekNumber": number, "totalPoints": points,
		"stats": map[string]any{"mins_played": []any{minutes, 0.0}}}
}

// Unai López as the feed published him on matchday six: J1 to J5 once each, and J6 four times,
// the 61 minutes he played followed by three rows of nothing. The strip drew nine matchdays.
func TestOneRowPerWeekKeepsTheMatchHePlayed(t *testing.T) {
	got := oneRowPerWeek([]map[string]any{
		week(1, 4, 90), week(2, 8, 90), week(3, 6, 78), week(4, 12, 90), week(5, 1, 75),
		week(6, 4, 61), week(6, 0, 0), week(6, 0, 0), week(6, 0, 0),
	})
	if len(got) != 6 {
		t.Fatalf("six matchdays have been played, not %d", len(got))
	}
	if points := number(got[5]["totalPoints"]); points != 4 {
		t.Errorf("J6 is the 61 minutes, so 4 points and not %.0f", points)
	}
}

// A matchday on the bench is a real row of zeros, so the copies cannot be told apart by points.
func TestOneRowPerWeekKeepsAMatchdayHeSatOut(t *testing.T) {
	got := oneRowPerWeek([]map[string]any{week(3, 0, 0), week(4, 7, 90)})
	if len(got) != 2 {
		t.Fatalf("a zero is a matchday too: %v", got)
	}
}

// The feed does not promise an order: Espart came back as J2, J1, J3.
func TestOneRowPerWeekReadsLeftToRight(t *testing.T) {
	got := oneRowPerWeek([]map[string]any{week(2, 14, 90), week(1, 7, 90), week(3, 8, 90)})
	for at, want := range []float64{1, 2, 3} {
		if number(got[at]["weekNumber"]) != want {
			t.Fatalf("the strip is the season in order: %v", got)
		}
	}
}
