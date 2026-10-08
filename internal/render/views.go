package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// The tabs' first screens: compact rows, cards and an aside, the way the approved mockup draws
// them. Every figure is read off the advice layer or the universe; the old tables stay below,
// folded under "Ver detalle".

// MinShownGain is the least a signing has to add to the best eleven to be listed: under a third
// of a point it does not show on the pitch.
const MinShownGain = 0.3

// ViewTabs are the tabs that open with a view, in page order.
var ViewTabs = []string{"decidir", "comprar", "vender", "clausulas", "plantilla", "partidos"}

// health is the ring a face wears: red for a confirmed absence, yellow for a doubt.
func health(player map[string]any) (ring, glyph, reason string) {
	absence := mapOf(player["absence"])
	kind, status := text(absence["kind"]), text(player["status"])
	reason = text(absence["reason"])
	severity, known := absence["severity"].(float64)
	pick := func(fallback string) string {
		if reason != "" {
			return reason
		}
		return fallback
	}
	switch {
	case status == "suspended" || status == "sanctioned" || kind == "sancionado":
		return "out", "card", pick("Sancionado")
	case status == "injured" || (kind == "lesionado" && known && severity == 0):
		return "out", "cross", pick("Lesionado")
	case status == "doubtful" || kind == "lesionado" || kind == "duda":
		if kind == "lesionado" {
			glyph = "cross"
		}
		return "doubt", glyph, pick("Duda")
	}
	return "", "", ""
}

func initials(name string) string {
	out := ""
	for _, word := range strings.Fields(strings.ReplaceAll(name, ".", " ")) {
		out += strings.ToUpper(string([]rune(word)[:1]))
		if len([]rune(out)) == 2 {
			break
		}
	}
	return out
}

// shieldName is the name, with a shield when nobody can pay his clause until a given hour.
func shieldName(player map[string]any) string {
	name := Esc(text(player["name"]))
	if !truthy(player["shielded"]) {
		return name
	}
	until := ""
	if stamp := text(player["shielded_until"]); stamp != "" {
		until = " hasta " + esWhen(stamp)
	}
	return name + ` <span class="shield-mark" title="blindado` + Esc(until) + `">🛡</span>`
}

// face is the round photo that opens the player's card, ringed by his status.
func face(player map[string]any, size string) string {
	ring, glyph, reason := health(player)
	class := "face face-" + size
	if ring != "" {
		class += " ring-" + ring
	}
	title := ""
	if reason != "" {
		title = ` title="` + Esc(reason) + `"`
	}
	image := ""
	if url := text(player["image"]); url != "" {
		image = `<img src="` + Esc(url) + `" alt="" loading="lazy" onerror="this.remove()">`
	}
	badge := ""
	switch glyph {
	case "cross":
		badge = `<span class="hb hb-cross">✚</span>`
	case "card":
		badge = `<span class="hb hb-card"></span>`
	}
	return fmt.Sprintf(`<span class="%s" data-pid="%s"%s><span class="ini">%s</span>%s%s</span>`,
		class, Esc(text(player["id"])), title, Esc(initials(text(player["name"]))), image, badge)
}

func posTag(player map[string]any) string {
	slug := positionSlug[int(number(player["position_id"]))]
	if slug == "" {
		slug = "ent"
	}
	label := text(player["position"])
	if label == "" {
		label = strings.ToUpper(slug)
	}
	return fmt.Sprintf(`<span class="pos pos-%s">%s</span>`, slug, Esc(label))
}

// clock is a live countdown chip; the label says what ends.
func clock(stamp, label string) string {
	if stamp == "" {
		return ""
	}
	return fmt.Sprintf(`<span class="mk-chip" data-deadline="%s" data-chip="1" title="%s">`+
		`<span class="clock">⏱</span><span class="left">%s</span></span>`, Esc(stamp),
		Esc(strings.TrimSpace(label+" "+esWhen(stamp))), Esc(esWhen(stamp)))
}

// row is one compact line: face, who, the number that decides, until when, the buttons.
func row(player map[string]any, meta, value, note, chip, action, tone string) string {
	if note != "" {
		note = `<span class="rnote">` + note + `</span>`
	}
	if action != "" {
		action = `<span class="ract">` + action + `</span>`
	}
	return fmt.Sprintf(`<li class="r %s" data-pid="%s">%s<span class="rwho"><b>%s</b>`+
		`<span class="meta">%s%s</span></span><span class="rval"><b>%s</b>%s</span>%s%s</li>`,
		tone, Esc(text(player["id"])), face(player, "sm"), shieldName(player),
		posTag(player), meta, value, note, chip, action)
}

func rowList(items []string, tight bool) string {
	class := "rows"
	if tight {
		class += " tight"
	}
	return `<ul class="` + class + `">` + strings.Join(items, "") + `</ul>`
}

func block(title, body, sub string, count int) string {
	badge := ""
	if count >= 0 {
		badge = fmt.Sprintf(`<span class="count">%d</span>`, count)
	}
	if sub != "" {
		sub = `<p>` + sub + `</p>`
	}
	return `<div class="block"><div class="sec-head"><h2>` + title + badge + `</h2>` + sub +
		`</div>` + body + `</div>`
}

func folded(summary, body string) string {
	return `<details class="fold"><summary>` + summary + `</summary>` + body + `</details>`
}

func empty(message string) string {
	return `<p class="mk-empty">` + message + `</p>`
}

// button is a row's action; class carries what wires it (op, bid, raise, raid-btn) and attrs
// what it needs to run.
func button(label, kind, class, attrs string) string {
	return fmt.Sprintf(`<button type="button" class="mb mb-%s %s"%s>%s</button>`, kind, class,
		attrs, Esc(label))
}

// actButton runs one of the card's own actions, the same the player popup offers.
func actButton(label, kind, playerID, op string) string {
	return button(label, kind, "act", fmt.Sprintf(` data-act="%s" data-act-player="%s"`, Esc(op),
		Esc(playerID)))
}

func view(id, tab, main, aside string) string {
	layout := `<div class="main solo">` + main + `</div>`
	if aside != "" {
		layout = `<div class="layout"><div class="main">` + main + `</div><aside class="side">` +
			aside + `</aside></div>`
	}
	return fmt.Sprintf(`<section id="%s" data-tab="%s" class="mk">%s</section>`, id, tab, layout)
}

// moreSection is the tab's "Ver detalle": the old tables, folded until asked for.
func moreSection(tab, what string) string {
	return fmt.Sprintf(`<section id="mas-%s" data-tab="%s" class="mk mk-more">`+
		`<button class="dfold" type="button" data-fold="%s" aria-expanded="false">`+
		`<span class="dfold-title">Ver detalle</span><span class="dfold-what">%s</span>`+
		`</button></section>`, tab, tab, tab, Esc(what))
}

func ratioClass(ratio float64) string {
	if ratio >= 1 {
		return "up"
	}
	return "down"
}

func esRatio(ratio float64) string {
	return strings.Replace(fmt.Sprintf("%.2f", ratio), ".", ",", 1) + "x"
}

var esShortDays = []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}

// esDay is "jue 8 oct".
func esDay(stamp string) string {
	when, ok := parseStamp(stamp)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s %d %s", esShortDays[int(when.Weekday())], when.Day(),
		months[int(when.Month())])
}

// esHour is "vie 21:00".
func esHour(stamp string) string {
	when, ok := parseStamp(stamp)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s %02d:%02d", esShortDays[int(when.Weekday())], when.Hour(), when.Minute())
}

func (d Document) playersByID() map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, player := range rows(d.Universe["players"]) {
		out[text(player["id"])] = player
	}
	for _, player := range rows(d.Advice["squad"]) {
		out[text(player["id"])] = player
	}
	return out
}

// Views are every tab's first screen, each followed by its "Ver detalle".
func (d Document) Views() []string {
	if len(d.Advice) == 0 {
		return nil
	}
	return []string{
		d.decideSection(), moreSection("decidir", "plan ideal · todas las decisiones · caja · chollos"),
		d.buyView(), moreSection("comprar", "fichajes · en venta · mis pujas · siguiendo · cómo acabaron"),
		d.sellView(), moreSection("vender", "mis ventas · ofertas · siempre en mercado"),
		d.clauseView(), moreSection("clausulas", "subir · programados · calendario · vencimientos · oportunidades"),
		Pitch, d.squadView(), moreSection("plantilla", "plantilla · candidatos a vender"),
		d.matchesView(), moreSection("partidos", "cómo va la jornada · calendario de partidos"),
		d.rivalsView(),
	}
}

// --- Decidir ---------------------------------------------------------------------------

// decideSection is "Qué hacer ahora": the cards, and beside them the eleven they are about.
func (d Document) decideSection() string {
	if len(d.Advice) == 0 {
		return ""
	}
	cards := d.decisionCards(time.Now())
	shown := cards
	if len(shown) > MaxCards {
		shown = shown[:MaxCards]
	}
	var main strings.Builder
	main.WriteString(`<div class="sec-head"><h2>Qué hacer ahora</h2></div>`)
	if len(shown) == 0 {
		main.WriteString(empty("Nada que decidir ahora: ninguna oferta que cobrar, ninguna " +
			"cláusula que subir y ningún fichaje que mejore tu once a su precio."))
	} else {
		main.WriteString(`<div class="cards">`)
		cash := number(d.Advice["budget"])
		for index, card := range shown {
			main.WriteString(cardHTML(index+1, card))
			cash += card.Cash
		}
		main.WriteString(`</div>`)
		count := []string{"", "la", "las dos", "las tres", "las cuatro", "las cinco"}[len(shown)]
		rest := fmt.Sprintf("Haciendo %s te quedan %s en caja. Lo demás puede esperar.", count,
			esMoney(cash))
		if len(shown) == 1 {
			rest = fmt.Sprintf("Haciéndolo te quedan %s en caja. Lo demás puede esperar.",
				esMoney(cash))
		}
		main.WriteString(`<p class="rest">` + Esc(rest) + `</p>`)
	}
	return view("ahora", "decidir", main.String(), d.elevenAside())
}

func cardHTML(rank int, card Card) string {
	player := card.Player
	faces := face(player, "lg")
	if card.In != nil {
		faces = `<span class="swap">` + face(player, "md") + `<span class="arrow">→</span>` +
			face(card.In, "md") + `</span>`
	}
	meta := fmt.Sprintf(`%s%s · %s xPts`, posTag(player), Esc(text(player["team_short"])),
		esNum(number(player["xpts"]), 1))
	note := ""
	if card.BigNote != "" {
		note = `<span class="big-note">` + Esc(card.BigNote) + `</span>`
	}
	var why strings.Builder
	for _, line := range card.Why {
		why.WriteString(`<li>` + Esc(line) + `</li>`)
	}
	tone := card.Tone
	if tone == "" {
		tone = "muted"
	}
	return fmt.Sprintf(`<article class="card tone-%s" data-kind="%s"><div class="rank">%d</div>`+
		`<div class="card-top">%s<div class="who" data-pid="%s"><b>%s</b>`+
		`<span class="meta">%s</span></div>%s</div>`+
		`<div class="verb">%s</div><div class="big">%s%s</div><ul class="why">%s</ul>`+
		`<div class="card-foot">%s<span class="impact">%s</span></div></article>`,
		tone, card.Kind, rank, faces, Esc(text(player["id"])), shieldName(player), meta,
		clock(card.Deadline, card.DeadlineLabel), Esc(card.Verb), Esc(card.Big), note,
		why.String(), card.Button, Esc(card.Impact))
}

// elevenChips is an eleven by line, attack first, each chip coloured by its xPts.
func elevenChips(lines [][]map[string]any, arriving map[string]bool) string {
	labels := []string{"del", "med", "def", "por"}
	var out strings.Builder
	for index, line := range lines {
		var chips strings.Builder
		for _, player := range line {
			class := xptsClass(number(player["xpts"]))
			if arriving[text(player["id"])] {
				class += " is-new"
			}
			fmt.Fprintf(&chips, `<span class="tchip %s" data-pid="%s">%s<span class="tname">%s</span>`+
				`<span class="tx">%s</span></span>`, class, Esc(text(player["id"])),
				face(player, "xs"), shieldName(player), esNum(number(player["xpts"]), 1))
		}
		fmt.Fprintf(&out, `<div class="line"><span class="pos pos-%s">%s</span>`+
			`<div class="chips">%s</div></div>`, labels[index], strings.ToUpper(labels[index]),
			chips.String())
	}
	return out.String()
}

// byLines turns a choice into its four lines of players, attack first, best first.
func byLines(byID map[string]map[string]any, attack, middle, defence []string,
	keeper string) [][]map[string]any {
	pick := func(ids []string) []map[string]any {
		out := []map[string]any{}
		for _, id := range ids {
			if player := byID[id]; player != nil {
				out = append(out, player)
			}
		}
		sort.SliceStable(out, func(one, two int) bool {
			return number(out[one]["xpts"]) > number(out[two]["xpts"])
		})
		return out
	}
	return [][]map[string]any{pick(attack), pick(middle), pick(defence), pick([]string{keeper})}
}

// elevenAside is the best eleven the squad can field, or the one the plan leaves.
func (d Document) elevenAside() string {
	squad := rows(d.Advice["squad"])
	if len(squad) == 0 {
		return ""
	}
	byID := map[string]map[string]any{}
	for _, player := range squad {
		byID[text(player["id"])] = player
	}
	_, now := bestElevenOf(squad)
	planned, arriving := planSquad(squad, rows(d.Swaps["moves"]))
	for _, player := range planned {
		byID[text(player["id"])] = player
	}
	choice, total := bestElevenOf(planned)
	shape := choice.Shape.Name
	summary := `<div class="total"><span class="to">` + esNum(total, 1) + ` xPts</span></div>`
	if len(arriving) > 0 {
		shape += " si haces el plan"
		summary = `<div class="total"><span class="from">` + esNum(now, 1) + `</span>` +
			`<span class="to">` + esNum(total, 1) + ` xPts</span><span class="gain">` +
			esSigned(total-now) + `</span></div>`
	}
	fresh := ""
	if len(arriving) > 0 {
		fresh = "● fichaje nuevo · "
	}
	legend := `<div class="mk-legend">` + fresh + `xPts por jornada: <span class="x-hi-t">≥6</span> · ` +
		`<span class="x-lo-t">2–3,5</span> · <span class="x-bad-t">&lt;2</span></div>`
	lines := byLines(byID, choice.Attack, choice.Middle, choice.Defence, choice.Keeper)
	more := `<nav class="more"><span>Ver todo en</span>` +
		`<button type="button" data-goto="clausulas">Cláusulas</button>` +
		`<button type="button" data-goto="comprar">Comprar</button>` +
		`<button type="button" data-goto="vender">Vender</button></nav>`
	finish := ""
	if week, place := d.myFinish(); place > 0 {
		finish = fmt.Sprintf(`<p class="mk-note finish">Tu puesto previsto en la J%d: `+
			`<button class="linkish" type="button" data-goto="partidos"><b>%dº</b></button></p>`,
			week, place)
	}
	return block("Tu once", `<div class="pitchlist">`+summary+elevenChips(lines, arriving)+legend+
		`</div>`+finish, Esc(shape), -1) + more
}

// --- Comprar ---------------------------------------------------------------------------

// buyRow is a signing priced by what it adds to the eleven, with the button that makes it.
func (d Document) buyRow(item map[string]any, window *windowState) string {
	gain, cost := number(item["xi_gain"]), number(item["entry_cost"])
	affordable := truthy(item["affordable"])
	listing := mapOf(item["market"])
	id, name := text(item["id"]), text(item["name"])
	price := esMoney(cost)
	if !affordable {
		price += " · no llegas"
	}
	kind := "primary"
	tone := ""
	if !affordable {
		kind, tone = "ghost", "dim"
	}
	var note, chip, action string
	switch text(item["route"]) {
	case "clausula":
		note = "de " + Esc(text(item["owner"]))
		if window.open {
			chip = clock(window.closes, "cláusulas abiertas hasta")
			action = button("Pagar "+esMoney(cost), kind, "op", fmt.Sprintf(` data-op="pay_clause" `+
				`data-op-player="%s" data-op-name="%s" data-op-amount="%d"`, Esc(id), Esc(name),
				int64(cost)))
		} else {
			chip = clock(window.opens, "se abren las cláusulas")
			action = button("Programar "+esMoney(cost), "ghost", "raid-btn", fmt.Sprintf(
				` data-raid="%s" data-raid-name="%s" data-raid-max="%d" data-raid-clause="%d"`,
				Esc(id), Esc(name), int64(cost), int64(number(item["clause"]))))
		}
	case "oferta al dueño":
		note = "vende " + Esc(text(listing["seller"]))
		chip = clock(text(listing["expires"]), "sale del mercado")
		action = d.listingButton(item, "Ofrecer "+esMoney(cost), kind)
	default:
		note = "libre"
		chip = clock(text(listing["expires"]), "sale del mercado")
		action = d.listingButton(item, "Pujar "+esMoney(cost), kind)
	}
	return row(item, Esc(text(item["team_short"]))+" · "+note, esSigned(gain)+" xPts",
		Esc(price), chip, action, tone)
}

// listingButton is the bid or offer button, or what is already on it when there is a bid of
// mine.
func (d Document) listingButton(item map[string]any, label, kind string) string {
	listing := mapOf(item["market"])
	if text(listing["market_id"]) == "" {
		return ""
	}
	if text(listing["my_bid_id"]) != "" {
		return BidButton(item)
	}
	operation := "bid"
	if text(listing["kind"]) == "venta" {
		operation = "buy_offer"
	}
	return bidButton(item, "mb mb-"+kind+" bid", operation, "", Esc(label))
}

type windowState struct {
	known, open   bool
	closes, opens string
}

func (d Document) window() *windowState {
	window, ok := d.clauseWindow()
	return &windowState{known: ok, open: ok && window.Open, closes: window.ClosesAt,
		opens: window.OpensAt}
}

// buyBlock lists the reachable ones first, three of them in sight and the rest folded.
func (d Document) buyBlock(title, sub string, items []map[string]any, window *windowState) string {
	var reachable, beyond []string
	for _, item := range items {
		if truthy(item["affordable"]) {
			reachable = append(reachable, d.buyRow(item, window))
		} else {
			beyond = append(beyond, d.buyRow(item, window))
		}
	}
	body := ""
	switch {
	case len(items) == 0:
		body = empty("Nada que mejore tu once.")
	case len(reachable) == 0:
		body = empty("Ninguno te llega con lo que tienes.")
	default:
		shown := min(3, len(reachable))
		body = rowList(reachable[:shown], false)
		if len(reachable) > shown {
			body += folded(fmt.Sprintf("%d más", len(reachable)-shown),
				rowList(reachable[shown:], false))
		}
	}
	if len(beyond) > 0 {
		body += folded(fmt.Sprintf("%d que no te llegan", len(beyond)), rowList(beyond, false))
	}
	return block(title, body, sub, len(reachable))
}

func (d Document) buyOptions() (clauses, offers, free []map[string]any) {
	for _, item := range rows(d.Money["bargains"]) {
		if number(item["xi_gain"]) <= MinShownGain {
			continue
		}
		switch text(item["route"]) {
		case "clausula":
			clauses = append(clauses, item)
		case "oferta al dueño":
			offers = append(offers, item)
		case "puja libre":
			free = append(free, item)
		}
	}
	return
}

func (d Document) buyView() string {
	window := d.window()
	clauses, offers, free := d.buyOptions()
	main := `<p class="lead">Ordenado por lo que gana <b>tu once</b> cada jornada, no por el ` +
		`descuento.</p>` +
		d.buyBlock("🔓 Cláusulas que puedes pagar", "", clauses, window) +
		d.buyBlock("🤝 En venta por rivales", "", offers, window) +
		d.buyBlock("🔨 Mercado libre", "", free, window)

	var bids []string
	for _, bid := range rows(d.Advice["my_bids"]) {
		bids = append(bids, row(bid, Esc(text(bid["team_short"])),
			esMoney(number(bid["my_bid"])), "pide "+esMoney(number(bid["asking"])),
			clock(text(bid["closes"]), "cierra"), BidButton(bid), ""))
	}
	body := empty("Ninguna ahora mismo.")
	if len(bids) > 0 {
		body = rowList(bids, false)
	}
	for _, raid := range d.Raids {
		if raidsStandingDown[text(raid["action"])] {
			continue
		}
		body += fmt.Sprintf(`<p class="mk-note">Clausulazo programado: <b>%s</b> hasta %s · `+
			`<button class="linkish" type="button" data-goto="clausulas">ver en Cláusulas</button></p>`,
			Esc(text(raid["name"])), esMoney(number(raid["max_pay"])))
	}
	main += block("Mis pujas en curso", body, "", -1)
	return view("v-comprar", "comprar", main, d.endingsAside()+d.starredAside())
}

func (d Document) endingsAside() string {
	glyphs := map[string][2]string{"aceptada": {"✓", "good"}, "rechazada": {"✕", "bad"},
		"perdida": {"✕", "bad"}, "caducada": {"⌛", "muted"}}
	line := func(ending map[string]any, full bool) string {
		outcome := text(ending["outcome"])
		glyph := glyphs[outcome]
		if glyph[0] == "" {
			glyph = [2]string{"·", "muted"}
		}
		what := outcome
		if full {
			switch {
			case outcome == "perdida" && text(ending["new_owner"]) != "":
				what = "perdida · ganó " + text(ending["new_owner"])
				if paid := number(ending["won_for"]); paid > 0 {
					what += " con " + esMoney(paid)
				}
			case outcome == "rechazada" && text(ending["who"]) != "":
				what = "rechazada por " + text(ending["who"])
			}
		}
		date := ""
		if when, ok := parseStamp(text(ending["at"])); ok {
			date = when.Format("02/01")
		}
		return fmt.Sprintf(`<li class="res %s" data-pid="%s"><span class="g">%s</span><span><b>%s</b>`+
			`<span class="meta">%s · %s</span></span><span class="rv">%s</span></li>`, glyph[1],
			Esc(text(ending["player_id"])), glyph[0], Esc(text(ending["player"])), Esc(what),
			date, esMoney(number(ending["amount"])))
	}
	var visible, rest []string
	for index, ending := range d.Endings {
		outcome := text(ending["outcome"])
		if index < 3 || outcome == "perdida" || outcome == "rechazada" {
			visible = append(visible, line(ending, true))
		} else {
			rest = append(rest, line(ending, false))
		}
	}
	body := empty("Todavía no se ha resuelto ninguna.")
	if len(visible) > 0 {
		body = `<ul class="reslist">` + strings.Join(visible, "") + `</ul>`
	}
	if len(rest) > 0 {
		body += folded(fmt.Sprintf("%d anteriores", len(rest)),
			`<ul class="reslist">`+strings.Join(rest, "")+`</ul>`)
	}
	return block("Cómo acabaron", body, "", len(d.Endings))
}

func (d Document) starredAside() string {
	starred := rows(d.Advice["starred"])
	if len(starred) == 0 {
		return ""
	}
	sort.SliceStable(starred, func(one, two int) bool {
		return number(starred[one]["xpts"]) > number(starred[two]["xpts"])
	})
	var items strings.Builder
	for _, player := range starred {
		owner := text(player["owner"])
		if truthy(player["is_mine"]) {
			owner = "tuyo"
		} else if owner == "" {
			owner = "libre"
		}
		fmt.Fprintf(&items, `<li class="mk-star" data-pid="%s">%s<span>%s</span>`+
			`<span class="meta">%s</span><span class="tx %s">%s</span></li>`,
			Esc(text(player["id"])), face(player, "xs"), shieldName(player), Esc(owner),
			xptsClass(number(player["xpts"])), esNum(number(player["xpts"]), 1))
	}
	return block("☆ Siguiendo", `<ul class="stars">`+items.String()+`</ul>`, "",
		len(starred))
}

// --- Vender ----------------------------------------------------------------------------

func (d Document) sellView() string {
	offers := rows(d.Advice["offers"])
	sort.SliceStable(offers, func(one, two int) bool {
		return number(offers[one]["vs_value"]) > number(offers[two]["vs_value"])
	})
	// The plan's sales: an offer it counts on is accepted to sign somebody else.
	planned := map[string]string{}
	for _, move := range rows(d.Swaps["moves"]) {
		if out, in := mapOf(move["out"]), mapOf(move["in"]); out != nil && in != nil {
			planned[text(out["id"])] = text(in["name"])
		}
	}
	var items []string
	for _, offer := range offers {
		amount, value := number(offer["offer_amount"]), number(offer["value"])
		ratio := number(offer["vs_value"])
		expires := text(offer["offer_expires"])
		common := fmt.Sprintf(` data-op-market="%s" data-op-offer="%s" data-op-player="%s" `+
			`data-op-name="%s" data-op-amount="%d"`, Esc(text(offer["market_id"])),
			Esc(text(offer["offer_id"])), Esc(text(offer["id"])), Esc(text(offer["name"])),
			int64(amount))
		accept := func(kind string) string {
			return button("Aceptar", kind, "op", ` data-op="accept_offer"`+common)
		}
		decline := button("Rechazar", "ghost", "op", ` data-op="decline_offer"`+common)
		meta := Esc(text(offer["team_short"]))
		if who := text(offer["offer_from"]); who != "" && !truthy(offer["offer_from_market"]) {
			meta += " · de " + Esc(who)
		}
		tone, actions := "", decline+accept("ghost")
		switch {
		case planned[text(offer["id"])] != "":
			meta += " · si fichas a " + Esc(planned[text(offer["id"])])
			tone, actions = "accent", accept("primary")+decline
		case truthy(offer["worth_taking"]):
			tone, actions = "good", accept("primary")+decline
		}
		glyph := "▲"
		if ratio < 1 {
			glyph = "▼"
		}
		note := fmt.Sprintf(`<span class="%s">%s %s</span> vale %s`, ratioClass(ratio), glyph,
			esRatio(ratio), esMoney(value))
		items = append(items, row(offer, meta, esMoney(amount), note,
			clock(expires, "caduca"), actions, tone))
	}
	sub := ""
	body := empty("Ninguna oferta ahora mismo.")
	if len(items) > 0 {
		body = rowList(items, false)
	}
	main := block("Ofertas recibidas", body, sub, len(items))

	listings := rows(d.Advice["my_listings"])
	sort.SliceStable(listings, func(one, two int) bool {
		return text(mapOf(listings[one]["market"])["expires"]) <
			text(mapOf(listings[two]["market"])["expires"])
	})
	var listed []string
	for _, player := range listings {
		listing := mapOf(player["market"])
		asking, value := number(listing["min_bid"]), number(player["value"])
		ratio := 0.0
		if value > 0 {
			ratio = asking / value
		}
		best := 0.0
		for _, offer := range rows(player["offers"]) {
			best = math.Max(best, number(offer["money"]))
		}
		meta := Esc(text(player["team_short"])) + " · sin ofertas"
		if best > 0 {
			meta = Esc(text(player["team_short"])) + " · mejor " + esMoney(best)
		}
		listed = append(listed, row(player, meta, esMoney(asking),
			fmt.Sprintf(`<span class="%s">%s</span> de su valor`, ratioClass(ratio), esRatio(ratio)),
			clock(text(listing["expires"]), "cierra"),
			actButton("Quitar", "ghost", text(player["id"]), "withdraw"), ""))
	}
	body = empty("No tienes a nadie en venta.")
	if len(listed) > 0 {
		body = rowList(listed, false)
	}
	main += block("En venta ahora", body, "", len(listed))

	return view("v-vender", "vender", main, d.idleAside()+d.alwaysAside())
}

// benchOf is the squad outside its best eleven, least useful first.
func (d Document) benchOf() []map[string]any {
	squad := rows(d.Advice["squad"])
	choice, _ := bestElevenOf(squad)
	in := map[string]bool{}
	for _, id := range choice.IDs() {
		in[id] = true
	}
	out := []map[string]any{}
	for _, player := range squad {
		if !in[text(player["id"])] {
			out = append(out, player)
		}
	}
	sort.SliceStable(out, func(one, two int) bool {
		return number(out[one]["xpts"]) < number(out[two]["xpts"])
	})
	return out
}

func startsMeta(player map[string]any) string {
	meta := fmt.Sprintf("titular %.0f %%", number(player["start_probability"]))
	if truthy(player["sale_locked"]) {
		meta += " · 🔒 " + esDay(text(player["hold_until"]))
	}
	return meta
}

func (d Document) idleAside() string {
	bench := d.benchOf()
	var items []string
	for _, player := range bench {
		action := ""
		if !truthy(player["sale_locked"]) && text(mapOf(player["market"])["market_id"]) == "" {
			action = actButton("Poner en venta", "ghost", text(player["id"]), "sell_to_market")
		}
		items = append(items, row(player, Esc(startsMeta(player)),
			esNum(number(player["xpts"]), 1)+" xPts", "vale "+esMoney(number(player["value"])),
			"", action, ""))
	}
	sub := ""
	if len(items) == 0 {
		return block("No juegan", empty("Juegan todos."), sub, 0)
	}
	return block("No juegan", rowList(items, true), sub, len(items))
}

func (d Document) alwaysAside() string {
	var items strings.Builder
	count := 0
	for _, rule := range d.Plan {
		count++
		amount := ""
		if value := number(rule["amount"]); value > 0 {
			amount = " · " + esMoney(value)
		}
		fmt.Fprintf(&items, `<li class="al" data-pid="%s"><b>%s</b><span class="meta">%s%s</span>`+
			`<span class="small">%s</span></li>`, Esc(text(rule["id"])), Esc(text(rule["name"])),
			Esc(strings.ReplaceAll(text(rule["action"]), "_", " ")), amount,
			Esc(strings.SplitN(text(rule["why"]), ";", 2)[0]))
	}
	body := empty("Ninguna regla activa: se arma desde la ficha de cada jugador.")
	if count > 0 {
		body = folded(fmt.Sprintf("%d reglas activas", count),
			`<ul class="always">`+items.String()+`</ul>`)
	}
	return block("Siempre en mercado", body, "", -1)
}

// --- Cláusulas -------------------------------------------------------------------------

func (d Document) clauseView() string {
	window := d.window()
	var worth, rest []map[string]any
	for _, raise := range rows(d.Raise["rows"]) {
		switch text(raise["verdict"]) {
		case "sube", "no te llega":
			if number(raise["pay"]) > 0 {
				worth = append(worth, raise)
				continue
			}
		}
		rest = append(rest, raise)
	}
	sort.SliceStable(worth, func(one, two int) bool {
		first, second := truthy(worth[one]["clause_locked"]), truthy(worth[two]["clause_locked"])
		if first != second {
			return !first
		}
		return number(worth[one]["xi_drop"]) > number(worth[two]["xi_drop"])
	})
	var items []string
	for _, raise := range worth {
		opens := "abierta ya"
		if truthy(raise["clause_locked"]) {
			opens = "se abre " + esDay(text(raise["clause_locked_until"]))
		}
		meta := opens
		if threat := text(raise["top_threat"]); threat != "" {
			meta += " · " + threat + " tiene " + esMoney(number(raise["top_threat_cash"]))
		}
		deadline, label := d.raiseDeadline(raise)
		pay := number(raise["pay"])
		kind := "primary"
		if text(raise["verdict"]) == "no te llega" {
			kind = "ghost"
		}
		action := button("Subir · pagas "+esMoney(pay), kind, "raise", fmt.Sprintf(` data-raise="%s" `+
			`data-raise-name="%s" data-raise-pay="%d" data-raise-slot="%s" data-raise-clause="%d" `+
			`data-raise-target="%d"`, Esc(text(raise["id"])), Esc(text(raise["name"])), int64(pay),
			Esc(text(raise["player_team_id"])), int64(number(raise["clause"])),
			int64(number(raise["target_clause"]))))
		tone := ""
		if !truthy(raise["clause_locked"]) {
			tone = "critical"
		}
		items = append(items, row(raise, Esc(meta), esMoney(number(raise["target_clause"])),
			fmt.Sprintf("hoy %s · −%s xPts si se va", esMoney(number(raise["clause"])),
				esNum(number(raise["xi_drop"]), 1)), clock(deadline, label), action, tone))
	}
	body := empty("Ninguna cláusula tuya merece subirse ahora.")
	if len(items) > 0 {
		body = rowList(items, false)
	}
	if len(rest) > 0 {
		var lines strings.Builder
		for _, raise := range rest {
			cost := ""
			if pay := number(raise["pay"]); pay > 0 {
				cost = " · subirla cuesta " + esMoney(pay)
			}
			fmt.Fprintf(&lines, `<li class="al" data-pid="%s"><b>%s</b><span class="meta">%s · %s%s</span></li>`,
				Esc(text(raise["id"])), Esc(text(raise["name"])), Esc(text(raise["verdict"])),
				Esc(text(raise["why"])), cost)
		}
		body += folded(fmt.Sprintf("%d que no merece la pena subir", len(rest)),
			`<ul class="always">`+lines.String()+`</ul>`)
	}
	main := block("Sube solo estas", body, "", len(items))

	byID := d.playersByID()
	var scheduled []string
	for _, raid := range d.Raids {
		player := byID[text(raid["player_id"])]
		if player == nil {
			player = map[string]any{"id": raid["player_id"], "name": raid["name"]}
		}
		clause, limit := number(raid["clause"]), number(raid["max_pay"])
		meta := "de " + Esc(text(raid["owner"]))
		tone := ""
		if limit > 0 && clause > limit {
			meta += " · ⚠ no se pagará: su cláusula pasa tu límite"
			tone = "warn"
		} else if why := text(raid["why"]); why != "" {
			meta += " · " + Esc(why)
		}
		chip := ""
		if truthy(player["clause_locked"]) {
			chip = clock(text(player["clause_locked_until"]), "se abre")
		}
		action := button("Cambiar límite", "ghost", "raid-btn", fmt.Sprintf(` data-raid="%s" `+
			`data-raid-name="%s" data-raid-max="%d" data-raid-clause="%d"`,
			Esc(text(raid["player_id"])), Esc(text(raid["name"])), int64(limit), int64(clause)))
		scheduled = append(scheduled, row(player, meta, "≤ "+esMoney(limit),
			"cláusula "+esMoney(clause), chip, action, tone))
	}
	body = empty("Ninguno programado: se programan desde la ficha de un rival.")
	if len(scheduled) > 0 {
		body = rowList(scheduled, false)
	}
	main += block("Clausulazos programados", body, "se pagan solos al abrirse", len(scheduled))

	clauses, _, _ := d.buyOptions()
	var payable []string
	for _, item := range clauses {
		if truthy(item["affordable"]) && len(payable) < 3 {
			payable = append(payable, d.buyRow(item, window))
		}
	}
	body = empty("Ninguna cláusula pagable mejora tu once.")
	if len(payable) > 0 {
		body = rowList(payable, false)
	}
	body += `<p class="mk-note"><button class="linkish" type="button" data-goto="comprar">` +
		`Todas en Comprar →</button></p>`
	main += block("Pagables hoy que mejoran tu once", body, "", -1)
	return view("v-clausulas", "clausulas", main, d.unlockCalendar())
}

// unlockCalendar is the next seven days of clauses coming unlocked: yours to watch, and the
// rivals' within reach that would add most to your eleven.
func (d Document) unlockCalendar() string {
	now := time.Now()
	horizon := now.Add(7 * 24 * time.Hour)
	squad := rows(d.Advice["squad"])
	_, base := bestElevenOf(squad)
	cash := number(d.Advice["budget"])
	type day struct {
		key, label   string
		mine, theirs []map[string]any
	}
	days := map[string]*day{}
	for _, player := range rows(d.Universe["players"]) {
		if text(player["owner"]) == "" || !truthy(player["clause_locked"]) {
			continue
		}
		when, ok := parseStamp(text(player["clause_locked_until"]))
		if !ok || !when.After(now) || when.After(horizon) {
			continue
		}
		local := when.Local()
		key := local.Format("2006-01-02")
		if days[key] == nil {
			days[key] = &day{key: key, label: esDay(text(player["clause_locked_until"]))}
		}
		if truthy(player["is_mine"]) {
			days[key].mine = append(days[key].mine, player)
		} else if number(player["clause"]) <= cash {
			days[key].theirs = append(days[key].theirs, player)
		}
	}
	keys := make([]string, 0, len(days))
	for key := range days {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, key := range keys {
		entry := days[key]
		var body strings.Builder
		for _, player := range entry.mine {
			fmt.Fprintf(&body, `<span class="mk-cal-mine" data-pid="%s">🛡 %s</span>`,
				Esc(text(player["id"])), Esc(text(player["name"])))
		}
		type gained struct {
			player map[string]any
			gain   float64
		}
		var best []gained
		for _, player := range entry.theirs {
			_, with := bestElevenOf(append(append([]map[string]any{}, squad...), player))
			if gain := with - base; gain > 1 {
				best = append(best, gained{player, gain})
			}
		}
		sort.SliceStable(best, func(one, two int) bool { return best[one].gain > best[two].gain })
		for index, item := range best {
			if index == 3 {
				break
			}
			fmt.Fprintf(&body, `<span class="mk-cal-them" data-pid="%s">%s <i>%s</i></span>`,
				Esc(text(item.player["id"])), Esc(text(item.player["name"])), esSigned(item.gain))
		}
		fmt.Fprintf(&body, `<span class="meta">%d a tu alcance</span>`, len(entry.theirs))
		fmt.Fprintf(&out, `<li class="mk-cal"><span class="mk-cal-day">%s</span>`+
			`<span class="mk-cal-body">%s</span></li>`, entry.label, body.String())
	}
	body := empty("Ninguna cláusula se abre en los próximos 7 días.")
	if out.Len() > 0 {
		body = `<ul class="calendar">` + out.String() + `</ul><p class="mk-note">🛡 tuyas que se ` +
			`abren · en gris, las de rivales que más suman a tu once (xPts)</p>`
	}
	return block("Próximos 7 días", body, "", -1)
}

// --- Plantilla -------------------------------------------------------------------------

func (d Document) squadView() string {
	squad := rows(d.Advice["squad"])
	if len(squad) == 0 {
		return ""
	}
	// Only the ones the advice gives a reason to sell, with that reason.
	var items []string
	for _, player := range rows(d.Advice["sells"]) {
		reasons := asStrings(player["reasons"])
		if len(reasons) == 0 {
			continue
		}
		listing := mapOf(player["market"])
		best := 0.0
		offerID := ""
		for _, offer := range rows(player["offers"]) {
			if amount := number(offer["money"]); amount > best {
				best, offerID = amount, text(offer["id"])
			}
		}
		when := "se puede ya"
		if truthy(player["sale_locked"]) {
			when = "🔒 hasta " + esDay(text(player["hold_until"]))
		}
		note := "vale " + esMoney(number(player["value"]))
		if best > 0 {
			note += " · oferta " + esMoney(best)
		}
		var action string
		switch {
		case best > 0 && offerID != "":
			action = button("Aceptar "+esMoney(best), "primary", "op", fmt.Sprintf(
				` data-op="accept_offer" data-op-market="%s" data-op-offer="%s" data-op-player="%s" `+
					`data-op-name="%s" data-op-amount="%d"`, Esc(text(listing["market_id"])),
				Esc(offerID), Esc(text(player["id"])), Esc(text(player["name"])), int64(best)))
		case text(listing["market_id"]) != "":
			action = actButton("Quitar", "ghost", text(player["id"]), "withdraw")
		case !truthy(player["sale_locked"]):
			action = actButton("Poner en venta", "ghost", text(player["id"]), "sell_to_market")
		}
		meta := Esc(text(player["team_short"])) + " · " + Esc(strings.Join(reasons, ", "))
		items = append(items, row(player, meta, esNum(number(player["xpts"]), 1)+" xPts", note,
			tag(when, map[bool]string{true: "warn", false: "ok"}[truthy(player["sale_locked"])]),
			action, ""))
	}
	body := empty("Nadie: el consejo no ve motivo para vender a ninguno.")
	if len(items) > 0 {
		body = rowList(items, false)
	}
	main := block("Para vender", body, "", len(items))

	value, clauses, change := 0.0, 0.0, 0.0
	for _, player := range squad {
		worth := number(player["value"])
		value += worth
		clauses += number(player["clause"])
		if pct := number(player["pct_7d"]); pct > -100 {
			change += worth * pct / (100 + pct)
		}
	}
	changeClass, sign := "up", "+"
	if change < 0 {
		changeClass, sign = "down", ""
	}
	summary := fmt.Sprintf(`<div class="kv"><span>Jugadores</span><b>%d</b></div>`+
		`<div class="kv"><span>Valor de plantilla</span><b>%s</b></div>`+
		`<div class="kv"><span>Suma de cláusulas</span><b>%s</b></div>`+
		`<div class="kv"><span>Valor últimos 7 días</span><b class="%s">%s%s</b></div>`,
		len(squad), esMoney(value), esMoney(clauses), changeClass, sign, esMoney(change))
	return view("v-plantilla", "plantilla", main, block("Resumen", summary, "", -1))
}

// --- Partidos --------------------------------------------------------------------------

func (d Document) matchesView() string {
	fixtures := rows(d.Universe["fixtures"])
	sort.SliceStable(fixtures, func(one, two int) bool {
		return text(fixtures[one]["kickoff"]) < text(fixtures[two]["kickoff"])
	})
	squad := rows(d.Advice["squad"])
	choice, total := bestElevenOf(squad)
	starter := map[string]bool{}
	for _, id := range choice.IDs() {
		starter[id] = true
	}
	byTeam := map[string][]map[string]any{}
	for _, player := range squad {
		byTeam[text(player["team_id"])] = append(byTeam[text(player["team_id"])], player)
	}
	mineIn := func(fixture map[string]any) []map[string]any {
		out := append(append([]map[string]any{}, byTeam[text(fixture["local_id"])]...),
			byTeam[text(fixture["visitor_id"])]...)
		sort.SliceStable(out, func(one, two int) bool {
			return number(out[one]["xpts"]) > number(out[two]["xpts"])
		})
		return out
	}
	week := mapOf(d.Universe["week"])
	weekNumber := int(number(week["weekNumber"]))
	teams := mapOf(d.Universe["league_teams"])
	me := mapOf(teams[text(d.Universe["my_team_id"])])

	card, players := "", ""
	if len(fixtures) > 0 {
		first, last := fixtures[0], fixtures[len(fixtures)-1]
		match := func(fixture map[string]any) string {
			return text(fixture["local"]) + " – " + text(fixture["visitor"])
		}
		average := ""
		if weekNumber > 1 {
			average = fmt.Sprintf("media real %s pts/jornada",
				esNum(number(me["points"])/float64(weekNumber-1), 1))
		}
		card = fmt.Sprintf(`<div class="block"><div class="matchday">`+
			`<div><span class="k">Primer partido · se cierra tu alineación</span>`+
			`<span class="v" data-deadline="%s" data-plain="1">%s</span><span class="s">%s · %s</span></div>`+
			`<div><span class="k">Último partido</span><span class="v">%s</span>`+
			`<span class="s">%s · %d tuyos</span></div>`+
			`<div><span class="k">Tu previsión</span><span class="v">%s xPts</span>`+
			`<span class="s">%s</span></div></div></div>`,
			Esc(text(first["kickoff"])), LeftUntil(text(first["kickoff"])), Esc(match(first)),
			Esc(esWhen(text(first["kickoff"]))), Esc(esHour(text(last["kickoff"]))),
			Esc(match(last)), len(mineIn(last)), esNum(total, 1), average)

		var list []string
		var idle []string
		for _, fixture := range fixtures {
			mine := mineIn(fixture)
			if len(mine) == 0 {
				idle = append(idle, Esc(match(fixture))+" "+Esc(esHour(text(fixture["kickoff"]))))
				continue
			}
			var chips strings.Builder
			for _, player := range mine {
				bench := ""
				if !starter[text(player["id"])] {
					bench = " bench"
				}
				fmt.Fprintf(&chips, `<span class="tchip %s%s" data-pid="%s">%s<span class="tname">%s</span>`+
					`<span class="tx">%s</span></span>`, xptsClass(number(player["xpts"])), bench,
					Esc(text(player["id"])), face(player, "xs"), shieldName(player),
					esNum(number(player["xpts"]), 1))
			}
			score := ""
			if fixture["local_score"] != nil && fixture["visitor_score"] != nil {
				score = fmt.Sprintf(" <span class=\"fx-score\">%d-%d</span>",
					int(number(fixture["local_score"])), int(number(fixture["visitor_score"])))
			}
			list = append(list, fmt.Sprintf(`<li class="fx"><span class="fx-when">%s</span>`+
				`<span class="fx-match">%s<b>%s – %s</b>%s%s</span><span class="chips">%s</span></li>`,
				Esc(esHour(text(fixture["kickoff"]))), crestOf(text(fixture["local_id"])),
				Esc(text(fixture["local"])), Esc(text(fixture["visitor"])),
				crestOf(text(fixture["visitor_id"])), score, chips.String()))
		}
		body := `<ul class="fixtures">` + strings.Join(list, "") + `</ul>`
		if len(idle) > 0 {
			body += folded(fmt.Sprintf("%d partidos sin tuyos", len(idle)),
				`<p class="mk-note">`+strings.Join(idle, " · ")+`</p>`)
		}
		players = block(fmt.Sprintf("Tus jugadores en la J%d", weekNumber), body,
			"en gris, los del banquillo", -1)
	} else {
		players = empty("Sin partidos de esta jornada en el calendario.")
	}
	main := players + d.matchdayBoard() + card
	aside := block("Previsto vs real", fmt.Sprintf(`<ul class="history" id="pv-history" `+
		`data-week="%d" data-planned="%.2f"><li class="mk-note">Cargando…</li></ul>`+
		`<p class="mk-legend"><span class="sw real"></span>tus puntos y puesto · `+
		`<span class="sw fore"></span>previsto · toca una jornada para verla entera</p>`,
		weekNumber, total), `<span id="pv-sub"></span>`, -1)
	return view("v-partidos", "partidos", main, aside)
}
