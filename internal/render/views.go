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

