package state

import (
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
)

func tempProfile(t *testing.T) func() config.Profile {
	t.Helper()
	profile := config.ProfileIn(t.TempDir(), "cuenta", "L")
	return func() config.Profile { return profile }
}

func armSelling(t *testing.T) func() config.Profile {
	t.Helper()
	profile := tempProfile(t)
	if err := policies.Open(profile()).Save(map[string]policies.Policy{
		"1": {ID: "1", Name: "Camavinga", AlwaysList: true, AutoSell: true},
		"2": {ID: "2", Name: "Yuri", AlwaysList: true},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	return profile
}

func TestForgetSoldDropsWhatIsNoLongerYours(t *testing.T) {
	profile := armSelling(t)
	(&State{profile: profile}).forgetSold(&model.Universe{Players: []model.Player{
		{ID: "1", Name: "Camavinga", IsMine: false},
		{ID: "2", Name: "Yuri", IsMine: true},
	}})

	left, err := policies.Open(profile()).Load()
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
	profile := armSelling(t)
	(&State{profile: profile}).forgetSold(&model.Universe{Players: []model.Player{
		{ID: "1", Name: "Camavinga", IsMine: false},
		{ID: "2", Name: "Yuri", IsMine: false},
	}})

	left, err := policies.Open(profile()).Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(left) != 2 {
		t.Fatalf("%d instructions left, want 2", len(left))
	}
}

// A raid target sold to the market: in the world, nobody's, and not yours. The order has no
// clause left to pay, so it goes, and the log says why.
func TestForgetSoldDisarmsARaidOnAPlayerNobodyOwns(t *testing.T) {
	profile := tempProfile(t)
	limit := 66_000_000.0
	if err := policies.Open(profile()).Save(map[string]policies.Policy{
		"2387": {ID: "2387", Name: "Camello", Raid: true, MaxPay: &limit, Owner: "La rataneta"},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	(&State{profile: profile}).forgetSold(&model.Universe{Players: []model.Player{
		{ID: "2387", Name: "Camello"},
		{ID: "2", Name: "Yuri", IsMine: true},
	}})

	left, err := policies.Open(profile()).Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if entry, still := left["2387"]; still && entry.Raid {
		t.Error("the raid on a player nobody owns survived")
	}
}

// A rival squad that fails to load leaves its players without an owner for one pass; that is not
// a sale, and the raids on them stay armed.
func TestAMissingSquadDoesNotDisarmRaids(t *testing.T) {
	profile := tempProfile(t)
	if err := policies.Open(profile()).Save(map[string]policies.Policy{
		"5": {ID: "5", Name: "Yoel Lago", Raid: true, Owner: "-papi—"},
	}); err != nil {
		t.Fatal(err)
	}
	(&State{profile: profile}).forgetSold(&model.Universe{UnreadSquads: 1, Players: []model.Player{
		{ID: "1", Name: "Mine", IsMine: true},
		{ID: "5", Name: "Yoel Lago"},
	}})
	left, err := policies.Open(profile()).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !left["5"].Raid {
		t.Error("a raid was disarmed because its target's squad failed to load")
	}
}
