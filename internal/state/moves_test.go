package state

import (
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

// The first pass must only learn: a restart that logged the whole season would bury the one line
// this exists for. After that, every new move is written exactly once.
func TestLogMovesLearnsFirstThenReportsOnce(t *testing.T) {
	world := &State{}
	name, buyer := "Lamine Yamal", "La rataneta"
	amount := 125_630_000.0
	first := &model.Universe{Activity: []model.Event{{
		Date: "2026-08-18T19:00:00", TypeID: 31, User1: "9", Kind: "compra",
		Player: &name, Buyer: &buyer, Amount: &amount,
	}}}

	world.logMoves(first)
	if len(world.seenMoves) != 1 {
		t.Fatalf("la primera pasada deberia aprender un movimiento, sabe %d", len(world.seenMoves))
	}

	// Same world again: nothing new.
	world.logMoves(first)
	if len(world.seenMoves) != 1 {
		t.Errorf("no deberia haber aprendido nada nuevo, sabe %d", len(world.seenMoves))
	}

	// A new move arrives.
	other, seller := "Courtois", "Villaone"
	paid := 66_230_000.0
	second := &model.Universe{Activity: append([]model.Event{{
		Date: "2026-08-19T12:00:00", TypeID: 1, User1: "7", User2: strptr("9"), Kind: "traspaso",
		Player: &other, Buyer: &seller, Amount: &paid,
	}}, first.Activity...)}
	world.logMoves(second)
	if len(world.seenMoves) != 2 {
		t.Errorf("deberia conocer los dos, conoce %d", len(world.seenMoves))
	}
}

// Two different events must never collapse into one key, or one of them is silently never logged.
func TestMoveKeyTellsEventsApart(t *testing.T) {
	one := 1_000_000.0
	two := 2_000_000.0
	player := "10"
	base := model.Event{Date: "2026-08-19T12:00:00", TypeID: 31, User1: "9",
		PlayerID: &player, Amount: &one}
	other := base
	other.Amount = &two
	if moveKey(base) == moveKey(other) {
		t.Error("dos importes distintos no pueden compartir clave")
	}
	same := base
	if moveKey(base) != moveKey(same) {
		t.Error("el mismo movimiento tiene que dar la misma clave")
	}
}

func strptr(value string) *string { return &value }
