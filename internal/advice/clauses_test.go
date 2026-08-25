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

	able, margin := ClauseThreats(Row{"clause": 28_000_000.0, "value": 27_000_000.0}, teams)
	if len(able) != 1 {
		t.Fatalf("%d able to pay, want 1 (you are not a threat to yourself)", len(able))
	}
	if got := text(TopThreat(able)); got != "Villaone" {
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
