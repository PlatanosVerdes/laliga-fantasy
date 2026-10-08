package advice

import (
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
)

func clausulazoSquad() []Row {
	squad := []Row{{"id": "gk", "is_mine": true, "position_id": 1.0, "xpts": 5.0, "available": true}}
	for i, pos := range []float64{2, 2, 2, 2, 3, 3, 3, 3, 4, 4} {
		squad = append(squad, Row{"id": string(rune('a' + i)), "is_mine": true, "position_id": pos,
			"xpts": 3.0, "available": true})
	}
	squad = append(squad, Row{"id": "star", "is_mine": true, "position_id": 4.0, "xpts": 7.0,
		"available": true})
	return squad
}

// Selling my 7-xPts striker is harmless when his money pays a rival's 7.5 whose clause is open.
func TestSwapForSaleFindsTheClauseThatCoversHim(t *testing.T) {
	now := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	players := append(clausulazoSquad(),
		Row{"id": "r1", "owner": "Rival", "position_id": 4.0, "xpts": 7.5, "clause": 30e6,
			"available": true},
		Row{"id": "r2", "owner": "Rival", "position_id": 4.0, "xpts": 9.0, "clause": 300e6,
			"available": true},
		Row{"id": "r3", "owner": "Rival", "position_id": 4.0, "xpts": 9.0, "clause": 10e6,
			"shielded": true})
	open := schedule.Window{Open: true}
	move, ok := SwapForSale(players, "star", 25e6, 10e6, open, now, nil)
	if !ok || text(move.In["id"]) != "r1" || move.Gain < 0.4 || move.CashAfter != 5e6 {
		t.Fatalf("got %+v %v", move, ok)
	}
	// Shut until in three days: too far to count.
	shut := schedule.Window{Open: false, OpensAt: now.Add(72 * time.Hour).Format(time.RFC3339)}
	if _, ok := SwapForSale(players, "star", 25e6, 10e6, shut, now, nil); ok {
		t.Error("una cláusula que se abre en tres días no es un cambio de hoy")
	}
	soon := schedule.Window{Open: false, OpensAt: now.Add(20 * time.Hour).Format(time.RFC3339)}
	if move, ok := SwapForSale(players, "star", 25e6, 10e6, soon, now, nil); !ok || move.Opens == "" {
		t.Errorf("abre mañana: se programa, %+v", move)
	}
}

func TestClauseUpgradeNeedsTheBar(t *testing.T) {
	now := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	players := append(clausulazoSquad(), Row{"id": "r1", "owner": "Rival", "position_id": 3.0,
		"xpts": 6.0, "clause": 10e6, "available": true})
	open := schedule.Window{Open: true}
	if _, ok := ClauseUpgrade(players, 20e6, 0.2, open, now); !ok {
		t.Error("+3 xPts por 10M pasa una barra de 0,2 por millón")
	}
	if _, ok := ClauseUpgrade(players, 20e6, 0.5, open, now); ok {
		t.Error("y no una de 0,5")
	}
}
