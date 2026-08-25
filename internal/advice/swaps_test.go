package advice

import (
	"strings"
	"testing"
)

// Rows as thin as the code reads them: a squad player, and somebody for sale.
func mine(id string, position int, xpts float64) Row {
	return Row{"id": id, "name": "mio-" + id, "is_mine": true,
		"position_id": position, "xpts": xpts, "value": 10_000_000.0}
}

func forSale(id string, position int, xpts, cost float64) Row {
	return Row{"id": id, "name": "venta-" + id, "position_id": position, "xpts": xpts,
		"value": cost, "entry_cost": cost, "ideal_bid": cost, "cost": cost}
}

func squadOf(positions ...int) []Row {
	var squad []Row
	for index, position := range positions {
		squad = append(squad, mine(string(rune('a'+index)), position, 2))
	}
	return squad
}

// Ten players cannot field anybody, and every move the plan knew how to make needed a player
// going out. So it proposed upgrades to a team of ten, or nothing at all, and never the one
// thing that fixes it.
func TestAHoleInTheElevenIsSignedNotSwapped(t *testing.T) {
	// 1-4-3-2: a defender, a midfielder or a striker each complete a different formation.
	squad := squadOf(1, 2, 2, 2, 2, 3, 3, 3, 4, 4)
	buckets := Row{"squad": squad, "squad_ppm_benchmark": 5.0,
		"asks": []Row{
			forSale("cheap-striker", 4, 4, 2_000_000),
			forSale("dear-defender", 2, 5, 30_000_000),
		}}

	plan := Swaps(Row{}, buckets, 40_000_000)
	moves := rowsOf(plan["moves"])
	if len(moves) == 0 {
		t.Fatal("no move: the plan stayed quiet with ten players")
	}
	first := moves[0]
	if first["out"] != nil {
		t.Errorf("a signing has nobody leaving, got out = %v", first["out"])
	}
	// Points per million decides, and the squad's own rate does not apply: an empty slot
	// scores nothing, so 4 xPts for 2M wins over 5 xPts for 30M.
	if got := text(mapOf(first["in"])["id"]); got != "cheap-striker" {
		t.Errorf("signed %q, want cheap-striker", got)
	}
	if gain := number(first["gain"]); gain != 4 {
		t.Errorf("gain = %v, want the whole 4 xPts of an empty slot", gain)
	}
	if starters := number(plan["starters"]); starters != 10 {
		t.Errorf("starters = %v, want 10", starters)
	}
}

func TestAHoleWithNothingAffordableSaysSo(t *testing.T) {
	squad := squadOf(1, 2, 2, 2, 2, 3, 3, 3, 4, 4)
	buckets := Row{"squad": squad,
		"asks": []Row{forSale("dear", 4, 5, 30_000_000)}}

	plan := Swaps(Row{}, buckets, 1_000_000)
	if moves := rowsOf(plan["moves"]); len(moves) != 0 {
		t.Fatalf("%d moves, want none: nothing is affordable", len(moves))
	}
	if !hasWarning(plan, "no puedes alinear once") {
		t.Errorf("warnings = %v, want one about not fielding eleven", plan["warnings"])
	}
}

// The squad this was found with: eleven players that only fit one formation. Nobody is spare,
// and the plan has to say it once rather than four times.
func TestExactlyElevenSaysNobodyIsSpare(t *testing.T) {
	squad := squadOf(1, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4)
	plan := Swaps(Row{}, Row{"squad": squad}, 0)
	if got := text(plan["shape"]); got != "5-3-2" {
		t.Errorf("shape = %q, want 5-3-2", got)
	}
	if starters := number(plan["starters"]); starters != 11 {
		t.Errorf("starters = %v, want 11", starters)
	}
	if !hasWarning(plan, "el once justo") {
		t.Fatalf("warnings = %v, want the one about having no spare", plan["warnings"])
	}
	warnings := plan["warnings"].([]string)
	if len(warnings) != 1 {
		t.Errorf("%d warnings, want one: four positions saying the same thing is noise",
			len(warnings))
	}
}

// A swap out of a position with nobody spare has an order, and it is not the obvious one.
func TestASwapWithNoSpareSaysSignFirst(t *testing.T) {
	squad := squadOf(1, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4)
	buckets := Row{"squad": squad, "squad_ppm_benchmark": 0.0,
		"asks": []Row{forSale("better", 4, 6, 5_000_000)}}

	plan := Swaps(Row{}, buckets, 50_000_000)
	moves := rowsOf(plan["moves"])
	if len(moves) == 0 {
		t.Fatal("no move: the upgrade was there to be made")
	}
	if order := text(moves[0]["order"]); order == "" {
		t.Error("no order note on a swap that leaves the eleven short in between")
	}
}

// The number in the header claimed an eleven that could not take the field: eleven players
// with no striker fit no formation, and the top eleven scorers were added up anyway.
func TestBestElevenRefusesToInventALineup(t *testing.T) {
	strikerless := squadOf(1, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3)
	points, shape, starters := bestEleven(strikerless)
	if shape != "" {
		t.Errorf("shape = %q, want none: no formation fields this squad", shape)
	}
	if starters >= 11 {
		t.Errorf("starters = %d, want fewer than eleven", starters)
	}
	if points == 0 {
		t.Error("points = 0, want what the squad can actually put out")
	}
}

func hasWarning(plan Row, fragment string) bool {
	for _, warning := range plan["warnings"].([]string) {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

// A bid you have placed is money you cannot spend twice. The plan always knew that and this
// bucket did not, so the same page called a signing affordable in one section and out of
// reach in the other.
func TestSpendingPowerDiscountsLiveBids(t *testing.T) {
	universe := Row{"players": []Row{
		merge(mine("a", 2, 3), Row{}),
		// On the free market for 5M, and nothing else stands between you and him.
		merge(forSale("target", 3, 4, 5_000_000), Row{"available": true, "owner": "",
			"market": Row{"kind": "libre", "min_bid": 5_000_000.0}}),
		// A bid of yours already out there for 8M.
		merge(forSale("bidding", 4, 4, 8_000_000), Row{"available": true, "owner": "",
			"market": Row{"kind": "libre", "min_bid": 8_000_000.0,
				"my_bid_id": "b1", "my_bid": 8_000_000.0}}),
	}}

	buckets := Recommend(universe, 10_000_000, 0, 15)
	if committed := number(buckets["committed"]); committed != 8_000_000 {
		t.Fatalf("committed = %v, want the 8M already bid", committed)
	}
	if power := number(buckets["spending_power"]); power != 2_000_000 {
		t.Fatalf("spending_power = %v, want 2M of the 10M", power)
	}
	for _, row := range rowsOf(buckets["bids_now"]) {
		if text(row["id"]) == "target" && truthy(row["affordable"]) {
			t.Error("5M called affordable with 2M left after the live bid")
		}
	}
}
