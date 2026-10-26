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

// A star toggled right after a clause rise used to be shown as what took the money.
func TestSettleReportsOnlyWhatTheActionCanMove(t *testing.T) {
	cash, offers := 23.6e6, 0
	mine := "7"
	world := New(tempProfile(t), func() (*model.Universe, error) {
		player := model.Player{ID: "1", Name: "Pepelu", IsMine: true}
		for range offers {
			player.Offers = append(player.Offers, map[string]any{"id": "o"})
		}
		return &model.Universe{MyTeamID: &mine, Players: []model.Player{player},
			LeagueTeams: map[string]*model.LeagueTeam{mine: {TeamID: mine, EstimatedCash: cash}}}, nil
	})
	if err := world.Refresh("start"); err != nil {
		t.Fatal(err)
	}
	feed := world.Subscribe()
	cash, offers = 22.3e6, 1

	if err := world.Settle("decline_offer", []string{"offers"}); err != nil {
		t.Fatal(err)
	}

	reported := map[string][]string{}
	for {
		select {
		case raw := <-feed:
			var message map[string]any
			if json.Unmarshal(raw, &message) != nil || message["type"] != "effect" {
				continue
			}
			for key := range message["changed"].(map[string]any) {
				operation := message["operation"].(string)
				reported[operation] = append(reported[operation], key)
			}
		default:
			if got := reported["decline_offer"]; len(got) != 1 || got[0] != "offers" {
				t.Errorf("the action was credited with %v, want only offers", got)
			}
			if got := reported[Unattributed]; len(got) != 1 || got[0] != "cash" {
				t.Errorf("the unexplained change was %v, want cash", got)
			}
			return
		}
	}
}
