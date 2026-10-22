package writes

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/api/apitest"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

type side struct {
	account, league, team, slot, listing string
	member                               *api.Member
}

func twoSides(t *testing.T) (*apitest.LaLiga, *side, *side) {
	t.Helper()
	laliga := apitest.New(t)
	a := &side{account: "a", league: "LA", team: "TA", slot: "PA", listing: "MA"}
	b := &side{account: "b", league: "LB", team: "TB", slot: "PB", listing: "MB"}
	for _, one := range []*side{a, b} {
		one.member = laliga.Member(one.account, one.league, one.team)
	}
	return laliga, a, b
}

func (s *side) guard() *Guard {
	g := guardOf(s.member, 50_000_000)
	g.Squad = func() *Squad {
		return &Squad{Slots: map[string]bool{s.slot: true}, Listings: map[string]bool{s.listing: true}}
	}
	return g
}

func (s *side) args() Args {
	args := goldenArgs
	args.MarketID, args.PlayerTeamID = s.listing, s.slot
	return args
}

func operationNames() []string {
	names := make([]string, 0, len(Operations))
	for name := range Operations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestEveryOperationGoesWithItsMembersTokenLeagueAndTeam(t *testing.T) {
	laliga, a, b := twoSides(t)
	for _, name := range operationNames() {
		for _, pair := range [][2]*side{{a, b}, {b, a}} {
			own, other := pair[0], pair[1]
			g := own.guard()
			before := len(laliga.Writes())
			summary, err := g.Prepare(name, own.args(), goldenWho, true)
			if err != nil {
				t.Fatalf("%s for %s: prepare: %v", name, own.account, err)
			}
			if _, err := g.Confirm(summary.Token, true, false); err != nil {
				t.Fatalf("%s for %s: confirm: %v", name, own.account, err)
			}
			if _, err := g.Automatic(name, own.args(), goldenWho, true); err != nil {
				t.Fatalf("%s for %s: automatic: %v", name, own.account, err)
			}
			sent := laliga.Writes()[before:]
			if len(sent) < 2 {
				t.Fatalf("%s for %s sent %d writes", name, own.account, len(sent))
			}
			for _, call := range sent {
				body := string(call.Body)
				if call.Authorization != apitest.Bearer(own.account) {
					t.Errorf("%s for %s went with %q", name, own.account, call.Authorization)
				}
				if strings.Contains(call.Path, other.league) || strings.Contains(call.Path, other.team) ||
					strings.Contains(body, other.league) || strings.Contains(body, other.team) {
					t.Errorf("%s for %s carried the other's league or team: %s %s", name,
						own.account, call.Path, body)
				}
				if !strings.Contains(call.Path, "/"+own.league+"/") &&
					!strings.Contains(call.Path, "/"+own.team+"/") {
					t.Errorf("%s for %s is not addressed to its own league or team: %s", name,
						own.account, call.Path)
				}
			}
		}
	}
}

func TestCashIsReadOnlyFromItsOwnTeamWithItsOwnToken(t *testing.T) {
	laliga, a, b := twoSides(t)
	laliga.Answer("GET /api"+config.CMP+"/teams/TA/money", http.StatusOK, `{"teamMoney":1000}`)
	laliga.Answer("GET /api"+config.CMP+"/teams/TB/money", http.StatusOK, `{"teamMoney":9000}`)
	g := NewGuard(a.member)
	before := len(laliga.Calls())

	if cash, err := g.Cash("TA"); err != nil || cash != 1000 {
		t.Fatalf("A's cash: %d %v", cash, err)
	}
	if _, err := g.Cash(b.team); err == nil {
		t.Error("A's guard read B's cash")
	}
	summary, err := g.Prepare("bid", Args{MarketID: "M", Amount: 500}, Player{Name: "X"}, true)
	if err != nil || summary.CashBefore == nil || *summary.CashBefore != 1000 {
		t.Fatalf("prepare with A's cash: %+v %v", summary, err)
	}
	calls := laliga.Calls()[before:]
	if len(calls) != 2 {
		t.Fatalf("%d reads: %+v", len(calls), calls)
	}
	for _, call := range calls {
		if call.Method != http.MethodGet || call.Path != "/api"+config.CMP+"/teams/TA/money" ||
			call.Authorization != apitest.Bearer("a") {
			t.Errorf("cash read %s %s with %q", call.Method, call.Path, call.Authorization)
		}
	}
}

func TestATokenPreparedForOneMemberFailsForAnotherAndSurvives(t *testing.T) {
	laliga, a, b := twoSides(t)
	ga, gb := a.guard(), b.guard()
	summary, err := ga.PrepareFor("session", "bid", a.args(), goldenWho, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gb.ConfirmFor("session", summary.Token, true, false); err == nil {
		t.Error("B's guard confirmed A's token")
	}

	current := a.member
	switching := GuardOf(func() (*api.Member, error) { return current, nil })
	switching.Cash = ga.Cash
	switching.Squad = ga.Squad
	switched, err := switching.PrepareFor("session", "bid", a.args(), goldenWho, true)
	if err != nil {
		t.Fatal(err)
	}
	current = b.member
	if _, err := switching.ConfirmFor("session", switched.Token, true, false); err == nil ||
		!strings.Contains(err.Error(), "otro equipo") {
		t.Errorf("a token prepared for A confirmed for B: %v", err)
	}
	if sent := laliga.Writes(); len(sent) != 0 {
		t.Fatalf("a refused token sent %+v", sent)
	}

	current = a.member
	for _, confirm := range []func() (map[string]any, error){
		func() (map[string]any, error) { return ga.ConfirmFor("session", summary.Token, true, false) },
		func() (map[string]any, error) {
			return switching.ConfirmFor("session", switched.Token, true, false)
		},
	} {
		if _, err := confirm(); err != nil {
			t.Errorf("A's token was spent by the refusal: %v", err)
		}
	}
	for _, call := range laliga.Writes() {
		if call.Authorization != apitest.Bearer("a") || !strings.Contains(call.Path, "/LA/") {
			t.Errorf("A's bid went %s with %q", call.Path, call.Authorization)
		}
	}
}

func TestNothingIsDoneToAPlayerOrListingThatIsNotMine(t *testing.T) {
	laliga, a, b := twoSides(t)
	g := a.guard()
	g.Cash = NewGuard(a.member).Cash
	before := len(laliga.Calls())
	foreign := map[string]Args{
		"sell_to_market": {PlayerTeamID: b.slot, Amount: 1_000_000},
		"raise_clause":   {PlayerTeamID: b.slot, Amount: 1_000_000},
		"shield_player":  {PlayerTeamID: b.slot},
		"accept_offer":   {MarketID: b.listing, OfferID: "O", Amount: 1_000_000},
		"decline_offer":  {MarketID: b.listing, OfferID: "O"},
		"withdraw":       {MarketID: b.listing},
	}
	for name, args := range foreign {
		if _, err := g.Prepare(name, args, goldenWho, true); !errors.Is(err, ErrNotMine) {
			t.Errorf("%s prepared for somebody else's: %v", name, err)
		}
		if _, err := g.Automatic(name, args, goldenWho, true); !errors.Is(err, ErrNotMine) {
			t.Errorf("%s done unattended for somebody else's: %v", name, err)
		}
	}
	if calls := laliga.Calls()[before:]; len(calls) != 0 {
		t.Errorf("a refusal asked LaLiga first: %+v", calls)
	}

	sold := false
	g.Squad = func() *Squad {
		if sold {
			return &Squad{}
		}
		return &Squad{Slots: map[string]bool{a.slot: true}, Listings: map[string]bool{a.listing: true}}
	}
	summary, err := g.Prepare("withdraw", Args{MarketID: a.listing}, goldenWho, true)
	if err != nil {
		t.Fatal(err)
	}
	sold = true
	if _, err := g.Confirm(summary.Token, true, false); !errors.Is(err, ErrNotMine) {
		t.Errorf("confirmed for a listing gone since: %v", err)
	}
	g.Squad = nil
	if _, err := g.Automatic("sell_to_market", Args{PlayerTeamID: a.slot, Amount: 1},
		goldenWho, true); err == nil {
		t.Error("sold with no squad to check against")
	}
	for _, call := range laliga.Calls()[before:] {
		if call.Method != http.MethodGet {
			t.Errorf("sent %s %s", call.Method, call.Path)
		}
	}
}

func TestACallTamperedToAnotherLeagueOrTeamIsNeverSent(t *testing.T) {
	laliga, a, b := twoSides(t)
	built := func(name string) api.Call {
		call, err := Build(name, a.member, a.args())
		if err != nil {
			t.Fatal(err)
		}
		return api.Call(call)
	}
	bid, lineup, reward := built("bid"), built("save_lineup"), built("claim_daily_reward")
	tampered := map[string]api.Call{
		"league in the path": {Method: bid.Method, Body: bid.Body,
			Path: strings.Replace(bid.Path, "/LA/", "/LB/", 1)},
		"team in the path": {Method: lineup.Method, Body: lineup.Body,
			Path: strings.Replace(lineup.Path, "/TA/", "/TB/", 1)},
		"team in the body": {Method: reward.Method, Path: reward.Path,
			Body: map[string]any{"rewardedAdType": "dailyreward", "rewardedAd": 1, "teamId": "TB"}},
		"empty league": {Method: bid.Method, Body: bid.Body,
			Path: strings.Replace(bid.Path, "/LA/", "//", 1)},
		"neither league nor team": {Method: http.MethodPost, Body: bid.Body,
			Path: config.CMP + "/market/MA/bid"},
	}
	for what, call := range tampered {
		if _, err := a.member.Send(call); !errors.Is(err, api.ErrForeign) {
			t.Errorf("%s: %v", what, err)
		}
	}
	for _, call := range []api.Call{bid, lineup, reward} {
		if _, err := b.member.Send(call); !errors.Is(err, api.ErrForeign) {
			t.Errorf("B sent A's %s: %v", call.Path, err)
		}
	}
	if sent := laliga.Writes(); len(sent) != 0 {
		t.Fatalf("a refused call reached LaLiga: %+v", sent)
	}
	if _, err := a.member.Send(bid); err != nil || len(laliga.Writes()) != 1 {
		t.Errorf("A's own call: %v", err)
	}
}

func TestRehearsingOnOneGuardLeavesTheOtherSending(t *testing.T) {
	laliga, a, b := twoSides(t)
	ga, gb := a.guard(), b.guard()
	ga.Rehearsal = true
	for _, g := range []*Guard{ga, gb} {
		if _, err := g.Automatic("save_lineup", goldenArgs, goldenWho, true); err != nil {
			t.Fatal(err)
		}
	}
	sent := laliga.Writes()
	if len(sent) != 1 || sent[0].Authorization != apitest.Bearer("b") {
		t.Errorf("rehearsing on A changed what B sends: %+v", sent)
	}
}
