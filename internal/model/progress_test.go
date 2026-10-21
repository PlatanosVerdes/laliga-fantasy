package model

import (
	"testing"
	"time"
)

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
	if got.Percent != 4+2+8+12+6+9+3 || got.Step != "Plantillas de la liga" || got.Done != 6 {
		t.Errorf("halfway through the squads: %+v", got)
	}
	reached("Calendario", 18, 18)
	if again, _ := CurrentProgress(); again.Percent < got.Percent {
		t.Errorf("the bar went back: %d after %d", again.Percent, got.Percent)
	}
}

// End says where the time went: each stage is charged what ran up to its own report, what ran
// after the last report goes to the stage the bar was left on, and a second End has nothing.
func TestTrackerChargesEachStageItsOwnTime(t *testing.T) {
	tracker := NewTracker(Stage{"Uno", "one", 50}, Stage{"Dos", "two", 50}, Stage{"Tres", "three", 0})
	tracker.Start()
	time.Sleep(20 * time.Millisecond)
	tracker.Reached("Uno", 1, 1)
	tracker.Reached("Dos", 0, 1)
	time.Sleep(20 * time.Millisecond)
	spent := map[string]int64{}
	attrs := tracker.End()
	for index := 0; index+1 < len(attrs); index += 2 {
		spent[attrs[index].(string)] = attrs[index+1].(int64)
	}
	if spent["one_ms"] < 20 || spent["two_ms"] < 20 || spent["total_ms"] < 40 {
		t.Errorf("time charged to the wrong stages: %v", spent)
	}
	if _, ran := spent["three_ms"]; ran {
		t.Errorf("a stage that never ran has no time: %v", spent)
	}
	if _, running := tracker.Current(); running {
		t.Error("the run is over")
	}
	if tracker.End() != nil {
		t.Error("a second End reports a run that is not there")
	}
}
