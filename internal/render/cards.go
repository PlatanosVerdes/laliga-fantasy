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

// MaxOfferDrop is how many xPts an accepted offer may take out of the eleven when the card has
// no replacement to put in his place. Past it the money is not worth the matchdays.
const MaxOfferDrop = 2.0

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
	NoteClass     string
	Deadline      string
	DeadlineLabel string
	// Deadline2 is the other leg's, on a card with two.
	Deadline2      string
	DeadlineLabel2 string
	Why            []string
	Buttons        []Act `json:"buttons"`
	Impact         string
	Tone           string
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
	cards = append(cards, d.raiseCards(now)...)
	cards = append(cards, d.swapCards()...)
	cards = append(cards, d.signingCards(actions)...)
	cards = append(cards, d.saleSwapCards()...)
	covered := map[string]bool{}
	for _, card := range cards {
		covered[card.Key] = true
	}
	cards = append(cards, d.upgradeCards(covered)...)
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
		// At the same hour, money coming in before money going out: cash is what buys a crack.
		if first, second := list[one].card.Cash > 0, list[two].card.Cash > 0; first != second {
			return first
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
	squad := rows(d.Advice["squad"])
	_, xiNow := bestElevenOf(squad)
	var out []Card
	for _, row := range actions {
		if text(row["verdict"]) != "cash" {
			continue
		}
		stand := mapOf(row["stand_in"])
		drop := xiNow - elevenWithout(squad, text(row["id"]))
		if sale := sales[text(row["offer_id"])]; sale != nil {
			drop = number(sale["xi_drop"])
		}
		if stand == nil && drop > MaxOfferDrop {
			continue
		}
		amount, value := number(row["offer_amount"]), number(row["value"])
		if value <= 0 {
			continue
		}
		if take, _ := d.offerAdvice(text(row["id"]), amount/value, stand != nil); !take {
			continue
		}
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
		if stand != nil {
			why = append(why, fmt.Sprintf("Antes ficha a %s por %s: sin él no te queda once.",
				text(stand["name"]), esMoney(number(stand["cost"]))))
		} else {
			line := "Tu once no lo nota: entra otro igual."
			if drop >= 0.05 {
				line = fmt.Sprintf("Tu once pierde %s xPts.", esNum(drop, 1))
			}
			if sale := sales[text(row["offer_id"])]; truthy(sale["match_pending"]) {
				line += fmt.Sprintf(" Aún no ha jugado: regalas sus %s xPts de esta jornada.",
					esNum(number(sale["points_at_risk"]), 1))
			}
			why = append(why, line)
		}
		out = append(out, Card{
			Kind: "offer", Key: "own:" + text(row["id"]), Player: row,
			Verb: "Acepta la oferta", Big: esMoney(amount), BigNote: ratioNote(amount / value),
			NoteClass: ratioClassOf(amount / value),
			Deadline:  text(row["offer_expires"]), DeadlineLabel: "caduca la oferta",
			Why: why, Impact: impact, Tone: "good",
			Buttons: []Act{acceptAct(row, amount, "op op-primary dcard-go")},
			Weight:  math.Max(0, over) / 1e6 * rate, Cash: amount,
		})
	}
	return out
}

// raiseCards are the raises the clause plan fits in the balance, riskiest first.
func (d Document) raiseCards(now time.Time) []Card {
	var out []Card
	for _, row := range rows(d.Raise["rows"]) {
		if !truthy(row["in_plan"]) || number(row["pay"]) <= 0 || !d.raiseWanted(row) {
			continue
		}
		// Further off than two days it is the Cláusulas tab's business, not today's.
		deadline, label := d.raiseDeadline(row)
		if when, ok := parseStamp(deadline); !ok || when.After(now.Add(RaiseHorizon)) {
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
		out = append(out, Card{
			Kind: "raise", Key: "own:" + text(row["id"]), Player: row,
			Verb: "Sube su cláusula", Big: esMoney(target), BigNote: "hoy " + esMoney(clause),
			Deadline: deadline, DeadlineLabel: label,
			Why: []string{first,
				fmt.Sprintf("Si se lo lleva, tu once pierde %s xPts.", esNum(drop, 1))},
			Impact: fmt.Sprintf("retrasa tu crack %s · riesgo %.0f %%", esMoney(pay),
				number(row["risk"])*100),
			Tone: "critical",
			Buttons: []Act{{Label: "Subir cláusula · pagas " + esMoney(pay), Class: "raise dcard-go",
				Do: "raise", Args: map[string]any{"id": text(row["id"]), "name": text(row["name"]),
					"pay": int64(pay), "slot": text(row["player_team_id"]),
					"clause": int64(clause), "target": int64(target)}}},
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
		buttons := cardBuyButton(arriving, cost)
		if len(buttons) == 0 {
			continue
		}
		card := Card{
			Kind: "swap", Key: "in:" + text(arriving["id"]), Player: arriving,
			Big: esSigned(gain) + " xPts", Deadline: text(mapOf(arriving["market"])["expires"]),
			DeadlineLabel: "sale del mercado", Buttons: buttons, Tone: "accent", Weight: gain,
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
		if route := text(row["route"]); route != "clausula" && !d.worthItsPrice(row, route) {
			continue
		}
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
			card.Buttons = []Act{{Label: "Pagar " + esMoney(cost), Class: "op op-primary dcard-go",
				Do: "op", Args: map[string]any{"op": "pay_clause", "player_id": id,
					"name": text(row["name"]), "amount": int64(cost)}}}
			how = fmt.Sprintf("Cláusula de %s: se paga y nadie puede negarse.", text(row["owner"]))
		case "oferta al dueño":
			if !backed[id] {
				continue
			}
			card.Buttons = cardBuyButton(row, cost)
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
			card.Buttons = cardBuyButton(row, cost)
			card.Deadline, card.DeadlineLabel = text(listing["expires"]), "sale del mercado"
			how = "Mercado libre: es una subasta y se puede perder."
		default:
			continue
		}
		if len(card.Buttons) == 0 {
			continue
		}
		card.Why = []string{startsLine(row), how}
		out = append(out, card)
	}
	return out
}

// cardBuyButton is the listing's own bid or offer button with the amount on it, or nothing when
// there is no listing or an offer of mine is already on it.
func cardBuyButton(row map[string]any, cost float64) []Act {
	listing := mapOf(row["market"])
	if text(listing["market_id"]) == "" || text(listing["my_bid_id"]) != "" {
		return nil
	}
	verb := "Pujar "
	if text(listing["kind"]) == "venta" {
		verb = "Ofrecer "
	}
	return bidActs(row, "bid dcard-go", verb+esMoney(cost))
}

// acceptAct takes an offer through the two-step confirmation.
func acceptAct(offer map[string]any, amount float64, class string) Act {
	return Act{Label: "Aceptar " + esMoney(amount), Class: class, Do: "op", Args: map[string]any{
		"op": "accept_offer", "market_id": text(offer["market_id"]),
		"offer_id": text(offer["offer_id"]), "player_id": text(offer["id"]),
		"name": text(offer["name"]), "amount": int64(amount)}}
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

// planSquad is the squad once the plan's moves are done, and who in it arrives with them.
func planSquad(squad, moves []map[string]any) ([]map[string]any, map[string]bool) {
	leaving, arriving := map[string]bool{}, map[string]bool{}
	out := []map[string]any{}
	for _, move := range moves {
		if player := mapOf(move["out"]); player != nil {
			leaving[text(player["id"])] = true
		}
		if player := mapOf(move["in"]); player != nil {
			arriving[text(player["id"])] = true
			out = append(out, player)
		}
	}
	for _, player := range squad {
		if !leaving[text(player["id"])] {
			out = append(out, player)
		}
	}
	return out, arriving
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

func elevenWithout(squad []map[string]any, id string) float64 {
	rest := make([]map[string]any, 0, len(squad))
	for _, player := range squad {
		if text(player["id"]) != id {
			rest = append(rest, player)
		}
	}
	_, total := bestElevenOf(rest)
	return total
}

// xptsClass and the es* formatters below have twins in assets/ui/format.js; change both.
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
