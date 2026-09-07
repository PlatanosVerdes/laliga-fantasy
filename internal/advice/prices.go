package advice

// What a rival's player actually costs, which is not what his listing says.
//
// Nobody in this league sells at a loss. An offer under what the owner paid gets refused, and so
// does one under the clause he is protected by — why take 21 million for a player somebody would
// have to pay 23 to prise away. The asking price is only the floor the game puts on the listing,
// so planning with it produced swaps that read as bargains and offers that were rejected on
// arrival.
//
// The price to plan with is therefore the highest of the three, and which one it is belongs on
// the row: "piden 21.17M, pero pagó 24M por él" is a different conversation from "piden 21.17M y
// su cláusula son 23.13M".

// RealCost is what it would take for a rival to say yes, and the reason that number is the one.
// Free agents and my own listings are left alone: there is nobody to refuse.
func RealCost(player Row) (float64, string) {
	listing := mapOf(player["market"])
	asking := number(listing["min_bid"])
	if text(player["owner"]) == "" || truthy(listing["is_mine"]) {
		return asking, ""
	}

	cost, why := asking, ""
	if clause := number(player["clause"]); clause > cost {
		cost, why = clause, "su cláusula"
	}
	if paid := number(player["bought_for"]); paid > cost {
		cost, why = paid, "lo que pagó por él"
	}
	return cost, why
}
