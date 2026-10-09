package render

import (
	"fmt"
	"sort"
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

// --- Rivales ---------------------------------------------------------------------------

func (d Document) rivalsView() string {
	rivals := []map[string]any{}
	for _, team := range rows(d.Advice["rivals"]) {
		if !truthy(team["is_me"]) {
			rivals = append(rivals, team)
		}
	}
	if len(rivals) == 0 {
		return ""
	}
	sort.SliceStable(rivals, func(one, two int) bool {
		return number(rivals[one]["position"]) < number(rivals[two]["position"])
	})
	squad := rows(d.Advice["squad"])
	// Who each rival is the main threat to, read off the clause plan.
	threatens := map[string][]string{}
	for _, raise := range rows(d.Raise["rows"]) {
		if who := text(raise["top_threat"]); who != "" && !truthy(raise["clause_locked"]) &&
			!truthy(raise["shielded"]) {
			threatens[who] = append(threatens[who], text(raise["name"]))
		}
	}
	var items []string
	for _, team := range rivals {
		cash := number(team["estimated_cash"])
		manager := fallbackText(text(team["manager"]), text(team["name"]))
		var top map[string]any
		reach := 0
		for _, player := range squad {
			clause := number(player["clause"])
			if clause <= 0 || clause > cash || truthy(player["shielded"]) {
				continue
			}
			if !truthy(player["clause_locked"]) {
				reach++
			}
			if top == nil || clause > number(top["clause"]) {
				top = player
			}
		}
		pays := "no le llega a ninguna cláusula tuya"
		if top != nil {
			lock := ""
			if truthy(top["clause_locked"]) {
				lock = " 🔒"
			}
			pays = fmt.Sprintf("le llega hasta %s%s %s%s",
				face(top, "xs"), Esc(text(top["name"])), esMoney(number(top["clause"])), lock)
		}
		meta := fmt.Sprintf("%.0f pts · %.0f jugadores · plantilla %s", number(team["points"]),
			number(team["players"]), esMoney(number(team["squad_value"])))
		chip, tone := tag("no llega", "done"), ""
		switch {
		case len(threatens[manager]) > 0:
			chip = tag(fmt.Sprintf("amenaza a %d", len(threatens[manager])), "soon")
			chip = strings.Replace(chip, `class="mk-chip soon"`, fmt.Sprintf(
				`class="mk-chip soon" title="%s"`, Esc(strings.Join(threatens[manager], ", "))), 1)
			tone = "critical"
		case reach > 0:
			chip = tag(fmt.Sprintf("llega a %d", reach), "warn")
		}
		place := "—"
		if position := number(team["position"]); position > 0 {
			place = fmt.Sprintf("%.0fº", position)
		}
		note := `<span title="Reconstruida del historial de traspasos: es una estimación">caja estimada</span>`
		action := fmt.Sprintf(`<button type="button" class="mb mb-ghost" data-goto="rival-%s">`+
			`Ver plantilla</button>`, Esc(text(team["team_id"])))
		items = append(items, teamRow(text(team["team_id"]), place, manager, meta, pays, esMoney(cash),
			note, chip, action, tone))
	}
	main := block("Rivales", rowList(items), "", len(items))
	return view("v-rivales", "rivales", main, d.outlookAside())
}

func (d Document) outlookAside() string {
	forecast := rows(d.Advice["outlook"])
	if len(forecast) == 0 {
		return ""
	}
	week := int(number(forecast[0]["week"]))
	var items []string
	for index, row := range forecast {
		if index == 3 {
			break
		}
		why := ""
		if reasons := asStrings(row["reasons"]); len(reasons) > 0 {
			why = Esc(reasons[0])
		}
		items = append(items, teamRow(text(row["team_id"]), fmt.Sprintf("%dº", index+1),
			text(row["manager"]), why, "", esNum(number(row["xpts"]), 1)+" xPts",
			fmt.Sprintf("%.0fº de la liga", number(row["position"])), "", "", ""))
	}
	return block(fmt.Sprintf("Quién pinta peor la J%d", week), rowList(items),
		"", -1)
}

// rivalSquad is one rival's squad as compact rows, the full table folded under it.
func rivalSquad(team map[string]any, manager string, squad []map[string]any, table string) string {
	var items []string
	upgrades, payable := 0, 0
	for _, player := range squad {
		if number(player["vs_mine"]) > 0 {
			upgrades++
		}
		if !truthy(player["clause_locked"]) && !truthy(player["shielded"]) &&
			number(player["clause"]) > 0 {
			payable++
		}
		meta := Esc(text(player["team_short"]))
		if vs, ok := player["vs_mine"]; ok {
			class := "down"
			if number(vs) > 0 {
				class = "up"
			}
			meta += fmt.Sprintf(` · <span class="%s">%s</span> frente a %s`, class,
				esSigned(number(vs)), Esc(text(player["vs_who"])))
		}
		var chip string
		switch {
		case truthy(player["shielded"]):
			chip = tag("🛡 blindado", "done")
		case truthy(player["clause_locked"]):
			chip = clock(text(player["clause_locked_until"]), "se libera su cláusula")
		case number(player["asking"]) > 0:
			chip = tag("en venta "+esMoney(number(player["asking"])), "")
		case number(player["clause"]) > 0:
			chip = tag("🔓 pagable", "ok")
		}
		action := CompareButton(player)
		if raid := RaidButton(player); raid != Missing && !strings.HasPrefix(raid, "<span") {
			action += strings.Replace(raid, `class="raid-btn"`, `class="mb mb-ghost raid-btn"`, 1)
		}
		items = append(items, row(player, meta, esNum(number(player["xpts"]), 1)+" xPts",
			"cláusula "+esMoney(number(player["clause"])), chip, action, ""))
	}
	sub := fmt.Sprintf("%.0f pts · caja estimada %s · %d jugadores · %s · %s",
		number(team["points"]), esMoney(number(team["estimated_cash"])), len(squad),
		counted(upgrades, "mejora a los tuyos", "mejoran a los tuyos"),
		counted(payable, "pagable ya", "pagables ya"))
	title := Esc(manager)
	if position := number(team["position"]); position > 0 {
		title = fmt.Sprintf("%.0fº · %s", position, Esc(manager))
	}
	return mkSection("rival-"+text(team["team_id"]), "rivales", title,
		scrollList(items, 560)+folded("tabla completa", table), sub, -1)
}

// --- Ranking ---------------------------------------------------------------------------

func rankRow(player map[string]any, withChip bool) string {
	owner := text(player["owner"])
	switch {
	case truthy(player["is_mine"]):
		owner = "tuyo"
	case owner == "":
		owner = "libre"
	default:
		owner = "de " + owner
	}
	chip := ""
	if starts := asFloat(player["start_probability"]); starts != nil && withChip {
		chip = tag(fmt.Sprintf("titular %.0f %%", *starts), "")
	}
	return rowWith(player, filterAttrs(player, number(player["value"])),
		Esc(text(player["team_short"]))+" · "+Esc(owner), esNum(number(player["xpts"]), 1)+" xPts",
		fmt.Sprintf("%s pts/M · %s", esNum(number(player["points_value"]), 2),
			esMoney(number(player["value"]))), chip, Star(player)+CompareButton(player), "")
}

func rankList(players []map[string]any, height int, withChip bool) string {
	items := make([]string, 0, len(players))
	for _, player := range players {
		items = append(items, rankRow(player, withChip))
	}
	return scrollList(items, height)
}

// bestRow is a player of "Los mejores": his price today and whether my reach gets there.
func bestRow(player map[string]any, reach float64) string {
	owner := text(player["owner"])
	switch {
	case truthy(player["is_mine"]):
		owner = "tuyo"
	case owner == "":
		owner = "libre"
	default:
		owner = "de " + owner
	}
	meta := Esc(text(player["team_short"])) + " · " + Esc(owner)
	if starts := asFloat(player["start_probability"]); starts != nil {
		meta += fmt.Sprintf(" · titular %.0f %%", *starts)
	}
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
	return rowWith(player, filterAttrs(player, number(player["value"])), meta,
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
			`banquillo barato</p>`+rankList(byScore, 560, true), "", len(byScore))

	var byLine strings.Builder
	for _, positionID := range []int{1, 2, 3, 4} {
		shown := 0
		var items []string
		for _, player := range byXPts {
			if int(number(player["position_id"])) == positionID && shown < 3 {
				items = append(items, rankRow(player, false))
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
			`caja</p>`+rankList(byValue, 420, false), "", len(byValue))
	}
	return view("v-ranking", "ranking", main, aside)
}

// --- Partidos: who finishes where ------------------------------------------------------

// matchdayBoard is the matchday's table as it would end: what each one has, what his saved
// eleven still has to play, and the place that leaves him in.
func (d Document) matchdayBoard() string {
	matchday := mapOf(d.Advice["matchday"])
	managers := rows(matchday["managers"])
	if len(managers) == 0 {
		return ""
	}
	week := int(number(matchday["week"]))
	played := int(number(matchday["played"]))
	started := played > 0
	for _, manager := range managers {
		if number(manager["points"]) != 0 {
			started = true
		}
	}
	sort.SliceStable(managers, func(one, two int) bool {
		return number(managers[one]["projection_rank"]) < number(managers[two]["projection_rank"])
	})
	var items []string
	for _, manager := range managers {
		finish, now := int(number(manager["projection_rank"])), int(number(manager["points_rank"]))
		names := asStrings(manager["waiting_names"])
		waiting := int(number(manager["waiting"]))
		left := "ya no le queda nadie"
		if waiting > 0 {
			shown := names
			if len(shown) > 4 {
				shown = shown[:4]
			}
			left = "le quedan " + counted(waiting, "jugador", "jugadores")
			if len(shown) > 0 {
				left += ": " + Esc(strings.Join(shown, ", "))
				if len(names) > len(shown) {
					left += fmt.Sprintf(" +%d", len(names)-len(shown))
				}
			}
		}
		meta := fmt.Sprintf("%.0f pts · ahora %dº", number(manager["points"]), now)
		if !started {
			meta = fmt.Sprintf("%.0f pts en la temporada", number(manager["season_points"]))
		}
		if text(manager["source"]) == "techo" {
			left += " · sin su alineación: cuento su mejor once"
		}
		chip := ""
		if started && finish != now {
			if finish < now {
				chip = tag(fmt.Sprintf("▲ %d", now-finish), "ok")
			} else {
				chip = tag(fmt.Sprintf("▼ %d", finish-now), "soon")
			}
		}
		tone := ""
		if truthy(manager["is_me"]) {
			tone = "accent me"
		}
		if finish <= 3 {
			tone += fmt.Sprintf(" podium p%d", finish)
		}
		note := fmt.Sprintf("%.0f + %s por sumar", number(manager["points"]),
			esNum(number(manager["to_come"]), 1))
		items = append(items, teamRow(text(manager["team_id"]), fmt.Sprintf("%dº", finish),
			text(manager["manager"]), meta, left, esNum(number(manager["projection"]), 1)+" pts", note,
			chip, "", tone))
	}
	title := fmt.Sprintf("Cómo acabaría la J%d", week)
	sub := ""
	if !truthy(matchday["live"]) {
		title = fmt.Sprintf("Cómo acabó la J%d", week)
	} else if started {
		sub = fmt.Sprintf("%d de %d partidos jugados", played, int(number(matchday["matches"])))
	}
	return block(title, `<ul class="rows board">`+strings.Join(items, "")+`</ul>`, sub, -1)
}

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
