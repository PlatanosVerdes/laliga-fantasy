package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
)

func TestARivalsSquadSaysWhoHeFielded(t *testing.T) {
	profile := tempProfile(t)
	rival := "T2"
	world := state.New(profile, func() (*model.Universe, error) {
		return &model.Universe{
			LeagueTeams: map[string]*model.LeagueTeam{"T2": {TeamID: "T2"}, "T3": {TeamID: "T3"}},
			Players: []model.Player{{ID: "1", Name: "Titular", OwnerTeamID: &rival},
				{ID: "2", Name: "Suplente", OwnerTeamID: &rival}},
			Lineups: map[string][]model.LineupPlayer{"T2": {{ID: "1"}}},
		}, nil
	})
	if err := world.Refresh("arranque"); err != nil {
		t.Fatal(err)
	}
	handler := asOwner(t, New(world, Options{Profile: profile}))
	read := func(team string) map[string]any {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://x/api/manager/"+team, nil))
		var body map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %d %s", team, recorder.Code, recorder.Body)
		}
		return body
	}
	if got, _ := json.Marshal(read("T2")["lineup"]); string(got) != `["1"]` {
		t.Errorf("T2 fielded p1 only, got %s", got)
	}
	if lineup := read("T3")["lineup"]; lineup != nil {
		t.Errorf("an unread lineup is absent, not empty: %v", lineup)
	}
}
