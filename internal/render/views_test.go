package render

import (
	"strings"
	"testing"
)

// The live refresh swaps sections by id with a non-greedy match, so every view has to be one
// section with nothing nested, and each one names its tab.
func TestViewsAreFlatSectionsOfTheirTab(t *testing.T) {
	seen := map[string]bool{}
	for _, html := range filterEmpty(decidingDocument().Views()) {
		if strings.Count(html, "<section") != 1 || strings.Count(html, "</section>") != 1 {
			t.Errorf("one section, nothing nested: %.120s", html)
		}
		for _, tab := range ViewTabs {
			if strings.Contains(html, `data-tab="`+tab+`"`) {
				seen[tab] = true
			}
		}
	}
	for _, tab := range ViewTabs {
		if !seen[tab] {
			t.Errorf("%s has no view", tab)
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

// "Los mejores" is by xPts alone and says whether my reach gets to each one's price.
func TestRankingOpensWithTheBestAndTheirPrice(t *testing.T) {
	document := decidingDocument()
	star := map[string]any{"id": "99", "name": "Raphinha", "position": "DEL", "position_id": 4.0,
		"xpts": 9.5, "available": true, "owner": "Rival", "clause": 500_000_000.0}
	cheap := map[string]any{"id": "98", "name": "Barato", "position": "DEL", "position_id": 4.0,
		"xpts": 6.0, "available": true, "owner": "Rival", "clause": 1_000_000.0, "score": 9.0}
	html := document.rankingView([]map[string]any{cheap}, []map[string]any{star, cheap})
	if strings.Index(html, "Los mejores") > strings.Index(html, "Chollos") {
		t.Error("los mejores van primero")
	}
	if !strings.Contains(html, "te faltan") || !strings.Contains(html, "te llega") {
		t.Errorf("el alcance frente a cada precio: %s", html)
	}
}
