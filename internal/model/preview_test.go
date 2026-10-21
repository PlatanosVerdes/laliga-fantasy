package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
)

// league is a fake API that writes down every request with its lifetime, in order.
type league struct {
	calls []string
}

func (l *league) note(format string, args ...any) {
	l.calls = append(l.calls, fmt.Sprintf(format, args...))
}

func (l *league) RawList(path string, ttl time.Duration, tag string, target any) error {
	l.note("players %s", ttl)
	blob := `[{"id":"10","nickname":"Courtois","positionId":1,"teamId":"t1","marketValue":20000000,"points":40,"playerStatus":"ok"},
{"id":"11","nickname":"Pedri","positionId":3,"teamId":"t2","marketValue":30000000,"points":50,"playerStatus":"ok"},
{"id":"20","nickname":"Lewandowski","positionId":4,"teamId":"t2","marketValue":25000000,"points":45,"playerStatus":"ok"},
{"id":"30","nickname":"Isco","positionId":3,"teamId":"t3","marketValue":9000000,"points":20,"playerStatus":"ok"},
{"id":"99","nickname":"Ancelotti","positionId":5,"teamId":"t1","marketValue":1,"points":0}]`
	return json.Unmarshal([]byte(blob), target)
}

func (l *league) CurrentWeek(ttl time.Duration) (api.Week, error) {
	l.note("week %s", ttl)
	return api.Week{WeekNumber: 9}, nil
}

func (l *league) TeamsMaster(ttl time.Duration) ([]api.Team, error) {
	l.note("teams %s", ttl)
	return []api.Team{{ID: "t1", Name: "Real Madrid"}, {ID: "t2", Name: "Barcelona"},
		{ID: "t3", Name: "Betis"}}, nil
}

func (l *league) Standings(leagueID string, ttl time.Duration) ([]map[string]any, error) {
	l.note("standings %s", ttl)
	return []map[string]any{
		{"id": "rival", "manager": "Rafa", "points": 300.0, "position": 1.0},
		{"id": "me", "manager": "Jorge", "points": 250.0, "position": 2.0},
	}, nil
}

func (l *league) TeamSquad(leagueID, teamID string, ttl time.Duration) (map[string]any, error) {
	l.note("squad %s %s", teamID, ttl)
	players := map[string][]any{
		"me": {map[string]any{"playerMaster": map[string]any{"id": "10"}, "buyoutClause": 30000000.0},
			map[string]any{"playerMaster": map[string]any{"id": "11"}},
			map[string]any{"playerMaster": map[string]any{"id": "99"}}},
		"rival": {map[string]any{"playerMaster": map[string]any{"id": "20"}}},
	}
	return map[string]any{"players": players[teamID]}, nil
}

func (l *league) TeamLineupWeek(teamID string, week int, ttl time.Duration) (map[string]any, error) {
	l.note("lineup %s %d %s", teamID, week, ttl)
	return map[string]any{"formation": map[string]any{
		"goalkeeper": []any{map[string]any{"playerMaster": map[string]any{"id": "10"}}}}}, nil
}

func (l *league) Player(playerID string, ttl time.Duration) (map[string]any, error) {
	l.note("player %s %s", playerID, ttl)
	if playerID == "11" {
		return map[string]any{"playerStatus": "injured"}, nil
	}
	return map[string]any{"playerStatus": "ok"}, nil
}

func (l *league) Money(teamID string, ttl time.Duration) (api.Money, error) {
	l.note("money %s %s", teamID, ttl)
	return api.Money{TeamMoney: 4_500_000}, nil
}

func (l *league) MarketRaw(leagueID string, ttl time.Duration, store bool) ([]map[string]any, error) {
	l.note("market %s", ttl)
	return []map[string]any{{"id": "m1", "salePrice": 9500000.0,
		"playerMaster": map[string]any{"id": "30", "marketValue": 9000000.0}}}, nil
}

// My squad is shown before the market is asked for, and the market before any rival's squad;
// each stage carries everything the one before had.
func TestPreviewReadsMyTeamThenTheMarketThenTheRivals(t *testing.T) {
	source := &league{}
	var stages []Preview
	var askedBefore []int
	err := BuildPreview(source, "L", "me", func(preview Preview) {
		stages = append(stages, preview)
		askedBefore = append(askedBefore, len(source.calls))
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 3 {
		t.Fatalf("%d stages published, want 3", len(stages))
	}
	for index, want := range []int{PreviewMine, PreviewMarket, PreviewRivals} {
		if stages[index].Stage != want {
			t.Errorf("stage %d is %d", index, stages[index].Stage)
		}
	}
	before := func(stage int) string { return strings.Join(source.calls[:askedBefore[stage]], "\n") }
	if strings.Contains(before(0), "market") || strings.Contains(before(0), "squad rival") {
		t.Errorf("my team waited for the market or a rival:\n%s", before(0))
	}
	if !strings.Contains(before(1), "market") || strings.Contains(before(1), "squad rival") {
		t.Errorf("the market waited for a rival, or came without asking:\n%s", before(1))
	}

	mine := stages[0].Mine
	if mine.Name != "Jorge" || len(mine.Players) != 2 || mine.Value != 50_000_000 {
		t.Fatalf("my team: %+v", mine)
	}
	byID := map[string]PreviewPlayer{}
	for _, player := range mine.Players {
		byID[player.ID] = player
	}
	if !byID["10"].Starter || byID["11"].Starter {
		t.Errorf("the saved eleven: %+v", mine.Players)
	}
	if byID["11"].Status != "injured" {
		t.Errorf("his own page is graver than the list and wins: %q", byID["11"].Status)
	}
	if clause := byID["10"].Clause; clause == nil || *clause != 30_000_000 || byID["10"].Team != "Real Madrid" {
		t.Errorf("Courtois: %+v", byID["10"])
	}
	if stages[0].Cash == nil || *stages[0].Cash != 4_500_000 || stages[0].Market != nil || stages[0].Rivals != nil {
		t.Errorf("stage one: %+v", stages[0])
	}
	if market := stages[1].Market; len(market) != 1 || market[0].Player.Name != "Isco" ||
		market[0].Listing.MinBid != 9_500_000 || len(stages[1].Mine.Players) != 2 {
		t.Errorf("stage two: %+v", stages[1])
	}
	if rivals := stages[2].Rivals; len(rivals) != 1 || rivals[0].Name != "Rafa" ||
		len(rivals[0].Players) != 1 || len(stages[2].Market) != 1 {
		t.Errorf("stage three: %+v", stages[2])
	}
}

// The preview only moves requests to the front: each is one Build makes, with Build's lifetime,
// so the build after it reads them back from the cache instead of asking again.
func TestPreviewAsksWhatBuildAsksForAsLong(t *testing.T) {
	source := &league{}
	if err := BuildPreview(source, "L", "me", func(Preview) {}); err != nil {
		t.Fatal(err)
	}
	want := []string{"week 30m0s", "teams 24h0m0s", "players 6h0m0s", "standings 30m0s",
		"squad me 30m0s", "lineup me 9 5m0s", "player 11 " + OwnStatusTTL.String(),
		"player 10 " + OwnStatusTTL.String(), "money me 1m0s", "market 1m0s", "squad rival 30m0s"}
	if got := strings.Join(source.calls, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}
