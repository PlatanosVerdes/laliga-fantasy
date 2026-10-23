package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api/apitest"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

// 42M in cash, 13M already bid on p20, a 100M squad and a 10% debt rule.
func roomServer(t *testing.T) http.Handler {
	t.Helper()
	profile := tempProfile(t)
	wasRead, wasStored := readCeiling, storedCeiling
	t.Cleanup(func() { readCeiling, storedCeiling = wasRead, wasStored })
	readCeiling = func(string) (futbolfantasy.Detail, error) { return futbolfantasy.Detail{}, errors.New("offline") }
	storedCeiling = func(string) (futbolfantasy.Detail, bool) { return futbolfantasy.Detail{}, false }
	bidID, myBid := "b20", 13_000_000.0
	world := state.New(profile, func() (*model.Universe, error) {
		universe := &model.Universe{
			Market: []model.Listing{{MarketID: "mk-20", PlayerID: "20", MinBid: 10_000_000, MyBidID: &bidID, MyBid: &myBid},
				{MarketID: "mk-30", PlayerID: "30", MinBid: 1_000_000}},
			Players: []model.Player{{ID: "20", Name: "Pujado"}, {ID: "30", Name: "Libre"},
				{ID: "1", Name: "Mio", IsMine: true, Value: 100_000_000}},
		}
		universe.Players[0].MarketEntry = &universe.Market[0]
		universe.Players[1].MarketEntry = &universe.Market[1]
		return universe, nil
	})
	if err := world.Refresh("arranque"); err != nil {
		t.Fatal(err)
	}
	guard := writes.NewGuard(apitest.New(t).Member("cuenta", "L", "me"))
	guard.Cash = func(string) (int64, error) { return 42_000_000, nil }
	return asOwner(t, New(world, Options{AllowWrites: true, Guard: guard, LeagueID: "L", MyTeamID: "me",
		Profile: profile, MaxDebtPct: 10}))
}

func askRoom(t *testing.T, handler http.Handler, operation, player string) writes.Room {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet,
		"http://x/api/bid/room?operation="+operation+"&player_id="+player, nil))
	var room writes.Room
	if err := json.Unmarshal(recorder.Body.Bytes(), &room); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("%s %s: %d %s", operation, player, recorder.Code, recorder.Body)
	}
	return room
}

func prepareAmount(handler http.Handler, operation, market, player string, amount int64) int {
	body := fmt.Sprintf(`{"operation":%q,"amount":%d,"market_id":%q,"player_id":%q,"bid_id":"b20"}`,
		operation, amount, market, player)
	request := httptest.NewRequest(http.MethodPost, "http://x/api/bid/prepare", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func TestTheDialogIsToldTheLimitPrepareEnforces(t *testing.T) {
	handler := roomServer(t)
	cases := []struct {
		operation, market, player string
		want                      writes.Room
	}{
		{"bid", "mk-30", "30", writes.Room{Cash: 42_000_000, Committed: 13_000_000, DebtRoom: 10_000_000, Limit: 39_000_000}},
		{"modify_bid", "mk-20", "20", writes.Room{Cash: 42_000_000, DebtRoom: 10_000_000, Limit: 52_000_000}},
		{"buy_offer", "mk-30", "30", writes.Room{Cash: 42_000_000, Committed: 13_000_000, Limit: 29_000_000}},
	}
	for _, c := range cases {
		got := askRoom(t, handler, c.operation, c.player)
		if got != c.want {
			t.Errorf("%s: room %+v, want %+v", c.operation, got, c.want)
		}
		if code := prepareAmount(handler, c.operation, c.market, c.player, got.Limit); code != http.StatusOK {
			t.Errorf("%s: the limit itself is refused (%d)", c.operation, code)
		}
		if code := prepareAmount(handler, c.operation, c.market, c.player, got.Limit+1); code == http.StatusOK {
			t.Errorf("%s: one euro over the limit is accepted", c.operation)
		}
	}
}
