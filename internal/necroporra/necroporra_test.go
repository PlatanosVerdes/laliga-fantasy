package necroporra

import (
	"reflect"
	"testing"
	"time"
)

// Between rounds the page has no ballot, only the past rounds; your row there is the one
// tinted with --board-you.
func TestParseStateClosedReadsYourLastVote(t *testing.T) {
	body := `"children":"Nothing open"` + `"children":"Past rounds"` + `["$","$L2c",null,{"gameweeks":[5,6,7,8],"selected":8,"basePath":"/necroporra"}]` +
		`["$","li","1",{"children":[["$","div",null,{"children":[["$","span",null,{"className":"flex min-w-0 items-baseline gap-1.5","style":{"color":"$undefined"},"children":[["$","span",null,{"className":"truncate","children":"Millou912"}],null,null]}],["$","span",null,{"className":"min-w-0 text-right","style":{"color":"$undefined"},"children":["JMjugon, Villaone",""]}]]}]]}]` +
		`["$","li","2",{"children":[["$","div",null,{"children":[["$","span",null,{"className":"flex min-w-0 items-baseline gap-1.5","style":{"color":"var(--board-you)"},"children":[["$","span",null,{"className":"truncate","children":"PlatanosVerdes"}],null,null]}],["$","span",null,{"className":"min-w-0 text-right","style":{"color":"$undefined"},"children":["Villaone, tete alejo",""]}]]}]]}]`
	state := parseState(body, State{Gameweek: 8})
	if state.Open || state.LastRound != 8 || state.Problem != "" ||
		!reflect.DeepEqual(state.LastPicks, []string{"Villaone", "tete alejo"}) {
		t.Errorf("state: %+v", state)
	}
	open := parseState(`"gameweek":9,"teams":[{"id":"1","managerName"`, State{Gameweek: 8})
	if !open.Open || open.Gameweek != 9 {
		t.Errorf("open: %+v", open)
	}
}

// A page the parser no longer recognises, and a session bounced to the login, are said apart:
// one needs the code fixed, the other a fresh cookie.
func TestParseStateNamesWhatWentWrong(t *testing.T) {
	if got := parseState(`6:E{"digest":"NEXT_REDIRECT;replace;/login;307;"}`, State{}).Problem; got != ProblemLoggedOut {
		t.Errorf("login redirect: %q", got)
	}
	if got := parseState(`<html>a brand new necroporra</html>`, State{}).Problem; got != ProblemChanged {
		t.Errorf("unknown page: %q", got)
	}
	if got := parseState(`"children":"Nothing open"`, State{}).Problem; got != "" {
		t.Errorf("between rounds with no past round yet is fine: %q", got)
	}
}

func TestParseShields(t *testing.T) {
	body := `"children":"JMjugon"}],null,null,["$","span",null,{"aria-label":"Shielded, 1 round left","x":1}` +
		`"children":"tete alejo"}],null,null,["$","span",null,{"aria-label":"Shielded, 3 rounds left"}`
	got := parseShields(body)
	if got["JMjugon"] != 1 || got["tete alejo"] != 3 || len(got) != 2 {
		t.Errorf("shields: %v", got)
	}
}

func TestStateTTLReadsRarelyUntilTheRoundCloses(t *testing.T) {
	closes := time.Date(2026, 10, 30, 21, 0, 0, 0, time.UTC)
	open := State{Open: true, ClosesAt: closes}
	voted := State{Open: true, ClosesAt: closes, Chosen: []string{"1", "2"}}
	cases := []struct {
		name     string
		cached   State
		cachedAt time.Time
		now      time.Time
		want     time.Duration
	}{
		{"between rounds", State{}, closes, closes.Add(time.Minute), idleTTL},
		{"days before closing", open, closes.Add(-48 * time.Hour), closes.Add(-47 * time.Hour), idleTTL},
		{"wakes as the window opens", open, closes.Add(-3 * time.Hour), closes.Add(-150 * time.Minute), time.Hour},
		{"inside the window, no vote", open, closes.Add(-time.Hour), closes.Add(-time.Hour), closingTTL},
		{"inside the window, voted", voted, closes.Add(-time.Hour), closes.Add(-time.Hour), time.Hour},
		{"read before closing, now closed", voted, closes.Add(-time.Hour), closes.Add(time.Minute), time.Hour},
		{"read after closing", open, closes.Add(time.Minute), closes.Add(2 * time.Minute), idleTTL},
	}
	for _, c := range cases {
		if got := stateTTL(c.cached, c.cachedAt, c.now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
