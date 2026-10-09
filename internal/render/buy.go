package render

import (
	"sort"
)

// --- Comprar ---------------------------------------------------------------------------

func (d Document) buyView() SectionView { return shell("v-comprar", "comprar", "comprar") }

// BuyData is Comprar: the free market, the rivals' listings and the clauses, each ranked by
// what the signing adds to the eleven, beside my bids, how the last ones ended and who I follow.
func (d Document) BuyData() View {
	window := d.window()
	clauses, offers, free := d.buyOptions()
	bids := []Row{}
	for _, bid := range rows(d.Advice["my_bids"]) {
		line := playerLine(bid)
		line.Value, line.Note = esMoney(number(bid["my_bid"])), []Seg{{T: "pide " +
			esMoney(number(bid["asking"]))}}
		line.Chips = clockChip(text(bid["closes"]), "cierra")
		line.Acts = bidActs(bid, "bid", "")
		bids = append(bids, line)
	}
	bids = append(bids, d.raidLines()...)
	aside := []Block{{Title: "Mis pujas en curso", Rows: bids, Empty: "Ninguna ahora mismo."},
		d.endingsBlock()}
	if stars := d.starredBlock(); stars != nil {
		aside = append(aside, *stars)
	}
	return View{Filters: true,
		Main:  []Block{d.buyBlockData("Mercado rentable", "puja libre", free, window)},
		Aside: aside,
		Row2: []Block{d.buyBlockData("En venta por rivales", "oferta al dueño", offers, window),
			d.buyBlockData("Cláusulas que puedes pagar", "clausula", clauses, window)}}
}

// buyBlockData is every candidate of one route, best for the eleven first and the ones out of
// reach at the end, in a box that scrolls.
func (d Document) buyBlockData(title, route string, items []map[string]any,
	window *windowState) Block {
	gains := d.gains(route)
	cash, reach := d.buyingPower()
	if route == "clausula" {
		reach = cash
	}
	sort.SliceStable(items, func(one, two int) bool {
		first, second := items[one], items[two]
		if inOne, inTwo := number(first["entry_cost"]) <= reach,
			number(second["entry_cost"]) <= reach; inOne != inTwo {
			return inOne
		}
		gainOne, gainTwo := gains[text(first["id"])], gains[text(second["id"])]
		if (gainOne > MinShownGain) != (gainTwo > MinShownGain) {
			return gainOne > MinShownGain
		}
		if gainOne > MinShownGain && gainOne != gainTwo {
			return gainOne > gainTwo
		}
		return number(first["xpts"]) > number(second["xpts"])
	})
	lines := []Row{}
	for _, item := range items {
		lines = append(lines, d.buyLine(item, route, gains[text(item["id"])], window))
	}
	return Block{Title: title, Count: count(len(lines)), Rows: lines, Empty: "Nadie ahora mismo.",
		Scroll: 420}
}

// buyLine is a signing ranked by what it adds to the eleven, with the button that makes it.
// A clause is paid with today's cash; a market bid may also use the debt the league allows,
// and one that would start the matchday in the red says so.
func (d Document) buyLine(item map[string]any, route string, gain float64,
	window *windowState) Row {
	cost := number(item["entry_cost"])
	listing := mapOf(item["market"])
	id, name := text(item["id"]), text(item["name"])
	cash, reach := d.buyingPower()
	if route == "clausula" {
		reach = cash
	}
	line := playerLine(item)
	line.Find = findOf(item, cost)
	line.Value = esNum(number(item["xpts"]), 1) + " xPts"
	if gain > MinShownGain {
		line.Value = esSigned(gain) + " xPts"
	}
	note := []Seg{{T: esMoney(cost)}}
	if asking := number(item["asking"]); asking > 0 && asking < cost*0.99 {
		note = append(note, Seg{T: " · piden " + esMoney(asking)})
	}
	if route != "clausula" {
		if ceiling := number(item["ideal_bid"]); ceiling > 0 {
			class, mark := "up", "+"
			if ceiling < cost {
				class, mark = "down", ""
			}
			note = append(note, Seg{T: " · "},
				Seg{T: mark + esMoney(ceiling-cost) + " de margen", C: class})
		} else {
			note = append(note, Seg{T: " · "}, Seg{T: "sin margen", C: "down"})
		}
	}
	line.Note = note
	kind := "ghost"
	if gain > MinShownGain && d.worthItsPrice(item, route) {
		kind = "primary"
	}
	var verb string
	switch route {
	case "clausula":
		verb = "Programar"
		if window.open {
			verb = "Pagar"
			line.Chips = clockChip(window.closes, "cláusulas abiertas hasta")
			line.Acts = []Act{act("Pagar "+esMoney(cost), kind, "op", "op", map[string]any{
				"op": "pay_clause", "player_id": id, "name": name, "amount": int64(cost)})}
		} else {
			line.Chips = clockChip(window.opens, "se abren las cláusulas")
			line.Acts = []Act{act("Programar "+esMoney(cost), "ghost", "raid-btn", "raid",
				map[string]any{"id": id, "name": name, "max": int64(cost),
					"clause": int64(number(item["clause"]))})}
		}
	case "oferta al dueño":
		line.Chips = clockChip(text(listing["expires"]), "sale del mercado")
		verb = "Ofrecer"
		line.Acts = bidActs(item, "mb mb-"+kind+" bid", "Ofrecer "+esMoney(cost))
	default:
		line.Chips = clockChip(text(listing["expires"]), "sale del mercado")
		verb = "Pujar"
		line.Acts = bidActs(item, "mb mb-"+kind+" bid", "Pujar "+esMoney(cost))
	}
	switch {
	case cost > reach:
		line.Tone = "dim"
		short := "te faltan " + esMoney(cost-reach)
		line.Acts = []Act{{Label: verb + " " + esMoney(cost), Class: "mb mb-ghost", Off: true,
			Tip: short, Wrap: short}}
	case cost > cash:
		line.Chips = append([]Chip{{T: "⚠ en negativo", C: "soon",
			Tip: "si empiezas la jornada en negativo no puntúas"}}, line.Chips...)
	}
	return line
}

// raidLines are the scheduled clausulazos still standing, as rows of what I have put up: the
// limit, the clause today and whether it already passes it, when it opens, and the way out.
func (d Document) raidLines() []Row {
	byID := d.playersByID()
	out := []Row{}
	for _, raid := range d.Raids {
		if raidsStandingDown[text(raid["action"])] {
			continue
		}
		player := byID[text(raid["player_id"])]
		if player == nil {
			player = map[string]any{"id": raid["player_id"], "name": raid["name"],
				"owner": raid["owner"]}
		}
		clause, limit := number(raid["clause"]), number(raid["max_pay"])
		line := playerLine(player)
		line.Note = []Seg{{T: "cláusula " + esMoney(clause)}}
		if limit > 0 && clause > limit {
			line.Note = append(line.Note, Seg{T: " · "}, Seg{T: "pasa tu límite", C: "down"})
		}
		switch {
		case truthy(player["shielded"]):
			line.Chips = []Chip{{T: "🛡 blindado", C: "done"}}
		case truthy(player["clause_locked"]):
			line.Chips = clockChip(text(player["clause_locked_until"]), "se abre su cláusula")
		default:
			line.Chips = []Chip{{T: "pagable", C: "ok"}}
		}
		line.Value, line.Why = "hasta "+esMoney(limit), "clausulazo programado"
		line.Acts = []Act{act("Cancelar", "ghost", "op", "cancel_raid", map[string]any{
			"player_id": text(raid["player_id"]), "name": text(raid["name"])})}
		out = append(out, line)
	}
	return out
}

// outcomeLine is a resolved bid, offer or standing order: the icon of how it ended where the
// face would be.
func outcomeLine(id, name, outcome, why, what, date string, amount float64) Row {
	value := ""
	if amount > 0 {
		value = esMoney(amount)
	}
	return Row{Lead: []Seg{outcomeSeg(outcome, why)}, LeadC: "out-icon", Name: name,
		Meta: metaSegs(what), Value: value, Note: []Seg{{T: date}}, Pid: id}
}

func (d Document) endingsBlock() Block {
	lines := []Row{}
	for _, ending := range d.Endings {
		outcome := text(ending["outcome"])
		what := outcome
		switch {
		case outcome == "perdida" && text(ending["new_owner"]) != "":
			what = "perdida · ganó " + text(ending["new_owner"])
			if paid := number(ending["won_for"]); paid > 0 {
				what += " con " + esMoney(paid)
			}
		case outcome == "rechazada" && text(ending["who"]) != "":
			what = "rechazada por " + text(ending["who"])
		}
		detail := ""
		if outcome == "perdida" && text(ending["new_owner"]) != "" {
			detail = "ganó " + text(ending["new_owner"])
			if paid := number(ending["won_for"]); paid > 0 {
				detail += " (" + esMoney(paid) + ")"
			}
		}
		lines = append(lines, outcomeLine(text(ending["player_id"]), text(ending["player"]),
			outcome, detail, what, shortDate(text(ending["at"])), number(ending["amount"])))
	}
	return Block{Title: "Cómo acabaron", Count: count(len(d.Endings)), Rows: lines,
		Empty: "Todavía no se ha resuelto ninguna.", Scroll: 420}
}

// StarItem is a followed player in the compact list: face, name, owner and xPts.
type StarItem struct {
	Player map[string]any `json:"player"`
	Owner  string         `json:"owner"`
	XPts   string         `json:"xpts"`
	Class  string         `json:"class"`
}

func (d Document) starredBlock() *Block {
	starred := rows(d.Advice["starred"])
	if len(starred) == 0 {
		return nil
	}
	sort.SliceStable(starred, func(one, two int) bool {
		return number(starred[one]["xpts"]) > number(starred[two]["xpts"])
	})
	items := []StarItem{}
	for _, player := range starred {
		owner := text(player["owner"])
		if truthy(player["is_mine"]) {
			owner = "tuyo"
		} else if owner == "" {
			owner = "libre"
		}
		items = append(items, StarItem{Player: RowPlayer(player), Owner: owner,
			XPts: esNum(number(player["xpts"]), 1), Class: xptsClass(number(player["xpts"]))})
	}
	return &Block{Title: "☆ Siguiendo", Count: count(len(starred)), Kind: "stars", Data: items}
}
