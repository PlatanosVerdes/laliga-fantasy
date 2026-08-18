// Package render builds the page. A port of fantasy/report.py, and the primitives come
// first: every table on the page is these formatters repeated, so if a number is spelled
// differently here than there, every section differs and the diff is useless.
//
// Each one is compared against its Python original over a table of inputs, including the
// edges that look like nothing and are not — 999,500 rounds to "1.000K" in a naive
// implementation and must read "1.00M".
package render

import (
	"fmt"
	"math"
	"strings"
)

// Em dash for absent values, as the page has always used.
const Missing = "—"

// Esc escapes exactly as Python's html.escape does, which is not what Go's
// html.EscapeString does: Python writes &#x27; and &quot; where Go writes &#39; and &#34;.
// Both are valid HTML and neither renders differently, but the pages have to be
// comparable byte for byte, and a difference nobody can see is the worst kind to leave in
// a diff nobody can read.
func Esc(text string) string {
	return escaper.Replace(text)
}

var escaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;",
)

// Money is the page's money format: millions with two decimals, thousands with none, and
// dots as thousands separators.
func Money(value *float64) string {
	if value == nil {
		return Missing
	}
	amount := *value
	sign := ""
	if amount < 0 {
		sign = "-"
	}
	amount = math.Abs(amount)
	switch {
	// 999,500 upwards is written as millions: rounding it to "1.000K" would be a
	// thousand-fold lie in the unit.
	case amount >= 999_500:
		return sign + millions(amount/1e6) + "M"
	case amount >= 1e3:
		return sign + group(fmt.Sprintf("%.0f", amount/1e3)) + "K"
	default:
		return sign + group(fmt.Sprintf("%.0f", amount))
	}
}

func Pct(value *float64) string {
	if value == nil {
		return Missing
	}
	return fmt.Sprintf("%+.2f%%", *value)
}

func Num(value *float64, digits int) string {
	if value == nil {
		return Missing
	}
	return fmt.Sprintf("%.*f", digits, *value)
}

// millions writes 9.867 as "9.87" and 130.96 as "130.96": a dot decimal, and dots for
// thousands as well, because Python builds this by formatting with commas as thousands
// separators and then replacing every comma with a dot. Over a thousand million that
// yields "1.234.57M", which is ambiguous — and unreachable here, where the largest figure
// in the game is a team worth some 130M. Matching it is parity, not endorsement.
func millions(amount float64) string {
	text := fmt.Sprintf("%.2f", amount)
	whole, fraction, _ := strings.Cut(text, ".")
	return group(whole) + "." + fraction
}

// group inserts dots every three digits, from the right.
func group(text string) string {
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	var parts []string
	for len(text) > 3 {
		parts = append([]string{text[len(text)-3:]}, parts...)
		text = text[:len(text)-3]
	}
	parts = append([]string{text}, parts...)
	out := strings.Join(parts, ".")
	if negative {
		return "-" + out
	}
	return out
}

// DivergingBar is the sign and magnitude of the projected value change, with the number
// beside it. Two poles off a neutral midpoint, and the label is always rendered: the
// colour is a second reading of the same fact, never the only one.
func DivergingBar(pct *float64, scale float64) string {
	if pct == nil {
		return `<span class="bar-cell"><span class="bar-num">` + Missing + `</span></span>`
	}
	value := *pct
	// Each arm owns half the track, so the fill can never spill over the label.
	width := math.Min(50.0, math.Abs(value)/scale*50.0)
	side := "neg"
	if value >= 0 {
		side = "pos"
	}
	label := Pct(&value)
	tip := "Proyeccion a 7 dias: " + label
	if math.Abs(value) >= scale {
		tip += " (al tope de la escala)"
	}
	return fmt.Sprintf(`<span class="bar-cell" title="%s">`+
		`<span class="bar-num %s">%s</span>`+
		`<span class="bar-track" role="presentation">`+
		`<span class="bar-fill %s" style="width:%.1f%%"></span>`+
		`</span></span>`, Esc(tip), side, label, side, width)
}

// MagnitudeBar is a single-hue magnitude bar with its number, for points per million: one
// hue, one axis, a thin mark anchored at zero. A sequential encoding rather than a
// categorical one, so it needs no colour-blindness pairing.
func MagnitudeBar(value *float64, scale float64, digits int) string {
	if value == nil {
		return `<span class="bar-cell"><span class="bar-num">` + Missing + `</span></span>`
	}
	amount := math.Max(0.0, *value)
	width := math.Min(100.0, amount/scale*100.0)
	return fmt.Sprintf(`<span class="bar-cell" title="%s">`+
		`<span class="bar-num">%.*f</span>`+
		`<span class="mag-track"><span class="mag-fill" style="width:%.1f%%"></span></span>`+
		`</span>`,
		Esc(fmt.Sprintf("%.3f puntos esperados por millon", amount)),
		digits, amount, width)
}

// Sparkline is the value history, omitted rather than faked when there is not enough of it.
func Sparkline(series []float64, width, height int) string {
	if len(series) < 5 {
		return ""
	}
	low, high := series[0], series[0]
	for _, value := range series {
		low = math.Min(low, value)
		high = math.Max(high, value)
	}
	span := high - low
	if span == 0 {
		span = 1.0
	}
	step := float64(width) / float64(len(series)-1)

	points := make([]string, 0, len(series))
	for index, value := range series {
		y := float64(height) - 2 - (value-low)/span*(float64(height)-4)
		points = append(points, fmt.Sprintf("%.1f,%.1f", float64(index)*step, y))
	}
	pole := "pole-neg"
	if series[len(series)-1] >= series[0] {
		pole = "pole-pos"
	}
	first, last := series[0], series[len(series)-1]
	return fmt.Sprintf(`<svg class="spark" width="%d" height="%d" viewBox="0 0 %d %d" `+
		`aria-label="Historico de valor: %s a %s">`+
		`<polyline points="%s" fill="none" `+
		`stroke="var(--%s)" stroke-width="2" `+
		`stroke-linecap="round" stroke-linejoin="round"/></svg>`,
		width, height, width, height, Money(&first), Money(&last),
		strings.Join(points, " "), pole)
}

// NumericKinds are right-aligned, and the page's sort code reads the same list.
var NumericKinds = map[string]bool{
	"money": true, "pct": true, "num": true, "int": true, "pct_plain": true,
	"spark": true, "verdict": true, "mag": true, "ideal": true, "hours": true,
	"ratio": true,
}

// Column is a header, how to read the value out of a row, and how to render it.
type Column struct {
	Header string
	Read   func(map[string]any) any
	Kind   string
}

// Table renders the sortable table the whole page is made of. The data-sort attribute is
// what makes a column sortable without shipping the data twice.
func Table(columns []Column, rows []map[string]any, empty string) string {
	if len(rows) == 0 {
		return `<p class="empty">` + Esc(empty) + `</p>`
	}
	var head strings.Builder
	for _, column := range columns {
		class := ""
		if NumericKinds[column.Kind] {
			class = ` class="right"`
		}
		fmt.Fprintf(&head, `<th data-kind="%s"%s>%s</th>`, column.Kind, class,
			Esc(column.Header))
	}

	var body strings.Builder
	for _, row := range rows {
		body.WriteString("<tr>")
		for _, column := range columns {
			inner, sortKey := Cell(column.Read(row), column.Kind)
			class := ""
			if NumericKinds[column.Kind] && column.Kind != "pct" {
				class = ` class="num"`
			}
			if column.Kind == "pct" {
				class = ` class="bar"`
			}
			fmt.Fprintf(&body, `<td%s data-sort="%s">%s</td>`, class, Esc(sortKey), inner)
		}
		body.WriteString("</tr>")
	}
	return `<div class="table-wrap"><table class="sortable"><thead><tr>` + head.String() +
		"</tr></thead><tbody>" + body.String() + "</tbody></table></div>"
}

// Cell returns (inner HTML, sort key) for one value.
func Cell(value any, kind string) (string, string) {
	number := asFloat(value)
	switch kind {
	case "money":
		return Esc(Money(number)), sortKey(number)
	case "pct":
		return DivergingBar(number, 12.0), sortKey(number)
	case "num":
		return Esc(Num(number, 2)), sortKey(number)
	case "mag":
		return MagnitudeBar(number, 1.0, 2), sortKey(number)
	case "int":
		if number == nil {
			return Missing, sortKey(number)
		}
		return fmt.Sprintf("%d", int64(*number)), sortKey(number)
	case "spark":
		// Sorted by how much history there is, not by a value: the column is a shape.
		series := asSeries(value)
		return Sparkline(series, 74, 20), fmt.Sprintf("%d", len(series))
	}
	// The fallback is the text case, and the sort key is the *escaped* text: the browser
	// reads it out of an attribute, so it has to survive being written into one.
	if value == nil || value == "" {
		return Missing, Esc("")
	}
	return Esc(fmt.Sprint(value)), Esc(fmt.Sprint(value))
}

// sortKey is the numeric sort value the browser reads, rendered as Python's str(float(x)).
func sortKey(number *float64) string {
	amount := 0.0
	if number != nil {
		amount = *number
	}
	if amount == math.Trunc(amount) && math.Abs(amount) < 1e15 {
		return fmt.Sprintf("%.1f", amount)
	}
	return fmt.Sprintf("%v", amount)
}

func asFloat(value any) *float64 {
	switch typed := value.(type) {
	case nil:
		return nil
	case float64:
		return &typed
	case int:
		converted := float64(typed)
		return &converted
	case int64:
		converted := float64(typed)
		return &converted
	}
	return nil
}

func asSeries(value any) []float64 {
	switch typed := value.(type) {
	case []float64:
		return typed
	case []any:
		out := make([]float64, 0, len(typed))
		for _, item := range typed {
			if number := asFloat(item); number != nil {
				out = append(out, *number)
			}
		}
		return out
	}
	return nil
}
