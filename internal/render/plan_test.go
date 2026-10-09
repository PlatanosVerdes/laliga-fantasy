package render

import (
	"strings"
	"testing"
)

// A plan with no moves still carries its warnings, and "no hay once que alinear" is the one
// thing the eleven must never swallow.
func TestPlanWarningsSurviveAnEmptyPlan(t *testing.T) {
	html := asJSON(Document{Swaps: map[string]any{
		"moves":    []any{},
		"warnings": []string{"ahora mismo solo puedes alinear a 10"},
	}}.planWarnings())
	if !strings.Contains(html, "solo puedes alinear a 10") {
		t.Fatalf("the warning is gone:\n%s", html)
	}
}

// Rendered from a JSON dump the warnings arrive as []any, and a type assertion on []string
// would take the page down with it.
func TestPlanWarningsFromADump(t *testing.T) {
	html := asJSON(Document{Swaps: map[string]any{
		"moves":    []any{},
		"warnings": []any{"tienes el once justo"},
	}}.planWarnings())
	if !strings.Contains(html, "tienes el once justo") {
		t.Fatalf("the warning is gone:\n%s", html)
	}
}
