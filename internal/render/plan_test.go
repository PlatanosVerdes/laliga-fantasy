package render

import (
	"strings"
	"testing"
)

// A plan with no moves used to swallow its own warnings, and "no hay once que alinear" is the
// one thing this section must never swallow.
func TestPlanWarningsSurviveAnEmptyPlan(t *testing.T) {
	html := SwapPlan(map[string]any{
		"moves":    []any{},
		"warnings": []string{"ahora mismo solo puedes alinear a 10"},
	})
	if !strings.Contains(html, "solo puedes alinear a 10") {
		t.Fatalf("the warning is gone:\n%s", html)
	}
}

// A signing has nobody leaving. Drawn as a pair it printed an empty card and a sale price for
// a sale that never happens.
func TestPlanDrawsASigningAsAnEmptySlot(t *testing.T) {
	html := SwapPlan(map[string]any{
		"moves": []any{map[string]any{
			"in": map[string]any{"id": "9", "name": "Un delantero", "position": "DEL",
				"position_id": 4.0, "xpts": 4.0},
			"cost": 2_000_000.0, "net": 2_000_000.0, "gain": 4.0,
			"position": "delantero",
			"why":      "hueco en el once",
		}},
		"xpts_before": 28.0, "xpts_after": 32.0,
		"cash_before": 10_000_000.0, "cash_after": 8_000_000.0,
		"shape":    "4-4-2",
		"warnings": []string{},
	})
	for _, want := range []string{"plan-empty", "delantero sin cubrir", "hueco en el once",
		"con 4-4-2", "+4.00 xPts"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
	// No sale price, because nothing was sold.
	if strings.Contains(html, "vendiendolo") {
		t.Error("a signing printed a sale note")
	}
}

// Rendered from a JSON dump the warnings arrive as []any, and a type assertion on []string
// would take the page down with it.
func TestPlanWarningsFromADump(t *testing.T) {
	html := SwapPlan(map[string]any{
		"moves":    []any{},
		"warnings": []any{"tienes el once justo"},
	})
	if !strings.Contains(html, "tienes el once justo") {
		t.Fatalf("the warning is gone:\n%s", html)
	}
}
