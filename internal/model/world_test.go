package model

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/api/apitest"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/rules"
)

var update = flag.Bool("update", false, "rewrite testdata/universe.golden.json")

// p1 is injured only on his own page; T1 lists p5.
func fixtureLeague(t *testing.T) *apitest.LaLiga {
	t.Helper()
	laliga := apitest.New(t)
	cmp := "/api" + config.CMP
	answer := func(path, body string) { laliga.Answer("GET "+path, http.StatusOK, body) }

	answer(cmp+"/week/current", `{"weekNumber":3,"nextWeek":4}`)
	answer("/api/v3/teams-master", `[{"id":"1","name":"Club Uno","shortName":"UNO","slug":"club-uno"},
		{"id":"2","name":"Club Dos","shortName":"DOS","slug":"club-dos"}]`)
	answer(cmp+"/players", `[
		{"id":"1","nickname":"Uno","name":"Jugador Uno","positionId":1,"teamId":"1","marketValue":5000000,"points":30,"averagePoints":10,"lastSeasonPoints":120,"playerStatus":"ok"},
		{"id":"2","nickname":"Dos","positionId":2,"teamId":"1","marketValue":8000000,"points":12,"averagePoints":4,"lastSeasonPoints":0,"playerStatus":"doubtful"},
		{"id":"3","nickname":"Tres","positionId":3,"teamId":"2","marketValue":12000000,"points":21,"averagePoints":7,"lastSeasonPoints":150,"playerStatus":"ok"},
		{"id":"4","nickname":"Cuatro","positionId":4,"teamId":"2","marketValue":3000000,"points":6,"averagePoints":2,"lastSeasonPoints":40,"playerStatus":"ok"},
		{"id":"5","nickname":"Cinco","positionId":3,"teamId":"1","marketValue":9000000,"points":18,"averagePoints":6,"lastSeasonPoints":90,"playerStatus":"injured"},
		{"id":"6","nickname":"Seis","positionId":2,"teamId":"2","marketValue":1000000,"points":0,"averagePoints":0,"lastSeasonPoints":10},
		{"id":"9","nickname":"Mister","positionId":5,"teamId":"1","marketValue":1000000}]`)
	laliga.Handle("GET "+cmp+"/calendar", func(writer http.ResponseWriter, request *http.Request) {
		week, _ := strconv.Atoi(request.URL.Query().Get("weekNumber"))
		state, score := MatchPending, "null"
		if week < 3 {
			state, score = MatchFinished, "1"
		}
		_, _ = fmt.Fprintf(writer, `[{"id":"m%d","matchDate":"2026-09-%02dT18:00:00+00:00",`+
			`"localId":1,"visitorId":2,"matchState":%d,"localScore":%s,"visitorScore":%s}]`,
			week, week, state, score, score)
	})
	laliga.Handle("GET "+cmp+"/player/{id}", func(writer http.ResponseWriter, request *http.Request) {
		status := "ok"
		if request.PathValue("id") == "1" {
			status = "injured"
		}
		_, _ = writer.Write([]byte(`{"id":"` + request.PathValue("id") + `","playerStatus":"` + status + `"}`))
	})
	answer(cmp+"/player/{id}/market-value", `[{"date":"2020-01-01","marketValue":4000000},
		{"date":"2020-02-01","marketValue":6000000}]`)

	answer(cmp+"/leagues/L1/standing", `[
		{"id":"T1","userId":"u1","name":"Equipo A","manager":"Ana","points":40,"teamValue":30000000,"position":1},
		{"team":{"id":"T2","name":"Equipo B","teamValue":25000000,"manager":{"id":"u2","managerName":"Beto"},"points":35},"position":2}]`)
	answer(cmp+"/leagues/L1/teams/T1", `{"players":[
		{"playerTeamId":"pt1","buyoutClause":7000000,"buyoutClauseLockedEndTime":"2099-01-01T00:00:00+00:00","playerMaster":{"id":"1","marketValue":5000000,"images":{"transparent":{"256x256":"squad-1.png"}}}},
		{"playerTeamId":"pt2","buyoutClause":9000000,"isShielded":true,"shieldedEndDate":"2099-01-01T00:00:00+00:00","playerMaster":{"id":"2","marketValue":8000000}},
		{"playerTeamId":"pt5","buyoutClause":11000000,"playerMaster":{"id":"5","marketValue":9000000}}]}`)
	answer(cmp+"/leagues/L1/teams/T2", `{"players":[
		{"playerTeamId":"pt3","buyoutClause":15000000,"playerMaster":{"id":"3","marketValue":12000000}}]}`)
	laliga.Handle("GET "+cmp+"/leagues/L1/activity/{index}", func(writer http.ResponseWriter,
		request *http.Request) {
		if request.PathValue("index") != "0" {
			_, _ = writer.Write([]byte(`[]`))
			return
		}
		_, _ = writer.Write([]byte(`[
			{"activityTypeId":31,"user1Id":"u1","playerMasterId":"2","amount":8500000,"createdAt":"2020-01-15T10:00:00"},
			{"activityTypeId":1,"user1Id":"u2","user2Id":"u1","playerMasterId":"3","amount":14000000,"createdAt":"2020-01-20T10:00:00"},
			{"activityTypeId":6,"user1Id":"u1","amount":100000,"createdAt":"2020-01-21T10:00:00"}]`))
	})
	answer(cmp+"/teams/{team}/lineup/week/{week}", `{"formation":{"goalkeeper":[
		{"playerMaster":{"id":"1","nickname":"Uno","teamId":"1","positionId":1}}]}}`)

	answer(cmp+"/league/L1/market", `[
		{"id":"m4","discr":"marketPlayerLeague","salePrice":3100000,"numberOfBids":2,"expirationDate":"2099-01-01T00:00:00+00:00",
		 "bid":{"id":"b4","money":3300000,"status":"pending"},
		 "playerMaster":{"id":"4","marketValue":3000000,"images":{"transparent":{"256x256":"market-4.png"}}}},
		{"id":"m5","discr":"marketPlayerTeam","salePrice":9500000,"directOffer":true,
		 "sellerTeam":{"id":"T1","manager":{"managerName":"Ana"}},"playerMaster":{"id":"5","marketValue":9000000}}]`)
	answer(cmp+"/league/L1/playerTeam/pt5/offer", `[
		{"id":"o1","money":9800000,"status":"pending","buyerTeam":{"id":"T2","teamValue":25000000,"manager":{"managerName":"Beto"}}},
		{"id":"o2","money":9100000,"status":"pending","isFromMarket":true},
		{"id":"o3","money":9900000,"status":"rejected"}]`)
	answer(cmp+"/teams/T1/money", `{"teamMoney":4200000}`)
	answer(cmp+"/teams/T2/money", `{"teamMoney":900000}`)

	fichas, stored := readFicha, storedFicha
	t.Cleanup(func() { readFicha, storedFicha = fichas, stored })
	readFicha = func(slug, _ string, _ time.Duration) (map[string]any, error) {
		chance, week := 60, 3
		if slug != "dos" {
			chance = 90
		}
		return map[string]any{"start_probability": &chance, "start_week": &week}, nil
	}
	storedFicha = func(string) string { return "" }
	return laliga
}

var fixtureState = State{Starred: map[string]bool{"3": true}, Raids: map[string]bool{"3": true},
	House: rules.League{HoldDays: 5}}

func dump(t *testing.T, universe *Universe) []byte {
	t.Helper()
	blob, err := json.MarshalIndent(universe, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return hoursLeft.ReplaceAll(blob, []byte(`"$1": "now-dependent"`))
}

var hoursLeft = regexp.MustCompile(`"((?:clause_)?hours_left)": [-0-9.e+]+`)

// The golden was taken from Build before the split.
func TestComposeEqualsTheSingleBuild(t *testing.T) {
	member := fixtureLeague(t).Member("a", "L1", "T1")
	universe, err := Build(member, nil, fixtureState)
	if err != nil {
		t.Fatal(err)
	}
	got := dump(t, universe)
	golden := filepath.Join("testdata", "universe.golden.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("universe differs from %s:\n%s", golden, firstDifference(string(want), string(got)))
	}
}

func firstDifference(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for index := range min(len(wantLines), len(gotLines)) {
		if wantLines[index] != gotLines[index] {
			return "line " + strconv.Itoa(index+1) + "\n  want " + wantLines[index] + "\n  got  " + gotLines[index]
		}
	}
	return fmt.Sprintf("lengths %d and %d", len(wantLines), len(gotLines))
}

func TestComposeKeepsTheSharedWorldsIntact(t *testing.T) {
	laliga := fixtureLeague(t)
	a, b := laliga.Member("a", "L1", "T1"), laliga.Member("b", "L1", "T2")
	world, err := BuildGlobal(a.Global(), nil)
	if err != nil {
		t.Fatal(err)
	}
	league, err := BuildLeague(a.Global(), world, a.League())
	if err != nil {
		t.Fatal(err)
	}
	compose := func(member *api.Member) *Universe {
		t.Helper()
		universe, err := Compose(world, league, member, fixtureState)
		if err != nil {
			t.Fatal(err)
		}
		return universe
	}

	first := dump(t, compose(a))
	fromB := compose(b)
	if again := dump(t, compose(a)); string(again) != string(first) {
		t.Fatalf("composing for b changed a's universe:\n%s", firstDifference(string(first), string(again)))
	}

	byID := map[string]Player{}
	for _, player := range fromB.Players {
		byID[player.ID] = player
	}
	if !byID["3"].IsMine || byID["1"].IsMine {
		t.Fatalf("b's squad is T2: p3 mine %v, p1 mine %v", byID["3"].IsMine, byID["1"].IsMine)
	}
	if byID["1"].Status != "ok" || byID["1"].XPts == 0 {
		t.Fatalf("p1 is injured only on his own page, which only his owner reads: %+v", byID["1"])
	}
	if byID["2"].StartSource == FichaSource {
		t.Fatal("b read a ficha for a's player")
	}
	if fromB.LeagueTeams["T2"].CashIsEstimate || fromB.LeagueTeams["T2"].EstimatedCash != 900000 {
		t.Fatalf("b's own cash is measured: %+v", fromB.LeagueTeams["T2"])
	}
	if !league.Teams["T1"].CashIsEstimate || league.Teams["T1"].EstimatedCash != 0 {
		t.Fatalf("the league's teams were written to: %+v", league.Teams["T1"])
	}
	if world.rows[0].Status != "ok" || world.rows[0].IsMine || world.rows[0].Owner != nil {
		t.Fatalf("the global rows were written to: %+v", world.rows[0])
	}
}
