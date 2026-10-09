package render

import (
	"fmt"
	"sort"
	"strings"
)

// --- Plantilla -------------------------------------------------------------------------

// LineupShell is where the lineup editor goes; the browser draws it from /api/lineup.
var LineupShell = SectionView{ID: "once", Tab: "plantilla", UI: "lineup", Class: "mk mk-once"}

func (d Document) squadView() SectionView {
	if len(rows(d.Advice["squad"])) == 0 {
		return SectionView{}
	}
	return shell("v-plantilla", "plantilla", "plantilla")
}

func (d Document) squadSection() SectionView { return shell("plantilla", "plantilla", "squad") }

// KV is one line of a summary: a label and its figure, coloured when it is a change.
type KV struct {
	Label string `json:"label"`
	Value string `json:"value"`
	C     string `json:"c,omitempty"`
}

// SquadData is Plantilla's list view: who the advice gives a reason to sell, and the squad in
// figures.
func (d Document) SquadData() View {
	squad := rows(d.Advice["squad"])
	lines := []Row{}
	for _, player := range d.sellRows() {
		listing := mapOf(player["market"])
		best, offerID := 0.0, ""
		for _, offer := range rows(player["offers"]) {
			if amount := number(offer["money"]); amount > best {
				best, offerID = amount, text(offer["id"])
			}
		}
		line := playerLine(player)
		line.Value = esNum(number(player["xpts"]), 1) + " xPts"
		note := "vale " + esMoney(number(player["value"]))
		if best > 0 {
			note += " · oferta " + esMoney(best)
		}
		line.Note, line.Why = []Seg{{T: note}}, strings.Join(asStrings(player["reasons"]), ", ")
		switch {
		case best > 0 && offerID != "":
			line.Acts = []Act{act("Aceptar "+esMoney(best), "primary", "op", "op", map[string]any{
				"op": "accept_offer", "market_id": text(listing["market_id"]), "offer_id": offerID,
				"player_id": text(player["id"]), "name": text(player["name"]),
				"amount": int64(best)})}
		case text(listing["market_id"]) != "":
			line.Acts = []Act{cardAct("Quitar", text(player["id"]), "withdraw")}
		case !truthy(player["sale_locked"]):
			line.Acts = []Act{cardAct("Poner en venta", text(player["id"]), "sell_to_market")}
		}
		if !truthy(player["sale_locked"]) {
			line.Chips = []Chip{{T: "se puede ya", C: "ok"}}
		}
		lines = append(lines, line)
	}
	value, clauses, change := 0.0, 0.0, 0.0
	for _, player := range squad {
		worth := number(player["value"])
		value += worth
		clauses += number(player["clause"])
		if pct := number(player["pct_7d"]); pct > -100 {
			change += worth * pct / (100 + pct)
		}
	}
	signed := func(amount float64) (string, string) {
		if amount < 0 {
			return esMoney(amount), "down"
		}
		return "+" + esMoney(amount), "up"
	}
	summary := []KV{{Label: "Jugadores", Value: fmt.Sprintf("%d", len(squad))},
		{Label: "Valor de plantilla", Value: esMoney(value)},
		{Label: "Suma de cláusulas", Value: esMoney(clauses)}}
	text, class := signed(change)
	summary = append(summary, KV{Label: "Valor últimos 7 días", Value: text, C: class})
	if projected, ok := d.Money["projected_7d"]; ok {
		text, class := signed(number(projected))
		summary = append(summary, KV{Label: "Previsión próximos 7 días", Value: text, C: class})
	}
	return View{Main: []Block{{Title: "Para vender", Count: count(len(lines)), Rows: lines,
		Empty: "Nadie: el consejo no ve motivo para vender a ninguno."}},
		Aside: []Block{{Title: "Resumen", Kind: "kv", Data: summary}}}
}

// MySquadData is my squad line by line, each line saying whether it is short or has one spare.
func (d Document) MySquadData() View {
	squad := rows(d.Advice["squad"])
	shape := mapOf(d.Advice["shape"])
	lines := []Row{}
	for _, positionID := range []string{"1", "2", "3", "4"} {
		var line []map[string]any
		for _, player := range squad {
			if text(player["position_id"]) == positionID {
				line = append(line, player)
			}
		}
		if len(line) == 0 {
			continue
		}
		sort.SliceStable(line, func(one, two int) bool {
			return number(line[one]["xpts"]) > number(line[two]["xpts"])
		})
		state := ""
		if data := mapOf(shape[positionID]); data != nil {
			switch {
			case truthy(data["gap"]):
				state = " · falta uno"
			case truthy(data["surplus"]):
				state = " · sobra uno"
			}
		}
		slug := map[string]string{"1": "por", "2": "def", "3": "med", "4": "del"}[positionID]
		lines = append(lines, Row{HeadSegs: []Seg{{T: strings.ToUpper(slug), C: "pos pos-" + slug},
			{T: counted(len(line), "jugador", "jugadores") + state}}})
		for _, player := range line {
			lines = append(lines, squadLine(player))
		}
	}
	return View{Plain: true, Main: []Block{{Title: "Mi plantilla", Count: count(len(squad)),
		Rows: lines}}}
}

// squadLine is one of mine: what he gives, what he is worth and where his clause stands.
func squadLine(player map[string]any) Row {
	trend := number(player["pct_7d"])
	class, sign := "up", "+"
	if trend < 0 {
		class, sign = "down", ""
	}
	line := playerLine(player)
	line.Value = esNum(number(player["xpts"]), 1) + " xPts"
	line.Note = []Seg{{T: esMoney(number(player["value"])) + " · "},
		{T: sign + esNum(trend, 1) + " %", C: class}}
	if asking := number(mapOf(player["market"])["min_bid"]); asking > 0 {
		line.Note = append([]Seg{{T: "en venta " + esMoney(asking) + " · "}}, line.Note...)
	}
	switch {
	case truthy(player["shielded"]):
		line.Chips = []Chip{{T: "🛡 blindado", C: "done"}}
	case truthy(player["clause_locked"]):
		line.Chips = clockChip(text(player["clause_locked_until"]), "se libera su cláusula")
	case number(player["clause"]) > 0:
		line.Chips = []Chip{{T: "pagable " + esMoney(number(player["clause"])), C: "warn"}}
	}
	line.Acts = []Act{starAct(player), cmpAct(player, "cmp-add small")}
	return line
}
