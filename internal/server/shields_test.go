package server

import (
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
)

// With the round's two shields spent the card must not offer "Blindar 24h", which the API would
// refuse: it says the round is full and books the next one instead.
func TestFullRoundOffersOnlyTheNextOne(t *testing.T) {
	now := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	eight := schedule.Round{Week: 8, Start: now.Add(-48 * time.Hour), End: now.Add(24 * time.Hour)}
	nine := schedule.Round{Week: 9, Start: eight.End, End: eight.End.Add(7 * 24 * time.Hour)}
	budgetAt := func(at time.Time) shieldBudget {
		if eight.Contains(at) {
			return shieldBudget{Known: true, Round: eight, Limit: 2,
				Used:   []state.ShieldUse{{Player: "Pedri", At: now.Add(-time.Hour)}},
				Booked: []state.ShieldUse{{Player: "Gavi", At: now.Add(time.Hour)}}}
		}
		return shieldBudget{Known: true, Round: nine, Limit: 2}
	}

	actions := shieldActions("1", "pt1", false, "", "", now, budgetAt)
	if len(actions) != 2 {
		t.Fatalf("una nota y un boton, got %v", actions)
	}
	if label := actions[0]["label"].(string); actions[0]["kind"] != "note" ||
		!strings.HasPrefix(label, "Blindajes de la J8: 2 de 2 usados") {
		t.Errorf("la nota tiene que decir que la jornada esta llena: %v", actions[0])
	}
	button := actions[1]
	if button["label"] != "Programar otro blindaje" || button["now_allowed"] != false {
		t.Errorf("solo se puede programar para la siguiente: %v", button)
	}
	when, err := time.Parse(time.RFC3339, button["suggested"].(string))
	if err != nil || !nine.Contains(when) {
		t.Errorf("la hora propuesta tiene que caer en la J9: %v", button["suggested"])
	}
	if budget := button["budget"].(shieldBudget); budget.Round.Week != 9 {
		t.Errorf("el cupo que ensena el dialogo es el de la J9: %+v", budget)
	}
}

func TestRoundWithRoomStillShieldsNow(t *testing.T) {
	now := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	round := schedule.Round{Week: 8, Start: now.Add(-time.Hour), End: now.Add(time.Hour)}
	actions := shieldActions("1", "pt1", false, "", "", now, func(time.Time) shieldBudget {
		return shieldBudget{Known: true, Round: round, Limit: 2,
			Used: []state.ShieldUse{{Player: "Pedri", At: now}}}
	})
	if len(actions) != 1 || actions[0]["label"] != "Blindar 24h" || actions[0]["now_allowed"] != true {
		t.Errorf("con un blindaje libre se ofrece blindar ya: %v", actions)
	}
}
