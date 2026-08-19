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
