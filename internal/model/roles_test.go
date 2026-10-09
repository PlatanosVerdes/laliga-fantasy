package model

import (
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/matching"
)

// A hierarchy row goes to the player the market already matched by futbolfantasy's id, and
// when the market never saw him, to the one whose name and club fit.
func TestMatchRolesByIDThenByName(t *testing.T) {
	players := []map[string]any{
		{"id": "1", "name": "Vlachodimos", "nickname": "Vlachodimos", "teamId": "17", "positionId": 1.0},
		{"id": "2", "name": "Kike Salas", "nickname": "Kike Salas", "teamId": "17", "positionId": 2.0},
		{"id": "3", "name": "Kike García", "nickname": "Kike García", "teamId": "9", "positionId": 4.0},
	}
	market := map[string]map[string]any{"1": {"ff_id": "6235"}}
	roles := []map[string]any{
		{"ff_id": "6235", "ff_name": "Odysseas Vlachodimos", "team": "sevilla", "key": "clave"},
		{"ff_id": "999", "ff_name": "Kike Salas", "team": "sevilla", "key": "importante"},
		{"ff_id": "998", "ff_name": "Nadie Conocido", "team": "sevilla", "key": "descarte"},
	}
	teams := matching.BuildTeamIndex([]map[string]any{
		{"id": "17", "name": "Sevilla FC", "slug": "sevilla", "shortName": "SEV"},
		{"id": "9", "name": "Deportivo Alavés", "slug": "alaves", "shortName": "ALA"},
	})
	got := MatchRoles(players, market, roles, teams)
	if got["1"]["key"] != "clave" || got["2"]["key"] != "importante" {
		t.Errorf("by id and by name: %v", got)
	}
	if _, wrong := got["3"]; wrong || len(got) != 2 {
		t.Errorf("an unknown name is nobody's: %v", got)
	}
}
