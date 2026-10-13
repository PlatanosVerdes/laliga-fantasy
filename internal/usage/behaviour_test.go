package usage

import (
	"testing"
	"time"
)

func at(minute int) time.Time { return time.Date(2026, 10, 13, 10, minute, 0, 0, time.UTC) }

// One visit with every stumble the report is for: a bounce, a hop, a round trip, a Back, an
// abandoned dialog, a card closed at once and a dead click.
func TestBehaviourFindsWhereAVisitStumbles(t *testing.T) {
	visit := "s1"
	events := []Event{
		{At: at(0), Kind: "tab", What: "rivales", Seconds: 2, Session: visit},
		{At: at(1), Kind: "tab", What: "liga", Seconds: 40, Session: visit},
		{At: at(2), Kind: "tab", What: "comprar", Seconds: 60, Session: visit},
		{At: at(3), Kind: "tab", What: "clausulas", Seconds: 4, Session: visit},
		{At: at(4), Kind: "tab", What: "comprar", Seconds: 90, Session: visit},
		{At: at(5), Kind: "nav", What: "#comprar", Label: "#comprar/jugador/7", Session: visit},
		{At: at(6), Kind: "op", What: "abrir decline_offer", Session: visit},
		{At: at(6), Kind: "op", What: "cancelar decline_offer", Session: visit},
		{At: at(7), Kind: "drawer", What: "jugador", Seconds: 1, Session: visit},
		{At: at(8), Kind: "dead", What: "pos", Where: "plantilla", Session: visit},
		// An older event without a session, an hour later: a second visit.
		{At: at(70), Kind: "tab", What: "decidir", Seconds: 30},
	}
	got := Behave(events)
	if got.Sessions != 2 {
		t.Errorf("sessions = %d, want 2", got.Sessions)
	}
	want := map[string][]Count{
		"hops":  {{Name: "rivales → liga", Times: 1}},
		"trips": {{Name: "comprar → clausulas → comprar", Times: 1}},
		"backs": {{Name: "#comprar/jugador/7 → #comprar", Times: 1}},
		"dead":  {{Name: "plantilla · pos", Times: 1}},
	}
	for name, rows := range map[string][]Count{"hops": got.Hops, "trips": got.RoundTrips,
		"backs": got.Backs, "dead": got.Dead} {
		if len(rows) != 1 || rows[0].Name != want[name][0].Name {
			t.Errorf("%s = %+v, want %+v", name, rows, want[name])
		}
	}
	if len(got.Dialogs) != 1 || got.Dialogs[0].Opened != 1 || got.Dialogs[0].Cancelled != 1 {
		t.Errorf("dialogs = %+v", got.Dialogs)
	}
	if len(got.Drawers) != 1 || got.Drawers[0].Quick != 1 {
		t.Errorf("drawers = %+v", got.Drawers)
	}
	for _, tab := range got.Tabs {
		if tab.Name == "rivales" && tab.Bounces != 1 {
			t.Errorf("rivales should count one bounce: %+v", tab)
		}
		if tab.Name == "comprar" && (tab.Visits != 2 || tab.Median != 75) {
			t.Errorf("comprar = %+v", tab)
		}
	}
}

// A tab logged twice in a row is a reload, not a hop or a round trip.
func TestARepeatedTabIsNotAMove(t *testing.T) {
	events := []Event{
		{At: at(0), Kind: "tab", What: "decidir", Seconds: 1, Session: "s"},
		{At: at(1), Kind: "tab", What: "decidir", Seconds: 2, Session: "s"},
		{At: at(2), Kind: "tab", What: "decidir", Seconds: 20, Session: "s"},
	}
	if got := Behave(events); len(got.Hops) != 0 || len(got.RoundTrips) != 0 {
		t.Errorf("a reload read as a move: %+v %+v", got.Hops, got.RoundTrips)
	}
}
