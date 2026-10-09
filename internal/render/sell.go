package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// --- Vender ----------------------------------------------------------------------------

func (d Document) sellView() SectionView { return shell("v-vender", "vender", "vender") }

// SellData is Vender: the offers received with what to do about each, what is on sale, the
// rest of the squad and the standing listings.
func (d Document) SellData() View {
	return View{Main: []Block{d.offersBlock(), d.restBlock()},
		Aside: []Block{d.listedBlock(), d.alwaysBlock()}}
}

func (d Document) offersBlock() Block {
	offers := rows(d.Advice["offers"])
	sort.SliceStable(offers, func(one, two int) bool {
		return number(offers[one]["vs_value"]) > number(offers[two]["vs_value"])
	})
	// The plan's sales: an offer it counts on is accepted to sign somebody else.
	swaps := d.saleSwaps()
	plannedFor := map[string]string{}
	for _, move := range rows(d.Swaps["moves"]) {
		if out, in := mapOf(move["out"]), mapOf(move["in"]); out != nil && in != nil {
			plannedFor[text(out["id"])] = text(in["name"])
		}
	}
	sales := map[string]map[string]any{}
	for _, sale := range rows(d.Money["sell"]) {
		sales[text(sale["offer_id"])] = sale
	}
	squad := rows(d.Advice["squad"])
	_, xiNow := bestElevenOf(squad)
	lines := []Row{}
	for _, offer := range offers {
		amount, value := number(offer["offer_amount"]), number(offer["value"])
		ratio := number(offer["vs_value"])
		args := map[string]any{"market_id": text(offer["market_id"]),
			"offer_id": text(offer["offer_id"]), "player_id": text(offer["id"]),
			"name": text(offer["name"]), "amount": int64(amount)}
		// What the offer means beyond its price goes in the figure's tooltip, not in the row.
		var facts []string
		if who := text(offer["offer_from"]); who != "" && !truthy(offer["offer_from_market"]) {
			facts = append(facts, "oferta de "+who)
		}
		planned := plannedFor[text(offer["id"])] != ""
		swap, swapped := swaps[text(offer["id"])]
		take, why := d.offerAdvice(text(offer["id"]), ratio, planned || swapped)
		if swapped && !planned {
			why = fmt.Sprintf("%s si clausulas a %s (%s xPts)", ratioNote(ratio),
				text(swap.In["name"]), esSigned(swap.Gain))
			facts = append(facts, "si clausulas a "+text(swap.In["name"]))
		}
		// Both buttons, always Aceptar then Rechazar; only the recommended one is filled, and
		// in the accent colour, since red would read as danger.
		opArgs := func(op string) map[string]any {
			out := map[string]any{"op": op}
			for key, value := range args {
				out[key] = value
			}
			return out
		}
		accept := act("Aceptar", "ghost", "op", "op", opArgs("accept_offer"))
		decline := act("Rechazar", "primary", "op", "op", opArgs("decline_offer"))
		decline.Tip = "recomendado: " + why
		tone := ""
		if take {
			tone = "good"
			accept = act("Aceptar", "primary", "op", "op", opArgs("accept_offer"))
			accept.Tip = "recomendado: " + why
			decline = act("Rechazar", "ghost", "op", "op", opArgs("decline_offer"))
		}
		if planned {
			facts = append(facts, "si fichas a "+plannedFor[text(offer["id"])])
			tone = "accent"
		}
		drop := xiNow - elevenWithout(squad, text(offer["id"]))
		if drop >= 0.05 {
			facts = append(facts, "tu once pierde "+esNum(drop, 1)+" xPts")
		}
		if sale := sales[text(offer["offer_id"])]; truthy(sale["match_pending"]) {
			facts = append(facts, "aún no ha jugado: regalas "+
				esNum(number(sale["points_at_risk"]), 1)+" xPts")
		}
		glyph := "▲"
		if ratio < 1 {
			glyph = "▼"
		}
		note := []Seg{{T: glyph + " " + esRatio(ratio), C: ratioClass(ratio)},
			{T: " vale " + esMoney(value)}}
		if drop >= 0.05 {
			note = append(note, Seg{T: " · once −" + esNum(drop, 1)})
		}
		line := playerLine(offer)
		line.Value, line.Note, line.Why = esMoney(amount), note, strings.Join(facts, " · ")
		line.Chips, line.Acts, line.Tone = clockChip(text(offer["offer_expires"]), "caduca"),
			[]Act{accept, decline}, tone
		lines = append(lines, line)
	}
	return Block{Title: "Ofertas recibidas", Count: count(len(lines)), Rows: lines,
		Empty: "Ninguna oferta ahora mismo."}
}

func (d Document) listedBlock() Block {
	listings := rows(d.Advice["my_listings"])
	sort.SliceStable(listings, func(one, two int) bool {
		return text(mapOf(listings[one]["market"])["expires"]) <
			text(mapOf(listings[two]["market"])["expires"])
	})
	lines := []Row{}
	for _, player := range listings {
		listing := mapOf(player["market"])
		asking, value := number(listing["min_bid"]), number(player["value"])
		ratio := 0.0
		if value > 0 {
			ratio = asking / value
		}
		best := 0.0
		for _, offer := range rows(player["offers"]) {
			best = math.Max(best, number(offer["money"]))
		}
		line := playerLine(player)
		line.Value = esMoney(asking)
		line.Note = []Seg{{T: esRatio(ratio), C: ratioClass(ratio),
			Tip: "lo que pides frente a su valor"}}
		if best > 0 {
			line.Note = append(line.Note, Seg{T: " · mejor " + esMoney(best)})
		}
		line.Chips = clockChip(text(listing["expires"]), "cierra")
		line.Acts = []Act{cardAct("Quitar", text(player["id"]), "withdraw")}
		lines = append(lines, line)
	}
	return Block{Title: "En venta ahora", Count: count(len(lines)), Rows: lines,
		Empty: "No tienes a nadie en venta."}
}

// cardAct runs one of the player card's own actions, the same the popup offers.
func cardAct(label, playerID, op string) Act {
	return act(label, "ghost", "act", "card", map[string]any{"player_id": playerID, "op": op})
}

// restBlock is every player of mine not on sale, the bench before the eleven: listing a
// starter has to be a choice made on purpose.
func (d Document) restBlock() Block {
	squad := rows(d.Advice["squad"])
	choice, _ := bestElevenOf(squad)
	starter := map[string]bool{}
	for _, id := range choice.IDs() {
		starter[id] = true
	}
	always := map[string]bool{}
	for _, rule := range d.Plan {
		always[fallbackText(text(rule["player_id"]), text(rule["id"]))] = true
	}
	var rest []map[string]any
	for _, player := range squad {
		if text(mapOf(player["market"])["market_id"]) == "" {
			rest = append(rest, player)
		}
	}
	if len(rest) == 0 {
		return Block{}
	}
	sort.SliceStable(rest, func(one, two int) bool {
		return number(rest[one]["xpts"]) < number(rest[two]["xpts"])
	})
	var bench, eleven []Row
	for _, player := range rest {
		id := text(player["id"])
		trend := number(player["pct_7d"])
		class, sign := "up", "+"
		if trend < 0 {
			class, sign = "down", ""
		}
		line := playerLine(player)
		line.Value = esNum(number(player["xpts"]), 1) + " xPts"
		line.Note = []Seg{{T: esMoney(number(player["value"])) + " · "},
			{T: sign + esNum(trend, 1) + " %", C: class}, {T: " 7d"}}
		sell := cardAct("Poner en venta", id, "sell_to_market")
		if truthy(player["sale_locked"]) {
			why := "No se puede vender hasta el " + esWhen(text(player["hold_until"]))
			sell = Act{Label: "Poner en venta", Class: "mb mb-ghost", Off: true, Tip: why, Wrap: why}
		}
		label, class := "Siempre en mercado", "mb mb-ghost act"
		if always[id] {
			label, class = "● Siempre en mercado", "mb mb-ghost act on"
		}
		toggle := Act{Label: label, Class: class, Do: "always",
			Args: map[string]any{"player_id": id, "name": text(player["name"]), "on": always[id]},
			Tip:  "Lo mantiene en venta; importes y venta automática, en su ficha"}
		line.Acts = []Act{sell, toggle}
		if starter[id] {
			eleven = append(eleven, line)
		} else {
			bench = append(bench, line)
		}
	}
	lines := []Row{}
	if len(bench) > 0 {
		lines = append(lines, Row{Head: fmt.Sprintf("Fuera de tu once · %d", len(bench))})
		lines = append(lines, bench...)
	}
	if len(eleven) > 0 {
		lines = append(lines, Row{Head: fmt.Sprintf("En tu once · %d · venderlos baja tus xPts",
			len(eleven)), HeadC: "xi-head"})
		lines = append(lines, eleven...)
	}
	return Block{Title: "El resto de tu plantilla", Count: count(len(rest)),
		Sub: "lo que no tienes en venta", Rows: lines}
}

func (d Document) alwaysBlock() Block {
	byID := d.playersByID()
	lines := []Row{}
	for _, rule := range d.Plan {
		id := fallbackText(text(rule["player_id"]), text(rule["id"]))
		policy := d.Policies[id]
		terms := "no vende solo"
		if truthy(policy["auto_sell"]) {
			terms = "vende solo a lo que pides"
		}
		if above := number(policy["accept_above"]); above > 0 {
			terms = "acepta desde " + esMoney(above)
		}
		if floor := number(policy["min_price"]); floor > 0 {
			terms = "mínimo " + esMoney(floor) + " · " + terms
		}
		if result := text(rule["result"]); result != "" {
			terms += " · " + result
		}
		player := byID[id]
		if player == nil {
			player = map[string]any{"id": id, "name": rule["name"]}
		}
		line := playerLine(player)
		if amount := number(rule["amount"]); amount > 0 {
			line.Value = esMoney(amount)
		}
		line.Note = []Seg{{T: terms}}
		line.Why = strings.ReplaceAll(text(rule["action"]), "_", " ") + " · " +
			strings.SplitN(text(rule["why"]), ";", 2)[0]
		lines = append(lines, line)
	}
	return Block{Title: "Siempre en mercado", Count: count(len(d.Plan)), Rows: lines,
		Empty: "Ninguna regla activa: se arma con «Siempre en mercado».",
		Note: "Solo lo mantiene en venta. Para que se venda solo, activa la venta automática en " +
			"su ficha y fija desde qué oferta."}
}

// RowPlayer is a player as the client's list row reads him: what face, posTag and playerTags
// draw, with every judgement (owner word, health, the hold) already taken.
func RowPlayer(player map[string]any) map[string]any {
	ring, glyph, reason := health(player)
	owner := text(player["owner"])
	switch {
	case truthy(player["is_mine"]):
		owner = "tuyo"
	case owner == "":
		owner = fallbackText(text(mapOf(player["market"])["seller"]), "libre")
	}
	slug := positionSlug[int(number(player["position_id"]))]
	if slug == "" {
		slug = "ent"
	}
	position := text(player["position"])
	if position == "" {
		position = strings.ToUpper(slug)
	}
	_, crest := Crests[text(player["team_id"])]
	out := map[string]any{
		"id": text(player["id"]), "name": text(player["name"]), "image": text(player["image"]),
		"initials": initials(text(player["name"])), "pos": slug, "position": position,
		"team_id": text(player["team_id"]), "team_short": text(player["team_short"]),
		"crest": crest, "owner": owner, "shielded": truthy(player["shielded"]),
		"starred":        truthy(player["starred"]),
		"shielded_until": text(player["shielded_until"]),
		"health":         map[string]any{"ring": ring, "glyph": glyph, "reason": reason},
	}
	if until := text(player["shielded_until"]); until != "" {
		out["shielded_when"] = esWhen(until)
	}
	if role := mapOf(player["role"]); text(role["key"]) != "" {
		out["role"] = role
	}
	if starts := asFloat(player["start_probability"]); starts != nil {
		out["start_probability"] = math.Round(*starts)
	}
	if truthy(player["is_mine"]) && truthy(player["sale_locked"]) {
		out["locked_until"] = text(player["hold_until"])
		out["locked_when"] = esWhen(text(player["hold_until"]))
		out["locked_day"] = esDay(text(player["hold_until"]))
	}
	return out
}
