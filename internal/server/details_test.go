package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

// A futbolfantasy page landing behind the page redraws it without making anybody wait: a tab
// opened meanwhile gets the page there is, and the views asked for next are the redrawn ones.
func TestAPageLandingIsDrawnBehindTheOpenPage(t *testing.T) {
	server, world, painted := paintedServer(t)
	world.Redraw(map[string]any{"type": "details"})
	page, ready := server.current()
	if !ready || page.HTML != "page 1" {
		t.Fatalf("the page there is is served at once: %q %v", page.HTML, ready)
	}
	painted.release <- struct{}{}
	if got := server.render().HTML; got != "page 2" {
		t.Errorf("the redraw lands in the cache: %q", got)
	}
}

func biddingServer(t *testing.T, read func(string) (map[string]any, error)) http.Handler {
	t.Helper()
	config.StateDir = t.TempDir()
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	was := playerDetail
	t.Cleanup(func() { playerDetail = was })
	playerDetail = read
	ffID, bids := "ff-20", 0
	world := state.New(func() (*model.Universe, error) {
		universe := &model.Universe{
			Market:  []model.Listing{{MarketID: "mk-20", MinBid: 2_000_000, Bids: &bids}},
			Players: []model.Player{{ID: "20", Name: "Barato", FFID: &ffID, Value: 2_000_000}},
		}
		universe.Players[0].MarketEntry = &universe.Market[0]
		return universe, nil
	})
	if err := world.Refresh("arranque"); err != nil {
		t.Fatal(err)
	}
	guard := writes.NewGuard(nil)
	guard.Cash = func(string) (int64, error) { return 50_000_000, nil }
	return New(world, Options{AllowWrites: true, Guard: guard, LeagueID: "L", MyTeamID: "me"}).Handler()
}

func prepareBid(handler http.Handler) (int, map[string]any) {
	request := httptest.NewRequest(http.MethodPost, "http://x/api/bid/prepare", strings.NewReader(
		`{"operation":"bid","amount":3000000,"market_id":"mk-20","player_id":"20"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var body map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	return recorder.Code, body
}

// A bid is checked against his ceiling read there and then, whether or not the page has got to
// him yet; when it cannot be read the bid is refused, not warned as unprofitable.
func TestABidIsCheckedAgainstACeilingReadThereAndThen(t *testing.T) {
	read := 0
	handler := biddingServer(t, func(ffID string) (map[string]any, error) {
		read++
		if ffID != "ff-20" {
			t.Errorf("read the page of %s", ffID)
		}
		return map[string]any{"ideal_bid": 2_500_000.0}, nil
	})
	status, body := prepareBid(handler)
	if status != http.StatusOK || body["ideal_bid"] != 2_500_000.0 || read != 1 {
		t.Fatalf("prepared with the ceiling read: %d %v (%d reads)", status, body, read)
	}
	if !strings.Contains(strings.Join(asStrings(body["warnings"]), " "), "por encima de la puja maxima rentable") {
		t.Errorf("judged by the ceiling: %v", body["warnings"])
	}

	handler = biddingServer(t, func(string) (map[string]any, error) {
		return nil, errors.New("rate limited")
	})
	status, body = prepareBid(handler)
	message, _ := body["error"].(string)
	if status != http.StatusBadRequest || !strings.Contains(message, "ficha de futbolfantasy de Barato") {
		t.Errorf("refused with the reason: %d %v", status, body)
	}
	if strings.Contains(message, "no le ve rentabilidad") {
		t.Errorf("a page that could not be read is not futbolfantasy saying no: %s", message)
	}
}

func asStrings(value any) []string {
	var out []string
	for _, item := range value.([]any) {
		out = append(out, item.(string))
	}
	return out
}
