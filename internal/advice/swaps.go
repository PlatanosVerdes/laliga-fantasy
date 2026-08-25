package advice

import (
	"fmt"
	"log/slog"
	"math"
	"sort"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/eleven"
)

// Positions in the order the pitch reads, so a plan built from a map still comes out the same
// twice. What a legal eleven asks for lives in the eleven package.
var positionOrder = []int{eleven.Keeper, eleven.Defender, eleven.Midfielder, eleven.Striker}

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

	// What each of mine would fetch: an offer already on the table beats an estimate, and the
	// best one of several is the one that matters. Keyed by player, and the offers bucket now
	// carries one row per offer, so the best has to be picked rather than assumed.
	offerFor := map[string]Row{}
	for _, offer := range rowsOf(buckets["offers"]) {
		id := text(offer["id"])
		if previous, seen := offerFor[id]; !seen ||
			number(offer["offer_amount"]) > number(previous["offer_amount"]) {
			offerFor[id] = offer
		}
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
	var warnings []string
	// Money already promised is money you do not have: a bid you placed can land tomorrow.
	committed := pendingBids(universe)
	free := cash - committed

	// What you can field right now, and under which formation. Computed before anything moves,
	// because it is what every gain below is added to.
	before, shape, starters := bestEleven(squad)

	// A hole no swap can close. Every move below trades one of yours for somebody else's, so a
	// squad that cannot field eleven -- ten players, or eleven that no formation lines up --
	// had the plan proposing upgrades while the pitch was a man short. Signings come first: a
	// slot nobody fills scores nothing, which is worse than any upgrade is good.
	//
	// Several positions usually close the same hole, so the pick is whoever is actually for
	// sale and cheapest per point, not whichever formation happens to be listed first.
	for attempt := 0; attempt < 4 && !eleven.Any(counts); attempt++ {
		short := eleven.Missing(counts)
		var pick Row
		pickPosition, pickScore := 0, 0.0
		for _, positionID := range positionOrder {
			if short[positionID] == 0 {
				continue
			}
			in, score := fillFor(candidates, positionID, free, taken)
			if in != nil && score > pickScore {
				pick, pickPosition, pickScore = in, positionID, score
			}
		}
		if pick == nil {
			warnings = append(warnings, fmt.Sprintf(
				"no puedes alinear once y ninguno de los que hay a tiro entra en los %.2fM "+
					"que te quedan", free/1e6))
			break
		}
		cost := number(pick["cost"])
		taken[text(pick["id"])] = true
		free -= cost
		counts[pickPosition]++
		moves = append(moves, Row{
			"in": pick, "cost": cost, "net": cost, "gain": number(pick["xpts"]),
			"position": positionWord[pickPosition],
			"why": fmt.Sprintf("hueco en el once: con un %s mas ya hay formacion que "+
				"cubra los once", positionWord[pickPosition]),
			"cash_after": free,
		})
	}

	// Sell side, worst first: an unavailable player is dead weight however much he cost.
	sellable := append([]Row{}, squad...)
	sort.SliceStable(sellable, func(one, two int) bool {
		return exitScore(sellable[one], offerFor) < exitScore(sellable[two], offerFor)
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
		// Being at the minimum in his position does not forbid the swap: one out and one in of
		// the same position leaves the count where it was. What it forbids is doing it in the
		// wrong order, so the move says so instead of being dropped. This guard used to skip
		// them outright, which hid every replacement for the positions where you most need
		// one -- exactly the case worth being told about.
		tight := eleven.Room(counts, positionID) == 0
		sale := number(out["value"])
		how := "vendiendolo a su valor"
		if offer, ok := offerFor[text(out["id"])]; ok {
			if amount := number(offer["offer_amount"]); amount > 0 {
				sale = amount
				who := text(offer["offer_from"])
				if who == "" {
					who = "el mercado"
				}
				how = fmt.Sprintf("aceptando los %.2fM que ofrece %s", amount/1e6, who)
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
		if len(candidates) > 0 && slog.Default().Enabled(nil, slog.LevelDebug) {
			for _, in := range candidates {
				if int(number(in["position_id"])) != positionID {
					continue
				}
				slog.Debug("swap candidate", "out", text(out["name"]), "in", text(in["name"]),
					"cost", int64(number(in["cost"])), "ceiling", int64(number(in["ideal_bid"])),
					"xpts", number(in["xpts"]))
			}
		}
		if best != nil {
			in := best
			gain := number(in["xpts"]) - number(out["xpts"])
			net := number(in["cost"]) - sale
			taken[text(in["id"])] = true
			free -= net
			order := ""
			if tight {
				order = "ficha primero y vende despues: sin el no te queda once que alinear"
			}
			moves = append(moves, Row{
				"out": out, "in": in, "sale": sale, "sale_note": how,
				"cost": number(in["cost"]), "net": net, "gain": gain,
				"why":      reason(out),
				"position": positionWord[positionID],
				"order":    order,
				"cash_after": free,
			})
		}
	}

	// Every gain lands on the eleven measured before anything moved, and a signing fills a slot
	// that was scoring nothing, so its whole xPts is the gain.
	after := before
	for _, move := range moves {
		after += number(move["gain"])
	}

	if committed > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"la caja ya descuenta %.2fM comprometidos en pujas vivas", committed/1e6))
	}
	if starters < 11 {
		warnings = append(warnings, fmt.Sprintf(
			"ahora mismo solo puedes alinear a %d: ninguna formacion cubre el once con esta "+
				"plantilla", starters))
	} else {
		// Nobody spare anywhere reads as one sentence; a single tight position reads as its own.
		spare := 0
		for _, positionID := range positionOrder {
			spare += eleven.Room(counts, positionID)
		}
		if spare == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"tienes el once justo (%d jugadores, solo cuadra el %s): cualquier venta sin "+
					"fichar antes lo deja sin cubrir", len(squad), shape))
		} else {
			for _, positionID := range positionOrder {
				if counts[positionID] > 0 && eleven.Room(counts, positionID) == 0 {
					warnings = append(warnings, fmt.Sprintf(
						"no te sobra ningun %s: vender uno sin fichar antes deja el once sin "+
							"cubrir", positionWord[positionID]))
				}
			}
		}
	}
	slog.Info("plan ready", "moves", len(moves), "xpts_before", before,
		"xpts_after", math.Round(after*100)/100, "shape", shape, "starters", starters,
		"cash_before", int64(cash-committed), "cash_after", int64(free),
		"committed", int64(committed))

	return Row{
		"moves": moves, "cash_before": cash - committed, "cash_after": free,
		"committed": committed,
		"xpts_before": before, "xpts_after": after,
		"shape": shape, "starters": starters,
		"warnings": warnings,
	}
}

// fillFor is who to sign for a slot nobody fills: the most points per million among the ones
// you can actually pay for. The squad's own rate does not apply here -- an empty slot scores
// nothing at all, so any legal signing beats it, and refusing one for being inefficient is how
// the plan ends up recommending upgrades to a team of ten.
func fillFor(candidates []Row, positionID int, budget float64,
	taken map[string]bool) (Row, float64) {
	var best Row
	bestScore := 0.0
	for _, in := range candidates {
		if taken[text(in["id"])] || int(number(in["position_id"])) != positionID {
			continue
		}
		if truthy(in["sale_locked"]) {
			continue
		}
		cost := number(in["cost"])
		if cost > budget {
			continue
		}
		score := number(in["xpts"]) / math.Max(cost/1e6, 0.25)
		if score > bestScore {
			best, bestScore = in, score
		}
	}
	return best, bestScore
}

// exitScore ranks who should leave first: whoever cannot score, then whoever somebody is
// overpaying for, then the merely inefficient.
//
// The middle tier is the one that was missing. Being paid above market value is a reason to
// sell that has nothing to do with the player being bad, and the plan only looked at
// efficiency, so a good offer for a decent player never became a move.
func exitScore(player Row, offers map[string]Row) float64 {
	if forced(player) {
		return -1000
	}
	if offer, ok := offers[text(player["id"])]; ok {
		if over := number(offer["vs_value"]); over > 1 {
			// Ordered among themselves by how much over value they are paying.
			return -500 + (1 / over)
		}
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

// bestEleven is the best eleven you can actually field: the points, the formation it needs and
// how many of the eleven slots it fills.
//
// It used to be "the best eleven xPts" measured as the top eleven scorers with a floor per
// position, which assumes any eleven can line up together. They cannot: with four defenders and
// six midfielders there is no free formation to put them in, and the number quietly claimed an
// eleven that never took the field. Shapes are consulted now, and when none fits it reports the
// most players a shape can seat, which is what starters is for.
func bestEleven(squad []Row) (float64, string, int) {
	byPosition := map[int][]float64{}
	counts := map[int]int{}
	for _, player := range squad {
		positionID := int(number(player["position_id"]))
		byPosition[positionID] = append(byPosition[positionID], number(player["xpts"]))
		counts[positionID]++
	}
	for _, points := range byPosition {
		sort.Sort(sort.Reverse(sort.Float64Slice(points)))
	}

	best, shape, filled := 0.0, "", 0
	for _, option := range eleven.Shapes {
		total, used := 0.0, 0
		for _, positionID := range positionOrder {
			points := byPosition[positionID]
			for index := 0; index < option.Need[positionID] && index < len(points); index++ {
				total += points[index]
				used++
			}
		}
		// The shape that seats the most players wins, and points break the tie: a formation
		// that fields eleven is better than a richer one that fields ten, always.
		if used > filled || (used == filled && total > best) {
			best, shape, filled = total, option.Name, used
		}
	}
	if filled < 11 {
		// No shape fits, so naming one would read as a plan. The count is the honest part.
		shape = ""
	}
	return math.Round(best*100) / 100, shape, filled
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
