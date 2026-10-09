package render

import (
	"strings"
	"testing"
)

// A rival section that renders to nothing looks exactly like a rival with no players, which is
// why the missing table spec went out unnoticed. These assert the shape, not the wording.
func universeWithRivals() Document {
	players := []any{
		map[string]any{"id": "1", "name": "Mi medio", "position": "MED", "position_id": 3.0,
			"is_mine": true, "owner_team_id": "100", "xpts": 3.0, "value": 10_000_000.0},
		map[string]any{"id": "2", "name": "Su medio bueno", "position": "MED", "position_id": 3.0,
			"owner_team_id": "200", "owner": "cristian", "xpts": 4.5, "value": 20_000_000.0,
			"clause": 30_000_000.0, "clause_locked": false},
		map[string]any{"id": "3", "name": "Su medio malo", "position": "MED", "position_id": 3.0,
			"owner_team_id": "200", "owner": "cristian", "xpts": 1.0, "value": 5_000_000.0,
			"clause": 8_000_000.0, "clause_locked": true,
			"clause_locked_until": "2026-08-25T00:00:00Z"},
		map[string]any{"id": "4", "name": "Su portero", "position": "POR", "position_id": 1.0,
			"owner_team_id": "300", "owner": "tete", "xpts": 2.0, "value": 7_000_000.0,
			"market": map[string]any{"min_bid": 6_500_000.0}},
		map[string]any{"id": "5", "name": "Libre", "position": "DEL", "position_id": 4.0,
			"xpts": 9.0, "value": 1_000_000.0},
	}
	return Document{Universe: map[string]any{
		"my_team_id": "100",
		"players":    players,
		"league_teams": map[string]any{
			"100": map[string]any{"team_id": "100", "manager": "yo", "position": 1.0,
				"points": 20.0, "estimated_cash": 5_000_000.0},
			"200": map[string]any{"team_id": "200", "manager": "cristian", "position": 3.0,
				"points": 15.0, "estimated_cash": 2_000_000.0},
			"300": map[string]any{"team_id": "300", "manager": "tete", "position": 2.0,
				"points": 18.0, "estimated_cash": 40_000_000.0},
		},
	}}
}

func TestRivalSectionsOnePerRival(t *testing.T) {
	document := universeWithRivals()
	shells, views := document.rivalViews(rows(document.Universe["players"]))
	// The picker first, then one section per rival behind it.
	if len(shells) != 3 || len(views) != 3 {
		t.Fatalf("selector y dos rivales, salieron %d secciones", len(shells))
	}
	if shells[0].ID != "rivalpick" {
		t.Fatalf("la primera tiene que ser el desplegable: %+v", shells[0])
	}
	// Ordered by league position, so second in the table comes first.
	if shells[1].ID != "rival-300" {
		t.Errorf("el segundo de la liga deberia ir primero: %+v", shells[1])
	}
	picks := views["rivalpick"].(View).Main[0].Data.([]RivalPick)
	if picks[0].Value != "rival-300" || picks[1].Value != "rival-200" {
		t.Error("las opciones tienen que ir en orden de clasificacion")
	}
	for _, section := range shells[1:] {
		if section.Tab != "rivales" {
			t.Error("una seccion que no dice su pestaña queda invisible")
		}
	}
	for name, view := range views {
		if name != "rivalpick" && len(view.(View).Main[0].Data.(DataTable).Rows) == 0 {
			t.Errorf("seccion sin tabla, que es todo su contenido: %s", name)
		}
	}
}

func TestRivalSectionsExcludeMineAndFreeAgents(t *testing.T) {
	document := universeWithRivals()
	_, views := document.rivalViews(rows(document.Universe["players"]))
	joined := asJSON(views)
	for _, absent := range []string{`"id":"1"`, `"id":"5"`, `rival-100`} {
		if strings.Contains(joined, absent) {
			t.Errorf("%s no es de un rival y no deberia tener fila", absent)
		}
	}
	for _, present := range []string{`"id":"2"`, `"id":"3"`, `"id":"4"`} {
		if !strings.Contains(joined, present) {
			t.Errorf("falta la fila %s", present)
		}
	}
}

func TestRivalSectionsSayWhoBeatsMine(t *testing.T) {
	document := universeWithRivals()
	_, views := document.rivalViews(rows(document.Universe["players"]))
	view, ok := views["rival-200"]
	if !ok {
		t.Fatal("sin seccion de cristian")
	}
	cristian := asJSON(view)
	// 4.5 contra mi 3.0 es un jugador mejor, y 1.0 no: uno solo.
	if !strings.Contains(cristian, "1 mejora a los tuyos") {
		t.Errorf("la nota tiene que contar los que te mejoran: %.300s", cristian)
	}
	if !strings.Contains(cristian, "+1,5") {
		t.Error("falta la diferencia de xPts contra el tuyo de esa posicion")
	}
	// Su medio malo tiene la clausula bloqueada y el bueno no.
	if !strings.Contains(cristian, "pagable ya") {
		t.Error("una clausula libre tiene que decir que es pagable")
	}
	if !strings.Contains(cristian, "1 pagable ya") {
		t.Errorf("la nota tiene que contar las clausulas pagables: %.300s", cristian)
	}
}

func TestRivalSectionsWithoutLeagueTeams(t *testing.T) {
	document := Document{Universe: map[string]any{"players": []any{}}}
	if shells, _ := document.rivalViews(nil); shells != nil {
		t.Errorf("sin liga no hay rivales: %v", shells)
	}
}
