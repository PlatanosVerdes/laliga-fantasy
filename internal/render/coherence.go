package render

import (
	"fmt"
	"sort"
	"strings"
	"time"

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

// crackBox is the goal: the three best players I do not have, what each costs today, how far
// my cash and my sales reach, and what the cheapest would add to my eleven.
func (d Document) crackBox() string {
	squad := rows(d.Advice["squad"])
	if len(squad) == 0 {
		return ""
	}
	var others []map[string]any
	for _, player := range rows(d.Universe["players"]) {
		if !truthy(player["is_mine"]) && truthy(player["available"]) {
			others = append(others, player)
		}
	}
	sort.SliceStable(others, func(one, two int) bool {
		return number(others[one]["xpts"]) > number(others[two]["xpts"])
	})
	// The ones who would actually improve my eleven: a keeper below mine is no crack.
	_, base := bestElevenOf(squad)
	gains := map[string]float64{}
	var picked []map[string]any
	for _, player := range others {
		if len(picked) == 3 {
			break
		}
		_, with := bestElevenOf(append(append([]map[string]any{}, squad...), player))
		if with-base > 0.05 {
			gains[text(player["id"])] = with - base
			picked = append(picked, player)
		}
	}
	others = picked
	if len(others) == 0 {
		return ""
	}

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

	var items []string
	var cheapest map[string]any
	cheapestPrice := 0.0
	for _, player := range others {
		price, how := crackPrice(player)
		owner := text(player["owner"])
		if owner == "" {
			owner = "libre"
		}
		value := "no se puede"
		if price > 0 {
			value = esMoney(price)
			if cheapest == nil || price < cheapestPrice {
				cheapest, cheapestPrice = player, price
			}
		}
		items = append(items, row(player, Esc(owner)+" · "+Esc(how),
			esNum(number(player["xpts"]), 1)+" xPts", value, "", "", ""))
	}

	line := "Con tu caja llegas a " + esMoney(reach)
	if len(sold) > 0 {
		names := sold
		if len(names) > 4 {
			names = append(names[:4:4], fmt.Sprintf("%d más", len(sold)-4))
		}
		line = fmt.Sprintf("Con tu caja y vendiendo %s llegas a %s", strings.Join(names, ", "),
			esMoney(reach))
	}
	if cheapest != nil {
		gap := cheapestPrice - reach
		if gap > 0 {
			line += fmt.Sprintf("; a %s te faltan %s", text(cheapest["name"]), esMoney(gap))
		} else {
			line += fmt.Sprintf("; %s te llega", text(cheapest["name"]))
		}
		line += fmt.Sprintf(" (%s xPts a tu once).", esSigned(gains[text(cheapest["id"])]))
	} else {
		line += "; hoy ninguno se puede fichar."
	}
	return block("Objetivo: un crack", rowList(items, true)+`<p class="mk-note">`+Esc(line)+`</p>`,
		"", -1)
}
