package advice

import (
	"fmt"
	"math"
	"sort"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/eleven"
)

// Defending your own squad, which is the half of the clause nobody computes.
//
// A clause is not a price the game sets and forgets: it is the standing offer every rival can
// accept the second the window opens, and raising it is the only defence. But raising costs
// money — you pay an amount and the clause goes up by twice it — so the question is never
// "who could pay this" but "who gains by paying it, and what does keeping him out cost me".
//
// Both halves are computable from what the page already knows: the rivals' reconstructed cash,
// what each of their squads already returns per million, and what losing the player would do to
// the best eleven I can field.

// RaiseHeadroom is the sliver added over the clause that makes a rival indifferent, so the
// recommendation lands *past* the bar rather than exactly on it.
const RaiseHeadroom = 1.02

// DropWorthDefending is where losing a player stops being a shrug. Under one expected point of
// damage to the eleven, spending money to keep him is worse than letting him go and pocketing
// the clause.
const DropWorthDefending = 1.0

// ClausePlan is one row per player of mine whose clause somebody could pay, with what raising
// it would cost, what it would buy, and whether he is worth defending at all.
func ClausePlan(universe Row, cash float64) Row {
	players := rowsOf(universe["players"])
	var mine []Row
	for _, player := range players {
		if truthy(player["is_mine"]) {
			mine = append(mine, player)
		}
	}
	if len(mine) == 0 {
		return nil
	}

	teams := RivalCash(universe, cash)
	rates := SquadRates(players)
	shortOf := positionGaps(players)
	xiNow, _, _ := bestEleven(mine)

	rows := []Row{}
	for _, player := range mine {
		clause, value := number(player["clause"]), number(player["value"])
		if clause == 0 || value == 0 {
			continue
		}
		threats, margin := ClauseThreats(player, teams, rates)
		after, _, _ := bestEleven(without(mine, text(player["id"])))
		drop := xiNow - after

		row := merge(player, Row{
			"clause_margin": margin,
			"threats":       len(threats),
			"tempted":       Tempted(threats),
			"top_threat":    TopThreat(threats),
			"xi_drop":       drop,
			"risk":          RaidRisk(player, threats, shortOf),
		})
		// Nobody can pay it: there is nothing to defend against and no money to spend.
		if len(threats) == 0 {
			row["verdict"], row["why"] = "tranquilo", "nadie tiene caja para pagarla"
			rows = append(rows, row)
			continue
		}

		target := RaiseTarget(player, threats)
		pay := math.Ceil((target - clause) / 2)
		if pay < 0 {
			pay = 0
		}
		row["target_clause"], row["pay"] = target, pay
		row["new_margin"] = target / value
		row["affordable"] = pay <= cash
		if drop > 0 {
			row["per_point"] = pay / drop
		}

		switch {
		case pay == 0:
			row["verdict"], row["why"] = "tranquilo", "su clausula ya deja a todos sin premio"
		case drop < DropWorthDefending:
			row["verdict"] = "dejalo ir"
			row["why"] = fmt.Sprintf("perderlo te cuesta %.2f xPts: sale mas barato "+
				"quedarte la clausula", drop)
		case pay > cash:
			row["verdict"] = "no te llega"
			row["why"] = "subirla cuesta " + short(pay) + " y tienes " + short(cash)
		default:
			row["verdict"] = "sube"
			row["why"] = fmt.Sprintf("por %s deja de rentarle a %d rival(es)",
				short(pay), Tempted(threats))
		}
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(one, two int) bool {
		return number(rows[one]["risk"]) > number(rows[two]["risk"])
	})

	// The plan the balance can actually pay for, worst risk first: five recommendations that
	// add up to more than the cash are not a plan, they are a wish.
	left, total := cash, 0.0
	for _, row := range rows {
		if text(row["verdict"]) != "sube" {
			continue
		}
		pay := number(row["pay"])
		if pay > left {
			row["verdict"], row["why"] = "no te llega", "se te acaba la caja antes de llegar aqui"
			continue
		}
		row["in_plan"] = true
		left -= pay
		total += pay
	}
	return Row{"rows": rows, "spend": total, "cash_left": left, "xi_now": xiNow}
}

// RaidRisk estimates the chance somebody pays this clause once the window opens.
//
// It is a model and not a measured frequency, and the parts are on the row so it can be argued
// with: who can pay it at all, how much better than his own squad the player would be at that
// price, and whether that rival is actually short in the position. A rival who gains nothing is
// not zero — values move and people buy with their eyes — but he is a fifth of one who does.
func RaidRisk(player Row, threats []Threat, shortOf map[string]map[int]int) float64 {
	position := int(number(player["position_id"]))
	survives := 1.0
	for _, threat := range threats {
		chance := 0.05
		if threat.Worth && threat.Bar > 0 {
			edge := threat.PPM/threat.Bar - 1
			chance = 0.20 + 0.55*math.Min(1, edge/0.5)
		}
		if shortOf[threat.TeamID][position] > 0 {
			chance += 0.10
		}
		survives *= 1 - math.Min(0.85, chance)
	}
	return math.Round((1-survives)*100) / 100
}

// RaiseTarget is the clause that makes the raid pointless for everybody who could pay it:
// above the price at which each tempted rival would be beating his own squad's rate.
//
// When nobody is tempted, the clause is already doing its job and the target is the clause
// itself. When somebody is tempted but no price puts him off — his squad returns so little that
// anything beats it — the target is out of his reach instead: what he cannot pay he cannot pay.
func RaiseTarget(player Row, threats []Threat) float64 {
	clause, xpts := number(player["clause"]), number(player["xpts"])
	target := clause
	reach := 0.0
	for _, threat := range threats {
		reach = math.Max(reach, threat.Cash)
		if !threat.Worth || threat.Bar <= 0 || xpts <= 0 {
			continue
		}
		// The clause at which his points per million drop to his own squad's rate.
		indifferent := xpts / threat.Bar * 1e6 * RaiseHeadroom
		target = math.Max(target, indifferent)
	}
	// Never recommend more than putting him out of everybody's reach: past that, the extra
	// euros buy nothing at all.
	if reach > 0 {
		target = math.Min(target, reach*RaiseHeadroom)
	}
	return math.Ceil(target)
}

// positionGaps is, per team in the league, how many players each position is short of what the
// thinnest legal formation asks for. A rival missing a defender is a rival with a reason.
func positionGaps(players []Row) map[string]map[int]int {
	counts := map[string]map[int]int{}
	for _, player := range players {
		team := text(player["owner_team_id"])
		if team == "" {
			continue
		}
		if counts[team] == nil {
			counts[team] = map[int]int{}
		}
		counts[team][int(number(player["position_id"]))]++
	}
	out := make(map[string]map[int]int, len(counts))
	for team, byPosition := range counts {
		out[team] = eleven.Missing(byPosition)
	}
	return out
}
