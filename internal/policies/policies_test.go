package policies

import (
	"path/filepath"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func amount(value float64) *float64 { return &value }

// An instruction that outlives the sale it authorised is a row the page can never clear, so
// what counts as sold matters, and so does what does not.
func TestSold(t *testing.T) {
	armed := map[string]Policy{
		"1": {ID: "1", Name: "Camavinga", AlwaysList: true, AutoSell: true},
		"2": {ID: "2", Name: "Yuri", AlwaysList: true},
		"3": {ID: "3", Name: "Youssef", Raid: true, MaxPay: amount(1_200_000)},
		"4": {ID: "4", Name: "Isi", MinPrice: amount(5_000_000)},
		"5": {ID: "5", Name: "Outside the universe", AlwaysList: true},
	}
	mine := map[string]bool{"1": false, "2": true, "3": false, "4": false}

	got := Sold(mine, armed)
	want := []string{"1", "4"}
	if len(got) != len(want) {
		t.Fatalf("Sold() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sold() = %v, want %v", got, want)
		}
	}
}

func TestForgetKeepsTheRaid(t *testing.T) {
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	if err := Save(map[string]Policy{
		"1": {ID: "1", Name: "Camavinga", AlwaysList: true, AutoSell: true},
		"2": {ID: "2", Name: "Youssef", AlwaysList: true, MinPrice: amount(2_000_000),
			Raid: true, MaxPay: amount(1_200_000)},
		"3": {ID: "3", Name: "Yuri", AlwaysList: true},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	gone, err := Forget("1", "2", "9")
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if len(gone) != 2 || gone[0] != "Camavinga" || gone[1] != "Youssef" {
		t.Fatalf("gone = %v, want [Camavinga Youssef]", gone)
	}

	left, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, still := left["1"]; still {
		t.Fatal("a sell-only instruction survived the sale")
	}
	if !left["3"].AlwaysList {
		t.Fatal("an untouched instruction was dropped")
	}
	raid := left["2"]
	if !raid.Raid || raid.MaxPay == nil || *raid.MaxPay != 1_200_000 {
		t.Fatalf("the raid did not survive: %+v", raid)
	}
	if raid.AlwaysList || raid.AutoSell || raid.MinPrice != nil {
		t.Fatalf("the sell side survived on the raid: %+v", raid)
	}
}

// The automatic sale that made a hole. Eleven players, five defenders, an offer over the
// threshold: the old floor per position said two defenders were spare, so it sold one and left
// ten, which no formation fields.
func TestAutoSellStopsShortOfBreakingTheEleven(t *testing.T) {
	squad := []Row{}
	for index, position := range []int{1, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4} {
		squad = append(squad, Row{"id": string(rune('a' + index)), "name": "p",
			"is_mine": true, "position_id": position, "value": 10_000_000.0})
	}
	// The defender with a good offer on the table and permission to sell at that price.
	seller := squad[1]
	seller["offers"] = []Row{{"id": "o1", "money": 12_000_000.0}}
	seller["market"] = Row{"market_id": "m1", "min_bid": 11_000_000.0}

	armed := map[string]Policy{
		text(seller["id"]): {ID: text(seller["id"]), AlwaysList: true, AutoSell: true},
	}
	plan := Plan(squad, armed)
	if len(plan) != 1 {
		t.Fatalf("%d actions, want one", len(plan))
	}
	if action := text(plan[0]["action"]); action != "avisar" {
		t.Fatalf("action = %q, want avisar: selling him leaves ten players", action)
	}
	if room := SquadRoom(squad, 2); room != 0 {
		t.Errorf("SquadRoom(defender) = %d, want 0 with eleven players", room)
	}
}
