// Package advice turns the world into decisions: what to bid on, what to sell, which clause
// is worth paying, who can pay yours. A port of analysis.recommend().
//
// Every bucket here is sorted by a number that is written down rather than by "relevance",
// because a recommendation you cannot explain is not one. The two judgements that matter:
// only what is in today's market can be bid on (a free agent who is not listed is a
// watchlist entry, not a signing), and a clause is worth paying when it buys more points per
// million than the squad you already own — not when it looks cheap against futbolfantasy's
// ceiling, which the game's 1.5x pricing makes meaningless.
package advice

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// Squad rules of LaLiga Fantasy: eleven starters within a legal formation.
var (
	MinPerPosition   = map[int]int{1: 1, 2: 3, 3: 3, 4: 1}
	IdealPerPosition = map[int]int{1: 2, 2: 6, 3: 6, 4: 4}
	positions        = map[int]string{1: "POR", 2: "DEF", 3: "MED", 4: "DEL", 5: "ENT"}
)

type Row = map[string]any

// Shape is how the squad stands against the rules, per position.
func Shape(mine []Row) map[int]map[string]int {
	shape := map[int]map[string]int{}
	for positionID, ideal := range IdealPerPosition {
		owned := 0
		for _, player := range mine {
			if int(number(player["position_id"])) == positionID {
				owned++
			}
		}
		shape[positionID] = map[string]int{
			"owned": owned, "ideal": ideal, "minimum": MinPerPosition[positionID],
			"gap":     max(0, MinPerPosition[positionID]-owned),
			"surplus": max(0, owned-ideal),
		}
	}
	return shape
}

// Recommend builds every bucket the page reads.
func Recommend(universe Row, budget, maxDebt float64, limit int) Row {
	players := rowsOf(universe["players"])
	var mine, freeAgents, rivalPlayers []Row
	for _, player := range players {
		switch {
		case truthy(player["is_mine"]):
			mine = append(mine, player)
		case text(player["owner"]) == "":
			if truthy(player["available"]) {
				freeAgents = append(freeAgents, player)
			}
		default:
			rivalPlayers = append(rivalPlayers, player)
		}
	}
	shape := Shape(mine)
	spendingPower := budget + math.Max(0, maxDebt)

	entry := func(player Row, cost float64, route string, extra Row) Row {
		need := shape[int(number(player["position_id"]))]
		ideal := number(player["ideal_bid"])
		starred := 0.0
		if truthy(player["starred"]) {
			starred = 0.5
		}
		profitable := 0.0
		if ideal > 0 && cost <= ideal {
			profitable = 0.3
		}
		out := merge(player, Row{
			"entry_cost":   cost,
			"route":        route,
			"position_gap": need["gap"],
			"affordable":   cost <= spendingPower,
			"priority": number(player["score"]) + 0.35*float64(need["gap"]) +
				starred + profitable,
		})
		return merge(out, extra)
	}

	bidsNow, asks, myListings := []Row{}, []Row{}, []Row{}
	for _, player := range players {
		listing := mapOf(player["market"])
		if listing == nil {
			continue
		}
		cost := number(listing["min_bid"])
		if cost == 0 {
			cost = number(player["value"])
		}
		var over any
		if value := number(player["value"]); value != 0 {
			over = cost / value
		}
		switch {
		case truthy(listing["is_mine"]):
			ratio := number(over)
			myListings = append(myListings, merge(player, Row{
				"entry_cost": cost, "route": "mi venta", "ask_ratio": over,
				// Asking less than he is worth is worth flagging: it is usually a listing
				// left behind by a value that has since risen.
				"underpriced": over != nil && ratio < 1.0,
			}))
		case text(listing["kind"]) == "libre":
			if truthy(player["available"]) {
				bidsNow = append(bidsNow, entry(player, cost, "mercado libre",
					Row{"ask_ratio": over, "bids": listing["bids"]}))
			}
		default:
			ratio := number(over)
			asks = append(asks, entry(player, cost, "venta de rival", Row{
				"ask_ratio": over, "seller": listing["seller"],
				"overpriced": over != nil && ratio > 1.15,
			}))
		}
	}

	watchlist := []Row{}
	for _, player := range freeAgents {
		if mapOf(player["market"]) != nil {
			continue
		}
		if truthy(player["starred"]) || number(player["score"]) > 0.9 {
			watchlist = append(watchlist, entry(player, number(player["value"]),
				"sin listar", nil))
		}
	}

	var locked []Row
	var unlockDates []string
	for _, player := range rivalPlayers {
		if number(player["clause"]) != 0 && truthy(player["clause_locked"]) {
			locked = append(locked, player)
			if when := text(player["clause_locked_until"]); when != "" {
				unlockDates = append(unlockDates, when)
			}
		}
	}
	sort.Strings(unlockDates)

	raids := []Row{}
	for _, player := range rivalPlayers {
		clause := number(player["clause"])
		if clause == 0 || truthy(player["clause_locked"]) || clause > spendingPower {
			continue
		}
		need := shape[int(number(player["position_id"]))]
		var premium any
		if value := number(player["value"]); value != 0 {
			premium = clause / value
		}
		// Paying far over the market value is penalised rather than forbidden: it can still
		// be the right move for a player you need.
		over := math.Max(0, number(premiumOr(premium, 1.0))-1.5)
		raids = append(raids, merge(player, Row{
			"entry_cost": clause, "route": "clausula", "clause_premium": premium,
			"position_gap": need["gap"],
			"priority": number(player["score"]) + 0.35*float64(need["gap"]) - 0.5*over,
		}))
	}

	sells := []Row{}
	for _, player := range mine {
		need := shape[int(number(player["position_id"]))]
		// Empty rather than absent: "no reasons to sell him" is an answer, and the page
		// iterates the list.
		reasons := []string{}
		if !truthy(player["available"]) {
			reasons = append(reasons, "baja ("+text(player["status"])+")")
		}
		if projected := number(player["projected_pct"]); projected < -1.5 {
			reasons = append(reasons, "valor cayendo "+
				strconv.FormatFloat(projected, 'f', 1, 64)+"%/7d")
		}
		if probability := player["start_probability"]; probability != nil &&
			number(probability) < 40 {
			reasons = append(reasons, "titularidad "+
				strconv.FormatInt(int64(number(probability)), 10)+"%")
		}
		if need["surplus"] > 0 {
			reasons = append(reasons, "exceso de "+positions[int(number(player["position_id"]))])
		}
		if number(player["points_value"]) < 0.15 && number(player["value"]) > 5e6 {
			reasons = append(reasons, "pocos puntos por millon")
		}
		sells = append(sells, merge(player, Row{
			"reasons":  reasons,
			"pressure": -number(player["score"]) + 0.4*float64(len(reasons)),
		}))
	}

	rivalTeams := RivalCash(universe, budget)

	exposure := []Row{}
	for _, player := range mine {
		clause, value := number(player["clause"]), number(player["value"])
		if clause == 0 || value == 0 || truthy(player["clause_locked"]) {
			continue
		}
		var able []Row
		for _, team := range rivalTeams {
			if number(team["estimated_cash"]) >= clause {
				able = append(able, team)
			}
		}
		margin := clause / value
		// A cheap clause only matters if somebody in the league can actually pay it.
		if len(able) == 0 && margin >= 1.6 {
			continue
		}
		var top any
		if len(able) > 0 {
			top = able[0]["manager"]
		}
		risk := math.Max(0.2, number(player["score"])) *
			(0.4*float64(len(able)) + math.Max(0, 1.6-margin))
		exposure = append(exposure, merge(player, Row{
			"clause_margin": margin, "threats": len(able), "top_threat": top, "risk": risk,
		}))
	}

	for _, bucket := range [][]Row{bidsNow, asks, watchlist, raids} {
		byNumber(bucket, "priority")
	}
	byNumber(sells, "pressure")
	byNumber(exposure, "risk")
	byNumber(myListings, "entry_cost")

	// One row per offer, not per player. Two people bidding for the same player are two
	// different decisions, and collapsing them into "the best one" hid both who was asking
	// and when: the daily automatic bid and a rival's real offer looked identical.
	offers := []Row{}
	for _, player := range mine {
		received := rowsOf(player["offers"])
		if len(received) == 0 {
			continue
		}
		value := number(player["value"])
		if value == 0 {
			value = 1
		}
		listing := mapOf(player["market"])
		ask := number(listing["min_bid"])
		for _, offer := range received {
			amount := number(offer["money"])
			var vsAsk any
			if ask != 0 {
				vsAsk = amount / ask
			}
			who := text(offer["from"])
			if who == "" {
				who = "el mercado"
			}
			offers = append(offers, merge(player, Row{
				"offer_id": text(offer["id"]), "offer_amount": amount,
				"offer_expires": offer["expirationDate"], "offer_made": offer["createdAt"],
				"offer_from": who, "offer_from_market": truthy(offer["from_market"]),
				"offer_count": len(received),
				"market_id":   listing["market_id"], "ask": ask,
				"vs_value": amount / value, "vs_ask": vsAsk,
				// Worth taking when they pay over the market value, or over what you are asking.
				"worth_taking": amount >= value || (ask != 0 && amount >= ask),
			}))
		}
	}
	sort.SliceStable(offers, func(i, j int) bool {
		// People before the machine at equal money: a rival's offer expires on its own clock
		// and will not come back tomorrow.
		left, right := offers[i], offers[j]
		if truthy(left["offer_from_market"]) != truthy(right["offer_from_market"]) {
			return !truthy(left["offer_from_market"])
		}
		return number(left["vs_value"]) > number(right["vs_value"])
	})

	// The reference for "is this clause worth paying" is your own squad: do these euros buy
	// more points per million than what you already own? Benchmarking against today's market
	// instead brands everything a bargain, because a bad market day drags the median down and
	// says nothing about the player.
	var squadPPM []float64
	for _, player := range mine {
		value, xpts := number(player["value"]), number(player["xpts"])
		if value != 0 && xpts > 0 {
			squadPPM = append(squadPPM, xpts/(value/1e6))
		}
	}
	benchmark := median(squadPPM)

	clauses := mapOf(universe["clauses"])
	upcoming := []Row{}
	for _, player := range rowsOf(clauses["rivals_soon"]) {
		clause := number(player["clause"])
		if clause == 0 || number(player["score"]) <= 0.4 {
			continue
		}
		affordable := clause <= spendingPower
		ppm := number(player["xpts"]) / (clause / 1e6)
		var premium, vsMarket any
		if value := number(player["value"]); value != 0 {
			premium = clause / value
		}
		if benchmark != 0 {
			vsMarket = ppm / benchmark
		}
		upcoming = append(upcoming, merge(player, Row{
			"entry_cost": clause, "affordable": affordable, "clause_premium": premium,
			"ppm_at_clause": ppm, "vs_market": vsMarket,
			"verdict": RaidVerdict(ppm, benchmark, affordable, number(player["xpts"])),
		}))
	}

	// Rank the ones that cleared the gate: a tag is only useful if it separates them.
	var passed []Row
	for _, row := range upcoming {
		if text(row["verdict"]) == "" {
			passed = append(passed, row)
		}
	}
	sort.SliceStable(passed, func(i, j int) bool {
		return number(passed[i]["ppm_at_clause"]) > number(passed[j]["ppm_at_clause"])
	})
	for index, row := range passed {
		share := float64(index) / math.Max(1, float64(len(passed)-1))
		switch {
		case share <= 0.25:
			row["verdict"] = "chollo"
		case share <= 0.6:
			row["verdict"] = "renta"
		default:
			row["verdict"] = "justo"
		}
	}
	// Every clause in a league tends to unlock at the same instant, so the time is usually a
	// tie: break it by who is worth taking, not by squad order.
	raidOrder := map[string]int{"chollo": 0, "renta": 1, "justo": 2, "caro": 3,
		"sin datos": 4, "sin referencia": 4, "no te llega": 5}
	sort.SliceStable(upcoming, func(i, j int) bool {
		left, right := upcoming[i], upcoming[j]
		lo, ro := orderOr(raidOrder, text(left["verdict"]), 9), orderOr(raidOrder, text(right["verdict"]), 9)
		if lo != ro {
			return lo < ro
		}
		lh, rh := math.Round(number(left["hours_left"])), math.Round(number(right["hours_left"]))
		if lh != rh {
			return lh < rh
		}
		return number(left["ppm_at_clause"]) > number(right["ppm_at_clause"])
	})

	squad := append([]Row(nil), mine...)
	sort.SliceStable(squad, func(i, j int) bool {
		li, ri := int(number(squad[i]["position_id"])), int(number(squad[j]["position_id"]))
		if li != ri {
			return li < ri
		}
		return number(squad[i]["score"]) > number(squad[j]["score"])
	})

	starred := []Row{}
	for _, player := range players {
		if truthy(player["starred"]) {
			starred = append(starred, player)
		}
	}

	var unlockFrom any
	if len(unlockDates) > 0 {
		unlockFrom = unlockDates[0]
	}

	shapeOut := map[string]any{}
	for positionID, data := range shape {
		asAny := map[string]any{}
		for key, value := range data {
			asAny[key] = value
		}
		shapeOut[strconv.Itoa(positionID)] = asAny
	}

	return Row{
		"budget": budget, "max_debt": maxDebt, "spending_power": spendingPower,
		"squad": squad, "shape": shapeOut,
		"bids_now": head(bidsNow, limit), "asks": head(asks, limit),
		"watchlist": head(watchlist, limit), "my_listings": myListings,
		"offers": offers, "raids": head(raids, limit), "sells": head(sells, limit),
		"exposure": head(exposure, limit), "rivals": rivalTeams,
		"cash_model": universe["cash_model"], "free_agent_count": len(freeAgents),
		"clauses_locked": len(locked), "clauses_unlock_from": unlockFrom,
		"squad_ppm_benchmark": benchmark,
		"my_clauses_soon":     head(rowsOf(clauses["mine_soon"]), limit),
		"upcoming_raids":      head(upcoming, limit),
		"starred":             starred,
	}
}

// RaidVerdict is whether paying this clause is worth it, measured against your own squad.
//
// Not against futbolfantasy's ceiling: the game sets clauses at roughly 1.5x market value,
// so that comparison brands every raid expensive and tells you nothing. The question that
// discriminates is whether these euros buy more points per million than what you own.
func RaidVerdict(ppmAtClause, benchmark float64, affordable bool, xpts float64) string {
	if !affordable {
		return "no te llega"
	}
	if xpts <= 0 {
		return "sin datos"
	}
	if benchmark == 0 {
		return "sin referencia"
	}
	// Absolute gate first: paying more per point than what you already own is bad however it
	// ranks against the other candidates.
	if ppmAtClause < benchmark {
		return "caro"
	}
	return "" // ranked afterwards, once the whole candidate set is known
}

// RivalCash is the rivals sorted by spending power, and it puts your own row in the ranking:
// the number only means something next to the others.
func RivalCash(universe Row, myRealBudget float64) []Row {
	teams := mapOf(universe["league_teams"])
	myTeamID := text(universe["my_team_id"])

	var prices []float64
	for _, listing := range rowsOf(universe["market"]) {
		if bid := number(listing["min_bid"]); bid != 0 {
			prices = append(prices, bid)
		}
	}
	sort.Float64s(prices)
	medianPrice, topPrice := 5_000_000.0, 50_000_000.0
	if len(prices) > 0 {
		medianPrice = prices[len(prices)/2]
		topPrice = prices[len(prices)-1]
	}

	everyone := make([]Row, 0, len(teams))
	for _, value := range teams {
		team := mapOf(value)
		if team == nil {
			continue
		}
		cash := number(team["estimated_cash"])
		switch {
		case cash >= topPrice:
			team["power"], team["power_note"] = "holgado", "puede pagar lo mas caro del mercado"
		case cash >= medianPrice*2:
			team["power"], team["power_note"] = "normal", "le llega a la mayoria del mercado"
		default:
			team["power"] = "justo"
			team["power_note"] = "no le llega ni al jugador medio (" + short(medianPrice) + ")"
		}
		team["is_me"] = text(team["team_id"]) == myTeamID
		everyone = append(everyone, team)
	}

	sort.SliceStable(everyone, func(i, j int) bool {
		return number(everyone[i]["estimated_cash"]) > number(everyone[j]["estimated_cash"])
	})
	for index, team := range everyone {
		team["cash_position"] = index + 1
	}
	return everyone
}

func short(amount float64) string {
	if amount >= 1e6 {
		return strconv.FormatFloat(amount/1e6, 'f', 1, 64) + "M"
	}
	return strconv.FormatFloat(amount/1e3, 'f', 0, 64) + "K"
}

// --- helpers ----------------------------------------------------------------------------

func byNumber(bucket []Row, key string) {
	sort.SliceStable(bucket, func(i, j int) bool {
		return number(bucket[i][key]) > number(bucket[j][key])
	})
}

func head(bucket []Row, limit int) []Row {
	if limit > 0 && len(bucket) > limit {
		return bucket[:limit]
	}
	if bucket == nil {
		return []Row{}
	}
	return bucket
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func merge(base Row, extra Row) Row {
	out := make(Row, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func rowsOf(value any) []Row {
	switch typed := value.(type) {
	case []Row:
		return typed
	case []any:
		out := make([]Row, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(Row); ok {
				out = append(out, row)
			}
		}
		return out
	}
	return nil
}

func mapOf(value any) Row {
	if row, ok := value.(Row); ok {
		return row
	}
	return nil
}

// Rows reach here two ways: parsed from JSON, where every value is a plain float64 or
// string, and straight from the model, where absence is a nil pointer. Both have to read the
// same, and forgetting a pointer case is silent — a *float64 reads as zero and a whole table
// quietly loses its rows. It cost the actions table its four "vender" rows and three of its
// "fichar" ones before the page comparison caught it.
func text(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case *string:
		if typed == nil {
			return ""
		}
		return *typed
	case float64:
		if typed == math.Trunc(typed) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ""
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case *float64:
		if typed == nil {
			return 0
		}
		return *typed
	case *int:
		if typed == nil {
			return 0
		}
		return float64(*typed)
	case int:
		return float64(typed)
	case bool:
		if typed {
			return 1
		}
		return 0
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return parsed
	}
	return 0
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case *bool:
		return typed != nil && *typed
	case *float64:
		return typed != nil && *typed != 0
	case *string:
		return typed != nil && *typed != ""
	case float64:
		return typed != 0
	case string:
		return typed != ""
	}
	return false
}

func premiumOr(value any, fallback float64) any {
	if value == nil {
		return fallback
	}
	return value
}

func orderOr(table map[string]int, key string, fallback int) int {
	if value, ok := table[key]; ok {
		return value
	}
	return fallback
}
