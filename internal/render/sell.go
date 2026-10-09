package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// --- Vender ----------------------------------------------------------------------------

// SellView is the Vender tab as data: every decision taken here, so the page's client draws the
// same rows from JSON that sellView draws as HTML.
type SellView struct {
	Offers []SellOffer   `json:"offers"`
	Listed []SellListing `json:"listed"`
	Rest   []SellRest    `json:"rest"`
	Always []SellAlways  `json:"always"`
}

type SellOffer struct {
	Player   map[string]any `json:"player"`
	MarketID string         `json:"market_id"`
	OfferID  string         `json:"offer_id"`
	Amount   float64        `json:"amount"`
	Value    float64        `json:"value"`
	Ratio    float64        `json:"ratio"`
	// Drop is what the best eleven loses without him, zero when it is not worth saying.
	Drop    float64 `json:"drop"`
	Facts   string  `json:"facts"`
	Take    bool    `json:"take"`
	Why     string  `json:"why"`
	Tone    string  `json:"tone"`
	Expires string  `json:"expires"`
	source  map[string]any
}

type SellListing struct {
	Player  map[string]any `json:"player"`
	Asking  float64        `json:"asking"`
	Ratio   float64        `json:"ratio"`
	Best    float64        `json:"best"`
	Expires string         `json:"expires"`
	source  map[string]any
}

type SellRest struct {
	Player    map[string]any `json:"player"`
	XPts      float64        `json:"xpts"`
	Value     float64        `json:"value"`
	Trend     float64        `json:"trend"`
	Starter   bool           `json:"starter"`
	LockedWhy string         `json:"locked_why,omitempty"`
	Always    bool           `json:"always"`
	source    map[string]any
}

type SellAlways struct {
	Player map[string]any `json:"player"`
	Amount float64        `json:"amount"`
	Terms  string         `json:"terms"`
	Why    string         `json:"why"`
	source map[string]any
}

// SellData is what Vender shows, decided once for both renderers.
func (d Document) SellData() SellView {
	return SellView{Offers: d.sellOffers(), Listed: d.sellListed(), Rest: d.sellRest(),
		Always: d.sellAlways()}
}

func (d Document) sellOffers() []SellOffer {
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
	out := []SellOffer{}
	for _, offer := range offers {
		ratio := number(offer["vs_value"])
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
		tone := ""
		if take {
			tone = "good"
		}
		if planned {
			facts = append(facts, "si fichas a "+plannedFor[text(offer["id"])])
			tone = "accent"
		}
		drop := xiNow - elevenWithout(squad, text(offer["id"]))
		if drop >= 0.05 {
			facts = append(facts, "tu once pierde "+esNum(drop, 1)+" xPts")
		} else {
			drop = 0
		}
		if sale := sales[text(offer["offer_id"])]; truthy(sale["match_pending"]) {
			facts = append(facts, "aún no ha jugado: regalas "+
				esNum(number(sale["points_at_risk"]), 1)+" xPts")
		}
		out = append(out, SellOffer{Player: RowPlayer(offer), MarketID: text(offer["market_id"]),
			OfferID: text(offer["offer_id"]), Amount: number(offer["offer_amount"]),
			Value: number(offer["value"]), Ratio: ratio, Drop: drop,
			Facts: strings.Join(facts, " · "), Take: take, Why: why, Tone: tone,
			Expires: text(offer["offer_expires"]), source: offer})
	}
	return out
}

func (d Document) sellListed() []SellListing {
	listings := rows(d.Advice["my_listings"])
	sort.SliceStable(listings, func(one, two int) bool {
		return text(mapOf(listings[one]["market"])["expires"]) <
			text(mapOf(listings[two]["market"])["expires"])
	})
	out := []SellListing{}
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
		out = append(out, SellListing{Player: RowPlayer(player), Asking: asking, Ratio: ratio,
			Best: best, Expires: text(listing["expires"]), source: player})
	}
	return out
}

// sellRest is every player of mine not on sale, least useful first.
func (d Document) sellRest() []SellRest {
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
	sort.SliceStable(rest, func(one, two int) bool {
		return number(rest[one]["xpts"]) < number(rest[two]["xpts"])
	})
	out := []SellRest{}
	for _, player := range rest {
		id := text(player["id"])
		locked := ""
		if truthy(player["sale_locked"]) {
			locked = "No se puede vender hasta el " + esWhen(text(player["hold_until"]))
		}
		out = append(out, SellRest{Player: RowPlayer(player), XPts: number(player["xpts"]),
			Value: number(player["value"]), Trend: number(player["pct_7d"]),
			Starter: starter[id], LockedWhy: locked, Always: always[id], source: player})
	}
	return out
}

func (d Document) sellAlways() []SellAlways {
	byID := d.playersByID()
	out := []SellAlways{}
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
		why := strings.ReplaceAll(text(rule["action"]), "_", " ") + " · " +
			strings.SplitN(text(rule["why"]), ";", 2)[0]
		out = append(out, SellAlways{Player: RowPlayer(player), Amount: number(rule["amount"]),
			Terms: terms, Why: why, source: player})
	}
	return out
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
		"shielded_until": text(player["shielded_until"]),
		"health":         map[string]any{"ring": ring, "glyph": glyph, "reason": reason},
	}
	if role := mapOf(player["role"]); text(role["key"]) != "" {
		out["role"] = role
	}
	if starts := asFloat(player["start_probability"]); starts != nil {
		out["start_probability"] = math.Round(*starts)
	}
	if truthy(player["is_mine"]) && truthy(player["sale_locked"]) {
		out["locked_until"] = text(player["hold_until"])
	}
	return out
}

func (d Document) sellView() string {
	data := d.SellData()
	var items []string
	for _, offer := range data.Offers {
		common := fmt.Sprintf(` data-op-market="%s" data-op-offer="%s" data-op-player="%s" `+
			`data-op-name="%s" data-op-amount="%d"`, Esc(offer.MarketID), Esc(offer.OfferID),
			Esc(text(offer.source["id"])), Esc(text(offer.source["name"])), int64(offer.Amount))
		// Both buttons, always Aceptar then Rechazar; only the recommended one is filled, and
		// in the accent colour, since red would read as danger.
		recommend := func(label, kind, op, reason string) string {
			return button(label, kind, "op", fmt.Sprintf(` data-op="%s" title="recomendado: %s"`,
				op, Esc(reason))+common)
		}
		accept := button("Aceptar", "ghost", "op", ` data-op="accept_offer"`+common)
		decline := recommend("Rechazar", "primary", "decline_offer", offer.Why)
		if offer.Take {
			accept = recommend("Aceptar", "primary", "accept_offer", offer.Why)
			decline = button("Rechazar", "ghost", "op", ` data-op="decline_offer"`+common)
		}
		glyph := "▲"
		if offer.Ratio < 1 {
			glyph = "▼"
		}
		note := fmt.Sprintf(`<span class="%s">%s %s</span> vale %s`, ratioClass(offer.Ratio),
			glyph, esRatio(offer.Ratio), esMoney(offer.Value))
		if offer.Drop > 0 {
			note += " · once −" + esNum(offer.Drop, 1)
		}
		line := playerRow(offer.source)
		line.Value, line.Note, line.Why = esMoney(offer.Amount), note, Esc(offer.Facts)
		line.Chip, line.Action, line.Tone = clock(offer.Expires, "caduca"), accept+decline,
			offer.Tone
		items = append(items, line.HTML())
	}
	body := empty("Ninguna oferta ahora mismo.")
	if len(items) > 0 {
		body = rowList(items)
	}
	main := block("Ofertas recibidas", body, "", len(items))

	var listed []string
	for _, listing := range data.Listed {
		note := fmt.Sprintf(`<span class="%s" title="lo que pides frente a su valor">%s</span>`,
			ratioClass(listing.Ratio), esRatio(listing.Ratio))
		if listing.Best > 0 {
			note += " · mejor " + esMoney(listing.Best)
		}
		listed = append(listed, row(listing.source, "", esMoney(listing.Asking), note,
			clock(listing.Expires, "cierra"),
			actButton("Quitar", "ghost", text(listing.source["id"]), "withdraw"), ""))
	}
	body = empty("No tienes a nadie en venta.")
	if len(listed) > 0 {
		body = rowList(listed)
	}
	listedBlock := block("En venta ahora", body, "", len(listed))
	main += restOfSquadHTML(data.Rest)

	return view("v-vender", "vender", main, listedBlock+alwaysAsideHTML(data.Always))
}

// restOfSquad is every player of mine not on sale, the bench before the eleven: listing a
// starter has to be a choice made on purpose.
func (d Document) restOfSquad() string {
	return restOfSquadHTML(d.sellRest())
}

func restOfSquadHTML(rest []SellRest) string {
	if len(rest) == 0 {
		return ""
	}
	var bench, eleven []string
	for _, item := range rest {
		id := text(item.source["id"])
		class, sign := "up", "+"
		if item.Trend < 0 {
			class, sign = "down", ""
		}
		note := fmt.Sprintf(`%s · <span class="%s">%s%s %%</span> 7d`,
			esMoney(item.Value), class, sign, esNum(item.Trend, 1))
		sell := actButton("Poner en venta", "ghost", id, "sell_to_market")
		if item.LockedWhy != "" {
			sell = `<span title="` + Esc(item.LockedWhy) + `">` + button("Poner en venta", "ghost",
				"", ` disabled title="`+Esc(item.LockedWhy)+`"`) + `</span>`
		}
		label, on := "Siempre en mercado", ""
		if item.Always {
			label, on = "● Siempre en mercado", " on"
		}
		toggle := button(label, "ghost", "act"+on, fmt.Sprintf(` data-act="always" `+
			`data-act-player="%s" title="Lo mantiene en venta; importes y venta automática, en su ficha"`,
			Esc(id)))
		line := row(item.source, "", esNum(item.XPts, 1)+" xPts", note, "", sell+toggle, "")
		if item.Starter {
			eleven = append(eleven, line)
		} else {
			bench = append(bench, line)
		}
	}
	var body strings.Builder
	body.WriteString(`<ul class="rows">`)
	if len(bench) > 0 {
		fmt.Fprintf(&body, `<li class="line-head">Fuera de tu once · %d</li>%s`, len(bench),
			strings.Join(bench, ""))
	}
	if len(eleven) > 0 {
		fmt.Fprintf(&body, `<li class="line-head xi-head">En tu once · %d · venderlos baja tus `+
			`xPts</li>%s`, len(eleven), strings.Join(eleven, ""))
	}
	body.WriteString(`</ul>`)
	return block("El resto de tu plantilla", body.String(), "lo que no tienes en venta", len(rest))
}

func alwaysAsideHTML(rules []SellAlways) string {
	var items []string
	for _, rule := range rules {
		amount := ""
		if rule.Amount > 0 {
			amount = esMoney(rule.Amount)
		}
		line := playerRow(rule.source)
		line.Value, line.Note, line.Why = amount, Esc(rule.Terms), Esc(rule.Why)
		items = append(items, line.HTML())
	}
	body := empty("Ninguna regla activa: se arma con «Siempre en mercado».")
	if len(items) > 0 {
		body = rowList(items)
	}
	body += `<p class="mk-note">Solo lo mantiene en venta. Para que se venda solo, fija «aceptar ` +
		`desde» o marca la venta automática en su ficha; si no, una buena oferta solo avisa.</p>`
	return block("Siempre en mercado", body, "", len(rules))
}
