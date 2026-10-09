package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// The tabs' first screens: compact rows, cards and an aside, the way the approved mockup draws
// them. Every figure is read off the advice layer or the universe.

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
		badge = `<span class="hb hb-cross"><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M6.4 2.5h3.2v3.9h3.9v3.2H9.6v3.9H6.4V9.6H2.5V6.4h3.9z"/></svg></span>`
	case "card":
		badge = `<span class="hb hb-card"><svg viewBox="0 0 16 16" aria-hidden="true"><rect x="4.5" y="2.5" width="7" height="11" rx="1.3"/></svg></span>`
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
		`<span class="left">%s</span></span>`, Esc(stamp),
		Esc(strings.TrimSpace(label+" "+esWhen(stamp))), Esc(esWhen(stamp)))
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

// viewWithRow is a view with a second row under both columns: two blocks side by side, full
// width. On a phone it goes before the aside.
func viewWithRow(id, tab, main, aside, left, right string) string {
	return fmt.Sprintf(`<section id="%s" data-tab="%s" class="mk"><div class="layout with-row">`+
		`<div class="main">%s</div><aside class="side">%s</aside>`+
		`<div class="row2"><div class="duo">%s%s</div></div></div></section>`,
		id, tab, main, aside, left, right)
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

// Views are every tab's screen.
func (d Document) Views() []string {
	if len(d.Advice) == 0 {
		return nil
	}
	return []string{d.decideSection(), d.buyView(), d.sellView(), d.clauseView(), Pitch,
		d.squadView(), d.matchesView(), d.rivalsView()}
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
		note = `<span class="big-note ` + card.NoteClass + `">` + Esc(card.BigNote) + `</span>`
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
		clock(card.Deadline, card.DeadlineLabel)+clock(card.Deadline2, card.DeadlineLabel2),
		Esc(card.Verb), Esc(card.Big), note,
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
		`</div>`+d.planWarnings()+roleDrops(lines)+finish, Esc(shape), -1) + d.crackBox() + more
}

// roleDrops are the starters futbolfantasy's editors just moved down in their club.
func roleDrops(lines [][]map[string]any) string {
	var out strings.Builder
	for _, line := range lines {
		for _, player := range line {
			role := mapOf(player["role"])
			if text(role["change"]) != "down" {
				continue
			}
			note := ""
			if text(role["note"]) != "" {
				note = ": " + text(role["note"])
			}
			fmt.Fprintf(&out, `<p class="mk-note plan-warn" data-pid="%s">⚠ %s bajó a %s en su `+
				`equipo%s</p>`, Esc(text(player["id"])), Esc(text(player["name"])),
				Esc(text(role["label"])), Esc(note))
		}
	}
	return out.String()
}

// planWarnings are the plan's warnings about the eleven itself: a squad that cannot field
// eleven, or a position with nobody to spare.
func (d Document) planWarnings() string {
	var out strings.Builder
	for _, warning := range asStrings(d.Swaps["warnings"]) {
		out.WriteString(`<p class="mk-note plan-warn">⚠ ` + Esc(warning) + `</p>`)
	}
	return out.String()
}

// --- Comprar ---------------------------------------------------------------------------

// buyRow is a signing ranked by what it adds to the eleven, with the button that makes it.
// A clause is paid with today's cash; a market bid may also use the debt the league allows,
// and one that would start the matchday in the red says so.
func (d Document) buyRow(item map[string]any, route string, gain float64,
	window *windowState) string {
	cost := number(item["entry_cost"])
	listing := mapOf(item["market"])
	id, name := text(item["id"]), text(item["name"])
	cash, reach := d.buyingPower()
	if route == "clausula" {
		reach = cash
	}
	affordable := cost <= reach
	value := esNum(number(item["xpts"]), 1) + " xPts"
	if gain > MinShownGain {
		value = esSigned(gain) + " xPts"
	}
	note := esMoney(cost)
	if asking := number(item["asking"]); asking > 0 && asking < cost*0.99 {
		note += " · piden " + esMoney(asking)
	}
	if route != "clausula" {
		if ceiling := number(item["ideal_bid"]); ceiling > 0 {
			class, mark := "up", "+"
			if ceiling < cost {
				class, mark = "down", ""
			}
			note += fmt.Sprintf(` · <span class="%s">%s%s de margen</span>`, class, mark,
				esMoney(ceiling-cost))
		} else {
			note += ` · <span class="down">sin margen</span>`
		}
	}
	kind := "ghost"
	if gain > MinShownGain && d.worthItsPrice(item, route) {
		kind = "primary"
	}
	var chip, action, verb string
	switch route {
	case "clausula":
		verb = "Programar"
		if window.open {
			verb = "Pagar"
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
		chip = clock(text(listing["expires"]), "sale del mercado")
		verb = "Ofrecer"
		action = d.listingButton(item, "Ofrecer "+esMoney(cost), kind)
	default:
		chip = clock(text(listing["expires"]), "sale del mercado")
		verb = "Pujar"
		action = d.listingButton(item, "Pujar "+esMoney(cost), kind)
	}
	tone := ""
	switch {
	case !affordable:
		tone = "dim"
		short := "te faltan " + esMoney(cost-reach)
		action = `<span title="` + short + `">` + button(verb+" "+esMoney(cost), "ghost", "",
			` disabled title="`+short+`"`) + `</span>`
	case cost > cash:
		chip = `<span class="mk-chip soon" title="si empiezas la jornada en negativo no puntúas">` +
			`⚠ en negativo</span>` + chip
	}
	return rowWith(item, filterAttrs(item, cost), "", value, note, chip, action, tone)
}

// worthItsPrice is whether the price is one to recommend: within futbolfantasy's ceiling on the
// market, and for a clause the advice's own verdict that it returns more per million than the
// squad does.
func (d Document) worthItsPrice(item map[string]any, route string) bool {
	if route != "clausula" {
		ceiling := number(item["ideal_bid"])
		return ceiling > 0 && ceiling >= number(item["entry_cost"])
	}
	for _, raid := range append(rows(d.Advice["raids"]), rows(d.Advice["upcoming_raids"])...) {
		if text(raid["id"]) == text(item["id"]) {
			verdict := text(raid["verdict"])
			return verdict == "chollo" || verdict == "renta"
		}
	}
	return false
}

// buyingPower is today's cash, and what a market bid can reach with the debt the league allows
// over the squad's value.
func (d Document) buyingPower() (cash, reach float64) {
	cash = number(d.Advice["budget"])
	squad := 0.0
	for _, player := range rows(d.Advice["squad"]) {
		squad += number(player["value"])
	}
	return cash, cash + squad*d.MaxDebtPct/100
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

// buyBlock is every candidate of one route, best for the eleven first and the ones out of
// reach at the end, in a box that scrolls.
func (d Document) buyBlock(title, route string, items []map[string]any, window *windowState) string {
	gains := d.gains(route)
	cash, reach := d.buyingPower()
	if route == "clausula" {
		reach = cash
	}
	sort.SliceStable(items, func(one, two int) bool {
		first, second := items[one], items[two]
		if inOne, inTwo := number(first["entry_cost"]) <= reach,
			number(second["entry_cost"]) <= reach; inOne != inTwo {
			return inOne
		}
		gainOne, gainTwo := gains[text(first["id"])], gains[text(second["id"])]
		if (gainOne > MinShownGain) != (gainTwo > MinShownGain) {
			return gainOne > MinShownGain
		}
		if gainOne > MinShownGain && gainOne != gainTwo {
			return gainOne > gainTwo
		}
		return number(first["xpts"]) > number(second["xpts"])
	})
	var lines []string
	for _, item := range items {
		lines = append(lines, d.buyRow(item, route, gains[text(item["id"])], window))
	}
	body := empty("Nadie ahora mismo.")
	if len(lines) > 0 {
		body = scrollList(lines, 420)
	}
	return block(title, body, "", len(lines))
}

// gains is what each candidate of a route adds to the best eleven.
func (d Document) gains(route string) map[string]float64 {
	out := map[string]float64{}
	for _, item := range rows(d.Money["bargains"]) {
		if text(item["route"]) == route {
			out[text(item["id"])] = math.Max(out[text(item["id"])], number(item["xi_gain"]))
		}
	}
	return out
}

// buyOptions are the three routes' candidates: the clauses priced by the advice layer, and
// every listing of the market and of the rivals.
func (d Document) buyOptions() (clauses, offers, free []map[string]any) {
	for _, item := range rows(d.Money["bargains"]) {
		if text(item["route"]) == "clausula" {
			clauses = append(clauses, item)
		}
	}
	listed := func(source any) []map[string]any {
		var out []map[string]any
		for _, item := range rows(source) {
			if !truthy(item["is_mine"]) && text(mapOf(item["market"])["market_id"]) != "" {
				out = append(out, item)
			}
		}
		return out
	}
	return clauses, listed(d.Advice["asks"]), listed(d.Advice["bids_now"])
}

func (d Document) buyView() string {
	window := d.window()
	clauses, offers, free := d.buyOptions()
	main := `<div class="mk-filters">` + Filters + `</div>` +
		d.buyBlock("Mercado rentable", "puja libre", free, window)

	var bids []string
	for _, bid := range rows(d.Advice["my_bids"]) {
		bids = append(bids, row(bid, "",
			esMoney(number(bid["my_bid"])), "pide "+esMoney(number(bid["asking"])),
			clock(text(bid["closes"]), "cierra"), BidButton(bid), ""))
	}
	bids = append(bids, d.raidRows()...)
	body := empty("Ninguna ahora mismo.")
	if len(bids) > 0 {
		body = rowList(bids)
	}
	return viewWithRow("v-comprar", "comprar", main, block("Mis pujas en curso", body, "", -1)+
		d.endingsAside()+d.starredAside(),
		d.buyBlock("En venta por rivales", "oferta al dueño", offers, window),
		d.buyBlock("Cláusulas que puedes pagar", "clausula", clauses, window))
}

// outcomeRow is a resolved bid, offer or standing order as a list row: the icon of how it
// ended where the face would be.
func outcomeRow(id, name, outcome, why, what, date string, amount float64) string {
	value := ""
	if amount > 0 {
		value = esMoney(amount)
	}
	return ListRow{Lead: `<span class="rank-dot out-icon">` + outcomeIcon(outcome, why) + `</span>`,
		Name: Esc(name), Meta: Esc(what), Value: value, Note: date,
		Attrs: ` data-pid="` + Esc(id) + `"`}.HTML()
}

func shortDate(stamp string) string {
	if when, ok := parseStamp(stamp); ok {
		return when.Format("02/01")
	}
	return ""
}

// raidRows are the scheduled clausulazos still standing, as rows of what I have put up: the
// limit, the clause today and whether it already passes it, when it opens, and the way out.
func (d Document) raidRows() []string {
	byID := d.playersByID()
	var out []string
	for _, raid := range d.Raids {
		if raidsStandingDown[text(raid["action"])] {
			continue
		}
		player := byID[text(raid["player_id"])]
		if player == nil {
			player = map[string]any{"id": raid["player_id"], "name": raid["name"],
				"owner": raid["owner"]}
		}
		clause, limit := number(raid["clause"]), number(raid["max_pay"])
		note := "cláusula " + esMoney(clause)
		if limit > 0 && clause > limit {
			note += ` · <span class="down">pasa tu límite</span>`
		}
		var chip string
		switch {
		case truthy(player["shielded"]):
			chip = tag("🛡 blindado", "done")
		case truthy(player["clause_locked"]):
			chip = clock(text(player["clause_locked_until"]), "se abre su cláusula")
		default:
			chip = tag("pagable", "ok")
		}
		action := button("Cancelar", "ghost", "op", fmt.Sprintf(` data-op="cancel_raid" `+
			`data-op-player="%s" data-op-name="%s"`, Esc(text(raid["player_id"])),
			Esc(text(raid["name"]))))
		line := playerRow(player)
		line.Value, line.Note, line.Why = "hasta "+esMoney(limit), note, "clausulazo programado"
		line.Chip, line.Action = chip, action
		out = append(out, line.HTML())
	}
	return out
}

func (d Document) endingsAside() string {
	var lines []string
	for _, ending := range d.Endings {
		outcome := text(ending["outcome"])
		what := outcome
		switch {
		case outcome == "perdida" && text(ending["new_owner"]) != "":
			what = "perdida · ganó " + text(ending["new_owner"])
			if paid := number(ending["won_for"]); paid > 0 {
				what += " con " + esMoney(paid)
			}
		case outcome == "rechazada" && text(ending["who"]) != "":
			what = "rechazada por " + text(ending["who"])
		}
		detail := ""
		if outcome == "perdida" && text(ending["new_owner"]) != "" {
			detail = "ganó " + text(ending["new_owner"])
			if paid := number(ending["won_for"]); paid > 0 {
				detail += " (" + esMoney(paid) + ")"
			}
		}
		lines = append(lines, outcomeRow(text(ending["player_id"]), text(ending["player"]),
			outcome, detail, what, shortDate(text(ending["at"])), number(ending["amount"])))
	}
	body := empty("Todavía no se ha resuelto ninguna.")
	if len(lines) > 0 {
		body = scrollList(lines, 420)
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

// --- Cláusulas -------------------------------------------------------------------------

func (d Document) clauseView() string {
	window := d.window()
	var worth, rest []map[string]any
	for _, raise := range rows(d.Raise["rows"]) {
		switch text(raise["verdict"]) {
		case "sube", "no te llega":
			if number(raise["pay"]) > 0 && d.raiseWanted(raise) {
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
		if risk := number(raise["risk"]); risk > 0 {
			meta += fmt.Sprintf(" · riesgo %.0f %%", risk*100)
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
		line := playerRow(raise)
		line.Value = esMoney(number(raise["target_clause"]))
		line.Note = fmt.Sprintf("hoy %s · −%s xPts si se va", esMoney(number(raise["clause"])),
			esNum(number(raise["xi_drop"]), 1))
		line.Why, line.Chip, line.Action, line.Tone = Esc(meta), clock(deadline, label), action, tone
		items = append(items, line.HTML())
	}
	body := empty("Ninguna cláusula tuya merece subirse ahora.")
	if len(items) > 0 {
		body = rowList(items)
	}
	if len(rest) > 0 {
		var lines []string
		for _, raise := range rest {
			cost, note := "", ""
			if pay := number(raise["pay"]); pay > 0 {
				cost, note = esMoney(pay), "subirla"
			}
			verdict, why := text(raise["verdict"]), text(raise["why"])
			if !d.raiseWanted(raise) {
				verdict = "déjalo ir"
				why = fmt.Sprintf("su cláusula ya es %s su valor: si se lo llevan, es buena venta",
					strings.TrimSuffix(ratioNote(number(raise["clause"])/number(raise["value"])),
						" su valor"))
			}
			line := playerRow(raise)
			line.Value, line.Note, line.Why = cost, Esc(verdict), Esc(why)
			if note != "" {
				line.Note = Esc(verdict) + " · " + note
			}
			lines = append(lines, line.HTML())
		}
		body += folded(fmt.Sprintf("%d que no merece la pena subir", len(rest)), rowList(lines))
	}
	main := block("Recomendaciones", body, "", len(items))

	byID := d.playersByID()
	var scheduled []string
	for _, raid := range d.Raids {
		player := byID[text(raid["player_id"])]
		if player == nil {
			player = map[string]any{"id": raid["player_id"], "name": raid["name"],
				"owner": raid["owner"]}
		}
		clause, limit := number(raid["clause"]), number(raid["max_pay"])
		tone := ""
		standing := raidsStandingDown[text(raid["action"])]
		extra, why := "", text(raid["why"])
		if limit > 0 && clause > limit {
			extra = tg("⚠ pasa tu límite", "tg-warn")
			why = "no se pagará: su cláusula pasa tu límite"
			tone = "warn"
		}
		chip := ""
		switch {
		case standing:
			chip = outcomeIcon(text(raid["action"]), text(raid["why"])) +
				tag(Esc(strings.ReplaceAll(text(raid["action"]), "_", " ")), "warn")
			tone = "warn"
		case truthy(player["clause_locked"]):
			chip = clock(text(player["clause_locked_until"]), "se abre")
		}
		action := button("Cambiar límite", "ghost", "raid-btn", fmt.Sprintf(` data-raid="%s" `+
			`data-raid-name="%s" data-raid-max="%d" data-raid-clause="%d"`,
			Esc(text(raid["player_id"])), Esc(text(raid["name"])), int64(limit), int64(clause)))
		if standing {
			action += button("Cancelar", "ghost", "op", fmt.Sprintf(` data-op="cancel_raid" `+
				`data-op-player="%s" data-op-name="%s"`, Esc(text(raid["player_id"])),
				Esc(text(raid["name"]))))
		}
		line := playerRow(player)
		line.Tags += extra
		line.Value, line.Note, line.Why = "≤ "+esMoney(limit), "cláusula "+esMoney(clause), Esc(why)
		line.Chip, line.Action, line.Tone = chip, action, tone
		scheduled = append(scheduled, line.HTML())
	}
	body = empty("Ninguno programado: se programan desde la ficha de un rival.")
	if len(scheduled) > 0 {
		body = rowList(scheduled)
	}
	body += d.orderHistory()
	planned := block("Clausulazos programados", body, "se pagan solos al abrirse, si siguen por "+
		"debajo de tu límite", len(scheduled))

	clauses, _, _ := d.buyOptions()
	gains := d.gains("clausula")
	sort.SliceStable(clauses, func(one, two int) bool {
		return gains[text(clauses[one]["id"])] > gains[text(clauses[two]["id"])]
	})
	var payable []string
	listed := map[string]bool{}
	for _, item := range clauses {
		gain := gains[text(item["id"])]
		if truthy(item["affordable"]) && gain > MinShownGain && len(payable) < 3 {
			listed[text(item["id"])] = true
			payable = append(payable, d.buyRow(item, "clausula", gain, window))
		}
	}
	body = empty("Ninguna cláusula pagable mejora tu once.")
	if len(payable) > 0 {
		body = rowList(payable)
	}
	body += `<p class="mk-note"><button class="linkish" type="button" data-goto="comprar">` +
		`Todas en Comprar →</button></p>`
	main += block("Pagables hoy que mejoran tu once", body, "", -1)
	main += d.cheapClauses(listed)
	return view("v-clausulas", "clausulas", main, planned+d.unlockCalendar())
}

// orderHistory is what became of the standing orders no longer armed: paid or cancelled, and
// when. The game keeps no record of them.
func (d Document) orderHistory() string {
	if len(d.Orders) == 0 {
		return ""
	}
	var lines []string
	for _, order := range d.Orders {
		outcome := text(order["outcome"])
		what := outcome
		if why := text(order["why"]); why != "" {
			what += " · " + why
		}
		lines = append(lines, outcomeRow(text(order["player_id"]), text(order["player"]),
			outcome, text(order["why"]), what, shortDate(text(order["at"])),
			number(order["amount"])))
	}
	return folded(fmt.Sprintf("Historial de tus órdenes · %d", len(lines)), scrollList(lines, 320))
}

// cheapClauses are the clauses that return more points per million than the squad already
// does: rarely for the eleven, but the cheap way to fill the bench.
func (d Document) cheapClauses(seen map[string]bool) string {
	var found []map[string]any
	for _, item := range append(rows(d.Advice["raids"]), rows(d.Advice["upcoming_raids"])...) {
		id := text(item["id"])
		verdict := text(item["verdict"])
		if seen[id] || (verdict != "chollo" && verdict != "renta") || truthy(item["shielded"]) {
			continue
		}
		seen[id] = true
		found = append(found, item)
	}
	if len(found) == 0 {
		return ""
	}
	sort.SliceStable(found, func(one, two int) bool {
		return number(found[one]["vs_market"]) > number(found[two]["vs_market"])
	})
	var items []string
	for _, item := range found {
		chip := tag("pagable", "ok")
		if stamp := text(item["unlock_at"]); stamp != "" {
			if when, ok := parseStamp(stamp); ok && when.After(time.Now()) {
				chip = clock(stamp, "se abre su cláusula")
			}
		}
		note := fmt.Sprintf("%s · %s pts/M de tu plantilla", Esc(text(item["verdict"])),
			esRatio(number(item["vs_market"])))
		action := strings.Replace(RaidButton(item), `class="raid-btn"`,
			`class="mb mb-ghost raid-btn"`, 1)
		items = append(items, row(item, "", esMoney(number(item["clause"])), note, chip,
			action, ""))
	}
	return block("Baratas que rentan", scrollList(items, 420), "más puntos por millón que tu plantilla: para el "+
		"banquillo", len(items))
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
	for _, player := range d.sellRows() {
		reasons := asStrings(player["reasons"])
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
		line := playerRow(player)
		line.Value, line.Note, line.Why = esNum(number(player["xpts"]), 1)+" xPts", note,
			Esc(strings.Join(reasons, ", "))
		if !truthy(player["sale_locked"]) {
			line.Chip = tag(when, "ok")
		}
		line.Action = action
		items = append(items, line.HTML())
	}
	body := empty("Nadie: el consejo no ve motivo para vender a ninguno.")
	if len(items) > 0 {
		body = rowList(items)
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
	if projected, ok := d.Money["projected_7d"]; ok {
		ahead, mark := "up", "+"
		if number(projected) < 0 {
			ahead, mark = "down", ""
		}
		summary += fmt.Sprintf(`<div class="kv"><span>Previsión próximos 7 días</span>`+
			`<b class="%s">%s%s</b></div>`, ahead, mark, esMoney(number(projected)))
	}
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
		weekNumber, total)+d.forecastHit(), `<span id="pv-sub"></span>`, -1)
	return view("v-partidos", "partidos", main, aside+d.comingWeeks(weekNumber))
}

// forecastHit is how the last matchday played compared with its forecast, mine and the
// league's error per player.
func (d Document) forecastHit() string {
	last := mapOf(mapOf(d.Universe["forecast_review"])["last"])
	if number(last["counted"]) == 0 {
		return ""
	}
	line := fmt.Sprintf("Así acertó la previsión en la J%d: ", int(number(last["week"])))
	if mine := mapOf(last["mine"]); number(mine["counted"]) > 0 {
		forecast, actual := number(mine["forecast"]), number(mine["actual"])
		line += fmt.Sprintf("previsto %s · real %s (%s). ", esNum(forecast, 1),
			esNum(actual, 1), esSigned(actual-forecast))
	}
	line += fmt.Sprintf("En la liga falló %s puntos por jugador, de media.",
		esNum(number(last["mean_abs_error"]), 1))
	return `<p class="mk-note">` + Esc(line) + `</p>`
}

// comingWeeks is the fixture list after this matchday: how many of mine play each week and
// where, plus any match of a past week still to be played.
func (d Document) comingWeeks(current int) string {
	mine := map[string]int{}
	for _, player := range rows(d.Advice["squad"]) {
		mine[text(player["team_id"])]++
	}
	type week struct {
		label   string
		kickoff string
		yours   int
		matches []string
	}
	weeks := map[int]*week{}
	var order []int
	for _, fixture := range rows(d.Universe["schedule"]) {
		round := int(number(fixture["week"]))
		if int(number(fixture["state"])) == FinishedMatch || round == current {
			continue
		}
		key := round
		if round < current {
			key = 0
		}
		entry := weeks[key]
		if entry == nil {
			entry = &week{label: fmt.Sprintf("J%d", round), kickoff: text(fixture["kickoff"])}
			if key == 0 {
				entry.label = "Aplazados"
			}
			weeks[key] = entry
			order = append(order, key)
		}
		if text(fixture["kickoff"]) < entry.kickoff {
			entry.kickoff = text(fixture["kickoff"])
		}
		local, visitor := text(fixture["local_id"]), text(fixture["visitor_id"])
		count := mine[local] + mine[visitor]
		entry.yours += count
		if count > 0 || key == 0 {
			entry.matches = append(entry.matches, fmt.Sprintf(
				`<span class="mk-cal-them">%s%s–%s%s <i>%d</i></span>`, crestOf(local),
				Esc(text(fixture["local"])), Esc(text(fixture["visitor"])), crestOf(visitor), count))
		}
	}
	sort.Ints(order)
	var items strings.Builder
	shown := 0
	for _, key := range order {
		entry := weeks[key]
		if key > 0 {
			if shown == 3 {
				continue
			}
			shown++
		}
		fmt.Fprintf(&items, `<li class="mk-cal"><span class="mk-cal-day">%s<span class="meta">%s</span>`+
			`</span><span class="mk-cal-body">%s<span class="meta">%s</span></span></li>`,
			entry.label, Esc(esDay(entry.kickoff)), strings.Join(entry.matches, ""),
			counted(entry.yours, "tuyo", "tuyos"))
	}
	if items.Len() == 0 {
		return ""
	}
	return block("Próximas jornadas", `<ul class="calendar">`+items.String()+`</ul>`+
		`<p class="mk-note">cada partido con cuántos tuyos juegan</p>`, "", -1)
}
