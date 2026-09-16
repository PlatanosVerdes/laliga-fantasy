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

// A matchday over is the only one whose squads are worth reconstructing, and it was the one
// that lost the way in: the "jugada" line overwrote the button instead of joining it.
func TestFinishedMatchdayKeepsItsSquadsButton(t *testing.T) {
	fixture := func(week int, state int) map[string]any {
		return map[string]any{"week": float64(week), "state": float64(state),
			"kickoff": "2026-08-15T19:00:00+02:00", "local": "ALA", "visitor": "GET",
			"local_id": "1", "visitor_id": "2", "local_score": 3.0, "visitor_score": 0.0}
	}
	done := MatchCalendar([]map[string]any{fixture(1, FinishedMatch)}, nil, nil)
	if !strings.Contains(done, `data-matchday="1"`) {
		t.Error("a finished matchday can still be opened: " + done)
	}
	if !strings.Contains(done, "jugada") {
		t.Error("and it still says it is over")
	}
	running := MatchCalendar([]map[string]any{fixture(2, FinishedMatch), fixture(2, 0)}, nil, nil)
	if !strings.Contains(running, `data-matchday="2"`) {
		t.Error("one still running keeps it too: " + running)
	}
}

// Which of these is a clausulazo you can pay today was a question answered by reading every row
// of the table, and the clausulazo is the move nobody can refuse.
func TestBargainsOfferTheClausulazosYouCanPay(t *testing.T) {
	row := func(name, route string, affordable bool) map[string]any {
		return map[string]any{"id": name, "name": name, "route": route, "value": 10_000_000.0,
			"entry_cost": 9_000_000.0, "position": "MED", "affordable": affordable}
	}
	document := Document{Money: map[string]any{"bargains": []any{
		row("Camello", "clausula", true),
		row("Pepelu", "clausula", true),
		row("Bartra", "clausula", false),
		row("Gueye", "oferta al dueño", true),
	}}}
	built := document.bargainsSection()

	if !strings.Contains(built, `data-only="clausula" data-only-count="2"`) {
		t.Errorf("dos clausulazos pagables, no mas: %s", built)
	}
	if !strings.Contains(built, `data-route="clausula" data-afford="1"`) ||
		!strings.Contains(built, `data-route="oferta al dueño" data-afford="1"`) {
		t.Error("cada fila dice por que via va y si el dinero esta")
	}
}

// Nothing to offer is no button: a switch that narrows a table to nothing is a dead end.
func TestBargainsWithoutAffordableClausulazosOffersNothing(t *testing.T) {
	document := Document{Money: map[string]any{"bargains": []any{
		map[string]any{"id": "1", "name": "Bartra", "route": "clausula", "value": 10_000_000.0,
			"entry_cost": 90_000_000.0, "affordable": false},
	}}}
	if built := document.bargainsSection(); strings.Contains(built, "data-only=") {
		t.Errorf("sin ninguno que puedas pagar no hay boton: %s", built)
	}
}
