package model

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

func stubFichas(t *testing.T, pages map[string]map[string]any, fail map[string]error) *[]string {
	t.Helper()
	var mu sync.Mutex
	asked := []string{}
	before, beforeStored := readFicha, storedFicha
	readFicha = func(slug, ffID string, ttl time.Duration) (map[string]any, error) {
		mu.Lock()
		asked = append(asked, slug)
		mu.Unlock()
		if err := fail[slug]; err != nil {
			return nil, err
		}
		return pages[slug], nil
	}
	storedFicha = func(string) string { return "" }
	t.Cleanup(func() { readFicha, storedFicha = before, beforeStored })
	return &asked
}

func pct(value int) *int { return &value }

func squadEntry(id, name string) map[string]any {
	return map[string]any{"id": id, "nickname": name, "positionId": 2.0}
}

// The market list misses some of my players; their own page has the odds, and only they are read.
func TestMySquadMissingOddsAreReadFromTheirPage(t *testing.T) {
	asked := stubFichas(t, map[string]map[string]any{
		"asier-osambela": {"start_probability": pct(40), "start_week": pct(10)},
	}, map[string]error{"valentin-gomez": errors.New("boom")})
	listed := 80.0
	raw := []map[string]any{squadEntry("1", "Asier Osambela"), squadEntry("2", "Valentín Gómez"),
		squadEntry("3", "Listed"), squadEntry("4", "Not mine")}
	ownership := map[string]slot{"1": {TeamID: "me"}, "2": {TeamID: "me"}, "3": {TeamID: "me"},
		"4": {TeamID: "rival"}}
	bridge := &Bridge{Trends: map[string]Trend{"3": {StartProbability: &listed}}}

	odds := squadOdds(raw, ownership, "me", bridge, time.Hour)
	if len(*asked) != 2 {
		t.Errorf("only my players without odds are read: %v", *asked)
	}
	if got := odds["1"]; got == nil || got.Probability != 40 || got.Week == nil || *got.Week != 10 {
		t.Fatalf("odds from his page: %+v", got)
	}
	if odds["2"] != nil {
		t.Error("a page that fails leaves the odds empty")
	}

	var filled, failed, kept Player
	filled.applyOdds(nil, odds["1"])
	if filled.StartSource != FichaSource || *filled.StartProbability != 40 {
		t.Errorf("source and value: %+v", filled)
	}
	failed.applyOdds(nil, odds["2"])
	if failed.StartProbability != nil {
		t.Error("nothing invented")
	}
	trend := bridge.Trends["3"]
	kept.applyOdds(&trend, &Odds{Probability: 10})
	if *kept.StartProbability != 80 || kept.StartSource != "" {
		t.Errorf("the market list's value wins: %+v", kept)
	}
}

// xPts are computed with the odds read from the page, not as an unknown.
func TestXPtsUseTheOddsFromThePage(t *testing.T) {
	base := Player{Available: true, BaseWeek: 5, FixtureFactor: 1}
	unknown, filled := base, base
	unknown.score("ok")
	filled.applyOdds(nil, &Odds{Probability: 30})
	filled.score("ok")
	want := 5 * Availability(filled.StartProbability)
	if filled.XPts != want || filled.XPts == unknown.XPts {
		t.Errorf("xPts %v, want %v (unknown gave %v)", filled.XPts, want, unknown.XPts)
	}
}

// Rate limited, the rest of the squad waits for the next build instead of making it worse.
func TestSquadOddsStopOnRateLimit(t *testing.T) {
	limited := map[string]error{}
	raw := []map[string]any{}
	ownership := map[string]slot{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		limited[name] = &httpx.RateLimited{URL: name}
		raw = append(raw, squadEntry(name, name))
		ownership[name] = slot{TeamID: "me"}
	}
	asked := stubFichas(t, nil, limited)
	if odds := squadOdds(raw, ownership, "me", nil, time.Hour); len(odds) != 0 {
		t.Errorf("nothing read: %v", odds)
	}
	if len(*asked) > oddsWorkers {
		t.Errorf("stopped after the first answers, asked %d", len(*asked))
	}
}
