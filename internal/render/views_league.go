package render

import (
	"fmt"
	"strings"
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

// --- Ranking ---------------------------------------------------------------------------

func rankRow(player map[string]any) string {
	return rowWith(player, filterAttrs(player, number(player["value"])), "",
		esNum(number(player["xpts"]), 1)+" xPts",
		fmt.Sprintf("%s pts/M · %s", esNum(number(player["points_value"]), 2),
			esMoney(number(player["value"]))), "", Star(player)+CompareButton(player), "")
}

func rankList(players []map[string]any, height int) string {
	items := make([]string, 0, len(players))
	for _, player := range players {
		items = append(items, rankRow(player))
	}
	return scrollList(items, height)
}

// bestRow is a player of "Los mejores": his price today and whether my reach gets there.
func bestRow(player map[string]any, reach float64) string {
	price, how := crackPrice(player)
	note, chip := Esc(how), ""
	switch {
	case truthy(player["is_mine"]):
		note = "vale " + esMoney(number(player["value"]))
	case price > 0:
		note = esMoney(price) + " · " + Esc(how)
	}
	switch {
	case truthy(player["is_mine"]):
	case price > 0 && reach >= price:
		chip = tag("te llega", "ok")
	case price > 0:
		chip = tag("te faltan "+esMoney(price-reach), "warn")
	}
	return rowWith(player, filterAttrs(player, number(player["value"])), "",
		esNum(number(player["xpts"]), 1)+" xPts", note, chip, Star(player)+CompareButton(player), "")
}

func (d Document) rankingView(byScore, byXPts, byValue []map[string]any) string {
	reach, _ := d.crackReach()
	best := make([]string, 0, len(byXPts))
	for _, player := range byXPts {
		best = append(best, bestRow(player, reach))
	}
	main := `<div class="mk-filters">` + Filters + `</div>` +
		block("Los mejores", scrollList(best, 560), "", len(byXPts)) +
		block("Chollos", `<p class="lead">puntos por millón, titularidad y valor al alza: para el `+
			`banquillo barato</p>`+rankList(byScore, 560), "", len(byScore))

	var byLine strings.Builder
	for _, positionID := range []int{1, 2, 3, 4} {
		shown := 0
		var items []string
		for _, player := range byXPts {
			if int(number(player["position_id"])) == positionID && shown < 3 {
				items = append(items, rankRow(player))
				shown++
			}
		}
		if len(items) == 0 {
			continue
		}
		slug := positionSlug[positionID]
		fmt.Fprintf(&byLine, `<li class="line-head"><span class="pos pos-%s">%s</span></li>%s`,
			slug, strings.ToUpper(slug), strings.Join(items, ""))
	}
	aside := block("Los mejores por posición", `<ul class="rows">`+byLine.String()+`</ul>`,
		"", -1)
	if len(byValue) > 0 {
		aside += block("Más xPts por millón", `<p class="lead">lo que manda cuando vas justo de `+
			`caja</p>`+rankList(byValue, 420), "", len(byValue))
	}
	return view("v-ranking", "ranking", main, aside)
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
