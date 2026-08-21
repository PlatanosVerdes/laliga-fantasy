package main

import (
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

// What the standing instruction asks for has to reach the API as the API addresses it: by squad
// slot and with the price. An empty slot came back as 400 "Player not found" every cycle, and the
// amount travelled as zero because the plan writes int64 and the conversion only knew int.
func TestAutomaticListingCarriesTheSlotAndThePrice(t *testing.T) {
	rows := []map[string]any{{
		"id": "1300", "name": "Camavinga", "player_team_id": "25009250",
		"is_mine": true, "available": true, "value": 9_333_631.0, "position_id": 3.0,
	}}
	armed := map[string]policies.Policy{"1300": {AlwaysList: true}}

	plan := policies.Plan(rows, armed)
	if len(plan) != 1 || text(plan[0]["action"]) != "poner_en_venta" {
		t.Fatalf("el plan deberia ponerlo en venta, dice %v", plan)
	}

	args := automaticArgs(plan[0], "L", "T")
	if args.PlayerTeamID != "25009250" {
		t.Errorf("ficha %q, esperaba la de su plantilla", args.PlayerTeamID)
	}
	if args.Amount != 9_333_631 {
		t.Errorf("importe %d, esperaba 9.333.631", args.Amount)
	}

	call, err := writes.Build("sell_to_market", args)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if call.Body["playerId"] != "25009250" || call.Body["salePrice"] != int64(9_333_631) {
		t.Errorf("la peticion viaja como %v", call.Body)
	}
}

// The clause raid travels the same way, and its amount is written as int64 too.
func TestAutomaticRaidCarriesTheSlotAndTheAmount(t *testing.T) {
	clause := 1_000_000.0
	limit := 1_200_000.0
	rows := []map[string]any{{
		"id": "2568", "name": "Youssef", "player_team_id": "31000001",
		"owner": "otro", "clause": clause,
	}}
	armed := map[string]policies.Policy{"2568": {Raid: true, MaxPay: &limit}}

	plan := policies.RaidPlan(rows, armed, 50_000_000)
	if len(plan) != 1 || text(plan[0]["action"]) != "pagar_clausula" {
		t.Fatalf("el plan deberia pagar la clausula, dice %v", plan)
	}
	args := automaticArgs(plan[0], "L", "T")
	if args.PlayerTeamID != "31000001" || args.Amount != 1_000_000 {
		t.Errorf("ficha %q e importe %d", args.PlayerTeamID, args.Amount)
	}
}

// The manual path checks a sale against the league's hold rule; the unattended one has to check
// it against the same thing, or the instruction sells what nobody was allowed to sell.
func TestAutomaticPlayerCarriesTheHoldRule(t *testing.T) {
	row := map[string]any{"name": "Camavinga", "value": 9_333_631.0, "available": true,
		"sale_locked": true, "hold_until": "2026-08-25T00:00:00Z", "clause": 4_000_000.0}

	who := automaticPlayer(row, "lesiones")
	if !who.SaleLocked || who.HoldUntil == "" {
		t.Errorf("la norma de la liga no llega al guardian: %+v", who)
	}
	if who.Value != 9_333_631 || who.Clause != 4_000_000 || !who.Available {
		t.Errorf("faltan datos del jugador: %+v", who)
	}
	if who.HoldExceptions != "lesiones" {
		t.Errorf("excepciones %q", who.HoldExceptions)
	}
}
