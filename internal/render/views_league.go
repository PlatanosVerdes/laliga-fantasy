package render

import (
	"fmt"
)

// The league tabs in the same language as the others: Rivales, Liga and Ranking.

// mkSection is a section of its own (so the live refresh swaps it by id) drawn as a block.
func mkSection(id, tab, title, body, sub string, count int) string {
	return fmt.Sprintf(`<section id="%s" data-tab="%s" class="mk"><div class="main">%s</div></section>`,
		id, tab, block(title, body, sub, count))
}

func tag(text, class string) string {
	return `<span class="mk-chip ` + class + `">` + text + `</span>`
}

// --- Partidos: who finishes where ------------------------------------------------------

// myFinish is the place my saved eleven would finish the matchday in, or 0.
func (d Document) myFinish() (int, int) {
	matchday := mapOf(d.Advice["matchday"])
	for _, manager := range rows(matchday["managers"]) {
		if truthy(manager["is_me"]) {
			return int(number(matchday["week"])), int(number(manager["projection_rank"]))
		}
	}
	return 0, 0
}
