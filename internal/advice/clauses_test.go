package advice

import "testing"

func team(id, manager string, cash float64) Row {
	return Row{"team_id": id, "manager": manager, "estimated_cash": cash}
}

// The threat count named the wrong person: with the biggest balance in the league you were
// counted among the rivals who could pay your own clause, and printed as the richest of them.
func TestClauseThreatsLeaveYouOut(t *testing.T) {
	teams := RivalCash(Row{"my_team_id": "mine", "league_teams": Row{
		"a": team("mine", "PlatanosVerdes", 100_000_000),
		"b": team("other", "Villaone", 55_000_000),
		"c": team("poor", "LILTEAM", 0),
	}}, 100_000_000)

	threats, margin := ClauseThreats(
		Row{"clause": 28_000_000.0, "value": 27_000_000.0, "xpts": 3.0}, teams, nil)
	if len(threats) != 1 {
		t.Fatalf("%d able to pay, want 1 (you are not a threat to yourself)", len(threats))
	}
	if got := text(TopThreat(threats)); got != "Villaone" {
		t.Errorf("top threat = %q, want Villaone", got)
	}
	if margin < 1.03 || margin > 1.04 {
		t.Errorf("margin = %.2f, want about 1.04", margin)
	}
}

// A clause nobody can pay, priced far above what the player is worth, is not a warning: raising
// it costs money and there is nothing to defend against.
func TestAClauseNobodyCanPayIsNotAWarning(t *testing.T) {
	universe := Row{
		"my_team_id": "mine",
		"league_teams": Row{"a": team("mine", "PlatanosVerdes", 100_000_000),
			"b": team("other", "Villaone", 5_000_000)},
		"players": []Row{},
		"clauses": Row{"mine_soon": []Row{
			// 2.60x his value and out of everybody's reach: nothing to do.
			{"id": "1", "name": "Tárrega", "clause": 23_815_596.0, "value": 9_175_591.0,
				"score": 1.7, "hours_left": 8.0},
			// Barely above his value, and the rival can pay it: this is the real one.
			{"id": "2", "name": "Javi Rodríguez", "clause": 4_671_794.0, "value": 4_237_650.0,
				"score": 1.3, "hours_left": 8.0},
		}},
	}

	buckets := Recommend(universe, 100_000_000, 0, 15)
	soon := rowsOf(buckets["my_clauses_soon"])
	if len(soon) != 1 {
		t.Fatalf("%d rows, want 1: the unpayable one should be gone", len(soon))
	}
	if got := text(soon[0]["name"]); got != "Javi Rodríguez" {
		t.Fatalf("kept %q, want Javi Rodríguez", got)
	}
	if threats := number(soon[0]["threats"]); threats != 1 {
		t.Errorf("threats = %v, want 1", threats)
	}
	if got := text(soon[0]["top_threat"]); got != "Villaone" {
		t.Errorf("top threat = %q, want Villaone", got)
	}
}

// Being able to pay is not wanting to. The one that costs you the player is the rival for whom
// the clause buys more points per million than his own squad already gives him.
func TestOnlySomeOfThemWouldActuallyPayIt(t *testing.T) {
	teams := RivalCash(Row{"my_team_id": "mine", "league_teams": Row{
		"a": team("mine", "PlatanosVerdes", 100_000_000),
		"b": team("rich", "Villaone", 55_000_000),
		"c": team("good", "TheMessias", 41_000_000),
	}}, 100_000_000)
	// Villaone's squad returns 0.10 pts per million, TheMessias's 0.40.
	rates := map[string]float64{"rich": 0.10, "good": 0.40}

	// 3 xPts for a 20M clause is 0.15 per million: a step up for Villaone, a step down for
	// TheMessias, even though both are holding more than enough cash.
	threats, _ := ClauseThreats(
		Row{"clause": 20_000_000.0, "value": 19_000_000.0, "xpts": 3.0}, teams, rates)
	if len(threats) != 2 {
		t.Fatalf("%d can pay, want 2", len(threats))
	}
	if tempted := Tempted(threats); tempted != 1 {
		t.Fatalf("tempted = %d, want 1", tempted)
	}
	if got := text(TopThreat(threats)); got != "Villaone" {
		t.Errorf("top threat = %q, want Villaone, the one it pays off for", got)
	}
}

// The ranking follows: a clause several rivals can pay but none would profit from ranks below a
// cheaper one somebody would, which is what "sube la clausula" on eleven players was hiding.
func TestRiskRanksWantingOverBeingAble(t *testing.T) {
	teams := RivalCash(Row{"my_team_id": "mine", "league_teams": Row{
		"a": team("mine", "PlatanosVerdes", 100_000_000),
		"b": team("one", "Villaone", 55_000_000),
		"c": team("two", "TheMessias", 55_000_000),
		"d": team("three", "JMjugon", 55_000_000),
	}}, 100_000_000)
	rates := map[string]float64{"one": 0.30, "two": 0.30, "three": 0.30}

	// Expensive for what he gives: three can pay, none of them trades up.
	dear := Row{"clause": 24_000_000.0, "value": 9_000_000.0, "xpts": 3.0, "score": 1.8}
	// Cheap for what he gives: the same three, and all of them trade up.
	cheap := Row{"clause": 9_000_000.0, "value": 8_500_000.0, "xpts": 3.0, "score": 1.0}

	dearThreats, dearMargin := ClauseThreats(dear, teams, rates)
	cheapThreats, cheapMargin := ClauseThreats(cheap, teams, rates)
	if Tempted(dearThreats) != 0 || Tempted(cheapThreats) != 3 {
		t.Fatalf("tempted: dear %d (want 0), cheap %d (want 3)",
			Tempted(dearThreats), Tempted(cheapThreats))
	}
	if clauseRisk(cheap, cheapThreats, cheapMargin) <=
		clauseRisk(dear, dearThreats, dearMargin) {
		t.Error("the clause somebody would pay has to rank above the one nobody would")
	}
}

// Every squad measured the same way as your own: the median of points per million.
func TestSquadRatesPerTeam(t *testing.T) {
	rates := SquadRates([]Row{
		{"owner_team_id": "a", "value": 10_000_000.0, "xpts": 4.0},
		{"owner_team_id": "a", "value": 10_000_000.0, "xpts": 2.0},
		{"owner_team_id": "b", "value": 5_000_000.0, "xpts": 1.0},
		// No value and no points: nothing to measure, and no zero dragging the median down.
		{"owner_team_id": "b", "value": 0.0, "xpts": 0.0},
	})
	if got := rates["a"]; got < 0.29 || got > 0.31 {
		t.Errorf("rate a = %.2f, want 0.30", got)
	}
	if got := rates["b"]; got < 0.19 || got > 0.21 {
		t.Errorf("rate b = %.2f, want 0.20", got)
	}
}
