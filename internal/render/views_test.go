package render

import (
	"strings"
	"testing"
)

// The live refresh swaps sections by id with a non-greedy match, so every view has to be one
// section with nothing nested, and each one names its tab.
func TestViewsAreFlatSectionsOfTheirTab(t *testing.T) {
	views := decidingDocument().Views()
	if len(views) < 2*len(ViewTabs) {
		t.Fatalf("a view and a 'Ver detalle' per tab, got %d", len(views))
	}
	for index, html := range views[:2*len(ViewTabs)] {
		tab := ViewTabs[index/2]
		if strings.Count(html, "<section") != 1 || strings.Count(html, "</section>") != 1 {
			t.Errorf("%s: one section, nothing nested: %.120s", tab, html)
		}
		if !strings.Contains(html, `data-tab="`+tab+`"`) {
			t.Errorf("%s: the view has to name its tab: %.120s", tab, html)
		}
	}
}

// A confirmed absence rings red with its badge, a doubt yellow.
func TestFacesWearTheStatusRing(t *testing.T) {
	out := face(map[string]any{"id": "1", "name": "Pedri", "status": "injured",
		"absence": map[string]any{"kind": "lesionado", "reason": "Rotura"}}, "sm")
	if !strings.Contains(out, "ring-out") || !strings.Contains(out, "hb-cross") ||
		!strings.Contains(out, `title="Rotura"`) {
		t.Errorf("lesionado: %s", out)
	}
	doubt := face(map[string]any{"id": "2", "name": "Gavi", "status": "doubtful"}, "sm")
	if !strings.Contains(doubt, "ring-doubt") || strings.Contains(doubt, "hb-") {
		t.Errorf("duda: %s", doubt)
	}
}

// The board answers who finishes where: ordered by the projected place, mine marked.
func TestMatchdayBoardOrdersByProjectedFinish(t *testing.T) {
	document := Document{Advice: map[string]any{"matchday": map[string]any{"week": 8.0,
		"live": true, "played": 2.0, "matches": 10.0, "managers": []any{
			map[string]any{"team_id": "a", "manager": "Ana", "points": 30.0, "points_rank": 1.0,
				"projection": 40.0, "projection_rank": 2.0, "to_come": 10.0, "waiting": 2.0},
			map[string]any{"team_id": "b", "manager": "Yo", "is_me": true, "points": 20.0,
				"points_rank": 2.0, "projection": 50.0, "projection_rank": 1.0, "to_come": 30.0,
				"waiting": 5.0, "waiting_names": []any{"Pedri"}},
		}}}}
	board := document.matchdayBoard()
	if strings.Index(board, "Yo") > strings.Index(board, "Ana") {
		t.Error("el que acabaría primero va primero")
	}
	if !strings.Contains(board, "accent me") || !strings.Contains(board, "▲ 1") ||
		!strings.Contains(board, "Pedri") {
		t.Errorf("mi fila marcada, la subida y quién me queda: %s", board)
	}
	if week, place := document.myFinish(); week != 8 || place != 1 {
		t.Errorf("mi puesto previsto: J%d %dº", week, place)
	}
}
