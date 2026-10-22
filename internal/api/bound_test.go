package api

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx/httpxtest"
)

// Accounts a and b each have a league of their own (LA, LB) and share LS, where the list of
// leagues leaves the team out and only the standings say whose is whose.
func twoAccounts(t *testing.T) *httpxtest.Fake {
	t.Helper()
	roots := []string{config.ConfigRoot, config.StateRoot, config.CacheRoot}
	t.Cleanup(func() { config.ConfigRoot, config.StateRoot, config.CacheRoot = roots[0], roots[1], roots[2] })
	dir := t.TempDir()
	config.ConfigRoot, config.StateRoot, config.CacheRoot = dir, dir, filepath.Join(dir, "cache")
	for account, token := range map[string]string{"a": "token-a", "b": "token-b"} {
		if err := auth.For(account).Save(&auth.Tokens{AccessToken: token,
			ExpiresOn: time.Now().Add(time.Hour).Unix()}); err != nil {
			t.Fatal(err)
		}
	}
	byBearer := func(answers map[string]string) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.WriteString(writer, answers[request.Header.Get("Authorization")])
		}
	}
	fake := httpxtest.New(t)
	fake.Handle("GET /api"+config.CMP+"/leagues", byBearer(map[string]string{
		"Bearer token-a": `[{"id":"LA","team":{"id":"TA"}},{"id":"LS"}]`,
		"Bearer token-b": `[{"id":"LB","team":{"id":"TB"}},{"id":"LS"}]`}))
	fake.Handle("GET /api/v4/user/me", byBearer(map[string]string{
		"Bearer token-a": `{"id":"ua"}`, "Bearer token-b": `{"id":"ub"}`}))
	fake.Answer("GET /api"+config.CMP+"/leagues/LS/standing", http.StatusOK,
		`[{"userId":"ua","teamId":"SA"},{"userId":"ub","teamId":"SB"}]`)
	fake.Answer("GET /api/", http.StatusOK, `{}`)
	t.Cleanup(httpx.SetTransport(fake))
	t.Cleanup(httpx.SetQueue(httpx.NewQueue(nil, httpx.DefaultLimits(), nil)))
	return fake
}

func memberOf(t *testing.T, account, league, team string) *Member {
	t.Helper()
	member, err := AccountOf(config.ProfileFor(account, league)).Member(league, team)
	if err != nil {
		t.Fatal(err)
	}
	return member
}

func readEverything(t *testing.T, member *Member) {
	t.Helper()
	league := member.League()
	for _, read := range []func() error{
		func() error { _, err := member.Money(0); return err },
		func() error { _, err := member.Favourites(0); return err },
		func() error { _, err := member.OwnLineup(0); return err },
		func() error { _, err := member.Offers("slot", 0); return err },
		func() error { _, err := member.Market(0, false); return err },
		func() error { _, err := member.MarketRaw(0, true); return err },
		func() error { _, err := member.DailyRewardStatus(); return err },
		func() error { _, err := league.Standings(0); return err },
		func() error { _, err := league.TeamSquad("rival", 0); return err },
		func() error { _, err := league.ActivityRaw(0, 0, true); return err },
		func() error { _, err := league.TeamLineupWeek("rival", 9, 0); return err },
	} {
		if err := read(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEveryRequestOfAMemberCarriesOnlyItsOwnTokenLeagueAndTeam(t *testing.T) {
	fake := twoAccounts(t)
	members := map[string]*Member{"a": memberOf(t, "a", "LA", "TA"), "b": memberOf(t, "b", "LB", "TB")}
	for account, member := range members {
		mine := "Bearer token-" + account
		own := strings.ToUpper(account)
		other := map[string]string{"A": "B", "B": "A"}[own]
		before := len(fake.Calls())
		readEverything(t, member)
		calls := fake.Calls()[before:]
		if len(calls) != 11 {
			t.Fatalf("%s: %d requests", account, len(calls))
		}
		for index, call := range calls {
			if call.Authorization != mine {
				t.Errorf("%s %s went with %q", account, call.Path, call.Authorization)
			}
			if strings.Contains(call.Path, "L"+other) || strings.Contains(call.Path, "T"+other) {
				t.Errorf("%s asked for the other's league or team: %s", account, call.Path)
			}
			if index < 7 && !strings.Contains(call.Path, "/L"+own+"/") &&
				!strings.Contains(call.Path, "/T"+own+"/") {
				t.Errorf("%s: a member read outside its own league and team: %s", account, call.Path)
			}
		}
	}
}

func TestGlobalReadsSendNoToken(t *testing.T) {
	fake := twoAccounts(t)
	member := memberOf(t, "a", "LA", "TA")
	before := len(fake.Calls())
	for _, global := range []*Global{NewGlobal(), member.Global(), member.Account().Global()} {
		var players []map[string]any
		for _, read := range []func() error{
			func() error { _, err := global.CurrentWeek(0); return err },
			func() error { _, err := global.Calendar(9, 0); return err },
			func() error { _, err := global.TeamsMaster(0); return err },
			func() error { _, err := global.Player("10", 0); return err },
			func() error { _, err := global.PlayerMarketValue("10", 0); return err },
			func() error { _, err := global.DailyRewards(0); return err },
			func() error { _, err := global.Formations(true, 0); return err },
			func() error { return global.RawList(config.CMP+"/players", 0, "players", &players) },
		} {
			if err := read(); err != nil {
				t.Fatal(err)
			}
		}
	}
	calls := fake.Calls()[before:]
	if len(calls) != 24 {
		t.Fatalf("%d requests", len(calls))
	}
	for _, call := range calls {
		if call.Authorization != "" {
			t.Errorf("%s went with %q", call.Path, call.Authorization)
		}
	}
}

func TestAnAccountIsAMemberOnlyWithItsOwnTeam(t *testing.T) {
	twoAccounts(t)
	a := AccountOf(config.ProfileFor("a", ""))
	for _, refused := range [][2]string{{"LA", "TB"}, {"LB", "TB"}, {"LS", "SB"}, {"LA", ""}} {
		if _, err := a.Member(refused[0], refused[1]); err == nil {
			t.Errorf("a became the member %v", refused)
		}
	}
	member, err := a.Member("LS", "SA")
	if err != nil || member.LeagueID() != "LS" || member.TeamID() != "SA" {
		t.Fatalf("the team found in the standings: %v %+v", err, member)
	}
}

func TestAMembersTurnInTheQueueReachesItsLeagueReads(t *testing.T) {
	fake := twoAccounts(t)
	member := memberOf(t, "a", "LA", "TA")
	fake.Handle("GET /api"+config.CMP+"/week/current", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "60")
		writer.WriteHeader(http.StatusTooManyRequests)
	})
	t.Cleanup(httpx.SetQueue(httpx.NewQueue(nil, httpx.DefaultLimits(), nil,
		httpx.LaLigaHosts()...)))
	var limited *httpx.RateLimited
	if _, err := member.Global().CurrentWeek(0); !errors.As(err, &limited) {
		t.Fatalf("the 429: %v", err)
	}
	var throttled *httpx.Throttled
	if _, err := member.At(httpx.Probe).League().Standings(0); !errors.As(err, &throttled) {
		t.Errorf("a probe read went out during the pause: %v", err)
	}
	if _, err := member.At(httpx.Write).League().Standings(0); err != nil {
		t.Errorf("a write's read was held: %v", err)
	}
}
