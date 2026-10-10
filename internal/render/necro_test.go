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
