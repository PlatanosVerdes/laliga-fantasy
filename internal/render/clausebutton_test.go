package render

import "testing"

func TestAScheduledClausulazoIsOfferedForChangeAtItsOwnCeiling(t *testing.T) {
	player := map[string]any{"id": "7", "name": "P. Martínez", "clause": 3000000.0, "raid_scheduled": true}
	act := clauseButton(player, 3300000, "jue 19:00", "dcard-go", map[string]any{"7": 4500000.0})
	if act.Label != "Cambiar clausulazo programado" || act.Args["max"] != int64(4500000) {
		t.Errorf("scheduled: %q max %v", act.Label, act.Args["max"])
	}
	delete(player, "raid_scheduled")
	act = clauseButton(player, 3300000, "jue 19:00", "dcard-go", map[string]any{})
	if act.Label != "Programar clausulazo" || act.Args["max"] != int64(3300000) {
		t.Errorf("not scheduled: %q max %v", act.Label, act.Args["max"])
	}
}
