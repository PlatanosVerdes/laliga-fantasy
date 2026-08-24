package policies

import (
	"errors"
	"testing"
)

// The enforcement is the only code here that spends money without a person confirming it, so what
// it refuses to do matters more than what it does.
func TestEnforceOnlyActsOnReadyActions(t *testing.T) {
	plan := []Row{
		{"action": "poner_en_venta", "name": "Camavinga", "amount": 10_000_000.0},
		{"action": "avisar", "name": "M. Dituro", "amount": 9_000_000.0},
		{"action": "ninguna", "name": "Yuri"},
		{"action": "aceptar_oferta", "name": "Marc Bernal", "amount": 8_200_000.0,
			"offer_id": "1", "market_id": "2"},
	}
	raids := []Row{
		{"action": "esperando", "name": "Iñigo Vicente"},
		{"action": "bloqueada", "name": "Courtois"},
		{"action": "sin_saldo", "name": "Fornals"},
		{"action": "cancelada", "name": "Barrios"},
		{"action": "pagar_clausula", "name": "Youssef", "amount": 1_000_000.0,
			"player_team_id": "9"},
	}

	var ran []string
	done := Enforce(plan, raids, func(operation string, action Row) error {
		ran = append(ran, operation+":"+text(action["name"]))
		return nil
	})

	want := []string{"sell_to_market:Camavinga", "accept_offer:Marc Bernal", "pay_clause:Youssef"}
	if len(ran) != len(want) {
		t.Fatalf("ejecutadas %v, esperaba %v", ran, want)
	}
	for index, operation := range want {
		if ran[index] != operation {
			t.Errorf("en la posicion %d ejecuto %q, esperaba %q", index, ran[index], operation)
		}
	}
	if len(done) != 3 {
		t.Errorf("devolvio %d acciones, esperaba 3", len(done))
	}
	for _, action := range done {
		if !truthy(action["ok"]) {
			t.Errorf("%s deberia constar como hecha", text(action["name"]))
		}
	}
}

// A cap bounds a bug: three operations is a busy morning, thirty is a runaway loop.
func TestEnforceStopsAtTheCap(t *testing.T) {
	plan := []Row{}
	for index := 0; index < PerCycle+4; index++ {
		plan = append(plan, Row{"action": "poner_en_venta", "name": "uno", "amount": 1_000.0})
	}
	count := 0
	Enforce(plan, nil, func(string, Row) error { count++; return nil })
	if count != PerCycle {
		t.Fatalf("ejecuto %d, el tope es %d", count, PerCycle)
	}
}

// A failure has to be recorded and must not stop the rest: an offer that expired mid-cycle should
// not keep a clause from being paid.
func TestEnforceRecordsFailuresAndCarriesOn(t *testing.T) {
	plan := []Row{
		{"action": "aceptar_oferta", "name": "se cayo", "amount": 1.0},
		{"action": "poner_en_venta", "name": "sigue", "amount": 2.0},
	}
	done := Enforce(plan, nil, func(operation string, action Row) error {
		if text(action["name"]) == "se cayo" {
			return errors.New("la oferta ya no existe")
		}
		return nil
	})
	if len(done) != 2 {
		t.Fatalf("devolvio %d acciones, esperaba 2", len(done))
	}
	if truthy(done[0]["ok"]) || text(done[0]["error"]) == "" {
		t.Error("la primera deberia constar como fallida y con motivo")
	}
	if !truthy(done[1]["ok"]) {
		t.Error("la segunda deberia haberse ejecutado igualmente")
	}
	if Describe(done[0]) == "" {
		t.Error("Describe deberia explicar el fallo")
	}
}

// A raid with no written cap must never pay: the page always demands an amount, a hand-edited file
// does not, and "pay whatever it costs" is not something anybody authorised.
func TestRaidPlanRefusesWithoutACap(t *testing.T) {
	players := []Row{{
		"id": "1", "name": "Youssef", "owner": "LamineTheTuareg", "clause": 1_000_000.0,
		"is_mine": false, "clause_locked": false,
	}}

	without := RaidPlan(players, map[string]Policy{"1": {Raid: true}}, 90_000_000)
	if len(without) != 1 || text(without[0]["action"]) != "sin_limite" {
		t.Fatalf("sin limite deberia negarse, dijo %v", without)
	}

	cap := 1_200_000.0
	with := RaidPlan(players, map[string]Policy{"1": {Raid: true, MaxPay: &cap}}, 90_000_000)
	if len(with) != 1 || text(with[0]["action"]) != "pagar_clausula" {
		t.Fatalf("con limite por encima deberia pagar, dijo %v", with)
	}
	if amount := number(with[0]["amount"]); amount != 1_000_000 {
		t.Errorf("pagaria %v, la clausula es 1.000.000", amount)
	}

	low := 900_000.0
	under := RaidPlan(players, map[string]Policy{"1": {Raid: true, MaxPay: &low}}, 90_000_000)
	if text(under[0]["action"]) != "cancelada" {
		t.Errorf("con la clausula por encima del limite deberia cancelarse, dijo %v",
			text(under[0]["action"]))
	}

	broke := RaidPlan(players, map[string]Policy{"1": {Raid: true, MaxPay: &cap}}, 500_000)
	if text(broke[0]["action"]) != "sin_saldo" {
		t.Errorf("sin saldo deberia decirlo, dijo %v", text(broke[0]["action"]))
	}
}

// Two clauses open at once and each one spends the same balance: the queue has to stop at the
// balance instead of saying yes to both and leaving it negative.
func TestRaidPlanSpendsTheBalanceOnceAcrossRaids(t *testing.T) {
	cap := 30_000_000.0
	players := []Row{
		{"id": "1", "name": "el caro", "owner": "rival", "clause": 20_000_000.0, "xpts": 40.0},
		{"id": "2", "name": "el barato", "owner": "rival", "clause": 15_000_000.0, "xpts": 90.0},
	}
	armed := map[string]Policy{"1": {Raid: true, MaxPay: &cap}, "2": {Raid: true, MaxPay: &cap}}

	plan := RaidPlan(players, armed, 30_000_000)
	if len(plan) != 2 {
		t.Fatalf("esperaba una fila por clausulazo, dijo %v", plan)
	}
	// El barato rinde mas por millon, asi que va primero y se lleva el saldo.
	if text(plan[0]["name"]) != "el barato" || text(plan[0]["action"]) != "pagar_clausula" {
		t.Errorf("primero deberia pagar al barato, dijo %v", plan[0])
	}
	if text(plan[1]["action"]) != "sin_saldo" {
		t.Errorf("el segundo ya no cabe y deberia decirlo, dijo %v", plan[1])
	}

	// Con saldo para los dos se pagan los dos, y el mejor por millon sigue yendo primero.
	both := RaidPlan(players, armed, 40_000_000)
	for index, row := range both {
		if text(row["action"]) != "pagar_clausula" {
			t.Fatalf("con saldo de sobra los dos deberian pagarse, el %d dijo %v", index, row)
		}
	}
	if text(both[0]["name"]) != "el barato" {
		t.Errorf("la cola va por puntos por millon, empezo por %q", text(both[0]["name"]))
	}
}
