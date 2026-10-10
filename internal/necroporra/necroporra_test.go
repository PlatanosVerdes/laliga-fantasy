package necroporra

import (
	"reflect"
	"testing"
)

// Between rounds the page has no ballot, only the past rounds; your row there is the one
// tinted with --board-you.
func TestParseStateClosedReadsYourLastVote(t *testing.T) {
	body := `["$","$L2c",null,{"gameweeks":[5,6,7,8],"selected":8,"basePath":"/necroporra"}]` +
		`["$","li","1",{"children":[["$","div",null,{"children":[["$","span",null,{"className":"flex min-w-0 items-baseline gap-1.5","style":{"color":"$undefined"},"children":[["$","span",null,{"className":"truncate","children":"Millou912"}],null,null]}],["$","span",null,{"className":"min-w-0 text-right","style":{"color":"$undefined"},"children":["JMjugon, Villaone",""]}]]}]]}]` +
		`["$","li","2",{"children":[["$","div",null,{"children":[["$","span",null,{"className":"flex min-w-0 items-baseline gap-1.5","style":{"color":"var(--board-you)"},"children":[["$","span",null,{"className":"truncate","children":"PlatanosVerdes"}],null,null]}],["$","span",null,{"className":"min-w-0 text-right","style":{"color":"$undefined"},"children":["Villaone, tete alejo",""]}]]}]]}]`
	state := parseState(body, State{Gameweek: 8})
	if state.Open || state.LastRound != 8 ||
		!reflect.DeepEqual(state.LastPicks, []string{"Villaone", "tete alejo"}) {
		t.Errorf("state: %+v", state)
	}
	open := parseState(`"gameweek":9,"teams":[{"id":"1","managerName"`, State{Gameweek: 8})
	if !open.Open || open.Gameweek != 9 {
		t.Errorf("open: %+v", open)
	}
}
