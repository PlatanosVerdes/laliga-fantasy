package render

import (
	"strings"
	"testing"
	"time"
)

// The side game's nudge: only with an open round this server can vote in, gone once voted unless
// previewed, with the close as a live countdown.
func TestNecroReminderOnlyWhileUnvoted(t *testing.T) {
	document := decidingDocument()
	if document.necroReminder() != nil {
		t.Fatal("without a round there is no reminder")
	}
	document.NecroGameweek, document.NecroCanVote = 8, true
	if document.necroReminder() != nil {
		t.Error("a closed round has nothing to vote")
	}
	document.NecroOpen = true
	document.NecroClosesAt = time.Date(2999, 1, 1, 21, 0, 0, 0, time.UTC)
	remind := document.necroReminder()
	if remind == nil || remind.Week != 8 || remind.ClosesAt != "2999-01-01T21:00:00Z" ||
		!strings.Contains(asJSON(remind.Tail), `"href":"#necroporra"`) {
		t.Errorf("reminder: %+v", remind)
	}
	document.NecroChosen = []string{"1", "2"}
	if document.necroReminder() != nil {
		t.Error("voted: no reminder")
	}
	document.NecroPreview = true
	if document.necroReminder() == nil {
		t.Error("the preview forces it")
	}
	if view, ok := decidingDocument().NecroData(); ok || len(view.Main) != 0 {
		t.Error("no round, no tab")
	}
}

// Between rounds the tab stays, with your last vote and no toggles; open, the toggles return.
func TestNecroClosedShowsLastVote(t *testing.T) {
	document := universeWithRivals()
	document.NecroGameweek, document.NecroCanVote = 8, true
	document.NecroRoster = map[string]string{"cristian": "1", "tete": "2"}
	document.NecroLastRound, document.NecroLastPicks = 8, []string{"Villaone", "tete alejo"}
	view, ok := document.NecroData()
	if !ok {
		t.Fatal("a closed round still has a tab")
	}
	closed := asJSON(view.Main[0].Data)
	if !strings.Contains(closed, "Tu voto en la jornada 8: ") ||
		!strings.Contains(closed, "Villaone y tete alejo") || strings.Contains(closed, `"id":`) {
		t.Errorf("closed: %s", closed)
	}
	document.NecroOpen = true
	view, _ = document.NecroData()
	open := asJSON(view.Main[0].Data)
	if strings.Contains(open, "Tu voto en la jornada") || !strings.Contains(open, `"id":"1"`) {
		t.Errorf("open: %s", open)
	}
}

// The round you voted, while it is played: first to last, and every ballot read by the
// Constitution, now and projected. Villaone leads, so whoever named him owes an apology; tete
// alejo is last, so naming him is the point, and Villaone, leading and naming him, may denigrate.
func TestNecroLiveRoundReadsTheConstitution(t *testing.T) {
	manager := func(name string, points, projection float64, me bool) map[string]any {
		return map[string]any{"manager": name, "points": points, "projection": projection,
			"is_me": me, "waiting": 1.0}
	}
	document := Document{NecroLastRound: 8, NecroLastPicks: []string{"Villaone", "tete alejo"},
		NecroBallots: map[string][]string{
			"PlatanosVerdes": {"Villaone", "tete alejo"},
			"Villaone":       {"cristian1206", "tete alejo"},
			"cristian1206":   {"PlatanosVerdes", "tete alejo"},
		},
		Advice: map[string]any{"matchday": map[string]any{"week": 8.0, "live": true,
			"managers": []any{
				manager("PlatanosVerdes", 30, 60, true),
				manager("Villaone", 50, 55, false),
				manager("cristian1206", 40, 45, false),
				manager("tete alejo", 20, 50, false),
			}}}}
	view, ok := document.NecroData()
	if !ok {
		t.Fatal("a voted round in play has a tab")
	}
	teams := view.Main[0].Data.(Necro).Teams
	order := []string{}
	chips := map[string]string{}
	for _, team := range teams {
		order = append(order, team.Row.Name)
		chips[strings.TrimSuffix(team.Row.Name, " (tú)")] = asJSON(team.Row.Chips)
	}
	if strings.Join(order, ",") != "Villaone,cristian1206,PlatanosVerdes (tú),tete alejo" {
		t.Errorf("order: %v", order)
	}
	has := func(name, class string) bool { return strings.Contains(chips[name], class+`"`) }
	// You named Villaone, first now but projected second: the apology holds only now.
	if !has("PlatanosVerdes", "nc fading nc-perdon") {
		t.Errorf("apology now only: %s", chips["PlatanosVerdes"])
	}
	// tete alejo last now; projected, cristian1206 is last instead.
	if !has("PlatanosVerdes", "nc fading nc-punto") || !has("Villaone", "nc fading nc-denigra") {
		t.Errorf("point and denigration now only: %s / %s", chips["PlatanosVerdes"], chips["Villaone"])
	}
	if !has("tete alejo", "nc fading nc-desayuno") || !has("cristian1206", "nc proj nc-desayuno") {
		t.Errorf("breakfast now and projected: %s / %s", chips["tete alejo"], chips["cristian1206"])
	}
	if !strings.Contains(chips["PlatanosVerdes"], "te vota cristian1206") {
		t.Errorf("who named you: %s", chips["PlatanosVerdes"])
	}
	document.NecroOpen = true
	if _, live := document.necroLive(); live {
		t.Error("an open ballot is the voting view, not the live one")
	}
}

// A page the parser cannot read still shows the tab, with a banner saying so.
func TestNecroNoticeWhenTebasChanged(t *testing.T) {
	document := universeWithRivals()
	document.NecroGameweek, document.NecroProblem = 8, "changed"
	document.NecroRoster = map[string]string{"cristian": "1", "tete": "2"}
	view, ok := document.NecroData()
	if !ok || !strings.Contains(asJSON(view.Main[0].Data), "Tebas ha cambiado la página") {
		t.Errorf("no banner: %v", ok)
	}
}

// Breakfast walks up past the shielded: with the bottom two shielded the third from bottom brings
// it, and the shield shows as an icon of its own.
func TestNecroBreakfastSkipsTheShielded(t *testing.T) {
	manager := func(name string, points float64) map[string]any {
		return map[string]any{"manager": name, "points": points, "projection": points}
	}
	document := Document{NecroLastRound: 8, NecroLastPicks: []string{"A", "B"},
		NecroShields: map[string]int{"-papi—": 2, "JMjugon": 1},
		Advice: map[string]any{"matchday": map[string]any{"week": 8.0, "live": false,
			"managers": []any{manager("Villaone", 34), manager("JMjugon", 33),
				manager("PlatanosVerdes", 8), manager("-papi—", 4)}}}}
	view, _ := document.NecroData()
	for _, team := range view.Main[0].Data.(Necro).Teams {
		chips := asJSON(team.Row.Chips)
		breakfast := strings.Contains(chips, `nc nc-desayuno"`)
		if breakfast != (team.Row.Name == "PlatanosVerdes") {
			t.Errorf("%s breakfast=%v: %s", team.Row.Name, breakfast, chips)
		}
		if shielded := strings.Contains(chips, "nc-escudo"); shielded !=
			(team.Row.Name == "-papi—" || team.Row.Name == "JMjugon") {
			t.Errorf("%s shield=%v", team.Row.Name, shielded)
		}
	}
}
