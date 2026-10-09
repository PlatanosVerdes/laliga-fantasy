package render

import (
	"strings"
	"testing"
)

// Every tab with a view of its own has a section that names it.
func TestEveryTabHasItsSection(t *testing.T) {
	seen := map[string]bool{}
	for _, section := range decidingDocument().Views() {
		seen[section.Tab] = true
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
	board := asJSON(document.boardBlock())
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
	html := asJSON(document.rankingView([]map[string]any{cheap}, []map[string]any{star, cheap}, nil))
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
	block := document.restBlock()
	if rowOf(block, "7") != nil || rowOf(block, "13") != nil {
		t.Error("the ones already on sale do not belong here")
	}
	bench, eleven := headAt(block, "Fuera de tu once"), headAt(block, "En tu once")
	if bench < 0 || eleven < 0 || bench > eleven {
		t.Fatalf("the bench goes before the eleven: %+v", block.Rows)
	}
	if indexOf(block, "9") < eleven {
		t.Error("the keeper starts, so he is listed with the eleven")
	}
	rey := rowOf(block, "8")
	if rey.Player["locked_until"] == nil || !rey.Acts[0].Off || rey.Acts[0].Do != "" {
		t.Errorf("a held player cannot be put on sale: %+v", rey)
	}
	keeper := rowOf(block, "9")
	if keeper.Acts[0].Args["op"] != "sell_to_market" || keeper.Acts[1].Do != "always" ||
		keeper.Acts[1].Label != "● Siempre en mercado" {
		t.Errorf("the keeper can be sold and is always listed: %+v", keeper.Acts)
	}
}

// Each Comprar box holds every candidate in a scroll box, best for the eleven first; what cash
// cannot reach stays at the end, greyed out with what is missing.
func TestBuyBoxesListEveryCandidate(t *testing.T) {
	document := decidingDocument()
	view := document.BuyData()
	if !view.Filters || view.Main[0].Scroll == 0 {
		t.Fatalf("every candidate in a scroll box under the filter bar: %+v", view.Main[0])
	}
	blob := asJSON(view)
	if strings.Index(blob, "Barato") > strings.Index(blob, "Caro") {
		t.Error("the one adding more goes first")
	}
	if !strings.Contains(blob, "+1,0M de margen") || !strings.Contains(blob, `"price":2000000`) {
		t.Error("margin to the ceiling and the price the filter reads")
	}
	if !strings.Contains(blob, `"wrap":"te faltan 20,0M"`) {
		t.Error("an unreachable bid is disabled and says what is missing")
	}
	document.MaxDebtPct = 30
	blob = asJSON(document.BuyData())
	if strings.Contains(blob, "te faltan") || !strings.Contains(blob, "⚠ en negativo") {
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
	blob := asJSON(document.plannedBlock())
	for _, want := range []string{`"do":"cancel_raid"`, `"t":"cancelada"`,
		"Historial de tus órdenes", "Fofana", "15,2M"} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// futbolfantasy's role rides next to the starting odds, and a starter the editors moved down is
// flagged in his row and in Decidir.
func TestRoleChipAndTheDropWarning(t *testing.T) {
	document := decidingDocument()
	squad := rows(document.Advice["squad"])
	squad[6]["role"] = map[string]any{"key": "rotacion", "label": "Rotación", "change": "down",
		"note": "Pierde el puesto ante el nuevo fichaje."}
	squad[6]["start_probability"] = 60.0
	document.Advice["squad"] = squad
	rest := asJSON(document.restBlock())
	if !strings.Contains(rest, `"key":"rotacion"`) || !strings.Contains(rest, `"change":"down"`) {
		t.Errorf("the chip and the warning in my rows: %.400s", rest)
	}
	if aside := asJSON(document.elevenBlock()); !strings.Contains(aside, "Portero bajó a Rotación en su equipo") {
		t.Errorf("a starter moved down is a Decidir note: %.400s", aside)
	}
}

// A rival's risk chip opens who of mine his cash reaches, best first, with the top threats
// marked; when it reaches nobody it says what the cheapest clause is.
func TestReachListsWhoARivalCanPay(t *testing.T) {
	squad := []map[string]any{
		{"id": "1", "name": "Barato", "clause": 5_000_000.0, "xpts": 2.0},
		{"id": "2", "name": "Bueno", "clause": 9_000_000.0, "xpts": 6.0, "clause_locked": true,
			"clause_locked_until": "2999-01-01T19:00:00+02:00"},
		{"id": "3", "name": "Caro", "clause": 50_000_000.0, "xpts": 8.0},
	}
	reach := reachOf("Villaone", 10_000_000, squad, map[string]string{"2": "Villaone"})
	html := asJSON(reach)
	if reach.Title != "Al alcance de Villaone" || strings.Contains(html, "Caro") {
		t.Fatalf("only who his cash reaches: %s", html)
	}
	if strings.Index(html, "Bueno") > strings.Index(html, "Barato") || !strings.Contains(html, "amenaza") {
		t.Error("best first, the top threat marked")
	}
	none := reachOf("Pobre", 1_000_000, squad, nil)
	if none.Empty != "No le llega a ninguno: tu cláusula más barata es 5,0M." {
		t.Errorf("nobody reached: %+v", none)
	}
}

// Scheduled clausulazos sit in "Mis pujas en curso" as rows with their limit and a way out, and
// the box only says it is empty when there is nothing at all.
func TestScheduledRaidsAreRowsOfMyBids(t *testing.T) {
	document := decidingDocument()
	document.Raids = []map[string]any{{"player_id": "8", "name": "O. Rey", "owner": "tete",
		"clause": 25_000_000.0, "max_pay": 20_000_000.0, "action": "esperando"}}
	box := document.BuyData().Aside[0]
	blob := asJSON(box)
	for _, want := range []string{"hasta 20,0M", "pasa tu límite", `"do":"cancel_raid"`} {
		if !strings.Contains(blob, want) {
			t.Errorf("missing %q in %s", want, blob)
		}
	}
	if len(box.Rows) == 0 {
		t.Error("with a raid scheduled the box is not empty, and the raid is a row")
	}
}
