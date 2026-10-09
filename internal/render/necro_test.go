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
