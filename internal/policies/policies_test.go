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
