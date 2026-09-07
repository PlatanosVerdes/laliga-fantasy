package advice

import (
	"testing"
)

// A squad shaped so the replacement question has both answers in it: six defenders and a
// seventh nearly as good on the bench, so losing one of them is free, and one good midfielder
// whose clause sits at his value, so losing him is not.
func defenceWorld(cash float64) Row {
	player := func(id, name string, position int, value, xpts, clause float64) Row {
		return Row{"id": id, "name": name, "position_id": float64(position),
			"value": value, "xpts": xpts, "clause": clause, "is_mine": true,
			"owner_team_id": "mine", "score": 1.0}
	}
	rival := func(id string, position int, value, xpts float64, team string) Row {
		return Row{"id": id, "name": "rival" + id, "position_id": float64(position),
			"value": value, "xpts": xpts, "owner": "them", "owner_team_id": team,
			"score": 1.0}
	}
	return Row{
		"my_team_id": "mine",
		"league_teams": Row{
			"mine": Row{"team_id": "mine", "manager": "yo", "estimated_cash": cash},
			// Rich enough to pay anything below, and his squad returns very little per
			// million, so almost anything is an upgrade for him.
			"rich": Row{"team_id": "rich", "manager": "Villaone",
				"estimated_cash": 40_000_000.0},
			"poor": Row{"team_id": "poor", "manager": "pelado", "estimated_cash": 100_000.0},
		},
		"players": []any{
			player("1", "portero", 1, 30_000_000, 7.0, 31_000_000),
			player("2", "central1", 2, 10_000_000, 4.0, 10_500_000),
			player("3", "central2", 2, 9_000_000, 3.8, 9_500_000),
			player("4", "central3", 2, 8_000_000, 3.6, 8_500_000),
			player("5", "central4", 2, 7_000_000, 3.4, 7_500_000),
			player("6", "central5", 2, 6_000_000, 3.2, 6_500_000),
			// Cheap clause on a good player: the one worth defending.
			player("7", "medio1", 3, 20_000_000, 6.0, 20_000_000),
			player("8", "medio2", 3, 5_000_000, 2.0, 12_000_000),
			player("9", "medio3", 3, 4_000_000, 1.8, 11_000_000),
			player("10", "punta1", 4, 15_000_000, 5.0, 30_000_000),
			player("11", "punta2", 4, 3_000_000, 1.0, 20_000_000),
			// A real alternative on the bench: what makes losing the fifth defender free.
			player("12", "suplente", 2, 6_000_000, 3.1, 9_000_000),
			// The rivals' own squads, which is the bar their raids are measured against.
			rival("r1", 2, 20_000_000, 2.0, "rich"),
			rival("r2", 3, 20_000_000, 2.0, "rich"),
			rival("r3", 2, 10_000_000, 3.0, "poor"),
		},
	}
}

// The recommendation has to be affordable and worth it, in that order, and the reason has to be
// on the row: "sube la clausula" with no number behind it is an order, not advice.
func TestClausePlanDefendsOnlyWhatLosingWouldHurt(t *testing.T) {
	plan := ClausePlan(defenceWorld(20_000_000), 20_000_000)
	byName := map[string]Row{}
	for _, row := range rowsOf(plan["rows"]) {
		byName[text(row["name"])] = row
	}

	// One defender out of seven: the bench replaces him with an equal, so no amount of money
	// should be recommended to keep him.
	if got := text(byName["central5"]["verdict"]); got != "dejalo ir" && got != "tranquilo" {
		t.Errorf("el quinto central no se defiende, dijo %q (%s)", got,
			text(byName["central5"]["why"]))
	}
	if drop := number(byName["central5"]["xi_drop"]); drop >= DropWorthDefending {
		t.Errorf("entra el suplente y casi no se nota, dijo %v", drop)
	}

	// The good midfielder at 1.00x with a rich rival who gains by paying it: that is the row
	// the section exists for.
	medio := byName["medio1"]
	if text(medio["verdict"]) != "sube" {
		t.Fatalf("medio1 es el que hay que defender, dijo %q (%s)", text(medio["verdict"]),
			text(medio["why"]))
	}
	if number(medio["pay"]) <= 0 || number(medio["target_clause"]) <= number(medio["clause"]) {
		t.Errorf("la subida tiene que costar algo y dejar la clausula mas alta: %v", medio)
	}
	if !truthy(medio["in_plan"]) {
		t.Error("cabe en la caja, asi que entra en el plan")
	}
	if number(medio["risk"]) <= 0 {
		t.Error("con un rival que gana pagandola el riesgo no es cero")
	}
}

// Five recommendations that add up to more than the balance are not a plan. The ones that do not
// fit have to say so instead of being quietly counted.
func TestClausePlanStopsAtTheBalance(t *testing.T) {
	plan := ClausePlan(defenceWorld(500_000), 500_000)
	spend, left := number(plan["spend"]), number(plan["cash_left"])
	if spend > 500_000 {
		t.Errorf("el plan gasta %v con 500K en caja", spend)
	}
	if left < 0 {
		t.Errorf("la caja no puede quedar negativa: %v", left)
	}
	for _, row := range rowsOf(plan["rows"]) {
		if truthy(row["in_plan"]) && number(row["pay"]) > 500_000 {
			t.Errorf("%s no cabe y esta en el plan", text(row["name"]))
		}
	}
}

// Raising past what the richest rival can pay buys nothing: those euros defend against nobody.
func TestRaiseTargetStopsAtWhatAnybodyCanPay(t *testing.T) {
	player := Row{"clause": 5_000_000.0, "xpts": 6.0, "value": 5_000_000.0}
	threats := []Threat{
		// His own squad returns almost nothing, so no clause makes him indifferent.
		{Manager: "rico", TeamID: "rich", Cash: 9_000_000, PPM: 1.2, Bar: 0.01, Worth: true},
	}
	target := RaiseTarget(player, threats)
	if target > 9_000_000*RaiseHeadroom+1 {
		t.Errorf("target = %v, no tiene sentido pasar de lo que puede pagar (9M)", target)
	}
	if target <= 5_000_000 {
		t.Errorf("target = %v, tiene que subir de la clausula actual", target)
	}
}
