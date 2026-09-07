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

	sell, banked := sellNow(mine, pending)
	return Row{
		"cash":         cash,
		"squad_value":  squadValue,
		"projected_7d": projected,
		// What the wallet would hold tonight if every offer worth taking were taken.
		"cash_if_sold": cash + banked,
		"banked":       banked,
		"sell":         sell,
		"fading":       fading(mine),
		"bargains":     bargains(players, cash, now),
	}
}

// sellNow is one row per offer that pays over the player's market value, worst-kept secret of
// this game: the daily automatic offers are usually under it, and a rival's is usually over.
//
// The points a sale costs travel with it. Selling a player whose match has not kicked off gives
// away this matchday's points along with the player, and nothing on the page said so.
func sellNow(mine []Row, pending map[string]string) ([]Row, float64) {
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

// bargains is everything on sale for less than the game says it is worth, by whichever route
// it can be bought: a rival's listing, the free market, or a buyout clause under his value.
//
// The gap is paper profit the moment it lands — the player is immediately worth more than what
// left the balance — and it is the one number none of the other tables sorts by.
func bargains(players []Row, cash float64, now time.Time) []Row {
	out := []Row{}
	for _, player := range players {
		if truthy(player["is_mine"]) {
			continue
		}
		value := number(player["value"])
		if value <= 0 {
			continue
		}
		add := func(cost float64, route string) {
			if cost <= 0 || cost >= value {
				return
			}
			out = append(out, merge(player, Row{
				"entry_cost": cost, "route": route, "gap": value - cost,
				"affordable": cost <= cash,
			}))
		}

		listing := mapOf(player["market"])
		if truthy(listing["market_id"]) {
			route := "puja libre"
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
				add(number(listing["min_bid"]), route)
			}
		}
		// A clause is only a route while it is unlocked and nobody has shielded him.
		if text(player["owner"]) != "" && !truthy(player["shielded"]) &&
			!truthy(player["clause_locked"]) {
			add(number(player["clause"]), "clausula")
		}
	}
	sort.SliceStable(out, func(one, two int) bool {
		return number(out[one]["gap"]) > number(out[two]["gap"])
	})
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
