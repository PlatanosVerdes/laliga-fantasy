package advice

import (
	"math"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
)

// ClauseHorizon is how far ahead a clause opening still counts as "now" for a move.
const ClauseHorizon = 48 * time.Hour

// Clausulazo is a rival's player taken by his clause, alone or paid with one of mine sold.
type Clausulazo struct {
	In        Row
	Cost      float64
	Gain      float64
	CashAfter float64
	// Opens is when the clause can be paid, empty when it can be paid now.
	Opens string
}

// clauseOpens is when his clause can be paid, "" for now, and false when not within the horizon:
// his own lock and the league's window both have to be open.
func clauseOpens(player Row, window schedule.Window, now time.Time) (string, bool) {
	if text(player["owner"]) == "" || truthy(player["is_mine"]) || truthy(player["shielded"]) ||
		number(player["clause"]) <= 0 {
		return "", false
	}
	var opens time.Time
	if truthy(player["clause_locked"]) {
		until, err := time.Parse(time.RFC3339, text(player["clause_locked_until"]))
		if err != nil {
			return "", false
		}
		opens = until
	}
	if !window.Open {
		at, err := time.Parse(time.RFC3339, window.OpensAt)
		if err != nil {
			return "", false
		}
		if at.After(opens) {
			opens = at
		}
	}
	if opens.IsZero() || !opens.After(now) {
		return "", true
	}
	if opens.After(now.Add(ClauseHorizon)) {
		return "", false
	}
	return opens.Format(time.RFC3339), true
}

// bestClausulazo is the rival's player whose clause, paid with budget, leaves the best eleven of
// squad at least at floor, best by xPts gained per million spent.
func bestClausulazo(players, squad []Row, budget, spent, floor, base float64, window schedule.Window,
	now time.Time, exclude map[string]bool) (Clausulazo, bool) {
	var best Clausulazo
	score := math.Inf(-1)
	for _, player := range players {
		opens, ok := clauseOpens(player, window, now)
		clause := number(player["clause"])
		if !ok || clause > budget || exclude[text(player["id"])] {
			continue
		}
		with, _, _ := bestEleven(append(append([]Row{}, squad...), player))
		if with < floor {
			continue
		}
		gain := with - base
		net := (clause - spent) / 1e6
		perMillion := gain / math.Max(net, 0.1)
		if perMillion > score || (perMillion == score && gain > best.Gain) {
			score = perMillion
			best = Clausulazo{In: player, Cost: clause, Gain: gain, CashAfter: budget - clause,
				Opens: opens}
		}
	}
	return best, score > math.Inf(-1)
}

// SwapForSale is the clausulazo that makes selling one of mine at an offer harmless: paid with
// the cash plus the sale, the best eleven ends at least where it is today.
// exclude are rivals' players already taken by another move.
func SwapForSale(players []Row, sellID string, amount, cash float64, window schedule.Window,
	now time.Time, exclude map[string]bool) (Clausulazo, bool) {
	var mine, without []Row
	for _, player := range players {
		if !truthy(player["is_mine"]) {
			continue
		}
		mine = append(mine, player)
		if text(player["id"]) != sellID {
			without = append(without, player)
		}
	}
	if len(mine) == len(without) {
		return Clausulazo{}, false
	}
	xiNow, _, _ := bestEleven(mine)
	return bestClausulazo(players, without, cash+amount, amount, xiNow, xiNow, window, now, exclude)
}

// ClauseUpgrade is the clausulazo cash alone pays that adds most per million, when it clears the
// bar the swap plan holds every purchase to.
func ClauseUpgrade(players []Row, cash, bar float64, window schedule.Window,
	now time.Time) (Clausulazo, bool) {
	var mine []Row
	for _, player := range players {
		if truthy(player["is_mine"]) {
			mine = append(mine, player)
		}
	}
	xiNow, _, _ := bestEleven(mine)
	move, ok := bestClausulazo(players, mine, cash, 0, xiNow+0.05, xiNow, window, now, nil)
	if !ok || move.Gain/math.Max(move.Cost/1e6, 0.1) < bar {
		return Clausulazo{}, false
	}
	return move, true
}
