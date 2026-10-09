package render

import (
	"strings"
	"testing"
)

// Your own listings read from your side. Unai López advertised at 14.13M on a 14.92M value is
// 0.95x: a bargain to whoever buys him, and the same section's note calls him under market
// value in the line above, which is what the column used to contradict.
func TestRatioBadgeReadsFromTheSideYouAreOn(t *testing.T) {
	ratio := 0.947
	cases := []struct {
		side Side
		want string
	}{
		{Paying, "chollo"},
		{Asking, "por debajo"},
		{Offered, "por debajo"},
	}
	for _, one := range cases {
		if got := RatioBadge(&ratio, one.side); !strings.Contains(got, one.want) {
			t.Errorf("0.95x from side %d: want %q, got %s", one.side, one.want, got)
		}
	}
}

// The step is at 1.00 on purpose: the note above the table warns about anything under it.
func TestAskingStepsAtValue(t *testing.T) {
	for _, one := range []struct {
		ratio float64
		want  string
	}{
		{1.30, "por encima"},
		{1.05, "con margen"},
		{1.00, "a valor"},
		{0.999, "por debajo"},
		{0.85, "lo regalas"},
	} {
		ratio := one.ratio
		if got := RatioBadge(&ratio, Asking); !strings.Contains(got, one.want) {
			t.Errorf("%.3fx asking: want %q, got %s", one.ratio, one.want, got)
		}
	}
}

// At the start of the season every clause in the league opens the same morning, so that one day
// filled all eight cards and the calendar never reached a date anybody could still act on.
func TestCalendarStartsAtTheCurrentDay(t *testing.T) {
	entry := func(name, unlock string) map[string]any {
		return map[string]any{"id": name, "name": name, "unlock_at": unlock,
			"clause": 5_000_000.0, "position": "MED"}
	}
	entries := []map[string]any{}
	for _, day := range []string{"25", "26", "27", "28", "29", "30", "31"} {
		entries = append(entries, entry("agosto"+day, "2026-08-"+day+"T19:00:00+02:00"))
	}
	entries = append(entries,
		entry("Hoy", "2026-09-17T19:02:00+02:00"),
		entry("Quagliata", "2026-09-24T13:37:00+02:00"))

	built := Calendar(entries, 10_000_000, "2026-09-17")
	if strings.Contains(built, "agosto25") {
		t.Errorf("un dia ya pasado no ocupa una tarjeta: %s", built)
	}
	if !strings.Contains(built, "Hoy") || !strings.Contains(built, "Quagliata") {
		t.Errorf("hoy y lo que viene despues si: %s", built)
	}
	if !strings.Contains(built, "17 sep") {
		t.Errorf("y la primera tarjeta es la de hoy: %s", built)
	}
}

// With no day to compare against, the calendar is better showing every date it knows than
// deciding on its own that none of them counts.
func TestCalendarWithoutADayShowsEverything(t *testing.T) {
	entries := []map[string]any{{"id": "1", "name": "Bartra",
		"unlock_at": "2026-08-25T19:00:00+02:00", "clause": 5_000_000.0, "position": "DEF"}}
	if built := Calendar(entries, 10_000_000, ""); !strings.Contains(built, "Bartra") {
		t.Errorf("sin fecha de corte se pinta todo: %s", built)
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
	feed := FeedOf(events)
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
	feed := FeedOf([]map[string]any{
		{"type_id": 4.0, "kind": "tipo 4", "player": "El Hilali", "player_id": "9",
			"seller": "LamineTheTuareg", "actor": "LamineTheTuareg", "user1": "u1",
			"date": "2026-10-08T16:36:00"},
		{"type_id": 7.0, "kind": "tipo 7", "actor": "X", "date": "2026-10-08T10:00:00"},
		{"type_id": 10.0, "kind": "tipo 10", "date": "2026-10-08T09:00:00"},
		{"type_id": 12.0, "kind": "tipo 12", "player": "Raro", "player_id": "8",
			"date": "2026-10-08T08:00:00"},
	})
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
