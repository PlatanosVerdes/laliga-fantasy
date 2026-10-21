package state

import (
	"fmt"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
)

// A cold start's preview is shown, never acted on. Its squad lacks Camavinga, whose instruction
// a world would drop as sold; a preview leaves it, and every reader that acts sees no world.
func TestAPreviewIsShownButNeverActedOn(t *testing.T) {
	armSelling(t)
	config.StateDir = t.TempDir()
	built := &model.Universe{Players: []model.Player{
		{ID: "1", Name: "Camavinga", IsMine: true}, {ID: "2", Name: "Yuri", IsMine: true}}}
	world := New(func() (*model.Universe, error) { return built, nil })
	channel := world.Subscribe()
	defer world.Unsubscribe(channel)

	for _, stage := range []int{model.PreviewMine, model.PreviewMarket, model.PreviewRivals} {
		world.ShowPreview(model.Preview{Stage: stage,
			Mine: model.PreviewTeam{Players: []model.PreviewPlayer{{ID: "2", Name: "Yuri"}}}})
		if !world.Loading() || world.Universe() != nil {
			t.Fatalf("stage %d published a world", stage)
		}
		if got := world.Preview(); got == nil || got.Stage != stage {
			t.Fatalf("stage %d: preview %+v", stage, got)
		}
		if health := world.Health(); !health.Loading || health.Preview != stage {
			t.Errorf("stage %d: health says loading %v at %d", stage, health.Loading, health.Preview)
		}
		if message := string(<-channel); message != fmt.Sprintf(`{"stage":%d,"type":"preview"}`, stage) {
			t.Errorf("stage %d announced %s", stage, message)
		}
	}
	if payload := world.SchedulePayload(); len(payload.Players) != 0 || len(payload.Market) != 0 {
		t.Errorf("the scheduler saw the preview: %+v", payload)
	}
	if shot := world.Snapshot(); shot.Squad != 0 {
		t.Errorf("a snapshot counted the preview: %+v", shot)
	}
	if world.Patch("test", func(*model.Universe) bool { return true }) {
		t.Error("a patch landed on the preview")
	}
	left, err := policies.Load()
	if err != nil || len(left) != 2 {
		t.Fatalf("instructions after the preview: %v %v", left, err)
	}

	if err := world.Refresh("arranque"); err != nil {
		t.Fatal(err)
	}
	if world.Universe() != built {
		t.Error("the world published is not the one the build produced")
	}
	if world.Loading() || world.Health().Loading {
		t.Error("still loading once the world is built")
	}
	if got := world.Preview(); got == nil || got.Stage != model.PreviewComplete {
		t.Errorf("the preview should read as complete: %+v", got)
	}
	world.ShowPreview(model.Preview{Stage: model.PreviewMine})
	if got := world.Preview(); got.Stage != model.PreviewComplete {
		t.Errorf("a late preview stood in for the world: %+v", got)
	}
}
