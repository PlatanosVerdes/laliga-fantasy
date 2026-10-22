package writes

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx/httpxtest"
)

const (
	goldenLeague = "018012894"
	goldenTeam   = "38126981"
)

var goldenArgs = Args{MarketID: "14693409",
	BidID: "5501", OfferID: "37547860", PlayerID: "2541", PlayerTeamID: "25009250",
	Amount: 1_000_000, Goalkeeper: "11", Defender: []string{"21", "22", "23"},
	Midfield: []string{"31", "32", "33", "34"}, Striker: []string{"41", "42", "43"},
	Formation: []int{3, 4, 3}, Favourite: true}

var goldenWho = Player{Name: "X", Clause: 1_000_000, Available: true}

// What every operation put on the wire for the one live profile before writes were bound to a
// member, byte for byte: method, path, query, the headers that reach LaLiga and the body.
var golden = map[string][]string{
	"accept_offer": {"POST /api/v1/competition/1/league/018012894/market/14693409/offer/37547860/accept?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"offerMoney\":1000000}"},
	"bid":          {"POST /api/v1/competition/1/league/018012894/market/14693409/bid?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"money\":1000000}"},
	"buy_offer":    {"POST /api/v1/competition/1/league/018012894/market/14693409/offer?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"money\":1000000}"},
	"cancel_bid":   {"DELETE /api/v1/competition/1/league/018012894/market/14693409/bid/5501/cancel?x-lang=es auth=Bearer live-token type= app=2 lang=es body="},
	"cancel_offer": {"DELETE /api/v1/competition/1/league/018012894/market/14693409/offer/37547860/cancel?x-lang=es auth=Bearer live-token type= app=2 lang=es body="},
	"change_offer": {"DELETE /api/v1/competition/1/league/018012894/market/14693409/offer/37547860/cancel?x-lang=es auth=Bearer live-token type= app=2 lang=es body=",
		"POST /api/v1/competition/1/league/018012894/market/14693409/offer?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"money\":1000000}"},
	"claim_daily_reward": {"POST /api/v1/competition/1/league/018012894/team/daily-reward?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"rewardedAd\":1,\"rewardedAdType\":\"dailyreward\",\"teamId\":\"38126981\"}"},
	"decline_offer":      {"POST /api/v1/competition/1/league/018012894/market/14693409/offer/37547860/reject?x-lang=es auth=Bearer live-token type= app=2 lang=es body="},
	"direct_offer":       {"POST /api/v1/competition/1/league/018012894/market/direct-offer?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"money\":1000000,\"playerId\":\"14693409\"}"},
	"favourite":          {"POST /api/v1/competition/1/teams/38126981/favourite-players?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"favourite\":true,\"player\":2541}"},
	"modify_bid":         {"PUT /api/v1/competition/1/league/018012894/market/14693409/bid/5501?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"money\":1000000}"},
	"pay_clause":         {"POST /api/v1/competition/1/league/018012894/buyout/25009250/pay?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"buyoutClauseToPay\":1000000}"},
	"raise_clause":       {"PUT /api/v1/competition/1/league/018012894/buyout/player?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"factor\":2,\"playerId\":\"25009250\",\"valueToIncrease\":1000000}"},
	"save_lineup":        {"PUT /api/v1/competition/1/teams/38126981/lineup?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"defender\":[\"21\",\"22\",\"23\"],\"goalkeeper\":\"11\",\"midfield\":[\"31\",\"32\",\"33\",\"34\"],\"striker\":[\"41\",\"42\",\"43\"],\"tactical_formation\":[3,4,3]}"},
	"sell_to_market":     {"POST /api/v1/competition/1/league/018012894/market/sell?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"playerId\":\"25009250\",\"salePrice\":1000000}"},
	"shield_player":      {"PUT /api/v1/competition/1/league/018012894/shield/player?x-lang=es auth=Bearer live-token type=application/json app=2 lang=es body={\"playerId\":\"25009250\",\"rewardedAd\":1,\"rewardedAdType\":\"Blindaje\"}"},
	"withdraw":           {"DELETE /api/v1/competition/1/league/018012894/market/14693409/delete?x-lang=es auth=Bearer live-token type= app=2 lang=es body="},
}

func liveProfile(t *testing.T) *httpxtest.Fake {
	t.Helper()
	roots := []string{config.ConfigRoot, config.StateRoot, config.CacheRoot, config.Account,
		config.League}
	t.Cleanup(func() {
		config.ConfigRoot, config.StateRoot, config.CacheRoot = roots[0], roots[1], roots[2]
		config.Account, config.League = roots[3], roots[4]
	})
	dir := t.TempDir()
	config.ConfigRoot, config.StateRoot, config.CacheRoot = dir, dir, filepath.Join(dir, "cache")
	config.Account, config.League = "live", goldenLeague
	if err := auth.For("live").Save(&auth.Tokens{AccessToken: "live-token",
		ExpiresOn: time.Now().Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	fake := httpxtest.New(t)
	fake.Answer("/api/", 200, `{}`)
	fake.Answer("GET /api"+config.CMP+"/leagues", 200,
		`[{"id":"`+goldenLeague+`","team":{"id":"`+goldenTeam+`"}}]`)
	t.Cleanup(httpx.SetTransport(fake))
	t.Cleanup(httpx.SetQueue(httpx.NewQueue(nil, httpx.DefaultLimits(), nil)))
	return fake
}

func liveGuard(t *testing.T) *Guard {
	t.Helper()
	member, err := api.AccountOf(config.Default()).Member(goldenLeague, goldenTeam)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGuard(member)
	g.Cash = func(string) (int64, error) { return 50_000_000, nil }
	g.Squad = func() *Squad {
		return &Squad{Slots: map[string]bool{goldenArgs.PlayerTeamID: true},
			Listings: map[string]bool{goldenArgs.MarketID: true}}
	}
	return g
}

func wire(calls []httpxtest.Call) []string {
	var out []string
	for _, call := range calls {
		if call.Method == "GET" {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s?%s auth=%s type=%s app=%s lang=%s body=%s",
			call.Method, call.Path, call.Query, call.Header.Get("Authorization"),
			call.Header.Get("Content-Type"), call.Header.Get("x-app"), call.Header.Get("x-lang"),
			call.Body))
	}
	return out
}

func TestTheLiveProfileSendsTheSameBytesAsBefore(t *testing.T) {
	fake := liveProfile(t)
	g := liveGuard(t)
	names := make([]string, 0, len(Operations))
	for name := range Operations {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) != len(golden) {
		t.Errorf("%d operations, %d in the golden table", len(names), len(golden))
	}
	for _, name := range names {
		before := len(fake.Calls())
		summary, err := g.Prepare(name, goldenArgs, goldenWho, true)
		if err != nil {
			t.Errorf("%s prepare: %v", name, err)
			continue
		}
		if _, err := g.Confirm(summary.Token, true, false); err != nil {
			t.Errorf("%s confirm: %v", name, err)
			continue
		}
		got := wire(fake.Calls()[before:])
		if strings.Join(got, "\n") != strings.Join(golden[name], "\n") {
			t.Errorf("%s confirmed sent\n%s\nwant\n%s", name, strings.Join(got, "\n"),
				strings.Join(golden[name], "\n"))
		}

		before = len(fake.Calls())
		if _, err := g.Automatic(name, goldenArgs, goldenWho, true); err != nil {
			t.Errorf("%s automatic: %v", name, err)
			continue
		}
		want := golden[name][len(golden[name])-1:]
		if got := wire(fake.Calls()[before:]); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s automatic sent\n%s\nwant\n%s", name, strings.Join(got, "\n"),
				strings.Join(want, "\n"))
		}
	}
}
