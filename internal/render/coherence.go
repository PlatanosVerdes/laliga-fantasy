package render

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/advice"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
)

// How the owner plays, as rules: let them pay when they pay well, and do not spend protecting
// what is already a good sale.

// SellerFloor is the least a player the advice already wants out can go for.
const SellerFloor = 0.98

// ClauseIsASale is the clause, in multiples of value, past which being taken is a good sale.
const ClauseIsASale = 1.25

// RaiseHorizon is how far ahead a raise has to be decided to earn a card in Decidir.
const RaiseHorizon = 48 * time.Hour

// StarterSaleSlack is how many xPts the best eleven may lose when one of its players is sold.
const StarterSaleSlack = 1.0

// sellRows are the advice's sell candidates that are really for sale: one of the best eleven
// only when he cannot play or the eleven barely notices him gone.
func (d Document) sellRows() []map[string]any {
	keep := d.keepers()
	var out []map[string]any
	for _, player := range rows(d.Advice["sells"]) {
		if len(asStrings(player["reasons"])) == 0 || (keep[text(player["id"])] && truthy(player["available"])) {
			continue
		}
		out = append(out, player)
	}
	return out
}

// keepers are the players of my best eleven who can play and whose sale would cost it more
// than StarterSaleSlack: they are not for sale, whatever is offered.
func (d Document) keepers() map[string]bool {
	squad := rows(d.Advice["squad"])
	choice, xiNow := bestElevenOf(squad)
	byID := map[string]map[string]any{}
	for _, player := range squad {
		byID[text(player["id"])] = player
	}
	out := map[string]bool{}
	for _, id := range choice.IDs() {
		if truthy(byID[id]["available"]) && xiNow-elevenWithout(squad, id) > StarterSaleSlack {
			out[id] = true
		}
	}
	return out
}

// sellCandidates are the ids of sellRows.
func (d Document) sellCandidates() map[string]bool {
	out := map[string]bool{}
	for _, player := range d.sellRows() {
		out[text(player["id"])] = true
	}
	return out
}

// offerPays is whether an offer at this multiple of his value is worth taking.
func (d Document) offerPays(playerID string, ratio float64) bool {
	return ratio >= policies.GoodOverValue || (d.sellCandidates()[playerID] && ratio >= SellerFloor)
}

// offerAdvice is whether to take an offer at this multiple of his value, and why, in words.
// planned is the swap plan or a replacement counting on the sale.
func (d Document) offerAdvice(playerID string, ratio float64, planned bool) (bool, string) {
	words := ratioNote(ratio)
	squad := rows(d.Advice["squad"])
	_, xiNow := bestElevenOf(squad)
	drop := xiNow - elevenWithout(squad, playerID)
	switch {
	case planned:
		return true, words + ": lo pide el plan"
	case ratio >= policies.GoodOverValue && !d.keepers()[playerID]:
		return true, words
	case ratio >= SellerFloor && d.sellCandidates()[playerID]:
		return true, words + " y el consejo quiere venderlo"
	case ratio >= policies.GoodOverValue:
		return false, fmt.Sprintf("%s pero tu once pierde %s xPts", words, esNum(drop, 1))
	}
	return false, words + ": no compensa"
}

// keyPlayers are the ones worth defending at any clause: my three best outfield players by
// xPts and the keeper of my best eleven.
func (d Document) keyPlayers() map[string]bool {
	squad := rows(d.Advice["squad"])
	choice, _ := bestElevenOf(squad)
	out := map[string]bool{choice.Keeper: true}
	outfield := []map[string]any{}
	for _, player := range squad {
		if int(number(player["position_id"])) != 1 {
			outfield = append(outfield, player)
		}
	}
	sort.SliceStable(outfield, func(one, two int) bool {
		return number(outfield[one]["xpts"]) > number(outfield[two]["xpts"])
	})
	for index, player := range outfield {
		if index == 3 {
			break
		}
		out[text(player["id"])] = true
	}
	return out
}

// raiseWanted is false when his clause already makes losing him a good sale.
func (d Document) raiseWanted(row map[string]any) bool {
	value := number(row["value"])
	if value <= 0 || number(row["clause"])/value < ClauseIsASale {
		return true
	}
	return d.keyPlayers()[text(row["id"])]
}

func ratioNote(ratio float64) string {
	words, _ := ratioWords(ratio)
	return words
}

func ratioClassOf(ratio float64) string {
	_, class := ratioWords(ratio)
	return class
}

// ratioWords is an offer as a multiple of value, and the class it is coloured with.
func ratioWords(ratio float64) (string, string) {
	class := ""
	switch {
	case ratio >= policies.GoodOverValue:
		class = "up"
	case ratio < 1:
		class = "down"
	}
	return "×" + strings.Replace(fmt.Sprintf("%.2f", ratio), ".", ",", 1) + " su valor", class
}

// --- the crack -------------------------------------------------------------------------

// crackPrice is what he costs today, or why he cannot be had and until when.
func crackPrice(player map[string]any) (float64, string) {
	listing := mapOf(player["market"])
	if text(listing["market_id"]) != "" {
		price := number(listing["min_bid"])
		if text(player["owner"]) != "" {
			price = maxFloat(price, number(player["clause"]))
		}
		return price, "en venta"
	}
	owner := text(player["owner"])
	switch {
	case owner == "":
		return 0, "no está en el mercado"
	case truthy(player["shielded"]):
		return 0, "blindado hasta " + esWhen(text(player["shielded_until"]))
	case truthy(player["clause_locked"]):
		return 0, "cláusula libre el " + esWhen(text(player["clause_locked_until"]))
	}
	return number(player["clause"]), "cláusula"
}

func maxFloat(one, two float64) float64 {
	if one > two {
		return one
	}
	return two
}

// crackReach is what can be raised today, and from whom.
func (d Document) crackReach() (float64, []string) {
	// What can really be raised today: the cash, the offers on the table that pay, and the
	// bench players the hold rule already lets go.
	reach := number(d.Advice["budget"])
	sold := []string{}
	amounts := map[string]float64{}
	names := map[string]string{}
	keep := d.keepers()
	for _, offer := range rows(d.Advice["offers"]) {
		id := text(offer["id"])
		if number(offer["vs_value"]) >= policies.GoodOverValue && !keep[id] {
			amounts[id] = maxFloat(amounts[id], number(offer["offer_amount"]))
			names[id] = text(offer["name"])
		}
	}
	for _, player := range d.benchOf() {
		id := text(player["id"])
		if truthy(player["sale_locked"]) {
			continue
		}
		amounts[id] = maxFloat(amounts[id], number(player["value"]))
		names[id] = text(player["name"])
	}
	ids := make([]string, 0, len(amounts))
	for id := range amounts {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(one, two int) bool { return amounts[ids[one]] > amounts[ids[two]] })
	for _, id := range ids {
		reach += amounts[id]
		sold = append(sold, names[id])
	}

	return reach, sold
}

// --- selling a starter by taking somebody else's ---------------------------------------

// saleSwaps are, for each offer that pays but costs the eleven too much, the rival's player
// whose clause the sale pays for and who leaves the eleven at least where it is.
func (d Document) saleSwaps() map[string]advice.Clausulazo {
	window, _ := d.clauseWindow()
	keep := d.keepers()
	players := rows(d.Universe["players"])
	blocked := []map[string]any{}
	for _, offer := range rows(d.Advice["offers"]) {
		id := text(offer["id"])
		if number(offer["vs_value"]) >= policies.GoodOverValue && keep[id] {
			blocked = append(blocked, offer)
		}
	}
	// One rival's player can be taken once: the sale that gains most with him gets him, and
	// the rest look for somebody else.
	out := map[string]advice.Clausulazo{}
	taken := map[string]bool{}
	for len(out) < len(blocked) {
		bestID, best := "", advice.Clausulazo{}
		for _, offer := range blocked {
			id := text(offer["id"])
			if _, done := out[id]; done {
				continue
			}
			move, ok := advice.SwapForSale(players, id, number(offer["offer_amount"]),
				number(d.Advice["budget"]), window, time.Now(), taken, advice.RivalBankOf(d.Universe))
			if ok && (bestID == "" || move.Gain > best.Gain) {
				bestID, best = id, move
			}
		}
		if bestID == "" {
			break
		}
		out[bestID] = best
		taken[text(best.In["id"])] = true
	}
	return out
}

// clauseButton pays his clause now, or schedules the clausulazo for when it opens.
func clauseButton(player map[string]any, cost float64, opens, class string) Act {
	id, name := text(player["id"]), text(player["name"])
	if opens == "" {
		return Act{Label: "Pagar cláusula " + esMoney(cost), Class: "op " + class, Do: "op",
			Args: map[string]any{"op": "pay_clause", "player_id": id, "name": name,
				"amount": int64(cost)}}
	}
	return Act{Label: "Programar clausulazo", Class: "raid-btn " + class, Do: "raid",
		Args: map[string]any{"id": id, "name": name, "max": int64(cost),
			"clause": int64(number(player["clause"]))}}
}

// swapCards are the sales the eleven only survives with a clausulazo: both legs on one card.
func (d Document) saleSwapCards() []Card {
	swaps := d.saleSwaps()
	var out []Card
	for _, offer := range rows(d.Advice["offers"]) {
		id := text(offer["id"])
		move, ok := swaps[id]
		if !ok || text(offer["offer_id"]) == "" {
			continue
		}
		amount, value := number(offer["offer_amount"]), number(offer["value"])
		in := move.In
		owner := text(in["owner"])
		order := "Acepta primero: el dinero de la venta paga la cláusula."
		if number(d.Advice["budget"]) >= move.Cost {
			order = "Paga primero la cláusula y luego acepta: así no te quedas sin él."
		}
		if move.Opens != "" {
			order = "Acepta la oferta antes de que caduque y programa el clausulazo para cuando se abra."
		}
		accept := acceptAct(offer, amount, "op op-primary dcard-go")
		out = append(out, Card{
			Kind: "swap", Key: "own:" + id, Player: offer, In: in,
			Verb: "Vende y clausula", Big: esSigned(move.Gain) + " xPts",
			BigNote:  "te quedan " + esMoney(move.CashAfter),
			Deadline: text(offer["offer_expires"]), DeadlineLabel: "caduca la oferta",
			Deadline2: move.Opens, DeadlineLabel2: "se abre su cláusula",
			Why: []string{
				fmt.Sprintf("Vende %s %s (%s); paga la cláusula de %s (de %s) %s.",
					text(offer["name"]), esMoney(amount), ratioNote(amount/value), text(in["name"]),
					owner, esMoney(move.Cost)),
				order,
			},
			Impact: fmt.Sprintf("tu once %s xPts · te quedan %s", esSigned(move.Gain),
				esMoney(move.CashAfter)),
			Tone:    "accent",
			Buttons: []Act{accept, clauseButton(in, move.Cost, move.Opens, "dcard-go dcard-second")},
			Weight:  move.Gain, Cash: amount - move.Cost,
		})
	}
	return out
}

// upgradeCard is the clausulazo cash alone pays and the plan's bar per million approves.
func (d Document) upgradeCards(covered map[string]bool) []Card {
	window, _ := d.clauseWindow()
	move, ok := advice.ClauseUpgrade(rows(d.Universe["players"]), number(d.Advice["budget"]),
		number(d.Advice["squad_ppm_benchmark"]), window, time.Now(), advice.RivalBankOf(d.Universe))
	if !ok || covered["in:"+text(move.In["id"])] {
		return nil
	}
	in := move.In
	deadline, label := window.ClosesAt, "cláusulas abiertas"
	if move.Opens != "" {
		deadline, label = move.Opens, "se abre su cláusula"
	}
	return []Card{{
		Kind: "upgrade", Key: "in:" + text(in["id"]), Player: in,
		Verb: "Clausula a " + text(in["name"]), Big: esSigned(move.Gain) + " xPts",
		BigNote: "por jornada en tu once", Deadline: deadline, DeadlineLabel: label,
		Why: []string{fmt.Sprintf("Su cláusula (de %s) cuesta %s y nadie puede negarse.",
			text(in["owner"]), esMoney(move.Cost))},
		Impact: "te quedan " + esMoney(move.CashAfter), Tone: "accent",
		Buttons: []Act{clauseButton(in, move.Cost, move.Opens, "dcard-go")},
		Weight:  move.Gain, Cash: -move.Cost,
	}}
}
