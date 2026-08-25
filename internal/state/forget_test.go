package state

import (
	"path/filepath"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
)

func armSelling(t *testing.T) {
	t.Helper()
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	if err := policies.Save(map[string]policies.Policy{
		"1": {ID: "1", Name: "Camavinga", AlwaysList: true, AutoSell: true},
		"2": {ID: "2", Name: "Yuri", AlwaysList: true},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func TestForgetSoldDropsWhatIsNoLongerYours(t *testing.T) {
	armSelling(t)
	(&State{}).forgetSold(&model.Universe{Players: []model.Player{
		{ID: "1", Name: "Camavinga", IsMine: false},
		{ID: "2", Name: "Yuri", IsMine: true},
	}})

	left, err := policies.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, still := left["1"]; still {
		t.Error("the instruction of a sold player survived")
	}
	if _, still := left["2"]; !still {
		t.Error("the instruction of a player still yours was dropped")
	}
}

// A world where nothing is yours is a world that came back short. Acting on it would throw
// away amounts nobody can retype.
func TestForgetSoldIgnoresAWorldWithNothingYours(t *testing.T) {
	armSelling(t)
	(&State{}).forgetSold(&model.Universe{Players: []model.Player{
		{ID: "1", Name: "Camavinga", IsMine: false},
		{ID: "2", Name: "Yuri", IsMine: false},
	}})

	left, err := policies.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(left) != 2 {
		t.Fatalf("%d instructions left, want 2", len(left))
	}
}
