package state

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

// The futbolfantasy pages landing behind the page move the page and not the world: the cache
// key changes, the version does not, and an open tab hears of it once the page is drawn.
func TestARedrawMovesThePageButNotTheWorld(t *testing.T) {
	world := New(tempProfile(t), func() (*model.Universe, error) { return &model.Universe{}, nil })
	world.Redraw(map[string]any{"type": "details"})
	if key, _ := world.RenderStamp(); key != "0.0" {
		t.Errorf("nothing to redraw while the league loads: %s", key)
	}
	if err := world.Refresh("arranque"); err != nil {
		t.Fatal(err)
	}
	keyBefore, versionBefore := world.RenderStamp()
	drawn := make(chan string, 1)
	world.SetWarm(func() { key, _ := world.RenderStamp(); drawn <- key })
	channel := world.Subscribe()
	defer world.Unsubscribe(channel)

	world.Redraw(map[string]any{"type": "details", "players": []string{"7"}, "done": 1, "total": 3})
	key, version := world.RenderStamp()
	if key == keyBefore || version != versionBefore {
		t.Errorf("key %s → %s, version %d → %d", keyBefore, key, versionBefore, version)
	}
	select {
	case message := <-channel:
		if warmed := <-drawn; warmed != key {
			t.Errorf("announced before drawing the new page: warmed %s", warmed)
		}
		var got map[string]any
		_ = json.Unmarshal(message, &got)
		if got["type"] != "details" || got["version"] != float64(version) || got["done"] != 1.0 {
			t.Errorf("announced %s", message)
		}
	case <-time.After(time.Second):
		t.Fatal("an open tab was not told")
	}
}
