package server

import "testing"

func running(totals ...any) map[string]any {
	return map[string]any{"total": append([]any{}, totals...)}
}

// The league as it was after J1 and after J2: tete alejo led the first and was still leading
// the second, while cristian1206 went from sixth to second on 71 points that week.
func TestPlaceByWeekIsTheTableAfterEachMatchday(t *testing.T) {
	tete := running(61.0, 121.0)
	cristian := running(35.0, 106.0)
	lamine := running(43.0, 105.0)
	placeByWeek([]map[string]any{tete, cristian, lamine}, 2)

	for at, want := range [][]int{{1, 3, 2}, {1, 2, 3}} {
		for who, manager := range []map[string]any{tete, cristian, lamine} {
			if got := manager["place"].([]any)[at]; got != want[who] {
				t.Errorf("J%d, manager %d: %vº, want %dº", at+1, who, got, want[who])
			}
		}
	}
}

// Two on the same total are level, and the matchday nobody could be read for is a hole in the
// line and not a thirteenth place.
func TestPlaceByWeekSharesAndSkips(t *testing.T) {
	first := running(50.0, nil)
	level := running(50.0, nil)
	third := running(40.0, nil)
	placeByWeek([]map[string]any{first, level, third}, 2)

	if first["place"].([]any)[0] != 1 || level["place"].([]any)[0] != 1 {
		t.Error("the same total is the same place")
	}
	if third["place"].([]any)[0] != 3 {
		t.Errorf("after two firsts comes third, not %v", third["place"].([]any)[0])
	}
	if first["place"].([]any)[1] != nil {
		t.Errorf("a matchday with nothing to read has no place: %v", first["place"].([]any)[1])
	}
}
