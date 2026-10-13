package model

import "testing"

// The bar adds up the stages before the current one, never goes back, and says nothing when
// no build is running.
func TestProgressAddsUpTheStages(t *testing.T) {
	reached("Calendario", 1, 2)
	if _, building := CurrentProgress(); building {
		t.Fatal("progress reported with no build running")
	}
	buildStarted()
	defer buildEnded()
	reached("Plantillas de la liga", 6, 12)
	got, _ := CurrentProgress()
	if got.Percent != 15+8+12+14 || got.Step != "Plantillas de la liga" || got.Done != 6 {
		t.Errorf("halfway through the squads: %+v", got)
	}
	reached("Calendario", 18, 18)
	if again, _ := CurrentProgress(); again.Percent < got.Percent {
		t.Errorf("the bar went back: %d after %d", again.Percent, got.Percent)
	}
}
