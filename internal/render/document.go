package render

import (
	"encoding/json"
	"fmt"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
	"math"
	"sort"
	"strings"
	"time"
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

	// Necroporra is the side game: the open round's gameweek and deadline, a manager-name to
	// tebasfury team-id roster, your current vote (team ids) and whether this server may cast
	// one. Zero gameweek hides the section.
	NecroGameweek int
	NecroDeadline string
	NecroClosesAt time.Time
	NecroRoster   map[string]string
	NecroChosen   []string
	NecroCanVote  bool
	// NecroPreview forces the Decidir reminder to show even when you have already voted, for a
	// local look at the card.
	NecroPreview bool
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

// ViewData is every tab the browser draws, by name.
func (d Document) ViewData() map[string]any {
	meta := map[string]any{"mode": d.Mode, "build": Build}
	if budget := asFloat(d.Advice["budget"]); budget != nil {
		meta["cash"] = *budget
	}
	if len(d.Advice) == 0 {
		return map[string]any{"meta": meta}
	}
	return map[string]any{"meta": meta, "vender": d.SellData(), "comprar": d.BuyData(),
		"clausulas": d.ClauseData(), "partidos": d.MatchesData(), "rivales": d.RivalsData(),
		"plantilla": d.SquadData(), "squad": d.MySquadData(), "decidir": d.DecideData()}
}

// HTML renders the document.
func (d Document) HTML() string {
	page, _ := d.Render()
	return page
}

// Render is the page and the views it carries, built once: the page embeds them so the first
// paint needs no request, and the server keeps them for /api/views.
func (d Document) Render() (string, map[string]any) {
	views := d.ViewData()
	universe := d.Universe
	week := mapOf(universe["week"])
	players := rows(universe["players"])
	hasAdvice := len(d.Advice) > 0

	var sections []SectionView
	if hasAdvice {
		sections = append(sections, d.Views()...)
	}
	league := d.leagueShells()
	sections = append(sections, league[:2]...)
	if hasAdvice {
		sections = append(sections, d.squadSection())
		shells, rivals := d.rivalViews(players)
		sections = append(sections, shells...)
		for name, view := range rivals {
			views[name] = view
		}
	}
	sections = append(sections, league[2:]...)
	for name, view := range d.leagueViews() {
		views[name] = view
	}
	sections = append(sections, shell("v-ranking", "ranking", "ranking"))
	views["ranking"] = d.RankingData(players)
	necro, hasNecro := d.NecroData()
	if hasNecro {
		sections = append(sections, shell("necroporra", "necroporra", "necroporra"))
		views["necroporra"] = necro
	}
	sections = append(sections, SectionView{ID: "comparador", Tab: "comparador", UI: "comparador"})

	page := PageData{Stats: d.widgets(week, players), Sections: sections,
		Foot: Foot{Generated: d.Generated, League: d.LeagueName,
			Week: int(number(week["weekNumber"])), Mode: d.Mode,
			Weight: fmt.Sprintf("%.0f%%", number(universe["current_weight"])*100)}}
	if hasAdvice {
		for _, tab := range Tabs {
			// The side game's tab only while a round is configured and open.
			if tab.ID != "necroporra" || hasNecro {
				page.Tabs = append(page.Tabs, tab)
			}
		}
	}
	views["page"] = page
	blob, _ := json.Marshal(views)
	return Page(d.CSS, CrestCSS(), string(blob)), views
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

// --- widgets ---------------------------------------------------------------------------

// widgets are the four cards under the tab bar.
func (d Document) widgets(week map[string]any, players []map[string]any) []Stat {
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
	matchday := Stat{Label: fmt.Sprintf("Jornada %d", weekNumber),
		Value: value, Deadline: deadline, Note: note, Tab: "partidos"}
	var clauses []Stat
	if stat, ok := d.clauseStat(); ok {
		clauses = []Stat{stat}
	}

	if len(d.Advice) == 0 {
		return append(append([]Stat{matchday}, clauses...),
			Stat{Label: "Jugadores", Value: fmt.Sprintf("%d", len(players)),
				Note: fmt.Sprintf("%d con datos de futbolfantasy",
					int(number(d.Universe["matched_count"])))},
			Stat{Label: "Sesión", Value: "sin liga", Note: "solo datos públicos"})
	}

	teams := mapOf(d.Universe["league_teams"])
	me := mapOf(teams[text(d.Universe["my_team_id"])])
	budget := asFloat(d.Advice["budget"])

	position := "?"
	if seat := asFloat(me["position"]); seat != nil {
		position = fmt.Sprintf("%d", int(*seat))
	}
	exact := ""
	if budget != nil {
		exact = group(fmt.Sprintf("%.0f", *budget)) + " €"
	}
	stats := []Stat{{Label: "Caja", Value: esMoney(number(d.Advice["budget"])), ValueID: "kpi-cash",
		Note: exact, Tab: "rivales"}}
	stats = append(stats, clauses...)
	return append(stats, matchday, Stat{Label: "Liga", Value: position + "º",
		Small: fmt.Sprintf("de %d", len(teams)),
		Note:  fmt.Sprintf("%d pts", int(number(me["points"]))), Tab: "rivales"})
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

// actionRows are the advice's judgements, one per player, that the decision cards are drawn from.
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

var positionNames = map[string]string{
	"1": "Portero", "2": "Defensa", "3": "Centrocampista", "4": "Delantero", "5": "Entrenador",
}

var _ = math.Abs
