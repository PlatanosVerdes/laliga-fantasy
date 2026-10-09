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
		if seg := outcomeSeg(outcome, ""); seg.Icon != mark.Symbol || seg.Tip == "" {
			t.Errorf("%s: %+v", outcome, seg)
		}
	}
	for _, reason := range orderReasons {
		if !strings.Contains(IconSprite, `id="i-`+reason.Mark.Symbol+`"`) {
			t.Errorf("reason %q points at a missing symbol", reason.Words)
		}
	}
	if seg := outcomeSeg("algo_nuevo", ""); seg.Icon != "dot" || seg.Tip != "algo nuevo" {
		t.Errorf("unknown outcome falls back: %+v", seg)
	}
	if seg := outcomeSeg("cancelada", "Millou912 lo ha blindado"); seg.Icon != "shield" {
		t.Errorf("a cancelled order says why: %+v", seg)
	}
}

// A lost bid says who took him and for how much, when that is known.
func TestLostBidNamesTheWinner(t *testing.T) {
	seg := outcomeSeg("perdida", "ganó -papi— (30,0M)")
	if seg.Icon != "person" || seg.Tip != "perdida: ganó -papi— (30,0M)" {
		t.Errorf("perdida: %+v", seg)
	}
}
