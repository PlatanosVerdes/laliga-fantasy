package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/eleven"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
)

// MaxCards is how many decisions "Qué hacer ahora" shows before sending the rest to the detail.
const MaxCards = 5

// Card is one decision: who, what to do, the one number that decides it, until when, why, and
// the button that does it. Every card is read off a judgement the advice layer already made.
type Card struct {
	Kind          string
	Key           string
	Player        map[string]any
	In            map[string]any
	Verb          string
	Big           string
	BigNote       string
	Deadline      string
	DeadlineLabel string
	Why           []string
	Button        string
	Impact        string
	Tone          string
	// Weight is the impact in xPts per matchday. Money is converted at the squad's own points
	// per million, the bar the swap plan already measures every purchase against.
	Weight float64
	// Cash is what doing it does to the balance: positive comes in, negative goes out.
	Cash float64
}

// decisionCards is every card the advice supports, already in the order to act on them.
func (d Document) decisionCards(now time.Time) []Card {
	if len(d.Advice) == 0 {
		return nil
	}
	rate := number(d.Advice["squad_ppm_benchmark"])
	actions := d.actionRows()
	var cards []Card
	cards = append(cards, d.offerCards(actions, rate)...)
	cards = append(cards, d.raiseCards()...)
	cards = append(cards, d.swapCards()...)
	cards = append(cards, d.signingCards(actions)...)
	return orderCards(cards, now)
}

// orderCards puts what expires first on top, an hour being the grain at which two deadlines
// count as the same one, and within the hour what moves the eleven most. A card whose deadline
// has passed is gone, and only the first card per key survives: selling one of yours and
// defending his clause cannot both be the thing to do.
func orderCards(cards []Card, now time.Time) []Card {
	type keyed struct {
		card   Card
		bucket int64
	}
	list := make([]keyed, 0, len(cards))
	for _, card := range cards {
		bucket := int64(math.MaxInt64)
		if when, ok := parseStamp(card.Deadline); ok {
			if when.Before(now) {
				continue
			}
			bucket = when.Truncate(time.Hour).Unix()
		}
		list = append(list, keyed{card, bucket})
	}
	sort.SliceStable(list, func(one, two int) bool {
		if list[one].bucket != list[two].bucket {
			return list[one].bucket < list[two].bucket
		}
		return list[one].card.Weight > list[two].card.Weight
	})
	seen := map[string]bool{}
	out := make([]Card, 0, len(list))
	for _, item := range list {
		if item.card.Key != "" && seen[item.card.Key] {
			continue
		}
		seen[item.card.Key] = true
		out = append(out, item.card)
	}
	return out
}

// offerCards are the offers the actions table already calls "Cobrar": over value or at the
// asking price, and sellable without breaking the eleven or the league's hold rule.
func (d Document) offerCards(actions []map[string]any, rate float64) []Card {
	sales := map[string]map[string]any{}
	for _, sale := range rows(d.Money["sell"]) {
		sales[text(sale["offer_id"])] = sale
	}
	var out []Card
	for _, row := range actions {
		if text(row["verdict"]) != "cash" {
			continue
		}
		amount, value := number(row["offer_amount"]), number(row["value"])
		over := amount - value
		who := text(row["offer_from"])
		if who == "" {
			who = "el mercado"
		}
		why := []string{fmt.Sprintf("%s paga lo que pides por él.", capitalised(who))}
		impact := "vale " + esMoney(value)
		if over > 0 {
			why[0] = fmt.Sprintf("%s paga %s por encima de su valor.", capitalised(who),
				esMoney(over))
			impact = "+" + esMoney(over) + " sobre su valor"
		}
		if stand := mapOf(row["stand_in"]); stand != nil {
			why = append(why, fmt.Sprintf("Antes ficha a %s por %s: sin él no te queda once.",
				text(stand["name"]), esMoney(number(stand["cost"]))))
		} else if sale := sales[text(row["offer_id"])]; sale != nil {
			line := "Tu once no lo nota: entra otro igual."
			if drop := number(sale["xi_drop"]); drop > 0 {
				line = fmt.Sprintf("Tu once pierde %s xPts.", esNum(drop, 1))
			}
			if truthy(sale["match_pending"]) {
				line += fmt.Sprintf(" Aún no ha jugado: regalas sus %s xPts de esta jornada.",
					esNum(number(sale["points_at_risk"]), 1))
			}
			why = append(why, line)
		}
		out = append(out, Card{
			Kind: "offer", Key: "own:" + text(row["id"]), Player: row,
			Verb: "Acepta la oferta", Big: esMoney(amount), BigNote: "vale " + esMoney(value),
			Deadline: text(row["offer_expires"]), DeadlineLabel: "caduca la oferta",
			Why: why, Impact: impact, Tone: "good",
			Button: fmt.Sprintf(`<button class="op op-primary dcard-go" data-op="accept_offer" `+
				`data-op-market="%s" data-op-offer="%s" data-op-player="%s" data-op-name="%s" `+
				`data-op-amount="%d" type="button">Aceptar %s</button>`,
				Esc(text(row["market_id"])), Esc(text(row["offer_id"])), Esc(text(row["id"])),
				Esc(text(row["name"])), int64(amount), Esc(esMoney(amount))),
			Weight: math.Max(0, over) / 1e6 * rate, Cash: amount,
		})
	}
	return out
}

// raiseCards are the raises the clause plan fits in the balance, riskiest first.
func (d Document) raiseCards() []Card {
	var out []Card
	for _, row := range rows(d.Raise["rows"]) {
		if !truthy(row["in_plan"]) || number(row["pay"]) <= 0 {
			continue
		}
		pay, target, clause := number(row["pay"]), number(row["target_clause"]),
			number(row["clause"])
		threat, cash := text(row["top_threat"]), number(row["top_threat_cash"])
		threats, tempted := int(number(row["threats"])), int(number(row["tempted"]))
		var first string
		switch {
		case threats == 1:
			first = fmt.Sprintf("%s tiene %s y es el único que llega.", threat, esMoney(cash))
		case tempted > 0:
			first = fmt.Sprintf("%s tiene %s y le renta pagarla; llegan %d rivales.", threat,
				esMoney(cash), threats)
		default:
			first = fmt.Sprintf("Llegan %d rivales; el más rico, %s, tiene %s.", threats,
				threat, esMoney(cash))
		}
		drop := number(row["xi_drop"])
		deadline, label := d.raiseDeadline(row)
		out = append(out, Card{
			Kind: "raise", Key: "own:" + text(row["id"]), Player: row,
			Verb: "Sube su cláusula", Big: esMoney(target), BigNote: "hoy " + esMoney(clause),
			Deadline: deadline, DeadlineLabel: label,
			Why: []string{first,
				fmt.Sprintf("Si se lo lleva, tu once pierde %s xPts.", esNum(drop, 1))},
			Impact: fmt.Sprintf("riesgo estimado %.0f %%", number(row["risk"])*100),
			Tone:   "critical",
			Button: fmt.Sprintf(`<button class="raise dcard-go" data-raise="%s" data-raise-name="%s" `+
				`data-raise-pay="%d" data-raise-slot="%s" data-raise-clause="%d" `+
				`data-raise-target="%d" type="button">Subir cláusula · pagas %s</button>`,
				Esc(text(row["id"])), Esc(text(row["name"])), int64(pay),
				Esc(text(row["player_team_id"])), int64(clause), int64(target),
				Esc(esMoney(pay))),
			Weight: drop, Cash: -pay,
		})
	}
	return out
}

// raiseDeadline is until when the raise is worth making: before his own lock lifts, or before
// the window lets anybody pay, or while it stays open.
func (d Document) raiseDeadline(row map[string]any) (string, string) {
	if until := text(row["clause_locked_until"]); truthy(row["clause_locked"]) && until != "" {
		return until, "se libera su cláusula"
	}
	window, ok := d.clauseWindow()
	switch {
	case !ok:
		return "", ""
	case window.Open && window.ClosesAt != "":
		return window.ClosesAt, "cláusulas abiertas"
	case !window.Open && window.OpensAt != "":
		return window.OpensAt, "se abren las cláusulas"
	}
	return "", ""
}

func (d Document) clauseWindow() (schedule.Window, bool) {
	if d.Window != nil {
		return *d.Window, true
	}
	var fixtures []schedule.Fixture
	for _, fixture := range rows(d.Universe["schedule"]) {
		fixtures = append(fixtures, schedule.Fixture{Week: int(number(fixture["week"])),
			Kickoff: text(fixture["kickoff"])})
	}
	if len(fixtures) == 0 {
		return schedule.Window{}, false
	}
	return schedule.Clauses(fixtures, time.Now()), true
}

// swapCards are the plan's moves: one out and one in, or a signing that fills a hole.
func (d Document) swapCards() []Card {
	var out []Card
	for _, move := range rows(d.Swaps["moves"]) {
		leaving, arriving := mapOf(move["out"]), mapOf(move["in"])
		if arriving == nil {
			continue
		}
		gain, cost := number(move["gain"]), number(move["cost"])
		button := cardBuyButton(arriving, cost)
		if button == "" {
			continue
		}
		card := Card{
			Kind: "swap", Key: "in:" + text(arriving["id"]), Player: arriving,
			Big: esSigned(gain) + " xPts", Deadline: text(mapOf(arriving["market"])["expires"]),
			DeadlineLabel: "sale del mercado", Button: button, Tone: "accent", Weight: gain,
		}
		if leaving == nil {
			card.Verb, card.BigNote = "Ficha", "llena un hueco del once"
			card.Why = []string{fmt.Sprintf("Te falta un %s para alinear once.",
				text(move["position"])), startsLine(arriving)}
			card.Impact, card.Cash = "cuesta "+esMoney(cost), -cost
			out = append(out, card)
			continue
		}
		net := number(move["net"])
		card.Player, card.In = leaving, arriving
		card.Verb = "Cambia por " + text(arriving["name"])
		card.BigNote = esNum(number(leaving["xpts"]), 1) + " → " +
			esNum(number(arriving["xpts"]), 1)
		card.Why = []string{
			leavingLine(leaving),
			fmt.Sprintf("Vendes a %s por %s y pagas %s por %s.", text(leaving["name"]),
				esMoney(number(move["sale"])), esMoney(cost), text(arriving["name"])),
		}
		if text(move["order"]) != "" {
			card.Why[1] += " Ficha primero: sin él no te queda once."
		}
		card.Impact = "neto " + esMoney(net)
		if net < 0 {
			card.Impact = "neto +" + esMoney(-net) + " en caja"
		}
		card.Cash = -net
		out = append(out, card)
	}
	return out
}

// leavingLine is why he is the one to go: he cannot score, or he scores little for his price.
func leavingLine(player map[string]any) string {
	name := text(player["name"])
	if !truthy(player["available"]) {
		status := statusLabels[text(player["status"])]
		if status == "" {
			status = "no disponible"
		}
		return fmt.Sprintf("%s está %s: no puntúa.", name, status)
	}
	value := number(player["value"])
	if value <= 0 {
		return fmt.Sprintf("%s no tiene valor de mercado.", name)
	}
	return fmt.Sprintf("%s rinde %s pts por millón.", name,
		esNum(number(player["xpts"])/(value/1e6), 2))
}

// signingCards are the signings that improve the best eleven and that the advice already backs:
// a listing the actions table calls "Fichar", or a clause it grades as a bargain or a good rate.
// The points have to come at the squad's own rate per million, the bar the plan holds every
// purchase to, or the cards would recommend what "Plan ideal" just turned down.
func (d Document) signingCards(actions []map[string]any) []Card {
	rate := number(d.Advice["squad_ppm_benchmark"])
	backed := map[string]bool{}
	for _, row := range actions {
		if text(row["verdict"]) == "buy" {
			backed[text(row["id"])] = true
		}
	}
	for _, row := range rows(d.Advice["raids"]) {
		if verdict := text(row["verdict"]); verdict == "chollo" || verdict == "renta" {
			backed["clausula:"+text(row["id"])] = true
		}
	}
	window, windowKnown := d.clauseWindow()

	var out []Card
	for _, row := range rows(d.Money["bargains"]) {
		gain, cost := number(row["xi_gain"]), number(row["entry_cost"])
		if !truthy(row["affordable"]) || gain <= 0 || cost <= 0 {
			continue
		}
		if rate > 0 && gain/math.Max(cost/1e6, 0.25) < rate {
			continue
		}
		id := text(row["id"])
		card := Card{
			Kind: "sign", Key: "in:" + id, Player: row, Verb: "Ficha",
			Big: esSigned(gain) + " xPts", BigNote: "por jornada en tu once",
			Impact: "cuesta " + esMoney(cost), Tone: "accent", Weight: gain, Cash: -cost,
		}
		listing := mapOf(row["market"])
		var how string
		switch text(row["route"]) {
		case "clausula":
			if !backed["clausula:"+id] || !windowKnown || !window.Open {
				continue
			}
			card.Verb = "Paga su cláusula"
			card.Deadline, card.DeadlineLabel = window.ClosesAt, "cláusulas abiertas"
			card.Button = fmt.Sprintf(`<button class="op op-primary dcard-go" `+
				`data-op="pay_clause" data-op-player="%s" data-op-name="%s" data-op-amount="%d" `+
				`type="button">Pagar %s</button>`, Esc(id), Esc(text(row["name"])), int64(cost),
				Esc(esMoney(cost)))
			how = fmt.Sprintf("Cláusula de %s: se paga y nadie puede negarse.", text(row["owner"]))
		case "oferta al dueño":
			if !backed[id] {
				continue
			}
			card.Button = cardBuyButton(row, cost)
			card.Deadline, card.DeadlineLabel = text(listing["expires"]), "sale del mercado"
			how = fmt.Sprintf("%s pide %s; decide él si acepta.", text(listing["seller"]),
				esMoney(number(row["asking"])))
			if floor := text(row["cost_floor"]); floor != "" {
				how = fmt.Sprintf("%s pide %s, pero por debajo de %s no vende.",
					text(listing["seller"]), esMoney(number(row["asking"])), floor)
			}
		case "puja libre":
			if !backed[id] {
				continue
			}
			card.Button = cardBuyButton(row, cost)
			card.Deadline, card.DeadlineLabel = text(listing["expires"]), "sale del mercado"
			how = "Mercado libre: es una subasta y se puede perder."
		default:
			continue
		}
		if card.Button == "" {
			continue
		}
		card.Why = []string{startsLine(row), how}
		out = append(out, card)
	}
	return out
}

// cardBuyButton is the listing's own bid or offer button with the amount on it, or nothing when
// there is no listing or an offer of mine is already on it.
func cardBuyButton(row map[string]any, cost float64) string {
	listing := mapOf(row["market"])
	if text(listing["market_id"]) == "" || text(listing["my_bid_id"]) != "" {
		return ""
	}
	operation, verb := "bid", "Pujar "
	if text(listing["kind"]) == "venta" {
		operation, verb = "buy_offer", "Ofrecer "
	}
	return bidButton(row, "bid dcard-go", operation, "", Esc(verb+esMoney(cost)))
}

// startsLine is how likely he is to play and for whom.
func startsLine(row map[string]any) string {
	team := text(row["team_short"])
	if team == "" {
		team = text(row["team"])
	}
	where := ""
	if team != "" {
		where = " en el " + team
	}
	points := esNum(number(row["xpts"]), 1)
	if starts := asFloat(row["start_probability"]); starts != nil {
		return fmt.Sprintf("Titular al %.0f %%%s, %s xPts por jornada.", *starts, where, points)
	}
	return capitalised(strings.TrimSpace(fmt.Sprintf("%s %s xPts por jornada.",
		strings.TrimPrefix(where, " "), points)))
}

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
	main.WriteString(`<div class="dhead"><h2>Qué hacer ahora</h2>` +
		`<p>en este orden: lo que caduca antes y, a la misma hora, lo que más mueve tu once</p></div>`)
	if len(shown) == 0 {
		main.WriteString(`<p class="empty">Nada que decidir ahora: ninguna oferta que cobrar, ` +
			`ninguna cláusula que subir y ningún fichaje que mejore tu once a su precio.</p>`)
	} else {
		main.WriteString(`<div class="dcards">`)
		cash := number(d.Advice["budget"])
		for index, card := range shown {
			main.WriteString(cardHTML(index+1, card))
			cash += card.Cash
		}
		main.WriteString(`</div>`)
		rest := fmt.Sprintf("Haciendo estas %d te quedarían <b>%s</b> en caja.", len(shown),
			Esc(esMoney(cash)))
		if len(shown) == 1 {
			rest = fmt.Sprintf("Haciéndolo te quedarían <b>%s</b> en caja.", Esc(esMoney(cash)))
		}
		if more := len(cards) - len(shown); more > 0 {
			rest += fmt.Sprintf(" Hay %d más en el detalle de abajo.", more)
		}
		main.WriteString(`<p class="dcards-rest">` + rest + `</p>`)
	}
	return `<section id="ahora"><div class="dlayout"><div class="dmain">` + main.String() +
		`</div><aside class="dside">` + d.elevenAside() + `</aside></div></section>`
}

func cardHTML(rank int, card Card) string {
	player := card.Player
	faces := faceButton(player, "lg")
	if card.In != nil {
		faces = `<span class="dswap">` + faceButton(player, "md") +
			`<span class="dswap-arrow" aria-hidden="true">→</span>` + faceButton(card.In, "md") +
			`</span>`
	}
	slug := positionSlug[int(number(player["position_id"]))]
	if slug == "" {
		slug = "ent"
	}
	meta := fmt.Sprintf(`<span class="pos pos-%s">%s</span>%s · %s xPts`, slug,
		Esc(text(player["position"])), Esc(text(player["team_short"])),
		esNum(number(player["xpts"]), 1))
	clock := ""
	if card.Deadline != "" {
		clock = fmt.Sprintf(`<span class="dclock" title="%s %s"><span aria-hidden="true">⏱</span>`+
			`<span data-deadline="%s" data-plain="1">%s</span></span>`,
			Esc(card.DeadlineLabel), Esc(esWhen(card.Deadline)), Esc(card.Deadline),
			Esc(LeftUntil(card.Deadline)))
	}
	note := ""
	if card.BigNote != "" {
		note = `<span class="dcard-note">` + Esc(card.BigNote) + `</span>`
	}
	var why strings.Builder
	for _, line := range card.Why {
		why.WriteString(`<li>` + Esc(line) + `</li>`)
	}
	return fmt.Sprintf(`<article class="dcard dcard-%s" data-kind="%s">`+
		`<span class="dcard-rank">%d</span>`+
		`<div class="dcard-top">%s<div class="dcard-who">`+
		`<button class="p-name" type="button" data-detail="%s">%s</button>`+
		`<span class="dcard-meta">%s</span></div>%s</div>`+
		`<div class="dcard-verb">%s</div>`+
		`<div class="dcard-big">%s%s</div>`+
		`<ul class="dcard-why">%s</ul>`+
		`<div class="dcard-foot">%s<span class="dcard-impact">%s</span></div></article>`,
		card.Tone, card.Kind, rank, faces, Esc(text(player["id"])), Esc(text(player["name"])),
		meta, clock, Esc(card.Verb), Esc(card.Big), note, why.String(), card.Button,
		Esc(card.Impact))
}

// faceButton is the photo, with the initials under it for when it does not load, and opening
// his card like his name does.
func faceButton(player map[string]any, size string) string {
	name := text(player["name"])
	initials := ""
	for _, word := range strings.Fields(strings.ReplaceAll(name, ".", " ")) {
		initials += strings.ToUpper(string([]rune(word)[:1]))
		if len([]rune(initials)) == 2 {
			break
		}
	}
	image := ""
	if url := text(player["image"]); url != "" {
		image = `<img src="` + Esc(url) + `" alt="" loading="lazy" onerror="this.remove()">`
	}
	slug := positionSlug[int(number(player["position_id"]))]
	return fmt.Sprintf(`<button class="dface dface-%s dface-%s" type="button" data-detail="%s" `+
		`aria-label="Ficha de %s"><span class="dface-ini">%s</span>%s</button>`,
		size, slug, Esc(text(player["id"])), Esc(name), Esc(initials), image)
}

// elevenAside is the best eleven the squad can field today, by line, and where to look further.
func (d Document) elevenAside() string {
	squad := rows(d.Advice["squad"])
	if len(squad) == 0 {
		return ""
	}
	byID := map[string]map[string]any{}
	for _, player := range squad {
		byID[text(player["id"])] = player
	}
	choice, total := bestElevenOf(squad)

	head := `<div class="dhead"><h2>Tu once</h2><p>` + Esc(choice.Shape.Name) + `</p></div>`
	summary := `<div class="xi-total"><b>` + esNum(total, 1) + ` xPts</b> por jornada</div>`
	lines := []struct {
		slug string
		ids  []string
	}{
		{"del", choice.Attack}, {"med", choice.Middle}, {"def", choice.Defence},
		{"por", []string{choice.Keeper}},
	}
	var body strings.Builder
	for _, line := range lines {
		var chips strings.Builder
		for _, id := range line.ids {
			player := byID[id]
			if player == nil {
				continue
			}
			xpts := number(player["xpts"])
			fmt.Fprintf(&chips, `<button class="xi-chip %s" type="button" data-detail="%s">`+
				`%s<span class="xi-name">%s</span><span class="xi-x">%s</span></button>`,
				xptsClass(xpts), Esc(id), miniFace(player), Esc(text(player["name"])),
				esNum(xpts, 1))
		}
		fmt.Fprintf(&body, `<div class="xi-line"><span class="pos pos-%s">%s</span>`+
			`<div class="xi-chips">%s</div></div>`, line.slug, strings.ToUpper(line.slug),
			chips.String())
	}
	legend := `<p class="xi-legend">xPts por jornada: <span class="x-hi">≥6</span> · ` +
		`<span class="x-mid">3,5–6</span> · <span class="x-lo">2–3,5</span> · ` +
		`<span class="x-bad">&lt;2</span></p>`
	more := `<nav class="dmore"><span>Ver todo en</span>` +
		`<button type="button" data-goto="clausulas">Cláusulas</button>` +
		`<button type="button" data-goto="mercado">Mercado</button>` +
		`<button type="button" data-goto="misofertas">Mis ofertas</button>` +
		`<button type="button" data-goto="plantilla">Plantilla</button></nav>`
	return head + `<div class="xi-box">` + summary + body.String() + legend + `</div>` + more
}

func bestElevenOf(squad []map[string]any) (eleven.Choice, float64) {
	players := make([]eleven.Player, 0, len(squad))
	points := map[string]float64{}
	for _, row := range squad {
		id := text(row["id"])
		players = append(players, eleven.Player{ID: id, Name: text(row["name"]),
			Position: int(number(row["position_id"])), XPts: number(row["xpts"]),
			Available: truthy(row["available"])})
		points[id] = number(row["xpts"])
	}
	choice, _ := eleven.Best(players)
	total := 0.0
	for _, id := range choice.IDs() {
		total += points[id]
	}
	return choice, total
}

func miniFace(player map[string]any) string {
	if url := text(player["image"]); url != "" {
		return `<span class="xi-face"><img src="` + Esc(url) +
			`" alt="" loading="lazy" onerror="this.remove()"></span>`
	}
	return `<span class="xi-face"></span>`
}

func xptsClass(xpts float64) string {
	switch {
	case xpts >= 6:
		return "x-hi"
	case xpts >= 3.5:
		return "x-mid"
	case xpts >= 2:
		return "x-lo"
	}
	return "x-bad"
}

// detailToggle folds the four detailed sections of the tab under one button. They stay their
// own sections, so the live refresh keeps replacing them by id.
func (d Document) detailToggle() string {
	if len(d.Advice) == 0 {
		return ""
	}
	decisions := len(d.actionRows())
	return `<section id="detalle"><button class="dfold" type="button" data-fold="decidir" ` +
		`aria-expanded="false"><span class="dfold-title">Ver detalle</span>` +
		fmt.Sprintf(`<span class="dfold-what">plan ideal · las %d decisiones en tabla · `+
			`hacer caja · lo que cuesta fichar</span></button></section>`, decisions)
}

// esMoney is money the Spanish way, with a decimal comma: 21,7M, 850K.
func esMoney(amount float64) string {
	sign := ""
	if amount < 0 {
		sign, amount = "−", -amount
	}
	switch {
	case amount >= 999_500:
		return sign + strings.Replace(fmt.Sprintf("%.1f", amount/1e6), ".", ",", 1) + "M"
	case amount >= 1e3:
		return sign + group(fmt.Sprintf("%.0f", amount/1e3)) + "K"
	}
	return sign + fmt.Sprintf("%.0f €", amount)
}

func esNum(value float64, digits int) string {
	scale := math.Pow(10, float64(digits))
	rounded := math.Round(value*scale) / scale
	text := strings.Replace(fmt.Sprintf("%.*f", digits, math.Abs(rounded)), ".", ",", 1)
	if rounded < 0 {
		return "−" + text
	}
	return text
}

func esSigned(value float64) string {
	if value > 0 {
		return "+" + esNum(value, 1)
	}
	return esNum(value, 1)
}

var esDays = []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}

// esWhen is "jue 8 oct 19:00", in the stamp's own zone.
func esWhen(stamp string) string {
	when, ok := parseStamp(stamp)
	if !ok {
		return stamp
	}
	return fmt.Sprintf("%s %d %s %02d:%02d", esDays[int(when.Weekday())], when.Day(),
		months[int(when.Month())], when.Hour(), when.Minute())
}

func capitalised(sentence string) string {
	runes := []rune(sentence)
	if len(runes) == 0 {
		return sentence
	}
	return strings.ToUpper(string(runes[:1])) + string(runes[1:])
}
