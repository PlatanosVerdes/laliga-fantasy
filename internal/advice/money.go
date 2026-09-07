package advice

import (
	"math"
	"sort"
	"time"
)

// The same world read in euros instead of in points.
//
// Points win a matchday; euros are what buy the players who win the next one, and in this game
// they come from three places and no others: selling above value, a player gaining value while
// you hold him, and the daily reward. The rest of the page ranks by score, which is the right
// order for a squad and the wrong one for a wallet — an offer 300k over value and a clause two
// million under value are the same kind of decision, and neither shows up as money anywhere.
//
// FadingFloor is where "losing value" starts being worth a line. Under it the projection is
// noise: every player drifts a few thousand a day.
const FadingFloor = 200_000

// What a signing's clause becomes, measured rather than assumed: over nine transfers in this
// league the clause after a purchase was max(price, market value), never under a million, and
// locked for exactly 336 hours. See docs/clauses.md.
const (
	ClauseFloor = 1_000_000
	ClauseGrace = 14 * 24 * time.Hour
)

// Money is the wallet's view: what selling would bank, what holding is costing, and what is on
// sale for less than it is worth.
func Money(universe Row, cash float64, now time.Time) Row {
	players := rowsOf(universe["players"])
	pending := pendingKickoffs(universe, now)

	var mine []Row
	squadValue, projected := 0.0, 0.0
	for _, player := range players {
		if !truthy(player["is_mine"]) {
			continue
		}
		mine = append(mine, player)
		squadValue += number(player["value"])
		projected += number(player["projected_gain"])
	}

	// What the pitch is worth before anything moves. Selling is never the loss of a player's
	// points, it is the drop to whoever takes his slot, and buying a good one is only a gain if
	// he displaces somebody: that is the difference between a signing and a decoration.
	xiNow, shapeNow, _ := bestEleven(mine)

	sell, banked := sellNow(mine, pending, xiNow)
	return Row{
		"cash":         cash,
		"squad_value":  squadValue,
		"projected_7d": projected,
		"xi_now":       xiNow,
		"shape_now":    shapeNow,
		// What the wallet would hold tonight if every offer worth taking were taken.
		"cash_if_sold": cash + banked,
		"banked":       banked,
		"sell":         sell,
		"fading":       fading(mine),
		"bargains":     bargains(players, mine, cash, xiNow),
	}
}

// sellNow is one row per offer that pays over the player's market value, worst-kept secret of
// this game: the daily automatic offers are usually under it, and a rival's is usually over.
//
// The points a sale costs travel with it. Selling a player whose match has not kicked off gives
// away this matchday's points along with the player, and nothing on the page said so.
func sellNow(mine []Row, pending map[string]string, xiNow float64) ([]Row, float64) {
	out := []Row{}
	banked := 0.0
	for _, player := range mine {
		value := number(player["value"])
		for _, offer := range rowsOf(player["offers"]) {
			amount := number(offer["money"])
			if value <= 0 || amount <= value {
				continue
			}
			row := merge(player, Row{
				"offer_amount": amount,
				"offer_id":     text(offer["id"]),
				"market_id":    mapOf(player["market"])["market_id"],
				"over_value":   amount - value,
				"vs_value":     amount / value,
			})
			// His match is still to be played, so the sale hands over the points too.
			if kickoff, ok := pending[text(player["team_id"])]; ok {
				row["match_pending"] = true
				row["kickoff"] = kickoff
				row["points_at_risk"] = number(player["xpts"])
			}
			after, shape, starters := bestEleven(without(mine, text(player["id"])))
			row["xi_after"], row["xi_drop"] = after, xiNow-after
			row["shape_after"] = shape
			row["starters_after"] = starters
			out = append(out, row)
			banked += amount
		}
	}
	sort.SliceStable(out, func(one, two int) bool {
		return number(out[one]["over_value"]) > number(out[two]["over_value"])
	})
	return out, banked
}

// fading is what holding costs: the players the projection has going down, in euros, worst
// first. A player who is going to shed a million is a sale nobody is asking you to make.
func fading(mine []Row) []Row {
	out := []Row{}
	for _, player := range mine {
		if gain := number(player["projected_gain"]); gain <= -FadingFloor {
			out = append(out, merge(player, Row{"loses": math.Abs(gain)}))
		}
	}
	sort.SliceStable(out, func(one, two int) bool {
		return number(out[one]["loses"]) > number(out[two]["loses"])
	})
	return out
}

// bargains is what signing somebody would really cost and what the pitch would really get.
//
// It started as "everything under its market value", which in this league is almost nothing: no
// rival accepts an offer below what he paid or below the clause protecting the player, so the
// floor is the clause and the paper profit disappears. What is left is the pair of numbers that
// actually decide a signing — the price a yes costs, and how much better the best legal eleven
// gets — so the order is by the second and the first is written beside it.
func bargains(players, mine []Row, cash, xiNow float64) []Row {
	out := []Row{}
	for _, player := range players {
		if truthy(player["is_mine"]) {
			continue
		}
		value := number(player["value"])
		if value <= 0 {
			continue
		}
		add := func(cost float64, route, floor string) {
			// A price over value is normal now; what is never worth a row is a signing that
			// costs money and leaves the eleven exactly as it was.
			if cost <= 0 {
				return
			}
			// What his clause becomes the moment he is yours, measured over nine signings in
			// this league: max(price, value) with a floor of a million, locked for exactly 14
			// days. So a bargain arrives at 1.00x — the cheapest clause there is — and the
			// margin only gets worse as his value grows. The gap is real, the exposure comes
			// with it, and both belong on the same row.
			clause := math.Max(math.Max(cost, value), ClauseFloor)
			with, shape, _ := bestEleven(append(append([]Row{}, mine...), player))
			if with-xiNow <= 0 && cost >= value {
				return
			}
			out = append(out, merge(player, Row{
				"entry_cost": cost, "route": route, "gap": value - cost,
				"cost_floor": floor, "asking": number(mapOf(player["market"])["min_bid"]),
				"affordable": cost <= cash,
				// What he would actually add to the pitch. A striker who does not get into
				// the eleven is 21 million of decoration.
				"xi_with": with, "xi_gain": with - xiNow, "shape_with": shape,
				"clause_after": clause, "margin_after": clause / value,
				"safe_until":   ClauseGrace.Hours(),
			}))
		}

		listing := mapOf(player["market"])
		if truthy(listing["market_id"]) {
			route := "puja libre"
			cost, floor := RealCost(player)
			// A rival's listing is an offer he still has to accept, and the league's hold rule
			// applies to him too: asking for a player he agreed not to sell yet is asking him
			// to break the pact, so it is not an opportunity.
			if text(player["owner"]) != "" {
				route = "oferta al dueño"
				if truthy(player["sale_locked"]) {
					route = ""
				}
			}
			if route != "" {
				add(cost, route, floor)
			}
		}
		// A clause is only a route while it is unlocked and nobody has shielded him.
		if text(player["owner"]) != "" && !truthy(player["shielded"]) &&
			!truthy(player["clause_locked"]) {
			// The one route nobody can refuse, which is what makes it worth its own row even
			// when it costs more than the listing.
			add(number(player["clause"]), "clausula", "")
		}
	}
	// By what it does for the pitch, and the gap only breaks ties: 1.9M of paper profit on a
	// player who never starts is worth less than a point of xPts every matchday.
	sort.SliceStable(out, func(one, two int) bool {
		first, second := number(out[one]["xi_gain"]), number(out[two]["xi_gain"])
		if first != second {
			return first > second
		}
		return number(out[one]["gap"]) > number(out[two]["gap"])
	})
	return out
}

// without is the squad minus one player, by id.
func without(squad []Row, id string) []Row {
	out := make([]Row, 0, len(squad))
	for _, player := range squad {
		if text(player["id"]) != id {
			out = append(out, player)
		}
	}
	return out
}

// pendingKickoffs is, per team, when its match starts, for the teams that have not played yet.
// Read from this matchday's fixtures, so a squad row can say whether selling that player also
// gives away his points.
func pendingKickoffs(universe Row, now time.Time) map[string]string {
	out := map[string]string{}
	for _, fixture := range rowsOf(universe["fixtures"]) {
		kickoff := text(fixture["kickoff"])
		when, err := time.Parse(time.RFC3339, kickoff)
		if err != nil || !when.After(now) {
			continue
		}
		out[text(fixture["local_id"])] = kickoff
		out[text(fixture["visitor_id"])] = kickoff
	}
	return out
}
