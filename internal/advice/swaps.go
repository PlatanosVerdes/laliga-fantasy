package advice

import (
	"fmt"
	"math"
	"sort"
)

// The squad rules the plan must not break: eleven legal starters need a keeper, three
// defenders, three midfielders and a striker.
var minPerPosition = map[int]int{1: 1, 2: 3, 3: 3, 4: 1}

var positionWord = map[int]string{1: "portero", 2: "defensa", 3: "centrocampista",
	4: "delantero"}

// Swaps is the plan: which of yours to let go, who to bring in his place, and what each
// change does to your points and your cash.
//
// It exists because the tables answer "what is available" and nobody asks that. The question
// is "what do I actually do", and the honest form of that answer is a short list of pairs
// with the arithmetic attached — not another ranking to read.
//
// Deliberately conservative in three ways. Only same-position swaps, so the eleven stays
// legal without the plan having to reason about formations. Only candidates whose asking
// price is at or under futbolfantasy's profitable ceiling, so it never proposes overpaying.
// And every number is the one you would actually get: the offer on the table for a sale, the
// asking price for a purchase.
func Swaps(universe Row, buckets Row, cash float64) Row {
	// The bar every paid swap has to clear: your own squad's points per million. Buying
	// points at a worse rate than what you already own is how a budget disappears into one
	// name, and it is exactly the trade that feels like an upgrade.
	benchmark := number(buckets["squad_ppm_benchmark"])
	squad := rowsOf(buckets["squad"])
	if len(squad) == 0 {
		return Row{}
	}

	// What each of mine would fetch: an offer already on the table beats an estimate.
	offerFor := map[string]Row{}
	for _, offer := range rowsOf(buckets["offers"]) {
		offerFor[text(offer["id"])] = offer
	}

	counts := map[int]int{}
	for _, player := range squad {
		counts[int(number(player["position_id"]))]++
	}

	// Candidates: the free market first (you bid and it is yours if nobody outbids), then
	// what rivals are selling.
	var candidates []Row
	for _, bucket := range []string{"bids_now", "asks"} {
		for _, row := range rowsOf(buckets[bucket]) {
			cost := number(row["entry_cost"])
			if cost == 0 {
				cost = number(row["value"])
			}
			ceiling := number(row["ideal_bid"])
			if cost == 0 || number(row["xpts"]) <= 0 {
				continue
			}
			// No ceiling published, or a price above it, is futbolfantasy saying no. The plan
			// does not argue with that.
			if ceiling == 0 || cost > ceiling {
				continue
			}
			candidates = append(candidates, merge(row, Row{"cost": cost, "source": bucket}))
		}
	}


	taken := map[string]bool{}
	var moves []Row
	// Money already promised is money you do not have: a bid you placed can land tomorrow.
	committed := pendingBids(universe)
	free := cash - committed

	// Sell side, worst first: an unavailable player is dead weight however much he cost.
	sellable := append([]Row{}, squad...)
	sort.SliceStable(sellable, func(one, two int) bool {
		return exitScore(sellable[one]) < exitScore(sellable[two])
	})

	for _, out := range sellable {
		if len(moves) >= 4 {
			break
		}
		// La norma de la liga manda sobre el plan: proponer lo que no se puede hacer es peor
		// que no proponer nada.
		if truthy(out["sale_locked"]) {
			continue
		}
		positionID := int(number(out["position_id"]))
		// Selling a player you are already at the minimum for is only allowed when somebody
		// replaces him in the same move, which is exactly what this is.
		if counts[positionID] <= minPerPosition[positionID] && !forced(out) {
			continue
		}
		sale := number(out["value"])
		how := "vendiendolo a su valor"
		if offer, ok := offerFor[text(out["id"])]; ok {
			if amount := number(offer["offer_amount"]); amount > 0 {
				sale = amount
				how = "aceptando la oferta que ya tienes"
			}
		}

		// Pick by points gained per million spent, not by who scores most: the best player
		// available is usually also the one that eats the whole budget, and three cheap
		// upgrades beat one expensive one when there are eleven places to fill.
		var best Row
		bestScore := 0.0
		for _, in := range candidates {
			if taken[text(in["id"])] || int(number(in["position_id"])) != positionID {
				continue
			}
			// Un rival recien fichado tampoco puede salir: solo se llega a el por clausula.
			if truthy(in["sale_locked"]) {
				continue
			}
			gain := number(in["xpts"]) - number(out["xpts"])
			net := number(in["cost"]) - sale
			// A swap has to buy points. Paying for the same points is a lateral move with a
			// bill attached.
			if gain <= 0.3 || net > free {
				continue
			}
			// A swap that leaves you with more cash than you started is scored on the points
			// alone: dividing by a negative would rank it below everything.
			score := gain
			if net > 0 {
				score = gain / math.Max(net/1e6, 0.25)
				if benchmark > 0 && score < benchmark {
					continue
				}
			} else {
				score += 100
			}
			if score > bestScore {
				best, bestScore = in, score
			}
		}
		if best != nil {
			in := best
			gain := number(in["xpts"]) - number(out["xpts"])
			net := number(in["cost"]) - sale
			taken[text(in["id"])] = true
			free -= net
			moves = append(moves, Row{
				"out": out, "in": in, "sale": sale, "sale_note": how,
				"cost": number(in["cost"]), "net": net, "gain": gain,
				"why":      reason(out),
				"position": positionWord[positionID],
				"cash_after": free,
			})
		}
	}

	// What the eleven is worth before and after, so the plan states its own effect.
	before := bestEleven(squad)
	after := before
	for _, move := range moves {
		after += number(move["gain"])
	}

	var warnings []string
	if committed > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"la caja ya descuenta %.2fM comprometidos en pujas vivas", committed/1e6))
	}
	for positionID, minimum := range minPerPosition {
		if counts[positionID] <= minimum {
			warnings = append(warnings, fmt.Sprintf(
				"vas justo de %ss (%d, el minimo es %d): vender uno sin reemplazo deja el once ilegal",
				positionWord[positionID], counts[positionID], minimum))
		}
	}
	return Row{
		"moves": moves, "cash_before": cash - committed, "cash_after": free,
		"committed": committed,
		"xpts_before": before, "xpts_after": after,
		"warnings": warnings,
	}
}

// exitScore ranks who should leave first: unavailable before merely inefficient.
func exitScore(player Row) float64 {
	if forced(player) {
		return -1000
	}
	value := number(player["value"])
	if value == 0 {
		return 0
	}
	return number(player["xpts"]) / (value / 1e6)
}

func forced(player Row) bool {
	return !truthy(player["available"])
}

func reason(player Row) string {
	if forced(player) {
		status := text(player["status"])
		switch status {
		case "suspended", "sanctioned":
			return "sancionado: no puntua"
		case "injured":
			return "lesionado: no puntua"
		default:
			return "no disponible: no puntua"
		}
	}
	value := number(player["value"])
	if value == 0 {
		return "sin valor de mercado"
	}
	return fmt.Sprintf("rinde %.2f pts por millon", number(player["xpts"])/(value/1e6))
}

// bestEleven is the xPts of the best legal eleven, which is what a change actually moves.
func bestEleven(squad []Row) float64 {
	byPosition := map[int][]float64{}
	for _, player := range squad {
		positionID := int(number(player["position_id"]))
		byPosition[positionID] = append(byPosition[positionID], number(player["xpts"]))
	}
	total, used := 0.0, 0
	var rest []float64
	for positionID, minimum := range minPerPosition {
		points := byPosition[positionID]
		sort.Sort(sort.Reverse(sort.Float64Slice(points)))
		for index, value := range points {
			if index < minimum {
				total += value
				used++
			} else {
				rest = append(rest, value)
			}
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(rest)))
	for _, value := range rest {
		if used >= 11 {
			break
		}
		total += value
		used++
	}
	return math.Round(total*100) / 100
}

// pendingBids is money already promised: a bid you placed is cash you cannot spend twice.
func pendingBids(universe Row) float64 {
	total := 0.0
	for _, player := range rowsOf(universe["players"]) {
		listing := mapOf(player["market"])
		if text(listing["my_bid_id"]) != "" {
			total += number(listing["my_bid"])
		}
	}
	return total
}
