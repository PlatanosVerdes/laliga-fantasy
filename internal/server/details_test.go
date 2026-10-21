package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
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

func biddingServer(t *testing.T, read func(string) (futbolfantasy.Ceiling, error),
	stored map[string]futbolfantasy.Ceiling) http.Handler {
	t.Helper()
	config.StateDir = t.TempDir()
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	wasRead, wasStored := readCeiling, storedCeiling
	t.Cleanup(func() { readCeiling, storedCeiling = wasRead, wasStored })
	readCeiling = read
	storedCeiling = func(ffID string) (futbolfantasy.Ceiling, bool) {
		ceiling, ok := stored[ffID]
		return ceiling, ok
	}
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
// him yet; when it cannot be read the bid goes through saying so, not warned as unprofitable.
func TestABidIsCheckedAgainstACeilingReadThereAndThen(t *testing.T) {
	read := 0
	old := map[string]futbolfantasy.Ceiling{"ff-20": {IdealBid: 9_000_000,
		FetchedAt: time.Now().Add(-9 * time.Hour)}}
	handler := biddingServer(t, func(ffID string) (futbolfantasy.Ceiling, error) {
		read++
		if ffID != "ff-20" {
			t.Errorf("read the page of %s", ffID)
		}
		return futbolfantasy.Ceiling{IdealBid: 2_500_000, FetchedAt: time.Now()}, nil
	}, old)
	status, body := prepareBid(handler)
	if status != http.StatusOK || body["ideal_bid"] != 2_500_000.0 || read != 1 {
		t.Fatalf("prepared with the ceiling read: %d %v (%d reads)", status, body, read)
	}
	if !strings.Contains(strings.Join(asStrings(body["warnings"]), " "), "por encima de la puja maxima rentable") {
		t.Errorf("judged by the ceiling: %v", body["warnings"])
	}

	unreadable := func(string) (futbolfantasy.Ceiling, error) {
		return futbolfantasy.Ceiling{}, errors.New("rate limited")
	}
	// The stored one stands in, with its age, and is judged by.
	status, body = prepareBid(biddingServer(t, unreadable, old))
	warnings := strings.Join(asStrings(body["warnings"]), " | ")
	if status != http.StatusOK || body["ideal_bid"] != 9_000_000.0 ||
		!strings.Contains(warnings, "su techo rentable es el guardado hace 9 h") {
		t.Errorf("the stored ceiling with its age: %d %v", status, body)
	}

	handler = biddingServer(t, unreadable, nil)
	status, body = prepareBid(handler)
	warnings = strings.Join(asStrings(body["warnings"]), " | ")
	if status != http.StatusOK || body["token"] == "" {
		t.Fatalf("an unreadable page never blocks a bid: %d %v", status, body)
	}
	if !strings.Contains(warnings, "no he podido leer la ficha de futbolfantasy de Barato: sin techo rentable, decide tú") ||
		strings.Contains(warnings, "no le ve rentabilidad") {
		t.Errorf("the ceiling is said to be unknown, not missing: %s", warnings)
	}
}

func asStrings(value any) []string {
	var out []string
	for _, item := range value.([]any) {
		out = append(out, item.(string))
	}
	return out
}
