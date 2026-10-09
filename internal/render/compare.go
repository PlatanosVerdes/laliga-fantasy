package render

import (
	"fmt"
	"math"
	"sort"
	"strconv"
)

// --- Comparador ------------------------------------------------------------------------

// Compared is the comparator's table: a column per player, a row per figure with the best of
// each marked, their states, and a line saying whether the one being looked at improves on mine.
type Compared struct {
	Note    string        `json:"note"`
	Verdict []Seg         `json:"verdict,omitempty"`
	Heads   []CompareHead `json:"heads"`
	Rows    []CompareRow  `json:"rows"`
	States  [][]Seg       `json:"states"`
}

type CompareHead struct {
	Player map[string]any `json:"player"`
	Team   string         `json:"team"`
	Crest  bool           `json:"crest"`
	Owner  Seg            `json:"owner"`
}

type CompareRow struct {
	Label string      `json:"label"`
	Cells []CompareTD `json:"cells"`
}

type CompareTD struct {
	Segs []Seg  `json:"segs"`
	C    string `json:"c,omitempty"`
}

type compareSpec struct {
	label string
	key   string
	best  string // max, min, or none for text
	cost  bool
	write func(value *float64, player map[string]any) []Seg
}

func plain(text string) []Seg { return []Seg{{T: text}} }

func euros(value *float64) string {
	if value == nil {
		return Missing
	}
	return group(fmt.Sprintf("%.0f", *value)) + " €"
}

func orZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

var compareSpecs = []compareSpec{
	{"Valor", "value", "min", true, func(v *float64, _ map[string]any) []Seg { return plain(euros(v)) }},
	{"Clausula", "clause", "min", true, func(v *float64, _ map[string]any) []Seg {
		if orZero(v) == 0 {
			return plain(Missing)
		}
		return plain(euros(v))
	}},
	{"Techo rentable", "ideal_bid", "max", false, func(v *float64, _ map[string]any) []Seg {
		if orZero(v) == 0 {
			return plain("sin margen")
		}
		return plain(euros(v))
	}},
	{"xPts por jornada", "xpts", "max", false, func(v *float64, _ map[string]any) []Seg {
		return plain(fmt.Sprintf("%.2f", orZero(v)))
	}},
	{"Pts por millon", "points_value", "max", false, func(v *float64, _ map[string]any) []Seg {
		return plain(fmt.Sprintf("%.3f", orZero(v)))
	}},
	{"Score", "score", "max", false, func(v *float64, _ map[string]any) []Seg {
		return plain(fmt.Sprintf("%+.2f", orZero(v)))
	}},
	{"Titularidad", "start_probability", "max", false, func(v *float64, _ map[string]any) []Seg {
		if v == nil {
			return plain(Missing)
		}
		return plain(strconv.FormatFloat(*v, 'f', -1, 64) + "%")
	}},
	{"Puntos temporada", "season_points", "max", false, func(v *float64, _ map[string]any) []Seg {
		return plain(fmt.Sprintf("%.0f", math.Round(orZero(v))))
	}},
	{"Media", "season_avg", "max", false, func(v *float64, _ map[string]any) []Seg {
		return plain(fmt.Sprintf("%.1f", orZero(v)))
	}},
	{"Puntos 25/26", "last_season_points", "max", false, func(v *float64, _ map[string]any) []Seg {
		return plain(fmt.Sprintf("%.0f", math.Round(orZero(v))))
	}},
	{"Valor 7d", "projected_pct", "max", false, func(v *float64, _ map[string]any) []Seg {
		return plain(fmt.Sprintf("%+.2f%%", orZero(v)))
	}},
	{"Proximo rival", "", "", false, func(_ *float64, p map[string]any) []Seg {
		rival := text(p["next_rival"])
		if rival == "" {
			return plain(Missing)
		}
		var out []Seg
		if id := text(p["next_rival_id"]); id != "" {
			out = append(out, Seg{C: "crest crest-" + id})
		}
		return append(out, Seg{T: rival + " · " + Where(truthy(p["next_home"]))})
	}},
}

// CompareTable decides the comparator: the best of each row and the verdict.
func CompareTable(list []map[string]any) Compared {
	out := Compared{Note: fmt.Sprintf("%d jugadores · lo mejor de cada fila en verde", len(list)),
		Heads: []CompareHead{}, Rows: []CompareRow{}, States: [][]Seg{},
		Verdict: compareVerdict(list)}
	for _, p := range list {
		owner := Seg{T: fallbackText(text(p["owner"]), "libre")}
		if truthy(p["is_mine"]) {
			owner = Seg{T: "tuyo"}
		} else if text(p["owner"]) != "" && text(p["owner_team_id"]) != "" {
			owner = Seg{T: text(p["owner"]), C: "p-name", Team: text(p["owner_team_id"])}
		}
		out.Heads = append(out.Heads, CompareHead{Player: RowPlayer(p),
			Team:  fallbackText(text(p["team_short"]), text(p["team"])),
			Crest: text(p["team_id"]) != "", Owner: owner})
		out.States = append(out.States, compareChips(p))
	}
	for _, spec := range compareSpecs {
		row := CompareRow{Label: spec.label, Cells: []CompareTD{}}
		var target *float64
		if spec.best != "" {
			var numbers []float64
			for _, p := range list {
				if value := asFloat(p[spec.key]); value != nil && *value != 0 &&
					!math.IsInf(*value, 0) && !math.IsNaN(*value) {
					numbers = append(numbers, *value)
				}
			}
			if len(numbers) > 1 {
				sort.Float64s(numbers)
				pick := numbers[len(numbers)-1]
				if spec.best == "min" {
					pick = numbers[0]
				}
				// All equal teaches nothing: highlighting there only stains the table.
				if numbers[0] != numbers[len(numbers)-1] {
					target = &pick
				}
			}
		}
		for _, p := range list {
			value := asFloat(p[spec.key])
			cell := CompareTD{Segs: spec.write(value, p)}
			if target != nil && value != nil && *value == *target {
				cell.C = "cmp-best"
				if spec.cost {
					cell.C = "cmp-cheap"
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

// compareChips is what can be done about him today, as the comparator's state cell.
func compareChips(p map[string]any) []Seg {
	var out []Seg
	if listing := mapOf(p["market"]); text(listing["market_id"]) != "" {
		out = append(out, Seg{T: "en venta " + shortMillions(number(listing["min_bid"])), C: "chip"})
	}
	switch {
	case truthy(p["shielded"]) && text(p["shielded_until"]) != "":
		out = append(out, Seg{C: "chip chip-warn", Kids: []Seg{{T: "blindado "},
			{Until: text(p["shielded_until"])}}})
	case truthy(p["shielded"]):
		out = append(out, Seg{T: "blindado", C: "chip chip-warn"})
	case truthy(p["clause_locked"]) && text(p["clause_locked_until"]) != "":
		out = append(out, Seg{C: "chip chip-warn", Kids: []Seg{{T: "clausula en "},
			{Until: text(p["clause_locked_until"])}}})
	case number(p["clause"]) > 0 && !truthy(p["is_mine"]):
		out = append(out, Seg{T: "clausula pagable", C: "chip chip-good"})
	}
	if truthy(p["sale_locked"]) {
		out = append(out, Seg{T: "🔒 recien fichado", C: "chip chip-warn"})
	}
	if !truthy(p["available"]) {
		out = append(out, Seg{T: "no puntua", C: "chip chip-bad"})
	}
	if len(out) == 0 {
		out = append(out, Seg{T: "sin nada", C: "cmp-quiet"})
	}
	return out
}

// shortMillions is the page's oldest short money: 2.50M, 850K.
func shortMillions(amount float64) string {
	switch {
	case math.Abs(amount) >= 1e6:
		return fmt.Sprintf("%.2fM", amount/1e6)
	case math.Abs(amount) >= 1e3:
		return fmt.Sprintf("%.0fK", amount/1e3)
	}
	return fmt.Sprintf("%.0f", amount)
}

// compareVerdict says whether the one being looked at improves on what I have, which is the
// only reason to be comparing; with no single outsider it names the best on each count.
func compareVerdict(list []map[string]any) []Seg {
	var outside, ours []map[string]any
	for _, p := range list {
		if truthy(p["is_mine"]) {
			ours = append(ours, p)
		} else {
			outside = append(outside, p)
		}
	}
	by := func(items []map[string]any, key string) []map[string]any {
		out := append([]map[string]any{}, items...)
		sort.SliceStable(out, func(one, two int) bool { return number(out[one][key]) > number(out[two][key]) })
		return out
	}
	bold := func(name string) Seg { return Seg{T: name, El: "b"} }
	if len(outside) != 1 || len(ours) == 0 {
		if len(list) == 0 {
			return nil
		}
		top, eff := by(list, "xpts")[0], by(list, "points_value")[0]
		return []Seg{{T: "Mas xPts: "}, bold(text(top["name"])),
			{T: fmt.Sprintf(" (%.2f). Mas puntos por millon: ", number(top["xpts"]))},
			bold(text(eff["name"])), {T: fmt.Sprintf(" (%.3f).", number(eff["points_value"]))}}
	}
	him := outside[0]
	line := fallbackText(text(him["position"]), "esa posicion")
	ranked := by(ours, "xpts")
	best, worst := ranked[0], ranked[len(ranked)-1]
	gap := func(a, b map[string]any) float64 { return number(a["xpts"]) - number(b["xpts"]) }
	money := func(a, b map[string]any) string {
		diff := number(a["value"]) - number(b["value"])
		if diff == 0 {
			return "y cuesta lo mismo"
		}
		word := "menos"
		if diff > 0 {
			word = "mas"
		}
		return "y cuesta " + shortMillions(math.Abs(math.Round(diff))) + " " + word
	}
	// Improving on one who is suspended is no merit: saying so stops the verdict being misread.
	why := func(p map[string]any) string {
		if available, known := p["available"].(bool); known && !available {
			return " (que ahora no puntua)"
		}
		return ""
	}
	name := text(him["name"])
	switch {
	case gap(him, best) > 0:
		return []Seg{bold(name), {T: " mejora a tu mejor " + line + ": "},
			bold(fmt.Sprintf("+%.2f xPts", gap(him, best))),
			{T: " sobre " + text(best["name"]) + " " + money(him, best) + "."}}
	case gap(him, worst) > 0:
		return []Seg{bold(name), {T: " no llega a " + text(best["name"]) + ", pero si mejora a "},
			bold(text(worst["name"])), {T: why(worst) + fmt.Sprintf(": +%.2f xPts ", gap(him, worst)) +
				money(him, worst) + "."}}
	}
	return []Seg{bold(name), {T: " no mejora a ninguno de tus " + line + ": " +
		text(worst["name"]) + fmt.Sprintf(" ya le saca %.2f xPts.", math.Abs(gap(him, worst)))}}
}
