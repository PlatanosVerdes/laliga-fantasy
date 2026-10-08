package model

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func transfer(id, player, seller, buyer, at string, amount float64) Event {
	return Event{TypeID: 1, PlayerID: &player, Seller: &seller, Buyer: &buyer, Amount: &amount,
		Raw: map[string]any{"id": id, "createdAt": at}}
}

// Valles was paid at the clause JMjugon had raised to, which only the clause seen before can
// tell; Luismi Cruz at the price tete alejo paid, which the log alone tells; Morcillo was an
// offer accepted inside the lock, which neither may call a clausulazo.
func TestMarkClausulazos(t *testing.T) {
	config.ClauseFile = filepath.Join(t.TempDir(), "clauses.json")
	jm, me, tete, papi := "JMjugon", "PlatanosVerdes", "tete alejo", "-papi—"
	clause := 71_200_626.0
	universe := &Universe{
		LeagueTeams: map[string]*LeagueTeam{"1": {Manager: &jm}},
		Players:     []Player{{ID: "2541", OwnerTeamID: strPtr("1"), Clause: &clause}},
	}
	if err := MarkClausulazos(universe, time.Date(2026, 10, 6, 13, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	universe.Activity = []Event{
		transfer("a", "2541", jm, me, "2026-10-06T15:20:05+02:00", 71_200_626),
		transfer("b", "3001", tete, me, "2026-10-06T15:49:47+02:00", 15_000_000),
		transfer("c", "4001", me, papi, "2026-10-04T14:08:30+02:00", 850_000),
		{TypeID: 31, PlayerID: strPtr("3001"), Buyer: &tete, Amount: f64(15_000_000),
			Raw: map[string]any{"id": "d", "createdAt": "2026-09-15T18:00:00+02:00"}},
		{TypeID: 31, PlayerID: strPtr("4001"), Buyer: &me, Amount: f64(850_000),
			Raw: map[string]any{"id": "e", "createdAt": "2026-09-22T19:02:00+02:00"}},
	}
	if err := MarkClausulazos(universe, time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, event := range universe.Activity {
		got[text(event.Raw["id"])] = event.Clausulazo
	}
	if !got["a"] || !got["b"] || got["c"] {
		t.Errorf("clausulazos = %v, want a and b only", got)
	}
}

func strPtr(value string) *string { return &value }
func f64(value float64) *float64  { return &value }
