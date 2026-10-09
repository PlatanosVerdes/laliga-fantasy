package render

import (
	"strings"
	"testing"
	"time"
)

// At the start of the season every clause in the league opens the same morning, so a day
// already gone filled the calendar and it never reached a date anybody could still act on.
func TestCalendarStartsAtTheCurrentDay(t *testing.T) {
	stamp := func(shift time.Duration) string { return time.Now().Add(shift).Format(time.RFC3339) }
	player := func(name, until string) map[string]any {
		return map[string]any{"id": name, "name": name, "owner": "x", "clause_locked": true,
			"clause_locked_until": until, "clause": 5_000_000.0, "position": "MED", "is_mine": true}
	}
	doc := Document{Universe: map[string]any{"players": []any{
		player("Agosto", stamp(-48*time.Hour)), player("Quagliata", stamp(30*time.Hour)),
		player("Lejos", stamp(9*24*time.Hour))}},
		Advice: map[string]any{"budget": 10_000_000.0}}
	page := asJSON(doc.calendarBlock().Data)
	if strings.Contains(page, "Agosto") || strings.Contains(page, "Lejos") {
		t.Errorf("only the next seven days: %s", page)
	}
	if !strings.Contains(page, "Quagliata") {
		t.Errorf("what is coming stays: %s", page)
	}
}

// The league log is one list the page can reorder: one rail, the toggle, and every row
// carrying its size, the prize counted as none.
func TestFeedIsOneSortableList(t *testing.T) {
	events := []map[string]any{}
	for i := 0; i < 9; i++ {
		events = append(events, map[string]any{"kind": "compra", "amount": float64(i) * 1e6,
			"player": "P", "player_id": "1", "date": "2026-10-07T19:00:00"})
	}
	events = append(events, map[string]any{"kind": "recompensa", "amount": 4.8e6,
		"date": "2026-10-06T03:00:00"})
	feed := feedOf(events, nil, nil, "")
	if !feed.Sortable || len(feed.Lines) != 10 {
		t.Errorf("una sola lista, ordenable: %+v", feed)
	}
	if feed.Lines[8].Size != 8000000 {
		t.Error("falta el importe de cada fila")
	}
	if feed.Lines[9].Size != 0 {
		t.Error("el premio de la jornada no cuenta como operacion")
	}
}

// A shield is not a sale: it reads as one, with no amount, and the notices that are not
// movements stay out of the log; an unknown type still gets a word, not "tipo N".
func TestFeedNamesShieldsAndHidesNotices(t *testing.T) {
	block := Document{Universe: map[string]any{"activity": []any{
		map[string]any{"type_id": 4.0, "kind": "tipo 4", "player": "El Hilali", "player_id": "9",
			"seller": "LamineTheTuareg", "actor": "LamineTheTuareg", "user1": "u1",
			"date": "2026-10-08T16:36:00"},
		map[string]any{"type_id": 7.0, "kind": "tipo 7", "actor": "X", "date": "2026-10-08T10:00:00"},
		map[string]any{"type_id": 10.0, "kind": "tipo 10", "date": "2026-10-08T09:00:00"},
		map[string]any{"type_id": 12.0, "kind": "tipo 12", "player": "Raro", "player_id": "8",
			"date": "2026-10-08T08:00:00"},
	}}}.feedBlock()
	feed, _ := block.Data.(Feed)
	page := asJSON(feed)
	if feed.Lines[0].Kind != "blindaje" || !strings.Contains(page, "blindado por") ||
		strings.Contains(page, "vendido por") || !feed.Lines[0].Quiet {
		t.Errorf("the shield: %s", page)
	}
	if strings.Contains(page, "tipo ") || feed.Lines[1].Kind != "movimiento" {
		t.Errorf("no 'tipo N' left: %s", page)
	}
	if len(feed.Lines) != 2 {
		t.Errorf("types 7 and 10 are not movements: %s", page)
	}
}
