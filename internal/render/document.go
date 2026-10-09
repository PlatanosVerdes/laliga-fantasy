package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
)

// Document assembles the whole page: the order of the sections, their titles, their notes
// and the widgets on top. A port of report.build().
//
// The prose is here rather than in a template because it is not decoration: every note says
// what a number is and, more often, what it is not — a projection is not a promise, a clause
// the game sets at 1.5x value can never look profitable by futbolfantasy's ceiling, cash is
// reconstructed and carries a bounded error. Somebody about to spend money reads these.
//
// The advice buckets arrive as data. Which player earns which verdict is the advice layer's
// judgement, and the only part of it here is the composition of the "what to do" table.
type Document struct {
	Universe map[string]any
	Advice   map[string]any
	// Generated is passed in rather than read from the clock, so a page can be compared.
	Generated  string
	LeagueName string
	CSS        string
	JS         string
	Modal      string
	Drawer     string
	// Plan and Raids are the standing instructions' two tables, already computed.
	Plan  []map[string]any
	Raids []map[string]any
	// Policies is the file's contents, for the two amount columns in the plan table.
	Policies map[string]map[string]any
	// Swaps is the "this one out, this one in" plan, computed by the advice layer.
	Swaps map[string]any
	// Mode is what the server running this page may do: auto, manual or solo lectura.
	Mode string
	// Orders is what happened to the standing instructions: cancelled, paid, spent. Kept for
	// the same reason Endings is — the game has no idea they ever existed.
	Orders []map[string]any

	// Endings is how my previous bids and offers finished, read from where they were saved.
	Endings []map[string]any
	// MineByWeek is how many of my players each team had on a past matchday, keyed by week.
	// Reconstructed outside, because that needs the transfer log and this package only draws.
	MineByWeek map[int]map[string]int
	// Money is the wallet's view of the same world, computed by the advice layer.
	Money map[string]any
	// Raise is the clause-raise plan: what each of my players is exposed to and what
	// defending him would cost.
	Raise map[string]any
	// ClauseWindow is when the game accepts a clause payment at all. Nil is nobody having
	// worked it out, and then the page says nothing rather than guessing an hour.
	Window *schedule.Window
	// The league's house rules: the hold period, its exceptions, the debt a market bid may run
	// into (percent of the squad's value) and the social pacts.
	HoldDays       int
	MaxDebtPct     float64
	HoldExceptions string
	RuleNotes      []string
}

func rows(source any) []map[string]any {
	list, ok := source.([]any)
	if !ok {
		if already, ok := source.([]map[string]any); ok {
			return already
		}
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func mapOf(source any) map[string]any {
	if asMap, ok := source.(map[string]any); ok {
		return asMap
	}
	return nil
}

// HTML renders the document.
func (d Document) HTML() string {
	universe, advice := d.Universe, d.Advice
	week := mapOf(universe["week"])
	players := rows(universe["players"])
	hasAdvice := len(advice) > 0

	var sections []string
	if hasAdvice {
		sections = append(sections, d.Views()...)
	}
	sections = append(sections, d.seasonSection())
	sections = append(sections, d.feedSection())
	if hasAdvice {
		sections = append(sections, d.squadSection())
		sections = append(sections, d.rivalSections(players)...)
	}
	sections = append(sections, d.rulesSection())
	sections = append(sections, d.rankingSection(players))
	sections = append(sections, CompareShell)

	stats, more := d.widgets(week, players)
	header := Header(stats, hasAdvice, asFloat(d.Advice["budget"])) + d.pageFacts()
	footer := PageFoot(d.Generated, d.LeagueName, int(number(week["weekNumber"])), more, d.Mode) +
		Footer(number(universe["current_weight"]))

	body := IconSprite + strings.Join(filterEmpty(sections), "")
	return Page(d.CSS, d.JS, CrestCSS(), header, body, footer, d.Modal, d.Drawer)
}

// pageFacts is what the player card needs to know about the league and cannot ask for: the
// clause window and the exceptions to the hold rule.
func (d Document) pageFacts() string {
	attrs := ""
	if d.Window != nil {
		open := "0"
		if d.Window.Open {
			open = "1"
		}
		attrs += fmt.Sprintf(` data-window-open="%s" data-opens="%s" data-closes="%s"`, open,
			Esc(d.Window.OpensAt), Esc(d.Window.ClosesAt))
	}
	if d.HoldExceptions != "" {
		attrs += ` data-hold-except="` + Esc(d.HoldExceptions) + `"`
	}
	return `<div id="page-facts" hidden` + attrs + `></div>`
}

func filterEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// Pitch and Filters are read from assets/ by the caller and injected, like the CSS.
var Pitch, Filters string

// --- widgets ---------------------------------------------------------------------------

// widgets are the four cards under the tab bar and, apart, the figures behind "más datos".
func (d Document) widgets(week map[string]any, players []map[string]any) ([]string, []string) {
	// The one number on this card that keeps changing is how long is left: to the first
	// kick-off, then to the last one, then to the close the game stamps hours after it.
	closing, opening := text(week["closingWeekDate"]), text(week["openingWeekDate"])
	last := d.lastKickoff(opening)
	now := time.Now()
	state, deadline := "hasta que se cierra la alineación", opening
	if when, ok := parseStamp(opening); !ok || !now.Before(when) || truthy(week["isLive"]) {
		state, deadline = "en juego, hasta el último partido", last
		if when, ok := parseStamp(last); !ok || !now.Before(when) {
			state, deadline = "acabando, se cierra", closing
		}
	}
	value := state
	if deadline != "" {
		value = LeftUntil(deadline)
	}
	note := state
	switch {
	case deadline == opening && esWhen(opening) != "":
		note = "empieza " + esWhen(opening)
	case deadline == last && esWhen(last) != "":
		note = "último partido " + esWhen(last)
	}
	weekNumber := int(number(week["weekNumber"]))
	matchday := StatCard(Stat{Label: fmt.Sprintf("Jornada %d", weekNumber),
		Value: value, Deadline: deadline, Note: note, Tab: "partidos"})
	clauses := ""
	if stat, ok := d.clauseStat(); ok {
		clauses = StatCard(stat)
	}

	if len(d.Advice) == 0 {
		return filterEmpty([]string{matchday, clauses,
				StatCard(Stat{Label: "Jugadores", Value: fmt.Sprintf("%d", len(players)),
					Note: fmt.Sprintf("%d con datos de futbolfantasy",
						int(number(d.Universe["matched_count"])))}),
				StatCard(Stat{Label: "Sesión", Value: "sin liga", Note: "solo datos públicos"})}),
			nil
	}

	squad := rows(d.Advice["squad"])
	squadValue := 0.0
	xpts := make([]float64, 0, len(squad))
	for _, player := range squad {
		squadValue += number(player["value"])
		xpts = append(xpts, number(player["xpts"]))
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(xpts)))
	bestEleven := 0.0
	for index, value := range xpts {
		if index >= 11 {
			break
		}
		bestEleven += value
	}

	teams := mapOf(d.Universe["league_teams"])
	me := mapOf(teams[text(d.Universe["my_team_id"])])
	var cashes, values, points []float64
	for _, team := range teams {
		row := mapOf(team)
		cashes = append(cashes, number(row["estimated_cash"]))
		values = append(values, number(row["squad_value"]))
		points = append(points, number(row["points"]))
	}
	budget := asFloat(d.Advice["budget"])
	cashRank, cashShare, cashStatus := RankOf(number(d.Advice["budget"]), cashes)
	valueRank, valueShare, valueStatus := RankOf(squadValue, values)
	pointsRank, pointsShare, pointsStatus := RankOf(number(me["points"]), points)

	position := "?"
	if seat := asFloat(me["position"]); seat != nil {
		position = fmt.Sprintf("%d", int(*seat))
	}
	exact := ""
	if budget != nil {
		exact = group(fmt.Sprintf("%.0f", *budget)) + " €"
	}
	stats := filterEmpty([]string{
		StatCard(Stat{Label: "Caja", Value: esMoney(number(d.Advice["budget"])), ValueID: "kpi-cash",
			Note: exact, Tab: "rivales"}),
		clauses, matchday,
		StatCard(Stat{Label: "Liga", Value: position + "º",
			Small: fmt.Sprintf("de %d", len(teams)),
			Note:  fmt.Sprintf("%d pts", int(number(me["points"]))), Tab: "rivales"}),
	})

	more := []string{
		Widget(KPI{Label: "Mi puesto", Value: position + "º",
			Hint: fmt.Sprintf("%d puntos", int(number(me["points"]))),
			Rank: pointsRank, Meter: &pointsShare, Status: pointsStatus, Tab: "rivales"}),
		Widget(KPI{Label: "Mi saldo", Value: Money(budget),
			Hint: text(me["power_note"]), Rank: cashRank, Meter: &cashShare,
			Status: cashStatus, Tab: "rivales"}),
		Widget(KPI{Label: "Valor de plantilla", Value: Money(&squadValue),
			Hint: fmt.Sprintf("%d jugadores", len(squad)),
			Rank: valueRank, Meter: &valueShare, Status: valueStatus, Tab: "plantilla"}),
	}

	var goodOffers []map[string]any
	for _, offer := range rows(d.Advice["offers"]) {
		if truthy(offer["worth_taking"]) {
			goodOffers = append(goodOffers, offer)
		}
	}
	if len(goodOffers) > 0 {
		names := make([]string, 0, 3)
		for index, offer := range goodOffers {
			if index >= 3 {
				break
			}
			names = append(names, text(offer["name"]))
		}
		more = append(more, Widget(KPI{Label: "Ofertas que interesan",
			Value: fmt.Sprintf("%d", len(goodOffers)), Hint: strings.Join(names, ", "),
			Rank: "cobra", Status: "good", Tab: "vender"}))
	}

	bids := rows(d.Advice["bids_now"])
	asks := rows(d.Advice["asks"])
	raids := rows(d.Advice["raids"])
	clauseHint := "desbloqueadas y pagables"
	if len(raids) == 0 && number(d.Advice["clauses_locked"]) > 0 {
		from := text(d.Advice["clauses_unlock_from"])
		if len(from) > 10 {
			from = from[:10]
		}
		clauseHint = "bloqueadas hasta " + from
	}
	more = append(more,
		Widget(KPI{Label: "xPts del mejor 11", Value: Num(&bestEleven, 1), Hint: "por jornada"}),
		Widget(KPI{Label: "Pujables ahora", Value: fmt.Sprintf("%d", len(bids)),
			Hint: fmt.Sprintf("%d mas en venta por rivales", len(asks)), Tab: "comprar"}),
		Widget(KPI{Label: "Cláusulas a tiro", Value: fmt.Sprintf("%d", len(raids)),
			Hint: clauseHint, Tab: "clausulas"}))
	return stats, more
}

// lastKickoff is the matchday's last match, leaving out any moved weeks away from the rest.
func (d Document) lastKickoff(opening string) string {
	fixtures := []schedule.Fixture{}
	for _, fixture := range rows(d.Universe["fixtures"]) {
		fixtures = append(fixtures, schedule.Fixture{Week: 1, Kickoff: text(fixture["kickoff"])})
	}
	when, ok := parseStamp(opening)
	if !ok {
		return ""
	}
	round, ok := schedule.ShieldRound(fixtures, when)
	if !ok {
		return ""
	}
	return round.End.Add(-schedule.RoundTail).Format(time.RFC3339)
}

// clauseStat counts down to the next change of the clause window: the game shuts it the day
// before a matchday and reopens it at the first kick-off, for you and for every rival.
func (d Document) clauseStat() (Stat, bool) {
	fixtures := []schedule.Fixture{}
	for _, fixture := range rows(d.Universe["schedule"]) {
		fixtures = append(fixtures, schedule.Fixture{Week: int(number(fixture["week"])),
			Kickoff: text(fixture["kickoff"])})
	}
	if len(fixtures) == 0 {
		return Stat{}, false
	}
	window := schedule.Clauses(fixtures, time.Now())
	icon, label, deadline, note := "", "Cláusulas abiertas", window.ClosesAt, "hasta "
	if !window.Open {
		icon, label, deadline, note = "🔒", "Cláusulas cerradas", window.OpensAt, "se abren "
	}
	if deadline == "" {
		return Stat{}, false
	}
	return Stat{Icon: icon, Label: label, Value: LeftUntil(deadline), Deadline: deadline,
		Note: note + esWhen(deadline), Tab: "clausulas"}, true
}

// --- the sections ----------------------------------------------------------------------

// actionRows composes the one table that says what to do. This is advice-layer judgement,
// not rendering, and it is here because the table is meaningless without it.
// AdequateReplacement is how much of the leaving player's output a stand-in has to keep to
// count as a replacement rather than a hole.
const AdequateReplacement = 0.85

// replacementFor is who could take his place: same position, affordable with what the sale
// brings in, and never above futbolfantasy's ceiling when it publishes one.
//
// The cheapest one that keeps the position standing, not the best one on the market: ranking by
// points alone answered "spend 36.85M to gain 0.25 xPts", which is a true sentence and terrible
// advice. When nobody clears the bar the best available is returned anyway, marked, because
// "there is no replacement" is a worse answer than "this is what there is and it costs you
// three points" — the eleven has to be legal either way.
func (d Document) replacementFor(leaving map[string]any, spendable float64) map[string]any {
	positionID := int(number(leaving["position_id"]))
	bar := number(leaving["xpts"]) * AdequateReplacement

	var adequate, fallback map[string]any
	for _, bucket := range []string{"bids_now", "asks"} {
		for _, candidate := range rows(d.Advice[bucket]) {
			if int(number(candidate["position_id"])) != positionID {
				continue
			}
			if truthy(candidate["sale_locked"]) || number(candidate["xpts"]) <= 0 {
				continue
			}
			cost := number(candidate["entry_cost"])
			if cost == 0 {
				cost = number(candidate["value"])
			}
			// A forced replacement is not a bargain hunt: no published ceiling is allowed
			// here, but paying over a published one is still refused.
			ceiling := number(candidate["ideal_bid"])
			if cost == 0 || cost > spendable || (ceiling > 0 && cost > ceiling) {
				continue
			}
			row := merge(candidate, map[string]any{"cost": cost})

			if number(row["xpts"]) >= bar {
				// Cheapest of the ones that hold the position.
				if adequate == nil || cost < number(adequate["cost"]) {
					adequate = row
				}
				continue
			}
			// Otherwise the least bad: most points per million, since none of them is enough.
			if fallback == nil ||
				number(row["xpts"])/cost > number(fallback["xpts"])/number(fallback["cost"]) {
				fallback = row
			}
		}
	}
	if adequate != nil {
		return merge(adequate, map[string]any{"adequate": true})
	}
	if fallback != nil {
		return merge(fallback, map[string]any{"adequate": false})
	}
	return nil
}

func (d Document) actionRows() []map[string]any {
	var out []map[string]any

	for _, player := range rows(d.Advice["squad"]) {
		if !truthy(player["available"]) {
			status := text(player["status"])
			if label := statusLabels[status]; label != "" {
				status = label
			}
			out = append(out, merge(player, map[string]any{
				"verdict": "out", "entry_cost": nil,
				"why": fmt.Sprintf("esta %s: no puntua", status)}))
		}
	}

	// Being in the market makes a player biddable, not worth bidding on. Only rows whose own
	// numbers back the call get the green badge — otherwise it would contradict the reason
	// printed beside it.
	buys := 0
	for _, player := range append(rows(d.Advice["bids_now"]), rows(d.Advice["asks"])...) {
		if !truthy(player["affordable"]) {
			continue
		}
		// His owner cannot sell him yet, so recommending the signing is recommending something
		// the league does not allow. He still shows in the market tables, with the padlock.
		if truthy(player["sale_locked"]) {
			continue
		}
		ideal := asFloat(player["ideal_bid"])
		_, hasVerdict := player["ideal_bid"]
		cost := number(player["entry_cost"])
		profitable := ideal != nil && *ideal > 0 && cost <= *ideal

		listing := mapOf(player["market"])
		mine := text(listing["my_bid_id"]) != ""

		var why string
		switch {
		case mine:
			why = "ya tienes " + Money(asFloat(listing["my_bid"])) + " puestos"
			if profitable {
				why += " · el techo esta en " + Money(ideal)
			}
		case profitable:
			why = "puja hasta " + Money(ideal)
		case hasVerdict:
			// futbolfantasy has an opinion and it is no: either no margin at all, or the
			// asking price is already above the ceiling. Neither is a recommendation.
			continue
		case number(player["score"]) > 1.0:
			why = "buen score y entra en tu presupuesto"
		default:
			continue
		}
		if seller := text(player["seller"]); seller != "" {
			why += " · lo vende " + seller
		}
		if truthy(player["position_gap"]) {
			why += " · te falta un " +
				strings.ToLower(positionNames[text(player["position_id"])])
		}
		verdict := "buy"
		if mine {
			// Already bid: it is no longer a decision to take, it is one taken. Counting it
			// as a buy would also make "nothing is worth buying today" wrong.
			verdict = "bidding"
		}
		out = append(out, merge(player, map[string]any{"verdict": verdict, "why": why}))
		if verdict == "buy" {
			buys++
		}
		if buys >= 8 {
			break
		}
	}

	for index, player := range rows(d.Advice["raids"]) {
		if index >= 6 {
			break
		}
		why := "de " + text(player["owner"])
		if premium := asFloat(player["clause_premium"]); premium != nil && *premium != 0 {
			why += fmt.Sprintf(", cláusula a %.2fx su valor", *premium)
		}
		out = append(out, merge(player, map[string]any{"verdict": "clause", "why": why}))
	}

	for index, player := range rows(d.Advice["upcoming_raids"]) {
		if index >= 6 {
			break
		}
		why := fmt.Sprintf("cláusula de %s se abre en %.0fh (%s)", text(player["owner"]),
			number(player["hours_left"]), Money(asFloat(player["clause"])))
		if !truthy(player["affordable"]) {
			why += " · no te llega el saldo"
		}
		out = append(out, merge(player, map[string]any{"verdict": "clause", "why": why}))
	}

	for index, player := range rows(d.Advice["exposure"]) {
		if index >= 6 {
			break
		}
		threats := int(number(player["threats"]))
		why := fmt.Sprintf("cláusula a solo %.2fx su valor", number(player["clause_margin"]))
		if threats > 0 {
			plural := "es"
			if threats == 1 {
				plural = ""
			}
			why = fmt.Sprintf("%d rival%s pueden pagarla", threats, plural)
		}
		if top := text(player["top_threat"]); top != "" {
			why += " · el mas rico: " + top
		}
		out = append(out, merge(player, map[string]any{
			"verdict": "protect", "entry_cost": player["clause"], "why": why}))
	}

	// Riskiest first, not soonest: in this league every clause opens within the same hour, so
	// the hour sorts nothing and "quedas expuesto" on all of them said nothing either.
	soon := append([]map[string]any{}, rows(d.Advice["my_clauses_soon"])...)
	sort.SliceStable(soon, func(one, two int) bool {
		return number(soon[one]["risk"]) > number(soon[two]["risk"])
	})
	for index, player := range soon {
		if index >= 6 {
			break
		}
		why := fmt.Sprintf("se desbloquea en %.0fh", number(player["hours_left"]))
		able, tempted := int(number(player["threats"])), int(number(player["tempted"]))
		switch {
		case able == 0:
			// Kept, because it is cheap enough to be a bargain the day somebody sells.
			why += " y hoy nadie tiene caja para pagarla"
		case tempted == 0:
			// They could and it would still be a bad trade for them, which is the difference
			// between a clause worth raising and a clause already doing its job.
			why += fmt.Sprintf(" y %d pueden pagarla, pero a ninguno le renta a ese precio",
				able)
		case tempted == 1:
			why += fmt.Sprintf(" y a %s le renta pagarla", text(player["top_threat"]))
		default:
			why += fmt.Sprintf(" y a %d de los %d que pueden pagarla les renta · el mas rico: %s",
				tempted, able, text(player["top_threat"]))
		}
		if margin := number(player["clause_margin"]); margin != 0 {
			why += fmt.Sprintf(" · esta a %.2fx su valor", margin)
		}
		out = append(out, merge(player, map[string]any{
			"verdict": "protect", "entry_cost": player["clause"], "why": why}))
	}

	// Money already on the table is the most decidable thing on the page and it was only in its
	// own section: an offer expires whether or not you looked at the right tab.
	squad := rows(d.Advice["squad"])
	for _, offer := range rows(d.Advice["offers"]) {
		if !truthy(offer["worth_taking"]) {
			continue
		}
		who := text(offer["offer_from"])
		if who == "" {
			who = "el mercado"
		}
		amount := asFloat(offer["offer_amount"])
		why := fmt.Sprintf("%s paga %s (%.2fx su valor)", who, Money(amount),
			number(offer["vs_value"]))
		if left, _ := Ago(text(offer["offer_made"])); left != Missing {
			why += " · ofrecida " + left
		}

		// Selling him has to be possible. Two different things can stop it, and only one of
		// them is final: the league's hold rule is a no, while being the last one in his
		// position is a no *until you sign somebody*, which is an instruction rather than a
		// refusal.
		blocked, verdict := "", "cash"
		var standIn map[string]any
		if truthy(offer["sale_locked"]) {
			until := text(offer["hold_until"])
			if len(until) > 10 {
				until = until[:10]
			}
			blocked = "no puedes venderlo hasta el " + until + " (norma de la liga)"
			verdict = "cash_blocked"
		} else if room := policies.SquadRoom(squad, int(number(offer["position_id"]))); room <= 0 {
			spare := number(offer["offer_amount"]) + number(d.Advice["budget"])
			position := strings.ToLower(positionNames[text(offer["position_id"])])
			if stand := d.replacementFor(offer, spare); stand != nil {
				gap := number(stand["xpts"]) - number(offer["xpts"])
				if truthy(stand["adequate"]) {
					standIn = stand
					// The net is the number that decides: the offer pays for part of the
					// replacement, and sometimes for all of it.
					net := number(stand["cost"]) - number(offer["offer_amount"])
					effect := fmt.Sprintf("te cuesta %s neto", Money(&net))
					if net <= 0 {
						gained := -net
						effect = fmt.Sprintf("y encima ganas %s", Money(&gained))
					}
					why += fmt.Sprintf(" · venderlo te deja sin once: fichas antes a un %s, "+
						"%s por %s (%+.2f xPts, %s), y entonces puedes venderlo", position,
						text(stand["name"]), Money(asFloat(stand["cost"])), gap, effect)
				} else {
					// Legal but worse: say the price in points so the trade is judged, not sold.
					why += fmt.Sprintf(" · venderlo te deja sin once y el unico %s a tiro es "+
						"%s por %s, con %+.2f xPts: probablemente no compense",
						position, text(stand["name"]), Money(asFloat(stand["cost"])), gap)
					verdict = "cash_blocked"
				}
			} else {
				blocked = "venderlo te deja sin once y no hay ningun " + position +
					" a tiro para cubrirlo"
				verdict = "cash_blocked"
			}
		}
		if blocked != "" {
			why += " · pero " + blocked
		}
		row := merge(offer, map[string]any{
			"verdict": verdict, "entry_cost": offer["offer_amount"], "why": why})
		if standIn != nil {
			row["stand_in"] = standIn
		}
		out = append(out, row)
	}

	for index, player := range rows(d.Advice["sells"]) {
		if index >= 6 {
			break
		}
		reasons := asStrings(player["reasons"])
		if truthy(player["available"]) && len(reasons) > 0 {
			out = append(out, merge(player, map[string]any{
				"verdict": "sell", "entry_cost": nil, "why": strings.Join(reasons, "; ")}))
		}
	}

	// Stable sort by severity, so the order inside a verdict is the order it was collected.
	order := map[string]int{}
	for index, name := range VerdictOrder {
		order[name] = index
	}
	sort.SliceStable(out, func(i, j int) bool {
		return order[text(out[i]["verdict"])] < order[text(out[j]["verdict"])]
	})
	return out
}

func merge(base map[string]any, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

// raidsStandingDown are the raids that are not going to happen as they are: the clause rose
// over the limit, the owner shielded him, there is no cap written or no cash left. They are
// not pending, and listing them next to the ones still on their way made the section read as
// if all of them were.
var raidsStandingDown = map[string]bool{
	"cancelada": true, "bloqueada": true, "sin_limite": true, "sin_saldo": true,
	"ninguna": true,
}

// counted is a number with its noun. The package already has a plural() for the "s" that goes on
// the end of an English-shaped word, which is not what "jugador" needs.
func counted(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", count, many)
}

// rulesSection is the league's own pact. Everything here is invisible to the API, so if it
// is not written down it does not exist.
func (d Document) rulesSection() string {
	if d.HoldDays == 0 && len(d.RuleNotes) == 0 {
		return ""
	}
	return mkSection("normas", "liga", "Normas de la liga",
		`<div class="mk-box">`+HouseRules(d.HoldDays, d.HoldExceptions, d.RuleNotes)+`</div>`,
		"", len(d.RuleNotes)+map[bool]int{true: 1, false: 0}[d.HoldDays > 0])
}

// managerTeams is the user-id to team-id map the feed needs to make its names clickable. Built
// from the standings, which is the only place both ids appear together.
func fallbackText(value, other string) string {
	if value != "" {
		return value
	}
	return other
}

func (d Document) managerTeams() map[string]string {
	out := map[string]string{}
	// league_teams is a map keyed by team id, not a list, so it is walked as one.
	teams, _ := d.Universe["league_teams"].(map[string]any)
	for teamID, entry := range teams {
		team := mapOf(entry)
		if user := text(team["user_id"]); user != "" {
			out[user] = fallbackText(text(team["team_id"]), teamID)
		}
	}
	return out
}

// seasonSection is only the frame: the line of every manager across the season is thirteen
// lineups per matchday, so it is asked for when the tab is opened and not on every rebuild.
func (d Document) seasonSection() string {
	return mkSection("evolucion", "liga", "La liga jornada a jornada",
		`<div class="evo" data-season="1"><p class="empty">Cargando…</p></div>`,
		"", -1)
}

// feedMarks is which players of the log concern me, strongest first: mine (now, or bought or
// sold by me), one I bid for, one I starred. The manager name is mine too.
func (d Document) feedMarks() (map[string]string, string) {
	marks := map[string]string{}
	for _, player := range rows(d.Universe["players"]) {
		if truthy(player["starred"]) {
			marks[text(player["id"])] = "fav"
		}
	}
	for _, ending := range d.Endings {
		marks[text(ending["player_id"])] = "bid"
	}
	for _, player := range rows(d.Universe["players"]) {
		if market := mapOf(player["market"]); market != nil && market["my_bid"] != nil {
			marks[text(player["id"])] = "bid"
		}
		if truthy(player["is_mine"]) {
			marks[text(player["id"])] = "mine"
		}
	}
	me := text(mapOf(mapOf(d.Universe["league_teams"])[text(d.Universe["my_team_id"])])["manager"])
	return marks, me
}

func (d Document) feedSection() string {
	events := rows(d.Universe["activity"])
	if len(events) == 0 {
		return Section("Movimientos de la liga",
			`<p class="empty">Sin movimientos todavia. Si la liga ya tiene actividad y esto `+
				`sigue vacio, la respuesta del API ha cambiado de forma: <code>probe activity</code> `+
				`la vuelca cruda.</p>`, "", "", "movimientos")
	}
	moves := 0
	for _, event := range events {
		if !strings.Contains(text(event["kind"]), "alinea") {
			moves++
		}
	}
	ManagerTeams = d.managerTeams()
	FeedMarks, FeedMe = d.feedMarks()
	return mkSection("movimientos", "liga", "Movimientos de la liga", Feed(events),
		"", moves)
}

func (d Document) squadSection() string {
	squad := rows(d.Advice["squad"])
	shape := mapOf(d.Advice["shape"])
	var groups strings.Builder
	for _, positionID := range []string{"1", "2", "3", "4"} {
		var line []map[string]any
		for _, player := range squad {
			if text(player["position_id"]) == positionID {
				line = append(line, player)
			}
		}
		if len(line) == 0 {
			continue
		}
		sort.SliceStable(line, func(one, two int) bool {
			return number(line[one]["xpts"]) > number(line[two]["xpts"])
		})
		state := ""
		if data := mapOf(shape[positionID]); data != nil {
			switch {
			case truthy(data["gap"]):
				state = " · falta uno"
			case truthy(data["surplus"]):
				state = " · sobra uno"
			}
		}
		slug := map[string]string{"1": "por", "2": "def", "3": "med", "4": "del"}[positionID]
		fmt.Fprintf(&groups, `<li class="line-head"><span class="pos pos-%s">%s</span>%s%s</li>`,
			slug, strings.ToUpper(slug), counted(len(line), "jugador", "jugadores"), state)
		for _, player := range line {
			groups.WriteString(squadRow(player))
		}
	}
	return mkSection("plantilla", "plantilla", "Mi plantilla",
		`<ul class="rows">`+groups.String()+`</ul>`, "", len(squad))
}

// squadRow is one of mine: what he gives, what he is worth and where his clause and his sale
// stand.
func squadRow(player map[string]any) string {
	trend := number(player["pct_7d"])
	class, sign := "up", "+"
	if trend < 0 {
		class, sign = "down", ""
	}
	note := fmt.Sprintf(`%s · <span class="%s">%s%s %%</span>`, esMoney(number(player["value"])),
		class, sign, esNum(trend, 1))
	if asking := number(mapOf(player["market"])["min_bid"]); asking > 0 {
		note = "en venta " + esMoney(asking) + " · " + note
	}
	var chip string
	switch {
	case truthy(player["shielded"]):
		chip = tag("🛡 blindado", "done")
	case truthy(player["clause_locked"]):
		chip = clock(text(player["clause_locked_until"]), "se libera su cláusula")
	case number(player["clause"]) > 0:
		chip = tag("pagable "+esMoney(number(player["clause"])), "warn")
	}
	return row(player, "", esNum(number(player["xpts"]), 1)+" xPts", note, chip,
		Star(player)+CompareButton(player), "")
}

var positionNames = map[string]string{
	"1": "Portero", "2": "Defensa", "3": "Centrocampista", "4": "Delantero", "5": "Entrenador",
}

// rivalSections is one section per rival, each with his squad whole. Grouped by manager and
// not by player on purpose: "what does this one have" is how a league is actually read, and a
// single 158-row table answered a different question.
func (d Document) rivalSections(players []map[string]any) []string {
	teams := mapOf(d.Universe["league_teams"])
	if teams == nil {
		return nil
	}
	myTeamID := text(d.Universe["my_team_id"])

	// Your best in each position: the bar every rival's player is read against.
	best := map[string]map[string]any{}
	for _, player := range players {
		if !truthy(player["is_mine"]) {
			continue
		}
		position := text(player["position"])
		if current, seen := best[position]; !seen ||
			number(player["xpts"]) > number(current["xpts"]) {
			best[position] = player
		}
	}

	squads := map[string][]map[string]any{}
	for _, player := range players {
		owner := text(player["owner_team_id"])
		if owner == "" || owner == myTeamID || truthy(player["is_mine"]) {
			continue
		}
		row := make(map[string]any, len(player)+4)
		for key, value := range player {
			row[key] = value
		}
		if value := number(player["value"]); value > 0 {
			if clause := number(player["clause"]); clause > 0 {
				row["clause_x"] = clause / value
			}
		}
		// What he is listed at, if he is: a player already on sale is reachable without
		// paying his clause, and that changes the answer completely.
		if listing := mapOf(player["market"]); listing != nil {
			if asking := number(listing["min_bid"]); asking > 0 {
				row["asking"] = asking
			}
		}
		if mine := best[text(player["position"])]; mine != nil {
			row["vs_mine"] = number(player["xpts"]) - number(mine["xpts"])
			row["vs_who"] = mine["name"]
		}
		squads[owner] = append(squads[owner], row)
	}
	if len(squads) == 0 {
		return nil
	}

	// Ordered by the table, so the section order is the one the league is already read in.
	ordered := make([]map[string]any, 0, len(teams))
	for _, value := range teams {
		team := mapOf(value)
		if team == nil || text(team["team_id"]) == myTeamID {
			continue
		}
		if len(squads[text(team["team_id"])]) == 0 {
			continue
		}
		ordered = append(ordered, team)
	}
	sort.SliceStable(ordered, func(one, two int) bool {
		first, second := number(ordered[one]["position"]), number(ordered[two]["position"])
		if first != second && first > 0 && second > 0 {
			return first < second
		}
		return number(ordered[one]["points"]) > number(ordered[two]["points"])
	})

	// One rival at a time: twelve squads stacked is a lot of scrolling to answer a question
	// about one manager. The picker is the tab's own header, so it never scrolls away with the
	// squad it governs.
	options := make([]string, 0, len(ordered))
	out := make([]string, 0, len(ordered)+1)
	for _, team := range ordered {
		teamID := text(team["team_id"])
		squad := squads[teamID]
		// Read like a squad: keeper, defence, midfield, attack, and the best of each line first.
		sort.SliceStable(squad, func(one, two int) bool {
			first, second := number(squad[one]["position_id"]), number(squad[two]["position_id"])
			if first != second {
				return first < second
			}
			return number(squad[one]["xpts"]) > number(squad[two]["xpts"])
		})

		table, err := SectionTable("rivalsquad", squad)
		if err != nil {
			continue
		}
		manager := text(team["manager"])
		if manager == "" {
			manager = text(team["name"])
		}
		if manager == "" {
			manager = teamID
		}
		label := manager
		if position := number(team["position"]); position > 0 {
			label = fmt.Sprintf("%.0fº · %s", position, manager)
		}
		options = append(options, fmt.Sprintf(
			`<option value="rival-%s">%s · %d jugadores</option>`,
			Esc(teamID), Esc(label), len(squad)))
		out = append(out, rivalSquad(team, manager, squad, table))
	}

	picker := `<div class="pick-bar"><label>Equipo<select id="rival-pick">` +
		strings.Join(options, "") +
		`<option value="all">todos a la vez</option></select></label></div>`
	head := mkSection("rivalpick", "rivales", "Plantillas rivales", picker,
		"", len(ordered))
	return append([]string{head}, out...)
}

func (d Document) rankingSection(players []map[string]any) string {
	available := make([]map[string]any, 0, len(players))
	for _, player := range players {
		if truthy(player["available"]) {
			available = append(available, player)
		}
	}
	top := func(less func(one, two map[string]any) bool, keep func(map[string]any) bool,
		limit int) []map[string]any {
		out := []map[string]any{}
		for _, player := range available {
			if keep == nil || keep(player) {
				out = append(out, player)
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
		if len(out) > limit {
			out = out[:limit]
		}
		return out
	}
	byScore := top(func(one, two map[string]any) bool {
		return number(one["score"]) > number(two["score"])
	}, nil, 80)
	byXPts := top(func(one, two map[string]any) bool {
		return number(one["xpts"]) > number(two["xpts"])
	}, nil, 80)
	byValue := top(func(one, two map[string]any) bool {
		return number(one["points_value"]) > number(two["points_value"])
	}, func(player map[string]any) bool { return number(player["value"]) > 0 }, 40)
	return d.rankingView(byScore, byXPts, byValue)
}

var _ = math.Abs
