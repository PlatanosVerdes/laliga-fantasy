package render

import "strings"

// The outcome icons: small stroke drawings in the text's colour, defined once in the page and
// reused, so every way a bid, an offer or a standing order can end has a mark of its own.

// IconSprite is the set of symbols, put once at the top of the page.
const IconSprite = `<svg width="0" height="0" style="position:absolute" aria-hidden="true">` +
	`<symbol id="i-check" viewBox="0 0 16 16"><path d="M3 8.5l3 3 7-7"/></symbol>` +
	`<symbol id="i-slash" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6"/><path d="M3.8 12.2l8.4-8.4"/></symbol>` +
	`<symbol id="i-person" viewBox="0 0 16 16"><circle cx="8" cy="5.3" r="2.7"/><path d="M2.8 14c.5-3.1 2.6-4.9 5.2-4.9s4.7 1.8 5.2 4.9"/></symbol>` +
	`<symbol id="i-clock" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6"/><path d="M8 4.6V8l2.4 1.5"/></symbol>` +
	`<symbol id="i-coin" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6"/><path d="M10.2 5.6a2.7 2.7 0 1 0 0 4.8M5.2 7.3h3.6M5.2 8.8h3.6"/></symbol>` +
	`<symbol id="i-x" viewBox="0 0 16 16"><path d="M4.5 4.5l7 7M11.5 4.5l-7 7"/></symbol>` +
	`<symbol id="i-up" viewBox="0 0 16 16"><path d="M8 13.5V3M4.2 6.8L8 3l3.8 3.8"/></symbol>` +
	`<symbol id="i-shield" viewBox="0 0 16 16"><path d="M8 1.8l5 2v3.7c0 3.1-2.1 5.4-5 6.7-2.9-1.3-5-3.6-5-6.7V3.8z"/></symbol>` +
	`<symbol id="i-swap" viewBox="0 0 16 16"><path d="M2.5 5.5h10l-2.5-2.5M13.5 10.5h-10l2.5 2.5"/></symbol>` +
	`<symbol id="i-wallet" viewBox="0 0 16 16"><rect x="2" y="4" width="12" height="9" rx="1.5"/><path d="M10.5 8.5h1.5M4 4l6-2"/></symbol>` +
	`<symbol id="i-alert" viewBox="0 0 16 16"><path d="M8 2.2l6.2 11H1.8z"/><path d="M8 6.6v3M8 11.4v.1"/></symbol>` +
	`<symbol id="i-dot" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6"/><circle cx="8" cy="8" r="1.5" fill="currentColor"/></symbol>` +
	`</svg>`

type outcomeMark struct{ Symbol, Tone, Label string }

// outcomeMarks are every outcome the panel can show: how a bid or offer ended, what became of a
// standing order, and the state of a scheduled clausulazo.
var outcomeMarks = map[string]outcomeMark{
	// Bids and offers.
	"aceptada":  {"check", "good", "aceptada"},
	"rechazada": {"slash", "bad", "rechazada"},
	"perdida":   {"person", "warn", "perdida: se lo llevó otro"},
	"caducada":  {"clock", "muted", "caducada"},
	// Standing orders.
	"pagada":    {"coin", "good", "pagada"},
	"cumplida":  {"check", "good", "cumplida"},
	"cancelada": {"x", "muted", "cancelada"},
	// Scheduled clausulazos.
	"pagar_clausula": {"coin", "good", "se paga ahora"},
	"esperando":      {"clock", "muted", "esperando a que se abra"},
	"bloqueada":      {"shield", "muted", "blindado"},
	"sin_saldo":      {"wallet", "warn", "sin saldo"},
	"sin_limite":     {"alert", "warn", "sin límite escrito"},
	"ninguna":        {"dot", "muted", "nada que hacer"},
}

// orderReasons say more than "cancelada" when the reason is known.
var orderReasons = []struct {
	Words string
	Mark  outcomeMark
}{
	{"blind", outcomeMark{"shield", "muted", "cancelada: lo blindaron"}},
	{"subio", outcomeMark{"up", "warn", "cancelada: la cláusula pasó tu límite"}},
	{"limite", outcomeMark{"up", "warn", "cancelada: la cláusula pasó tu límite"}},
	{"ya no lo tiene nadie", outcomeMark{"swap", "muted", "cancelada: cambió de dueño"}},
	{"ya no esta donde estaba", outcomeMark{"swap", "muted", "cancelada: cambió de dueño"}},
	{"ya es tuyo", outcomeMark{"check", "good", "cumplida: ya es tuyo"}},
}

// outcomeIcon is the mark of an outcome, with its words as the tooltip. An outcome the code does
// not know gets the plain fallback rather than no mark at all.
func outcomeIcon(outcome, why string) string {
	mark := outcomeMarkOf(outcome, why)
	return `<span class="oi oi-` + mark.Tone + `" data-tip="` + Esc(mark.Label) + `">` +
		`<svg class="ic" aria-hidden="true"><use href="#i-` + mark.Symbol + `"></use></svg></span>`
}

func outcomeMarkOf(outcome, why string) outcomeMark {
	mark, known := outcomeMarks[outcome]
	if !known {
		mark = outcomeMark{"dot", "muted", strings.ReplaceAll(outcome, "_", " ")}
	}
	if outcome == "perdida" && why != "" {
		mark.Label = "perdida: " + why
	}
	if outcome == "cancelada" {
		plain := strings.ToLower(why)
		for _, reason := range orderReasons {
			if strings.Contains(plain, reason.Words) {
				mark = reason.Mark
				break
			}
		}
	}
	return mark
}
