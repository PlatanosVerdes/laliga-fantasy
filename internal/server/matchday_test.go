package server

import "testing"

func TestPadLinesOpensTheMissingSlot(t *testing.T) {
	lines := map[string][]map[string]any{
		"goalkeeper": {{"id": "1"}},
		"defender":   {{"id": "2"}, {"id": "3"}, {"id": "4"}, {"id": "5"}},
		"midfield":   {{"id": "6"}, {"id": "7"}, {"id": "8"}},
		"striker":    {{"id": "9"}, {"id": "10"}},
	}
	padLines(lines, []int{4, 4, 2})
	if len(lines["midfield"]) != 4 {
		t.Fatalf("un 4-4-2 con tres medios sigue pidiendo cuatro plazas: %d",
			len(lines["midfield"]))
	}
	if lines["midfield"][3] != nil {
		t.Errorf("la plaza que falta tiene que llegar vacia: %v", lines["midfield"][3])
	}
	if len(lines["defender"]) != 4 || len(lines["striker"]) != 2 {
		t.Errorf("las lineas completas no se tocan: %v", lines)
	}
}

func TestPadLinesAlwaysKeepsAKeeper(t *testing.T) {
	lines := map[string][]map[string]any{}
	padLines(lines, nil)
	if len(lines["goalkeeper"]) != 1 || lines["goalkeeper"][0] != nil {
		t.Errorf("sin formacion conocida, el portero sigue siendo una plaza: %v", lines)
	}
}

// J2 as it finished: tete alejo 60, cristian1206 71, La rataneta 41. A matchday is read as a
// result, so the place is what orders the panel.
func TestRankIsTheMatchdaysResult(t *testing.T) {
	managers := []map[string]any{
		{"manager": "tete alejo", "week_points": 60.0},
		{"manager": "cristian1206", "week_points": 71.0},
		{"manager": "La rataneta", "week_points": 41.0},
	}
	rank(managers)
	want := map[string]int{"cristian1206": 1, "tete alejo": 2, "La rataneta": 3}
	for _, manager := range managers {
		if got := manager["week_rank"]; got != want[text(manager["manager"])] {
			t.Errorf("%s finished %vº, not %v", manager["manager"],
				want[text(manager["manager"])], got)
		}
	}
}

// Level on points is level, and the place after them skips the one they share.
func TestRankSharesTheLevelPlace(t *testing.T) {
	managers := []map[string]any{
		{"manager": "a", "week_points": 33.0},
		{"manager": "b", "week_points": 33.0},
		{"manager": "c", "week_points": 30.0},
		{"manager": "sin alineacion"},
	}
	rank(managers)
	if managers[0]["week_rank"] != 1 || managers[1]["week_rank"] != 1 {
		t.Errorf("the same points is the same place: %v, %v",
			managers[0]["week_rank"], managers[1]["week_rank"])
	}
	if managers[2]["week_rank"] != 3 {
		t.Errorf("after two firsts comes third, not %v", managers[2]["week_rank"])
	}
	if _, ranked := managers[3]["week_rank"]; ranked {
		t.Error("a matchday with no lineup to read has no place")
	}
}

// Dmitrovic on matchday five, as the feed publishes him: 47 points of season and 13 of that
// matchday. The shirt used to wear the 47, in an eleven that made 57 between the lot of them.
func TestScoredInIsThatMatchdayAndNotTheSeason(t *testing.T) {
	master := map[string]any{"points": 47.0, "lastStats": []map[string]any{
		week(1, 11, 90), week(2, 5, 90), week(3, 7, 90), week(4, 8, 90), week(5, 13, 90),
		// The live matchday comes back four times, once played and three times empty.
		week(6, 3, 90), week(6, 0, 0), week(6, 0, 0), week(6, 0, 0),
	}}
	for _, one := range []struct {
		week int
		want float64
	}{{5, 13}, {6, 3}, {1, 11}, {7, 0}} {
		if got := scoredIn(master, one.week); got != one.want {
			t.Errorf("J%d: %.0f points, want %.0f", one.week, got, one.want)
		}
	}
}
