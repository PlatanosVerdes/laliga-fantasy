package render

import (
	"strings"
	"testing"
)

// Every outcome the panel can show has its own icon, drawn from the sprite, with its words as
// the tooltip; one it does not know still gets the fallback.
func TestEveryOutcomeHasAnIcon(t *testing.T) {
	for _, outcome := range []string{"aceptada", "rechazada", "perdida", "caducada", "pagada",
		"cumplida", "cancelada", "pagar_clausula", "esperando", "bloqueada", "sin_saldo",
		"sin_limite", "ninguna"} {
		mark, known := outcomeMarks[outcome]
		if !known {
			t.Errorf("%s has no icon", outcome)
			continue
		}
		if !strings.Contains(IconSprite, `id="i-`+mark.Symbol+`"`) {
			t.Errorf("%s points at i-%s, which the sprite lacks", outcome, mark.Symbol)
		}
		html := outcomeIcon(outcome, "")
		if !strings.Contains(html, `<use href="#i-`+mark.Symbol+`">`) || !strings.Contains(html, `data-tip="`) {
			t.Errorf("%s: %s", outcome, html)
		}
	}
	for _, reason := range orderReasons {
		if !strings.Contains(IconSprite, `id="i-`+reason.Mark.Symbol+`"`) {
			t.Errorf("reason %q points at a missing symbol", reason.Words)
		}
	}
	if html := outcomeIcon("algo_nuevo", ""); !strings.Contains(html, "#i-dot") ||
		!strings.Contains(html, `data-tip="algo nuevo"`) {
		t.Errorf("unknown outcome falls back: %s", html)
	}
	if html := outcomeIcon("cancelada", "Millou912 lo ha blindado"); !strings.Contains(html, "#i-shield") {
		t.Errorf("a cancelled order says why: %s", html)
	}
}

// A lost bid says who took him and for how much, when that is known.
func TestLostBidNamesTheWinner(t *testing.T) {
	html := outcomeIcon("perdida", "ganó -papi— (30,0M)")
	if !strings.Contains(html, "#i-person") || !strings.Contains(html, `data-tip="perdida: ganó -papi— (30,0M)"`) {
		t.Errorf("perdida: %s", html)
	}
}
