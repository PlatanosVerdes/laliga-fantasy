package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
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
	// Endings is how my previous bids and offers finished, read from where they were saved.
	Endings []map[string]any
	// MineByWeek is how many of my players each team had on a past matchday, keyed by week.
	// Reconstructed outside, because that needs the transfer log and this package only draws.
	MineByWeek map[int]map[string]int
	// The league's house rules: the hold period, its exceptions, and the social pacts.
	HoldDays       int
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
		sections = append(sections, d.swapSection())
		sections = append(sections, d.actionsSection())
		sections = append(sections, d.planSection())
		sections = append(sections, d.raidsSection())
		sections = append(sections, d.offersSection())
	}
	sections = append(sections, d.feedSection())
	if hasAdvice {
		sections = append(sections, Pitch)
		sections = append(sections, d.squadSection())
		sections = append(sections, d.marketSections()...)
		sections = append(sections, d.clauseSections()...)
	}
	sections = append(sections, d.scheduleSection(players))
	sections = append(sections, d.rulesSection())
	sections = append(sections, d.rankingSections(players)...)

	kpis := d.widgets(week, players)
	header := Header(d.Generated, d.LeagueName, int(number(week["weekNumber"])), kpis,
		hasAdvice, d.Mode)
	footer := Footer(number(universe["current_weight"]))

	body := strings.Join(filterEmpty(sections), "")
	return Page(d.CSS, d.JS, CrestCSS(), header, body, footer, d.Modal, d.Drawer)
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

func (d Document) widgets(week map[string]any, players []map[string]any) []string {
	// The one number on this card that keeps changing is how long is left, so that is the
	// number, ticking every second. What state the week is in becomes the small print.
	state := "cerrada"
	if truthy(week["isLive"]) {
		state = "en juego"
	}
	closing := text(week["closingWeekDate"])
	value := state
	deadline := ""
	if closing != "" {
		value = LeftUntil(closing)
		deadline = closing
	}
	notes := []string{}
	if closes := whenLabel(closing); closes != "" {
		notes = append(notes, "cierra "+closes)
	}
	if nextOpens := whenLabel(text(d.Universe["next_week_opens"])); nextOpens != "" {
		notes = append(notes, fmt.Sprintf("J%d desde %s", int(number(week["nextWeek"])), nextOpens))
	}

	kpis := []string{Widget(KPI{
		Label:    fmt.Sprintf("Jornada %d", int(number(week["weekNumber"]))),
		Value:    value,
		Deadline: deadline,
		Hint:     state, Notes: notes, Status: "neutral"})}

	if len(d.Advice) == 0 {
		kpis = append(kpis,
			Widget(KPI{Label: "Jugadores", Value: fmt.Sprintf("%d", len(players)),
				Hint: fmt.Sprintf("%d con datos de futbolfantasy",
					int(number(d.Universe["matched_count"])))}),
			Widget(KPI{Label: "Sesion", Value: "sin liga", Hint: "solo datos publicos"}))
		return kpis
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
	kpis = append(kpis,
		Widget(KPI{Label: "Mi puesto", Value: position + "º",
			Hint: fmt.Sprintf("%d puntos", int(number(me["points"]))),
			Rank: pointsRank, Meter: &pointsShare, Status: pointsStatus, Tab: "rivales"}),
		Widget(KPI{Label: "Mi saldo", Value: Money(budget),
			Hint: text(me["power_note"]), Rank: cashRank, Meter: &cashShare,
			Status: cashStatus, Tab: "rivales"}),
		Widget(KPI{Label: "Valor de plantilla", Value: Money(&squadValue),
			Hint: fmt.Sprintf("%d jugadores", len(squad)),
			Rank: valueRank, Meter: &valueShare, Status: valueStatus, Tab: "plantilla"}))

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
		kpis = append(kpis, Widget(KPI{Label: "Ofertas que interesan",
			Value: fmt.Sprintf("%d", len(goodOffers)), Hint: strings.Join(names, ", "),
			Rank: "cobra", Status: "good", Tab: "ofertas"}))
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
	kpis = append(kpis,
		Widget(KPI{Label: "xPts del mejor 11", Value: Num(&bestEleven, 1), Hint: "por jornada"}),
		Widget(KPI{Label: "Pujables ahora", Value: fmt.Sprintf("%d", len(bids)),
			Hint: fmt.Sprintf("%d mas en venta por rivales", len(asks)), Tab: "fichajes"}),
		Widget(KPI{Label: "Cláusulas a tiro", Value: fmt.Sprintf("%d", len(raids)),
			Hint: clauseHint, Tab: "clausulas"}))
	return kpis
}

// whenLabel is "vie 22 ago 19:30": the day matters more than the exact hour, and a weekday
// is easier to place than a date.
func whenLabel(value string) string {
	when, ok := parseStamp(value)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s %d %s %02d:%02d", weekdays[(int(when.Weekday())+6)%7],
		when.Day(), months[int(when.Month())], when.Hour(), when.Minute())
}

// --- the sections ----------------------------------------------------------------------

// swapSection is the plan: pairs of "this one out, this one in" with the arithmetic. It goes
// first because it is the answer to the question the rest of the page only supplies evidence
// for, and because twenty-six decisions in a table is exactly the moment somebody gives up.
func (d Document) swapSection() string {
	plan := d.Swaps
	if len(plan) == 0 {
		return ""
	}
	count := ""
	if moves := rows(plan["moves"]); len(moves) > 0 {
		count = fmt.Sprintf("%d cambios", len(moves))
	}
	return Section("Plan ideal", SwapPlan(plan),
		"Cambios de uno por uno, misma posicion, sin dejar el once ilegal y sin pagar por "+
			"encima del techo de futbolfantasy. Los puntos que compras tienen que salir "+
			"<strong>mas baratos por millon que lo que ya tienes</strong>, que es lo que "+
			"descarta al fichaje caro que parece un salto. El precio de venta es la oferta "+
			"que ya tienes encima de la mesa cuando la hay; si no, su valor de mercado, y "+
			"la caja descuenta lo que tengas comprometido en pujas.",
		count, "plan")
}

func (d Document) actionsSection() string {
	rowsOut := d.actionRows()
	legend := `<div class="legend">`
	// Same order as Python's dict literal, which is insertion order.
	for _, key := range []string{"buy", "bidding", "clause", "protect", "sell", "out"} {
		spec := Verdicts[key]
		legend += fmt.Sprintf(`<span><span class="swatch" style="background:var(--%s)"></span>%s</span>`,
			spec.Status, spec.Label)
	}
	legend += `<span><span class="swatch" style="background:var(--pole-pos)"></span>valor subiendo</span>` +
		`<span><span class="swatch" style="background:var(--pole-neg)"></span>valor bajando</span>` +
		`</div>`

	buys := 0
	for _, row := range rowsOut {
		if text(row["verdict"]) == "buy" {
			buys++
		}
	}
	if buys == 0 {
		// Saying "nothing is worth buying today" is a result, not an empty state.
		legend += `<p class="callout">Hoy <strong>ningun jugador del mercado sale rentable</strong> ` +
			`a lo que piden: ni el mercado libre ni las ventas de rivales estan por debajo ` +
			`del techo que calcula futbolfantasy. No pujar tambien es una decision.</p>`
	}

	table := TableIn(columnsFor("acciones"), rowsOut, "Nada urgente", "", false)
	return Section("Que hacer ahora", legend+table,
		"Todo lo accionable en una tabla, de lo urgente a lo que puede esperar. "+
			"Cada fila lleva el motivo escrito: el color repite el dato, no lo sustituye.",
		fmt.Sprintf("%d decisiones", len(rowsOut)), "acciones")
}

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

	for index, player := range rows(d.Advice["my_clauses_soon"]) {
		if index >= 6 {
			break
		}
		out = append(out, merge(player, map[string]any{
			"verdict": "protect", "entry_cost": player["clause"],
			"why": fmt.Sprintf("su cláusula se desbloquea en %.0fh: quedas expuesto",
				number(player["hours_left"]))}))
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
					// The net is the number that decides: the offer pays for part of the
					// replacement, and sometimes for all of it.
					net := number(stand["cost"]) - number(offer["offer_amount"])
					effect := fmt.Sprintf("te cuesta %s neto", Money(&net))
					if net <= 0 {
						gained := -net
						effect = fmt.Sprintf("y encima ganas %s", Money(&gained))
					}
					why += fmt.Sprintf(" · es el ultimo %s que te queda: fichas antes a %s por "+
						"%s (%+.2f xPts, %s) y entonces puedes venderlo", position,
						text(stand["name"]), Money(asFloat(stand["cost"])), gap, effect)
				} else {
					// Legal but worse: say the price in points so the trade is judged, not sold.
					why += fmt.Sprintf(" · es el ultimo %s que te queda y el unico repuesto a "+
						"tiro es %s por %s, con %+.2f xPts: probablemente no compense",
						position, text(stand["name"]), Money(asFloat(stand["cost"])), gap)
					verdict = "cash_blocked"
				}
			} else {
				blocked = "es el ultimo " + position + " que te queda y no hay repuesto a tiro"
				verdict = "cash_blocked"
			}
		}
		if blocked != "" {
			why += " · pero " + blocked
		}
		out = append(out, merge(offer, map[string]any{
			"verdict": verdict, "entry_cost": offer["offer_amount"], "why": why}))
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

func (d Document) planSection() string {
	if len(d.Plan) == 0 {
		return ""
	}
	// Only what will actually run counts as queued. An "avisar" is the plan saying it will not
	// act, and calling it a queued action was the page contradicting itself two lines later.
	pending := 0
	for _, action := range d.Plan {
		switch text(action["action"]) {
		case "poner_en_venta", "aceptar_oferta":
			pending++
		}
	}
	note := "Instrucciones permanentes. El interruptor solo mantiene al jugador " +
		"en el mercado. Para que se venda solo hay dos formas, y ninguna es " +
		"automatica por defecto: marcar <strong>vender automaticamente</strong> " +
		"en su ficha, que acepta cualquier oferta que llegue a lo que pides, o " +
		"fijar un importe en «aceptar desde». Sin una de las dos, una oferta " +
		"buena solo <strong>avisa</strong> y decides tu."
	if pending > 0 {
		note += fmt.Sprintf(" <strong>%d accion(es) en cola</strong>, "+
			"se ejecutan en el proximo ciclo si el servidor esta en modo auto.", pending)
	}
	table, _ := SectionTable("siempre", d.withPolicies(d.Plan))
	return Section("Siempre en mercado", table, note,
		fmt.Sprintf("%d", len(d.Policies)), "siempre")
}

// withPolicies pastes the two amounts onto each row, because the renderer does not read
// files: a section that depends on the filesystem cannot be compared.
func (d Document) withPolicies(plan []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(plan))
	for _, action := range plan {
		policy := d.Policies[text(action["player_id"])]
		out = append(out, merge(action, map[string]any{
			"policy_min_price":    policy["min_price"],
			"policy_accept_above": policy["accept_above"],
		}))
	}
	return out
}

func (d Document) raidsSection() string {
	if len(d.Raids) == 0 {
		return ""
	}
	armed := 0
	for _, raid := range d.Raids {
		if text(raid["action"]) == "pagar_clausula" {
			armed++
		}
	}
	note := "Clausulazos programados: se pagan solos en cuanto la cláusula se " +
		"libere, <strong>y solo si sigue por debajo del limite que fijaste</strong>. " +
		"Si el dueño la sube o blinda al jugador, se cancela en vez de pagar de mas."
	if armed > 0 {
		note += fmt.Sprintf(" <strong>%d listo(s) para ejecutar ahora.</strong>", armed)
	}
	table, _ := SectionTable("programados", d.Raids)
	return Section("Clausulazos programados", table, note,
		fmt.Sprintf("%d", len(d.Raids)), "programados")
}

func (d Document) offersSection() string {
	offers := rows(d.Advice["offers"])
	if len(offers) == 0 {
		return ""
	}
	var good []string
	for _, offer := range offers {
		if truthy(offer["worth_taking"]) {
			good = append(good, Esc(text(offer["name"])))
		}
	}
	note := "Lo que te ofrecen por los jugadores que tienes en venta. " +
		"<strong>Sobre su valor</strong> es lo que pagan comparado con lo que vale: " +
		"por encima de 1.00x te estan pagando de mas."
	if len(good) > 0 {
		note += " Ahora mismo interesan: <strong>" + strings.Join(good, ", ") + "</strong>."
	}
	table, _ := SectionTable("ofertas", offers)
	return Section("Ofertas que has recibido", table, note,
		fmt.Sprintf("%d", len(offers)), "ofertas")
}

// feedSection is outside the advice block on purpose: the league moves whether or not this
// run could read a squad, and an empty feed says something worth reading.
// scheduleSection is the fixture list ahead: what each of your players faces, month by month.
// It answers the question the weekly widget cannot — whether a good run of matches is coming.
func (d Document) scheduleSection(players []map[string]any) string {
	fixtures := rows(d.Universe["schedule"])
	if len(fixtures) == 0 {
		return ""
	}
	mine := map[string]int{}
	for _, player := range players {
		if truthy(player["is_mine"]) {
			mine[text(player["team_id"])]++
		}
	}
	return Section("Calendario de partidos", MatchCalendar(fixtures, mine, d.MineByWeek),
		"Las proximas jornadas, con los partidos de tus jugadores marcados. "+
			"Una racha buena o mala se ve aqui antes que en el precio.",
		fmt.Sprintf("%d jornadas", weeksIn(fixtures)), "partidos")
}

func weeksIn(fixtures []map[string]any) int {
	seen := map[int]bool{}
	for _, fixture := range fixtures {
		seen[int(number(fixture["week"]))] = true
	}
	return len(seen)
}

// rulesSection is the league's own pact. Everything here is invisible to the API, so if it
// is not written down it does not exist.
func (d Document) rulesSection() string {
	if d.HoldDays == 0 && len(d.RuleNotes) == 0 {
		return ""
	}
	count := ""
	if total := len(d.RuleNotes) + map[bool]int{true: 1, false: 0}[d.HoldDays > 0]; total > 0 {
		count = fmt.Sprintf("%d normas", total)
	}
	return Section("Normas de la liga", HouseRules(d.HoldDays, d.HoldExceptions, d.RuleNotes),
		"Lo que no sabe el juego. Solo la primera cambia lo que te propongo; las demas "+
			"estan aqui para consultarlas.", count, "normas")
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
	return Section("Movimientos de la liga", Feed(events),
		"Quien ha fichado y vendido, y por cuanto. Las operaciones grandes "+
			"cuentan quien se esta quedando sin caja.",
		fmt.Sprintf("%d operaciones", moves), "movimientos")
}

func (d Document) squadSection() string {
	squad := rows(d.Advice["squad"])
	shape := mapOf(d.Advice["shape"])
	var bits []string
	for _, positionID := range []string{"1", "2", "3", "4"} {
		data := mapOf(shape[positionID])
		if data == nil {
			continue
		}
		// A gap is bolded and a surplus is not: one of them is a hole in the eleven and the
		// other is a player you could sell.
		state := "ok"
		if truthy(data["gap"]) {
			state = "<strong>falta</strong>"
		} else if truthy(data["surplus"]) {
			state = "sobra"
		}
		bits = append(bits, fmt.Sprintf("%s %d/%d (%s)", positionNames[positionID],
			int(number(data["owned"])), int(number(data["ideal"])), state))
	}
	table, _ := SectionTable("plantilla", squad)
	return Section("Mi plantilla", table, strings.Join(bits, " · "), "", "plantilla")
}

var positionNames = map[string]string{
	"1": "Portero", "2": "Defensa", "3": "Centrocampista", "4": "Delantero", "5": "Entrenador",
}

func (d Document) marketSections() []string {
	var out []string

	bids := rows(d.Advice["bids_now"])
	expiry := ""
	if len(bids) > 0 {
		if listing := mapOf(bids[0]["market"]); listing != nil {
			expiry = text(listing["expires"])
		}
	}
	note := "Los jugadores sin dueño que el juego saca hoy: son los unicos que puedes " +
		"pujar en este momento. <strong>Puja maxima rentable</strong> es el techo que " +
		"calcula futbolfantasy; por encima compras caro."
	if expiry != "" {
		stamp := expiry
		if len(stamp) > 16 {
			stamp = stamp[:16]
		}
		note += fmt.Sprintf(" El mercado cierra <strong>%s</strong>.",
			Esc(strings.ReplaceAll(stamp, "T", " ")))
	}
	table, _ := SectionTable("mercado", bids)
	out = append(out, Section("Pujar ahora · mercado libre", Filters+table, note,
		fmt.Sprintf("%d", len(bids)), "fichajes"))

	asks := rows(d.Advice["asks"])
	table, _ = SectionTable("enventa", asks)
	out = append(out, Section("En venta por rivales", table,
		"Lo que los demas han puesto en el mercado, con lo que piden comparado con el "+
			"valor real. Aqui es donde aparecen los precios de fantasia.",
		fmt.Sprintf("%d", len(asks)), "enventa"))

	if sent := rows(d.Advice["my_bids"]); len(sent) > 0 {
		table, _ = SectionTable("mispujas", sent)
		out = append(out, Section("Lo que tienes puesto", table,
			"Tus pujas y ofertas vivas, con lo que has ofrecido, lo que piden y cuanto les "+
				"queda. Desde aqui se cambian o se retiran: hasta ahora habia que buscar al "+
				"jugador en otra tabla para verlas.",
			fmt.Sprintf("%d", len(sent)), "mispujas"))
	}

	// How the previous ones ended, which is the only place it is recorded: the API forgets a
	// refused offer the moment it is refused.
	//
	// Drawn even when empty, unlike the other optional sections: a section that only exists once
	// it has content cannot be found by somebody looking for where it will appear, and this one
	// fills days after it is switched on.
	if d.Mode != "informe" || len(d.Endings) > 0 {
		out = append(out, Section("Como acabaron", Endings(d.Endings),
			"Tus pujas y ofertas ya resueltas. <strong>Rechazada</strong> es que el dueño dijo "+
				"no; <strong>perdida</strong> es que se lo quedo otro, y ahi el precio importa "+
				"mas que el rival; <strong>caducada</strong> es que el anuncio cerro sin venta. "+
				"Se guarda aqui porque el juego no lo recuerda.",
			fmt.Sprintf("%d", len(d.Endings)), "resueltas"))
	}

	if listings := rows(d.Advice["my_listings"]); len(listings) > 0 {
		var under []string
		for _, row := range listings {
			if truthy(row["underpriced"]) {
				under = append(under, Esc(text(row["name"])))
			}
		}
		note := "Lo que tienes tu en venta ahora mismo."
		if len(under) > 0 {
			note += " <strong>Ojo:</strong> " + strings.Join(under, ", ") +
				" esta por debajo de su valor de mercado."
		}
		table, _ = SectionTable("misventas", listings)
		out = append(out, Section("Mis ventas en curso", table, note, "", "misventas"))
	}

	if watchlist := rows(d.Advice["watchlist"]); len(watchlist) > 0 {
		table, _ = SectionTable("seguimiento", watchlist)
		out = append(out, Section("Seguimiento · libres sin listar", table,
			"Buenos, sin dueño, pero <strong>no estan en el mercado</strong>: no se "+
				"pueden pujar hoy. Marcalos con la estrella y apareceran arriba en cuanto "+
				"salgan.", "", "seguimiento"))
	}

	raids := rows(d.Advice["raids"])
	table, _ = SectionTable("clausulas", raids)
	if len(raids) == 0 && number(d.Advice["clauses_locked"]) > 0 {
		from := text(d.Advice["clauses_unlock_from"])
		if len(from) > 10 {
			from = from[:10]
		}
		table = fmt.Sprintf(`<p class="empty">Ninguna: las %d cláusulas de la liga siguen `+
			`bloqueadas hasta %s.</p>`, int(number(d.Advice["clauses_locked"])), from)
	}
	out = append(out, Section("Cláusulas pagables", table,
		"Jugadores de rivales con la cláusula desbloqueada y dentro de tu poder de compra.",
		fmt.Sprintf("%d", len(raids)), "clausulas"))

	sells := rows(d.Advice["sells"])
	table, _ = SectionTable("ventas", sells)
	out = append(out, Section("Candidatos a vender", table,
		"Ordenados por presion de venta: score bajo, valor cayendo, poca titularidad "+
			"o exceso en la posicion.", "", "ventas"))

	exposure := rows(d.Advice["exposure"])
	table, _ = SectionTable("riesgo", exposure)
	out = append(out, Section("Riesgo de cláusula", table,
		"Tus jugadores buenos con cláusula baja, contando cuantos rivales tienen caja "+
			"para pagarla ahora mismo.", "", "riesgo"))
	return out
}

func (d Document) clauseSections() []string {
	var out []string

	mine := rows(d.Advice["my_clauses_soon"])
	table, _ := SectionTable("vencimientos", mine)
	out = append(out, Section("Mis cláusulas que vencen", table,
		"Cuando el candado cae, cualquiera con caja suficiente puede pagarla. "+
			"Subir la cláusula antes de esa fecha es la unica defensa.",
		fmt.Sprintf("%d", len(mine)), "vencimientos"))

	clauses := mapOf(d.Universe["clauses"])
	entries := append(rows(clauses["mine"]), rows(clauses["rivals"])...)
	out = append(out, Section("Calendario de clausulazos",
		Calendar(entries, number(d.Advice["spending_power"])),
		"Cuando se abre cada cláusula. Los tuyos van marcados: ese dia quedas "+
			"expuesto y a la vez puedes atacar. Al arrancar la temporada se abren todas "+
			"de golpe, asi que el dia importa mas que la hora.", "", "calendario"))

	upcoming := rows(d.Advice["upcoming_raids"])
	table, _ = SectionTable("proximas", upcoming)
	note := "El otro lado del mismo reloj: en cuanto se abren, se pueden pagar. " +
		"<strong>¿Renta?</strong> compara los puntos por millon que sacas " +
		"<em>pagando la cláusula</em> con la mediana de tu plantilla " +
		fmt.Sprintf("(%.3f pts/M): si es peor que lo que ", number(d.Advice["squad_ppm_benchmark"])) +
		"ya tienes, sale <em>caro</em>. No lo comparo con el techo de " +
		"futbolfantasy porque el juego fija las cláusulas sobre 1.5x el valor, " +
		"asi que por ese criterio ninguna saldria rentable nunca — la columna " +
		"esta ahi para que veas el dato."
	out = append(out, Section("Cláusulas de rivales que se abren", table, note,
		fmt.Sprintf("%d", len(upcoming)), "oportunidades"))

	if rivals := rows(d.Advice["rivals"]); len(rivals) > 0 {
		model := mapOf(d.Advice["cash_model"])
		note := "El API solo publica el saldo de tu equipo, y la cifra de la clasificacion es " +
			"solo el valor de la plantilla. Asi que la caja se reconstruye del historial: " +
			"<strong>caja = base + ventas &minus; compras</strong>. "
		if truthy(model["anchored"]) {
			note += fmt.Sprintf("La base (%s) esta medida sobre tu propio saldo "+
				"real, asi que absorbe la caja inicial y tus recompensas de una vez. "+
				"El error que queda es solo la diferencia de recompensas diarias entre "+
				"managers, como maximo %s.", Money(asFloat(model["base"])),
				Money(asFloat(model["uncertainty"])))
		} else {
			note += fmt.Sprintf("Sin saldo propio que leer, asumo %s de "+
				"caja inicial para todos.", Money(asFloat(model["base"])))
		}
		table, _ = SectionTable("rivales", rivals)
		out = append(out, Section("Poder de compra de la liga", table, note, "", "rivales"))
	}
	if held := d.heldSection(rows(d.Universe["players"])); held != "" {
		out = append(out, held)
	}
	return out
}

// heldSection is every rival squad, whole, in one table. It was answerable all along and
// simply had nowhere to be asked: the rival tables only ever showed what somebody had put on
// the market or whose clause was about to open, never the squads themselves.
func (d Document) heldSection(players []map[string]any) string {
	// Your best in each position, which is the bar every rival's player is read against.
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

	held := make([]map[string]any, 0, 160)
	owners := map[string]bool{}
	for _, player := range players {
		if truthy(player["is_mine"]) || text(player["owner_team_id"]) == "" {
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
		owners[text(player["owner_team_id"])] = true
		held = append(held, row)
	}
	if len(held) == 0 {
		return ""
	}
	// Best first: this is read looking for who to go after, not to audit a census.
	sort.SliceStable(held, func(one, two int) bool {
		return number(held[one]["xpts"]) > number(held[two]["xpts"])
	})

	table, err := SectionTable("quientiene", held)
	if err != nil {
		return ""
	}
	note := "Todos los jugadores que tienen los demas, ordenados por lo que rinden. " +
		"<strong>Frente a lo tuyo</strong> compara con tu mejor jugador de esa misma " +
		"posicion, que es lo que decide si merece la pena ir a por el. <strong>Se puede</strong> " +
		"dice si su clausula esta pagable hoy, cuanto le queda o si esta blindado. " +
		"El buscador tambien encuentra por manager, y el <strong>+</strong> mete al jugador " +
		"en el comparador."
	return Section("Plantillas rivales", Filters+table, note,
		fmt.Sprintf("%d jugadores de %d rivales", len(held), len(owners)), "quientiene")
}

func (d Document) rankingSections(players []map[string]any) []string {
	available := make([]map[string]any, 0, len(players))
	for _, player := range players {
		if truthy(player["available"]) {
			available = append(available, player)
		}
	}

	byScore := append([]map[string]any(nil), available...)
	sort.SliceStable(byScore, func(i, j int) bool {
		return number(byScore[i]["score"]) > number(byScore[j]["score"])
	})
	if len(byScore) > 80 {
		byScore = byScore[:80]
	}
	table, _ := SectionTable("plantilla", byScore)
	// The ranking uses the shared player columns, so it borrows the squad's spec but never
	// its section: these players are not yours and the "mio" flag has to show.
	table = TableIn(PlayerColumns(""), byScore, "Sin datos", "", true)
	out := []string{Section("Ranking global", Filters+table,
		"Los 80 mejores de LaLiga por score, con dueño o sin el. Filtra por posicion y "+
			"precio, o pincha una cabecera para reordenar.", "top 80", "ranking")}

	byValue := make([]map[string]any, 0, len(available))
	for _, player := range available {
		if number(player["value"]) > 0 {
			byValue = append(byValue, player)
		}
	}
	sort.SliceStable(byValue, func(i, j int) bool {
		return number(byValue[i]["points_value"]) > number(byValue[j]["points_value"])
	})
	if len(byValue) > 40 {
		byValue = byValue[:40]
	}
	out = append(out, Section("Mejor rentabilidad",
		TableIn(PlayerColumns(""), byValue, "Sin datos", "", false),
		"xPts esperados por jornada divididos entre el precio. La metrica que manda cuando "+
			"vas justo de saldo.", "", "rentabilidad"))
	return out
}

var _ = math.Abs
