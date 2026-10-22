package state

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

// Four clicks in a row started four rebuilds at once, and each announced the same change.
func TestRebuildsSideBySideAnnounceAChangeOnce(t *testing.T) {
	offers := 0
	world := New(tempProfile(t), func() (*model.Universe, error) {
		time.Sleep(20 * time.Millisecond)
		player := model.Player{ID: "1", Name: "Unai Lopez", IsMine: true}
		for range offers {
			player.Offers = append(player.Offers, map[string]any{"id": "o"})
		}
		return &model.Universe{Players: []model.Player{player}}, nil
	})
	if err := world.Refresh("start"); err != nil {
		t.Fatal(err)
	}
	feed := world.Subscribe()
	offers = 2

	var wait sync.WaitGroup
	for range 4 {
		wait.Add(1)
		go func() { defer wait.Done(); _ = world.RefreshWith("always", true) }()
	}
	wait.Wait()

	effects := 0
	for {
		select {
		case raw := <-feed:
			var message map[string]any
			if json.Unmarshal(raw, &message) == nil && message["type"] == "effect" {
				effects++
			}
		default:
			if effects != 1 {
				t.Errorf("the change was announced %d times, want once", effects)
			}
			return
		}
	}
}
