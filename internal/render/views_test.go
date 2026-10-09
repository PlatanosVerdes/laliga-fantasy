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
	html := document.rankingView([]map[string]any{cheap}, []map[string]any{star, cheap}, nil)
	if strings.Index(html, "Los mejores") > strings.Index(html, "Chollos") {
		t.Error("los mejores van primero")
	}
	if !strings.Contains(html, "te faltan") || !strings.Contains(html, "te llega") {
		t.Errorf("el alcance frente a cada precio: %s", html)
	}
}

// Vender lists everyone not on sale, the bench before the eleven, with the hold rule disabling
// the sale and the standing listing shown as on.
func TestRestOfSquadPutsTheBenchFirstAndHonoursTheHoldRule(t *testing.T) {
	document := decidingDocument()
	squad := rows(document.Advice["squad"])
	squad[1]["sale_locked"], squad[1]["hold_until"] = true, "2999-01-04T10:00:00+02:00"
	document.Advice["squad"] = squad
	document.Plan = []map[string]any{{"player_id": "9", "name": "Portero", "action": "ninguna"}}
	html := document.restOfSquad()
	if strings.Contains(html, `data-pid="7"`) || strings.Contains(html, `data-pid="13"`) {
		t.Error("the ones already on sale do not belong here")
	}
	bench, eleven := strings.Index(html, "Fuera de tu once"), strings.Index(html, "En tu once")
	if bench < 0 || eleven < 0 || bench > eleven {
		t.Fatalf("the bench goes before the eleven: %.300s", html)
	}
	if strings.Index(html, `data-pid="9"`) < eleven {
		t.Error("the keeper starts, so he is listed with the eleven")
	}
	rey := html[strings.Index(html, `data-pid="8"`):]
	rey = rey[:strings.Index(rey, "</li>")]
	if !strings.Contains(rey, "🔒 hasta") || !strings.Contains(rey, "disabled") ||
		strings.Contains(rey, `data-act="sell_to_market"`) {
		t.Errorf("a held player cannot be put on sale: %s", rey)
	}
	keeper := html[strings.Index(html, `data-pid="9"`):]
	keeper = keeper[:strings.Index(keeper, "</li>")]
	for _, want := range []string{`data-act="sell_to_market"`, `data-act="always"`,
		"● Siempre en mercado", "en tu once"} {
		if !strings.Contains(keeper, want) {
			t.Errorf("missing %q: %s", want, keeper)
		}
	}
}

// Each Comprar box holds every candidate in a scroll box, best for the eleven first; what cash
// cannot reach stays at the end, greyed out with what is missing.
func TestBuyBoxesListEveryCandidate(t *testing.T) {
	document := decidingDocument()
	html := document.buyView()
	if strings.Contains(html, "<details") || !strings.Contains(html, `class="scrollbox"`) ||
		!strings.Contains(html, `class="mk-filters"`) {
		t.Fatalf("every candidate in a scroll box under the filter bar: %.300s", html)
	}
	if strings.Index(html, "Barato") > strings.Index(html, "Caro") {
		t.Error("the one adding more goes first")
	}
	if !strings.Contains(html, "+1,0M de margen") || !strings.Contains(html, `data-price="2000000"`) {
		t.Error("margin to the ceiling and the price the filter reads")
	}
	if !strings.Contains(html, `title="te faltan 20,0M"`) {
		t.Error("an unreachable bid is disabled and says what is missing")
	}
	document.MaxDebtPct = 30
	html = document.buyView()
	if strings.Contains(html, "te faltan") || !strings.Contains(html, "⚠ en negativo") {
		t.Error("the allowed debt reaches it, with the warning about starting in the red")
	}
}

// Scheduled raids that stood down say so and can be called off; the order log follows them.
func TestStoodDownRaidsCanBeCancelledAndOrdersAreLogged(t *testing.T) {
	document := decidingDocument()
	document.Raids = []map[string]any{{"player_id": "2", "name": "El que subio",
		"owner": "tete", "clause": 30_000_000.0, "max_pay": 17_000_000.0, "action": "cancelada"}}
	document.Orders = []map[string]any{{"at": "2026-09-14T19:02:00Z", "player_id": "8",
		"player": "Fofana", "outcome": "pagada", "amount": 15_240_000.0}}
	html := document.clauseView()
	for _, want := range []string{`data-op="cancel_raid"`, ">cancelada<",
		"Historial de tus órdenes", "Fofana", "15,2M"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// The tables that sat under "Ver detalle" are gone with their switch.
func TestNoTabFoldsItsOldTables(t *testing.T) {
	page := strings.Join(decidingDocument().Views(), "")
	for _, gone := range []string{"Ver detalle", "mk-more", "data-fold"} {
		if strings.Contains(page, gone) {
			t.Errorf("%q is still on the page", gone)
		}
	}
}
