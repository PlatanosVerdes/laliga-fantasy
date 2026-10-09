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
	"sort"
	"strconv"
	"strings"
	"time"
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

// Positions carry a colour on the page, chosen by Jorge: keeper orange, defence lilac,
// midfield turquoise, attack yellow. Only the slug matters here; the hues live in the CSS.
var positionSlug = map[int]string{1: "por", 2: "def", 3: "med", 4: "del", 5: "ent"}

// The API reports status in English; the page is in Spanish.
var statusLabels = map[string]string{
	"injured": "lesionado", "doubtful": "duda", "sanctioned": "sancionado",
	"suspended": "sancionado", "out_of_league": "fuera de la liga",
	"unknown": "sin datos",
}

// AllMine are the sections where every row is yours by definition: repeating "mio" there
// is noise.
var AllMine = map[string]bool{
	"plantilla": true, "ventas": true, "ofertas": true, "misventas": true,
	"vencimientos": true, "riesgo": true, "siempre": true,
}

// Crests holds the team badges as data URIs, keyed by team id. Filled by the caller,
// because the images come from the API and this package does not fetch.
var Crests = map[string]string{}

// Star is the favourite toggle. Interactive when served; a static file just shows state.
func Star(row map[string]any) string {
	on := truthy(row["starred"])
	class, pressed, title, glyph := "", "false", "Marcar como favorito", "☆"
	if on {
		class, pressed, title, glyph = " on", "true", "Quitar de favoritos", "★"
	}
	return fmt.Sprintf(`<button class="star%s" data-player="%s" data-name="%s" type="button" `+
		`aria-pressed="%s" title="%s">%s</button>`,
		class, Esc(text(row["id"])), Esc(text(row["name"])), pressed, title, glyph)
}

// Starts is the share of matches a player has started, as a coloured pill. The number is
// always written, so the colour is a second reading and never the only one.
func Starts(value *float64) (string, string) {
	if value == nil {
		return Missing, "-1"
	}
	share := int(*value)
	status := "critical"
	switch {
	case share >= 75:
		status = "good"
	case share >= 50:
		status = "warning"
	case share >= 30:
		status = "serious"
	}
	return fmt.Sprintf(`<span class="pill-%s">%d%%</span>`, status, share),
		fmt.Sprintf("%d", share)
}

// PlayerCell is the name plus everything you need to know before clicking it: crest,
// position colour, team, and the flags that change what the row means — unavailable,
// doubtful, priced-by-prior, and whether he is already yours.
//
// `section` is passed rather than read from a global: "mio" is noise in a table where
// every row is yours.
func PlayerCell(row map[string]any, section string) string {
	teamID := text(row["team_id"])
	badge := ""
	if _, known := Crests[teamID]; known {
		team := text(row["team"])
		badge = fmt.Sprintf(`<span class="crest crest-%s" role="img" aria-label="%s" `+
			`data-tip="%s"></span>`, Esc(teamID), Esc(team), Esc(team))
	}
	slug := positionSlug[int(number(row["position_id"]))]
	if slug == "" {
		slug = "ent"
	}

	var flags []string
	if !truthy(row["available"]) {
		status := text(row["status"])
		label := statusLabels[status]
		if label == "" {
			label = status
		}
		flags = append(flags, `<span class="flag-critical">`+Esc(label)+`</span>`)
	} else if text(row["status"]) == "doubtful" {
		flags = append(flags, `<span class="flag-warning">duda</span>`)
	}
	if truthy(row["prior_based"]) {
		flags = append(flags,
			`<span class="flag-muted" data-tip="Sin historico: estimado por precio">est.</span>`)
	}
	if truthy(row["is_mine"]) && !AllMine[section] {
		flags = append(flags, `<span class="flag-mine">mio</span>`)
	}
	if truthy(row["sale_locked"]) {
		until := text(row["hold_until"])
		if len(until) > 10 {
			until = until[:10]
		}
		// A padlock rather than two words: it repeats on many rows and the words were louder
		// than the fact. The title still says it in full, and the yellow underline is what
		// tells this lock apart from the clause one.
		flags = append(flags, fmt.Sprintf(
			`<span class="flag-hold" data-tip="Norma de la liga: recien fichado, `+
				`no se puede vender hasta el %s" aria-label="Recien fichado">🔒</span>`, Esc(until)))
	}

	// The three-letter team only earns its space when there is no crest: with one, it is the
	// same fact printed twice.
	team := ""
	if badge == "" {
		team = text(row["team_short"])
		if team == "" {
			team = text(row["team"])
		}
	}
	meta := ""
	if team != "" {
		meta = `<span class="p-meta">` + Esc(team) + `</span>`
	}
	return fmt.Sprintf(`<span class="p-cell">%s`+
		`<button class="p-name" type="button" data-detail="%s">%s</button>`+
		`<span class="pos pos-%s">%s</span>`+
		`%s%s</span>`,
		badge, Esc(text(row["id"])), Esc(text(row["name"])), slug,
		Esc(text(row["position"])), meta, strings.Join(flags, ""))
}

// text is for identifiers and names: an id that arrived as 1300.0 has to read "1300", so a
// whole float loses its decimals here. Anything else goes through PyText.
func text(value any) string {
	if value == nil {
		return ""
	}
	if asString, ok := value.(string); ok {
		return asString
	}
	if pointer, ok := value.(*string); ok {
		if pointer == nil {
			return ""
		}
		return *pointer
	}
	if asFloat := number(value); asFloat == math.Trunc(asFloat) {
		return fmt.Sprintf("%d", int64(asFloat))
	}
	return PyText(value)
}

func number(value any) float64 {
	if converted := asFloat(value); converted != nil {
		return *converted
	}
	return 0
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case *bool:
		return typed != nil && *typed
	case *string:
		return typed != nil && *typed != ""
	case *float64:
		return typed != nil && *typed != 0
	case float64:
		return typed != 0
	case string:
		return typed != ""
	}
	return false
}

// Tabs are the page's groups. Rendered only when there is a session, because a chip
// that jumps nowhere is worse than no chip.
const Tabs = `<div class="tabs" id="tabs" role="tablist">` +
	`<button class="tab" role="tab" data-tab="decidir" aria-selected="false" type="button">Decidir</button>` +
	`<button class="tab" role="tab" data-tab="comprar" aria-selected="false" type="button">Comprar</button>` +
	`<button class="tab" role="tab" data-tab="vender" aria-selected="false" type="button">Vender</button>` +
	`<button class="tab" role="tab" data-tab="clausulas" aria-selected="false" type="button">Cláusulas</button>` +
	`<button class="tab" role="tab" data-tab="plantilla" aria-selected="false" type="button">Plantilla</button>` +
	`<button class="tab" role="tab" data-tab="partidos" aria-selected="false" type="button">Partidos<`+
	`/button>`+
	`<button class="tab" role="tab" data-tab="rivales" aria-selected="false" type="button">Rivales</button>` +
	`<button class="tab" role="tab" data-tab="liga" aria-selected="false" type="button">Liga</button>` +
	`<button class="tab" role="tab" data-tab="ranking" aria-selected="false" type="button">Ranking</button>` +
	`<button class="tab" role="tab" data-tab="comparador" aria-selected="false" type="button">Comparador</button></div>`

// CompareShell is the comparator's tab. The browser fills it from the tray it keeps, so the
// live refresh must leave its inside alone (CLIENT_OWNED in report.js).
const CompareShell = `<section id="comparador" data-tab="comparador">` +
	`<h2>Comparador</h2>` +
	`<div class="cmp-bar"><div class="cmp-find-wrap"><input class="cmp-find" type="search" ` +
	`autocomplete="off" spellcheck="false" placeholder="añadir jugador…" ` +
	`aria-label="Buscar jugador para comparar"><div class="cmp-results" hidden></div></div>` +
	`<div class="cmp-mine-wrap"><button type="button" class="cmp-mine">Mi plantilla</button>` +
	`<div class="cmp-results cmp-mine-list" hidden></div></div>` +
	`<button type="button" class="cmp-clear">Vaciar</button><span class="cmp-msg"></span></div>` +
	`<div class="cmp-body"><p class="empty">Busca jugadores arriba o pulsa ` +
	`<b>+ comparar</b> en la ficha de cualquiera.</p></div></section>`

// Build is the version of the binary serving this page, stamped at compile time. Empty renders
// nothing: a page built by hand should not claim a version it does not have.
var Build string


// Stat is one of the four cards under the tab bar: a label, one figure and a line under it.
type Stat struct {
	Icon, Label, Value, Small, Note string
	// ValueID lets the live refresh rewrite the figure without re-rendering the strip.
	ValueID string
	// Deadline turns the figure into a live countdown, outlined in red in its last six hours.
	Deadline string
	Tab      string
}

func StatCard(stat Stat) string {
	attrs := ""
	if stat.ValueID != "" {
		attrs += ` id="` + Esc(stat.ValueID) + `"`
	}
	if stat.Deadline != "" {
		attrs += ` data-deadline="` + Esc(stat.Deadline) + `" data-plain="1"`
	}
	value := Esc(stat.Value)
	if stat.Small != "" {
		value += ` <small>` + Esc(stat.Small) + `</small>`
	}
	label := Esc(stat.Label)
	if stat.Icon != "" {
		label = stat.Icon + " " + label
	}
	inner := `<span class="k">` + label + `</span><span class="v"` + attrs + `>` + value +
		`</span><span class="s">` + Esc(stat.Note) + `</span>`
	if stat.Tab != "" {
		return `<button class="stat" type="button" data-goto="` + Esc(stat.Tab) + `">` + inner +
			`</button>`
	}
	return `<div class="stat">` + inner + `</div>`
}

// Header is the tab bar, with the live dot, the balance and the search on its right, and the
// four cards under it. The live dot starts off: a static file is honest about not being live,
// and the script turns it on when the push channel connects.
func Header(stats []string, withTabs bool, cashAmount *float64) string {
	tabs := ""
	if withTabs {
		tabs = Tabs
	}
	// The exact balance, for the arithmetic a typed amount needs; the Caja card shows it.
	attrs := ""
	if cashAmount != nil {
		attrs = fmt.Sprintf(` data-cash="%.0f"`, *cashAmount)
	}
	// The version rides in the live dot's tooltip rather than taking room in the bar.
	build := ""
	if Build != "" {
		build = ` data-build="` + Esc(Build) + `"`
	}
	tip := strings.TrimSuffix("Sin conexión en vivo · "+Build, " · ")
	find := `<div class="head-find"><button class="find-btn" type="button" ` +
		`aria-label="Buscar jugador" data-tip="Buscar jugador (tecla /)">` +
		`<svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="7" cy="7" r="4.6"/>` +
		`<path d="M10.4 10.4l3.6 3.6"/></svg></button>` +
		`<div class="find-pop" hidden><input id="find" class="head-input" type="search" ` +
		`autocomplete="off" spellcheck="false" placeholder="buscar jugador…" ` +
		`aria-label="Buscar un jugador y abrir su ficha">` +
		`<div class="cmp-results find-results" hidden></div></div></div>`
	strip := ""
	if len(stats) > 0 {
		strip = `<div class="strip">` + strings.Join(stats, "") + `</div>`
	}
	return `<div class="topbar"` + attrs + `>` + tabs + `<div class="topright">` +
		`<span id="live-dot" class="live-off"` + build + ` data-tip="` + Esc(tip) + `"></span>` +
		versionTag() + find + `</div></div>` + strip
}

// PageFoot is where the page comes from and what the server may do, and the figures that did
// not make the four cards, folded.
func PageFoot(generated, leagueName string, week int, mode string) string {
	league := ""
	if leagueName != "" {
		league = ` · liga <strong>` + Esc(leagueName) + `</strong>`
	}
	return `<header class="topline"><h1>LaLiga Fantasy</h1>` +
		`<p>` + Esc(generated) + league + fmt.Sprintf(` · jornada %d</p>`, week) +
		`<span class="live"><span id="live-stamp">estatico</span></span>` +
		modeChip(mode) + `</header>`
}

// Footer says what the numbers are and what they are not. xPts is an estimate of ours, and
// the page has to say so where somebody about to spend money will read it.
func Footer(currentWeight float64) string {
	return "<footer>Datos: API oficial de LaLiga Fantasy y futbolfantasy.com. " +
		"<code>xPts</code> es una estimacion propia: puntos por jornada de la temporada pasada " +
		fmt.Sprintf("y de la actual (peso actual %.0f%%), ajustados por ", currentWeight*100) +
		"probabilidad de ser titular, dificultad del proximo rival y confianza del dato. " +
		"<code>est.</code> marca a quien no tiene historico y se estima por precio. " +
		"El barrido de valor a 7 dias es una proyeccion amortiguada, no una promesa. " +
		"Herramienta de consulta: no ejecuta ninguna operacion.</footer>"
}

// CrestCSS is one rule per team rather than a data URI repeated in every row: the same
// badge appeared 241 times and the page weighed 1.8 MB.
func CrestCSS() string {
	ids := make([]string, 0, len(Crests))
	for id := range Crests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var css strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&css, `.crest-%s{background-image:url(%s)}`, id, Crests[id])
	}
	return css.String()
}

// Page assembles the whole document: head, body, the two dialogs and the script. The
// wrapper order matters — the modal and the drawer live outside the wrap so they can cover
// it.
// Favicon is the LaLiga mark, embedded rather than linked: the report is also a single file
// you can open from disk, and a file that has to fetch its own icon is not one file.
const Favicon = `<link rel="icon" type="image/png" href="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAIAAAACACAYAAADDPmHLAAAAAXNSR0IArs4c6QAAAERlWElmTU0AKgAAAAgAAYdpAAQAAAABAAAAGgAAAAAAA6ABAAMAAAABAAEAAKACAAQAAAABAAAAgKADAAQAAAABAAAAgAAAAABIjgR3AAAKSklEQVR4Ae1dX28VRRSfS6FAEagQ/0aTexMxBoi9RB/ARL311QfLmz4hn6DlE9B+Ato33tpvQPkCtMQHfFB7SQSNmvQaiYoGvda0VltSz++WQ5fL3u3uzLm7M7Mzye3s7M7Ozvmd354zc3Z2W1EiaXhYqY0xampEqa0q5XX60b7Oj7KQBBBoKVWhn2rS77ZS+xaVaqNslCr6Z0Pp/41TpxqkdPqFlD8CFZBhxoQMGgRgxasJujiRICQ7EKjMERGmslqFjAQYukzCBsXbofFevZhUao2IkC6lJMCBqlID18jU19M1G2oVjEBLqcHRNNZgz+4dPXRBqT1LQfm7I2VRjSqNz0hnh8d269NAcoWOyZ+mOgeS64WjFiJAOtv6mMYF1LWNm736l0CAjvIne50Y9juDQCOJBD0IANOxddUZEUNHd0OASDDYIktA8YMnU8wgEAM++PwwxXsSKudLbSLBme6BYQwBhpZJ1Krz4gYBYhBA4GiVSLCTulxAx++T+Q/JUwRepPEA3fQbiyxfxAJ0TD/u/pD8RgCuoEaugHKK7uzIOniFtus75bDlKQI0PXz4L1uBRxYg3P2eKruXWI+twKNI4ECjV82w30sEhilSOAHJOBQ87qWYQagEBCrv4yC5gGD+E1Dy/NDgs2QBgvn3XMsJ4m2MwQWEkX8CRJ4fqoMAI54LGcTricBWlQjQWcTZs0o44DUCIzQIHNryWsQgXBIC7UCAJHhKcAxjgJBKjEAgQImVD9EDAQIBSo5AycUPFiAQoOQIlFz8YAECAUqOQMnF3+uy/NWh/Wrp3RE1vNdMjKnvf1KT3/2kBcXsyGvq01ee1zq3+6TFP1bU6K2vu3f3tey0C1g4e9pY+a1//tVW/njtJTHlox8Xmz/0VdlxjTtLgMsnXlXVg/vjZMq0b/TWnUz1uTKsz+Trr3LROJ8iC9T6Z924nawNOEkAKfBh+nVBl7A+rKy5e7+ruXu/cTHX3EkCAHzTBH+r6/elrA9kgOm/dLe41zGcI4AE+Cb+Vsr6MIHPf/Gtam9scjH33CkCNI4fFfG7uv52eN9eJWF9WMtwQc2VVS4WkjtDAICPKZdpMvG3EtaH+28y++A2JHJnCHDlZNV41A/QcffrJMz1J2jaJ5V0Zx9S1+d2nCAAwJcItly8/YPWqB9+/7LglA+DPt3ZBytOKreeAFLgw98uPvhLCzdJ0z9//w81vfyLVj/6cZL1BJAA38TfSkf7Lt1p9UOP2m1aTQAp06/rb6WnfLqzD23tpjjRWgIA/CunqilESK5iS7Rvhsx+UdG+JISsJcDsmyeMH/TA39oS7ZukMYiNyUoCwO83jh8xwgt+X9ff1o8cEgk4sQBwQUVG+7gfcbl1BJDyu7r+FgGna2+/EYeV1j4TF6R1wYwnWUcAiVCrSbRPIuDEOjCZfXAb/c6tIoDUlE/36ZrUrANKa29u0uoevbUG/VZ6tH1rCCBl+nWfruH6ktE+XRcUVU4e21YQQOopm8nTtSsna8bPGlhhcEE2Rfu4X3G5FQSQMv0mU76xF47F4ZN5n8kDp8wXEzihcALA75o+ZTPxt1Kuh3Xhiunn/hZKACm/awK6xKyDwbQ12sf9i8sLJYCE6TfxtxLXZ1A7Uz5Lo33cx7i8MAJITLlM/K3U8jIG1eZoH/cxLi+EAEWbflxfYnkZA2p7tI/7GZcXQgAJ02vibyWuz2C6EO3jvsbluRMAdx/Mv0ky8bcSrof7bjL74DaKznMnAO4+06Trb6VcD/ffZPbBbRSd506A+tFnjGQ28bdYYyDxPiEEMJl9GAEgfHL+BDg8pC2Cib+F5TFdY8AdN5l9cBu25GYv1ucsBYCfHTmR+arXf6WVQZLLumlhpy3LujOD0XVC7gSAEnXNcOMYrRLKELKHmYafXjh3qkts/SJmH/P3H+g3YNmZubuAm5pr87PgBsXXbnylLt7+Xl145TltwnVfE+SdKPBN3u7+SJRz/1YwInALZ+XuyCgIfMezeZa+FkjFbUev6/J27i4Ab+csPlgRG5ABfLzrD1MfffMnRPvS0TJ3C4BuYQyw9J75x53iFM9iS368qbmyps581uSmvcpzHwMAve1p1D1tIDtf0/r8TueLWtG7nhuUjvZhmZmvKXcXwEBOL/+sYKbHq+lfuU6647ld6Wgf3i3wze8zVsgLcQHRDiy9W1f1I8nBoTSK5zaxwEMq4INBJWYSPqdCXEAU0PNffNNxCdF9vL2bqed6nIdoHyORPi/cAqCr3dO1LHc8iwrTvzz6FheN81EaY8SNL4wbtqyBwsYAURwA9KW7LfXRi8eems5F6yVtS67twwOnMigfeFphAZIUm+YYTL9UrB8zlNqNL9Nc1os6hY8BTFHsx9o+0z65dL7TBAjRPnOqOU0AmH7dJ4vd0GHgqftmUXdbLpWdJYBktA9+v4hPtdtAFGcJUMY3eftBGCcJgLtfyvQj2mfjx5v6oey4Np0kAOIFEmn7oZSdH2+SkC9NG04SQCrWr/vp2DTAulLHikhgVrBM/0kUrjdPC0WREEfQTfjUu61f/0ork5ORwK0P30krX1/q4fuDvjwmdtICNP9eU3WD9wt0WaHzkEr3Wnmd5yQBrv/6IFcC+Kh4JpiTLgBTwOUP5B79Mhjduc+KZ1mdnAVg+jbT6t8396F4rAfAf/H0/bGwkxYA7MWn5fBvY6UCQmizDHc85IwmZwkAIfBRZ7z2ZTotLKPimQROEwBCTNReVvi+r04qs+IZL+cJAEGmT9XEl5czQL7nXhAASpJeXu674lk+bwiAweDCudOxg8Jg6lndT+feEACidS8v56d9ZX7c+7TKn9zjFQEgGgaF+FdveFs4KP5JZceVvCNAnJBhX28EnIwE9hYnHMmKQCBAVsQ8qx8I4JlCs4oTCJAVMc/qBwJ4ptCs4oAA7awnhfr+IBAI4I8udSRpgQB+fv5KB47SnVMBASo/lk7uIDAjcDtYAIainHmTQsHDw0r992c55S+71IM1sgBtmgVUFssORQnlp7FfuzMIJNm3bpYQgJKLXJkBAOQCkDpuYBkbnWL4UwIEBmsRCwA3oDqMKIHkQURVmYPyAcQjC4DNYAWAQjnS9t0PWQd2BF5fV2rfQSo3dvaFLQ8RmFLq73mWK2IBeNehJRoU1rkUcq8QaCm1Rr5/JyEQ1JUenqcdGBOE5BcCpNPB0W6RIi6AD22i4n0qjfGekPuAwMAnSq183i1JDAFQZaNJ4wG4hwZKITmPwJRSq1fjpOhBAFTdWAwkiIPMuX2k/LXJXr3GXb5LOkyu4OEsVRrepWI4bBcCbZrlX6I7fy6pWykIgNMPVJXas0AblIdkPwKVJllvGsxvB3uS+hszC4irvt56NH0gcxKSxQjQXa9IR6tn0igfcqS0AFGRO9ZgkvZciO4N24UiAMXP0OxtmhSP7dRJgwDcNogw0KDSeAgcMSZ553iMjye52RXPPTUgADeB/DEZ6lQYoU5VKccvJBkEcFfj1ySj/eN2vm8+690e15X/AXSKpy2qakiZAAAAAElFTkSuQmCC">`

func Page(css, js, crestCSS, header, body, footer, modal, drawer string) string {
	// Without it a phone lays the page out at a made-up 980px and shrinks it: 13px text
	// arrives at five real pixels, and no rule inside the document can undo that.
	return `<meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<title>Fantasy</title>` + Favicon + `<style>` + css + crestCSS + `</style>` +
		`<div class="wrap">` + header + body + footer + `</div>` +
		modal + drawer +
		// Ahead of the inline script, which looks this tag up to import the same module.
		`<script type="module" src="/assets/ui/main.js?v=` + Esc(Build) + `"></script>` +
		`<script>` + js + `</script>`
}


// modeChip is the one-word answer to what the server may do. Coloured by how much it can act,
// because "solo lectura" and "auto" are opposite ends of the same question.
func modeChip(mode string) string {
	if mode == "" {
		return ""
	}
	class := "mode-manual"
	switch mode {
	case "auto":
		class = "mode-auto"
	case "solo lectura", "informe":
		class = "mode-read"
	}
	// data-mode is how the page knows it is being served rather than opened as a file.
	return fmt.Sprintf(`<span class="mode %s" data-mode="%s" title="Que puede hacer este `+
		`servidor: auto ejecuta las instrucciones permanentes, manual solo lo que pulses, `+
		`solo lectura nada">Mode: <b>%s</b></span>`, class, Esc(mode), Esc(mode))
}

// feedHidden are the log's per-matchday and league-wide notices (types 7 and 10): no money and no
// player moves, so they are not movements.
var feedHidden = map[int]bool{7: true, 10: true}

// ShieldType is the log's shield bought (see model.ShieldType): it moves no money.
const ShieldType = 4

// Verdicts are the five recommendations, each with its glyph and status. The glyph is not
// decoration: it is what carries the meaning where the colour cannot be seen.
var Verdicts = map[string]struct{ Label, Icon, Status string }{
	"buy":     {"Fichar", "▲", "good"},
	"bidding": {"Pujado", "●", "neutral"},
	// Money offered for one of yours: good news when you can take it, and worth saying
	// out loud when you cannot, because the reason is a rule and not a price.
	"cash":         {"Cobrar", "€", "good"},
	"cash_blocked": {"No puedes", "€", "neutral"},
	"clause":  {"Clausulazo", "◆", "good"},
	"protect": {"Subir clausula", "!", "warning"},
	"sell":    {"Vender", "▼", "serious"},
	"out":     {"Baja", "✕", "critical"},
}

// VerdictOrder is the sort order, worst first, so a table sorted by the column reads as a
// severity list rather than alphabetically.
// Offers first after the emergencies: they are the only rows with somebody else's money on a
// clock, and the clock runs whether or not the page was open.
var VerdictOrder = []string{"out", "cash", "buy", "bidding", "clause", "protect", "sell",
	"cash_blocked"}

var powerStatus = map[string]string{"holgado": "good", "normal": "neutral", "justo": "critical"}

var raidStatus = map[string]string{
	"chollo": "good", "renta": "good", "justo": "warning", "caro": "critical",
	"no te llega": "critical", "sin datos": "neutral", "sin referencia": "neutral",
}

func VerdictBadge(verdict string) string {
	spec, known := Verdicts[verdict]
	if !known {
		return Missing
	}
	return fmt.Sprintf(`<span class="badge-%s"><span class="badge-icon" aria-hidden="true">`+
		`%s</span>%s</span>`, spec.Status, spec.Icon, spec.Label)
}

// RaidVerdict is whether paying this clause beats what you already own — not whether it is
// cheap, which is a different question and the one that misleads.
func RaidVerdict(row map[string]any) string {
	verdict := text(row["verdict"])
	if verdict == "" {
		return Missing
	}
	status := raidStatus[verdict]
	if status == "" {
		status = "neutral"
	}
	note := ""
	if ratio := asFloat(row["vs_market"]); ratio != nil && *ratio != 0 {
		// "5.0x tu plantilla" does not say of what, so the title spells it out with both of
		// the figures being compared.
		ppm := number(row["ppm_at_clause"])
		explain := fmt.Sprintf("Pagando su clausula sacas %.2f pts/M; la mediana de tu "+
			"plantilla es %.2f pts/M, asi que rinde %.1f veces mas por cada millon.",
			ppm, ppm / *ratio, *ratio)
		note = fmt.Sprintf(`<span class="pill-note" data-tip="%s">%s</span>`,
			Esc(explain), Esc(fmt.Sprintf("%.1fx pts/M de tu plantilla", *ratio)))
	}
	return fmt.Sprintf(`<span class="pill-%s">%s</span>`, status, Esc(verdict)) + note
}

// PlayerLink is a player's name, clickable when we know his id. Same idea as ManagerLink and for
// the same reason: the name shows up in the feed and in tables that are not player tables, and in
// all of them the next question is "who is he".
func PlayerLink(name, playerID string) string {
	if name == "" {
		return Missing
	}
	if playerID == "" {
		return Esc(name)
	}
	return fmt.Sprintf(`<button class="p-name" type="button" data-detail="%s">%s</button>`,
		Esc(playerID), Esc(name))
}

// ManagerLink is a manager's name, clickable when we know which team is his. One place decides
// it, because the name shows up in seven tables and the feed and they all want the same thing.
func ManagerLink(name, teamID string) string {
	if name == "" {
		return Missing
	}
	if teamID == "" {
		return Esc(name)
	}
	return fmt.Sprintf(`<button class="p-name" type="button" data-manager="%s">%s</button>`,
		Esc(teamID), Esc(name))
}

// buyerUser and sellerUser read the two sides of an event. cash == -1 means user1 paid, which
// loadActivity already used to fill buyer and seller, so the mapping has to match it.
func feedDate(event map[string]any) string {
	date := strings.ReplaceAll(text(event["date"]), "T", " ")
	if len(date) > 16 {
		date = date[:16]
	}
	return date
}

func buyerUser(event map[string]any) string {
	if text(event["buyer"]) == "" {
		return ""
	}
	if pays(event) {
		return text(event["user1"])
	}
	return text(event["user2"])
}

func sellerUser(event map[string]any) string {
	if text(event["seller"]) == "" {
		return ""
	}
	if pays(event) {
		return text(event["user2"])
	}
	return text(event["user1"])
}

// pays is whether user1 is the one who paid: types 1 (traspaso) and 31 (compra).
func pays(event map[string]any) bool {
	switch int(number(event["type_id"])) {
	case 1, 31:
		return true
	}
	return false
}


// PowerBadge is who can actually buy right now. Named, not just coloured.
func PowerBadge(row map[string]any) string {
	power := text(row["power"])
	if power == "" {
		return Missing
	}
	status := powerStatus[power]
	if status == "" {
		status = "neutral"
	}
	return fmt.Sprintf(`<span class="pill-%s">%s</span><span class="pill-note">%s</span>`,
		status, Esc(power), Esc(text(row["power_note"])))
}

// BidButton is only rendered where a bid is actually possible; the server re-validates
// anyway, because a page can be minutes old by the time somebody clicks.
// RiskBar is an estimated chance as a bar: the number reads first and the fill is the second
// reading, the same way every other magnitude on the page is drawn.
func RiskBar(share float64) string {
	percent := math.Max(0, math.Min(100, share*100))
	return fmt.Sprintf(`<span class="bar-cell" title="%s">`+
		`<span class="bar-num">%.0f%%</span>`+
		`<span class="mag-track"><span class="mag-fill" style="width:%.1f%%"></span></span>`+
		`</span>`,
		"probabilidad estimada de que alguien pague su cláusula al abrirse la ventana",
		percent, percent)
}

// RaiseButton is the recommendation as a click: the amount is already computed, so the button
// carries it and the modal opens on the confirmation rather than on an empty field.
func RaiseButton(row map[string]any) string {
	pay := number(row["pay"])
	if pay <= 0 || text(row["verdict"]) == "dejalo ir" || text(row["verdict"]) == "no compensa" {
		return Missing
	}
	return fmt.Sprintf(`<button class="raise" data-raise="%s" data-raise-name="%s" `+
		`data-raise-pay="%d" data-raise-slot="%s" data-raise-clause="%d" `+
		`data-raise-target="%d" type="button">Subir</button>`,
		Esc(text(row["id"])), Esc(text(row["name"])), int64(pay),
		Esc(text(row["player_team_id"])), int64(number(row["clause"])),
		int64(number(row["target_clause"])))
}

// RaiseVerdict is the sentence, coloured by what it asks for: spending, waiting, or letting him
// go. The reason travels in the same cell, because "sube" without it is an order.
func RaiseVerdict(row map[string]any) string {
	verdict, why := text(row["verdict"]), text(row["why"])
	status := map[string]string{
		"sube": "warning", "no te llega": "critical", "dejalo ir": "neutral",
		"no compensa": "neutral",
		"tranquilo": "good",
	}[verdict]
	if status == "" {
		status = "neutral"
	}
	return fmt.Sprintf(`<span class="pill-%s">%s</span>`, status, Esc(verdict)) +
		` <span class="muted">` + Esc(why) + `</span>`
}

func BidButton(row map[string]any) string {
	listing, _ := row["market"].(map[string]any)
	marketID := text(listing["market_id"])
	if marketID == "" {
		return Missing
	}
	// A rival's sale takes an offer, the game's own market takes a bid. The button carries the
	// operation because the two look identical and the API answers 404 to the wrong one.
	operation, label := "bid", "Pujar"
	if text(listing["kind"]) == "venta" {
		operation, label = "buy_offer", "Ofertar"
	}
	bid := ""
	if existing := text(listing["my_bid_id"]); existing != "" {
		// On a rival's sale there is no "change the offer" route, only withdraw: so the row says
		// what you have on it and offers to take it back, and offering again is one more click.
		if operation == "buy_offer" {
			amount := asFloat(listing["my_bid"])
			return fmt.Sprintf(`<span class="mine-bid">Ofreciste %s</span> `+
				`<button class="op danger" data-op="cancel_offer" data-op-market="%s" `+
				`data-op-offer="%s" data-op-player="%s" data-op-name="%s" type="button">`+
				`Retirar</button>`, Esc(Money(amount)), Esc(marketID), Esc(existing),
				Esc(text(row["id"])), Esc(text(row["name"])))
		}
		bid = ` data-bid="` + Esc(existing) + `"`
		// The amount is the point: "mi puja" told you nothing you could act on.
		label = "Tu puja"
		if amount := asFloat(listing["my_bid"]); amount != nil {
			label = "Tu puja " + Money(amount)
		}
	}
	return bidButton(row, "bid", operation, bid, label)
}

// bidButton is the markup openBid reads. The count and the closing time travel with the button:
// without them the dialog said "pujas vigentes: ninguna" on a listing with four.
func bidButton(row map[string]any, class, operation, bid, label string) string {
	listing, _ := row["market"].(map[string]any)
	return fmt.Sprintf(`<button class="%s" type="button" data-market="%s" `+
		`data-operation="%s" data-bids="%d" data-expires="%s" `+
		`data-player="%s" data-name="%s" data-min="%d" data-ideal="%d" data-value="%d"%s>%s</button>`,
		class, Esc(text(listing["market_id"])), Esc(operation), int64(number(listing["bids"])),
		Esc(text(listing["expires"])), Esc(text(row["id"])), Esc(text(row["name"])),
		int64(number(listing["min_bid"])), int64(number(row["ideal_bid"])),
		int64(number(row["value"])), bid, label)
}

// RaidButton schedules a clause raid from the row that told you the clause is coming. The
// whole point is arming it *before* the lock lifts, so the button lives in the table that
// shows the countdown.
func RaidButton(row map[string]any) string {
	if truthy(row["is_mine"]) || text(row["owner"]) == "" {
		return Missing
	}
	if truthy(row["shielded"]) {
		return `<span class="pill-critical">blindado</span>`
	}
	clause := number(row["clause"])
	suggested := int64(number(row["max_pay"]))
	if suggested == 0 {
		// A fifth over the clause if there is one, half over the value if there is not:
		// enough headroom that a small raise does not cancel the raid.
		if clause > 0 {
			suggested = int64(clause * 1.2)
		} else {
			suggested = int64(number(row["value"]) * 1.5)
		}
	}
	label := "Programar"
	if truthy(row["raid_scheduled"]) {
		label = "Reprogramar"
	}
	return fmt.Sprintf(`<button class="raid-btn" type="button" `+
		`data-raid="%s" data-raid-name="%s" data-raid-max="%d" data-raid-clause="%d">%s</button>`,
		Esc(text(row["id"])), Esc(text(row["name"])), suggested, int64(clause), label)
}

// OfferButtons are accept and reject, side by side, carrying everything the write needs so
// the browser never has to guess an id.
func OfferButtons(row map[string]any) string {
	if text(row["offer_id"]) == "" {
		return Missing
	}
	common := fmt.Sprintf(`data-op-market="%s" data-op-offer="%s" data-op-player="%s" `+
		`data-op-name="%s" data-op-amount="%d"`,
		Esc(text(row["market_id"])), Esc(text(row["offer_id"])), Esc(text(row["id"])),
		Esc(text(row["name"])), int64(number(row["offer_amount"])))
	// The button says whose money it is: two offers for the same player differ only in that.
	label := "Aceptar"
	if who := text(row["offer_from"]); who != "" && !truthy(row["offer_from_market"]) {
		label = "Aceptar de " + Esc(who)
	}
	return fmt.Sprintf(`<button class="op op-primary" data-op="accept_offer" %s type="button">%s`+
		`</button> <button class="op danger" data-op="decline_offer" %s `+
		`type="button">Rechazar</button>`, common, label, common)
}

// Ago is how long ago something happened, in the words a person would use.
func Ago(stamp string) (string, string) {
	if stamp == "" {
		return Missing, "0"
	}
	when, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return Esc(stamp), stamp
	}
	elapsed := time.Since(when)
	label := ""
	switch hours := int(elapsed.Hours()); {
	case elapsed < time.Minute:
		label = "ahora mismo"
	case elapsed < time.Hour:
		label = fmt.Sprintf("hace %dm", int(elapsed.Minutes()))
	case hours < 24:
		label = fmt.Sprintf("hace %dh", hours)
	default:
		label = fmt.Sprintf("hace %dd", hours/24)
	}
	// Sorted by the instant, not by the words, so "hace 2d" and "hace 10h" order correctly.
	return Esc(label), fmt.Sprintf("%d", when.Unix())
}

// LeftUntil is the first render of a countdown: how long from now to the instant, in the same
// words the browser will keep writing.
func LeftUntil(stamp string) string {
	when, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return "?"
	}
	left := time.Until(when)
	if left <= 0 {
		return "ya"
	}
	hours := int(left.Hours())
	switch {
	case hours >= 24:
		return fmt.Sprintf("%dd %dh", hours/24, hours%24)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, int(left.Minutes())%60)
	default:
		return fmt.Sprintf("%dm", int(left.Minutes()))
	}
}

// Countdown is a live countdown: the server renders a first value and stamps the deadline,
// so the browser keeps ticking it every second without asking again.
func Countdown(row map[string]any) string {
	if row == nil {
		return Missing
	}
	hours := asFloat(row["hours_left"])
	if hours == nil {
		return Missing
	}
	deadline := text(row["unlock_at"])
	if deadline == "" {
		deadline = text(row["expires"])
	}
	stamp := ""
	if deadline != "" {
		stamp = ` data-deadline="` + Esc(deadline) + `"`
	}
	if *hours <= 0 {
		return `<span class="pill-critical"` + stamp + `>ya</span>`
	}
	status, label := "neutral", fmt.Sprintf("%.0fd", *hours/24)
	switch {
	case *hours < 24:
		status, label = "critical", fmt.Sprintf("%.0fh", *hours)
	case *hours < 72:
		status, label = "warning", fmt.Sprintf("%.1fd", *hours/24)
	}
	return fmt.Sprintf(`<span class="pill-%s"%s>%s</span>`, status, stamp, label)
}

// Side is which end of a price you are standing on.
type Side int

const (
	// Paying is what he would cost you.
	Paying Side = iota
	// Offered is what somebody has put on the table for one of yours.
	Offered
	// Asking is the price you yourself put on one of yours.
	Asking
)

// RatioBadge is a price against market value, named as well as coloured.
//
// The same multiple means opposite things depending on the side: 0.95x is a bargain to whoever
// buys and a player being given away to whoever is selling. Reading your own listings on the
// buyer's scale had the table calling them bargains in the same breath as the note above it
// warned they were under market value. The name is written out so the colour is never the only
// reading.
func RatioBadge(ratio *float64, side Side) string {
	if ratio == nil {
		return Missing
	}
	value := *ratio
	var status, label string
	switch side {
	case Offered:
		switch {
		case value >= 1.15:
			status, label = "good", "te pagan de mas"
		case value >= 1.02:
			status, label = "good", "buen precio"
		case value >= 0.98:
			status, label = "neutral", "a valor"
		case value >= 0.9:
			status, label = "warning", "por debajo"
		default:
			status, label = "critical", "te lowballean"
		}
	case Asking:
		// The step is at 1.00 exactly, which is where the section's own warning is: a listing
		// flagged as under market value cannot read "a valor" in the column beside it.
		switch {
		case value >= 1.15:
			status, label = "good", "por encima"
		case value >= 1.02:
			status, label = "good", "con margen"
		case value >= 1:
			status, label = "neutral", "a valor"
		case value >= 0.9:
			status, label = "warning", "por debajo"
		default:
			status, label = "critical", "lo regalas"
		}
	default:
		switch {
		case value <= 0.98:
			status, label = "good", "chollo"
		case value <= 1.06:
			status, label = "neutral", "a valor"
		case value <= 1.3:
			status, label = "warning", "algo caro"
		default:
			status, label = "critical", "muy caro"
		}
	}
	return fmt.Sprintf(`<span class="pill-%s">%.2fx</span><span class="pill-note">%s</span>`,
		status, value, label)
}

func asStrings(value any) []string {
	if already, ok := value.([]string); ok {
		return already
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, PyText(item))
	}
	return out
}

// parseStamp reads the API's dates, which come in more than one shape.
func parseStamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05",
		"2006-01-02"} {
		if when, err := time.Parse(layout, value); err == nil {
			return when, true
		}
	}
	return time.Time{}, false
}

// NumericKinds are right-aligned, and the page's sort code reads the same list.
var NumericKinds = map[string]bool{
	"money": true, "num": true, "num1": true, "int": true, "pct": true, "pct_plain": true,
	"mag": true, "ideal": true, "starts": true,
	// The three the matchday board adds: all of them are numbers with a word beside them.
	"live_points": true, "waiting": true, "projection": true,
}

// Column is a header, how to read the value out of a row, and how to render it.
type Column struct {
	Header string
	Read   func(map[string]any) any
	Kind   string
}

func Section(title, body, note, badge, anchor string) string {
	badgeHTML := ""
	if badge != "" {
		badgeHTML = `<span class="badge-count">` + Esc(badge) + `</span>`
	}
	noteHTML := ""
	if note != "" {
		// The note is HTML on purpose: it carries <code> and <strong> written by hand.
		noteHTML = `<p class="note">` + note + `</p>`
	}
	ident := ""
	if anchor != "" {
		ident = ` id="` + Esc(anchor) + `"`
	}
	return "<section" + ident + "><h2>" + Esc(title) + badgeHTML + "</h2>" + noteHTML +
		body + "</section>"
}

// Where is home or away, in words.
func Where(home bool) string {
	if home {
		return "en casa"
	}
	return "fuera"
}

func field(name string) func(map[string]any) any {
	return func(row map[string]any) any { return row[name] }
}

func whole(row map[string]any) any { return row }

// raidPlanStatus colours the state of a scheduled raid. "cancelada" is a warning rather
// than an error: standing down because the clause rose is the instruction working.
var raidPlanStatus = map[string]string{
	"pagar_clausula": "good", "esperando": "neutral", "cancelada": "warning",
	"bloqueada": "critical", "sin_saldo": "warning", "ninguna": "neutral",
	// No written cap means no automatic payment, and that is a state worth seeing.
	"sin_limite": "critical",
}

var months = []string{"", "ene", "feb", "mar", "abr", "may", "jun",
	"jul", "ago", "sep", "oct", "nov", "dic"}
var weekdays = []string{"lun", "mar", "mie", "jue", "vie", "sab", "dom"}

// Calendar lays the clause unlocks out by day.
//
// A table sorts; a calendar answers a different question — *when does the league open up* —
// and at the start of a season the answer is dramatic: everything on the same day. That is
// worth seeing as a shape rather than reading as 28 rows.
// FinishedMatch is the API's matchState for a played match. Kept here rather than importing the
// scheduler for one number: this package renders and depends on nothing of ours.
// See schedule.FinishedMatch, which has to agree.
const FinishedMatch = 7


func crestOf(teamID string) string {
	if _, known := Crests[teamID]; !known {
		return ""
	}
	return `<span class="crest crest-` + Esc(teamID) + `"></span>`
}

func Calendar(entries []map[string]any, spending float64, today string) string {
	if len(entries) == 0 {
		return `<p class="empty">Sin cláusulas con fecha conocida.</p>`
	}

	byDay := map[string][]map[string]any{}
	for _, row := range entries {
		stamp := text(row["unlock_at"])
		if len(stamp) >= 10 {
			day := stamp[:10]
			byDay[day] = append(byDay[day], row)
		}
	}
	// Days already gone are dropped: every clause in the league opened the same day the
	// season started, so without this the eight cards are all spent on that one August
	// morning and a date you could still act on never reaches the page.
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		if day < today {
			continue
		}
		days = append(days, day)
	}
	if len(days) == 0 {
		return `<p class="empty">Sin cláusulas que se abran de hoy en adelante.</p>`
	}
	sort.Strings(days)
	if len(days) > 8 {
		days = days[:8]
	}

	var cards strings.Builder
	for _, day := range days {
		rows := byDay[day]
		sort.SliceStable(rows, func(i, j int) bool {
			return number(rows[i]["score"]) > number(rows[j]["score"])
		})
		mine, targets := 0, 0
		for _, row := range rows {
			if truthy(row["is_mine"]) {
				mine++
			} else if number(row["clause"]) <= spending {
				targets++
			}
		}

		label := day
		if when, err := time.Parse("2006-01-02", day); err == nil {
			// Monday is 0 in Python's weekday(), 1 in Go's.
			label = fmt.Sprintf("%s %d %s", weekdays[(int(when.Weekday())+6)%7],
				when.Day(), months[int(when.Month())])
		}

		var chips strings.Builder
		for index, row := range rows {
			if index >= 14 {
				break
			}
			chips.WriteString(calendarChip(row))
		}
		more := ""
		if len(rows) > 14 {
			more = fmt.Sprintf(`<span class="cal-more">+%d mas</span>`, len(rows)-14)
		}
		fmt.Fprintf(&cards, `<div class="cal-day"><div class="cal-head"><strong>%s</strong>`+
			`<span class="cal-count">%d</span></div>`+
			`<div class="cal-meta">%d tuyos · %d a tu alcance</div>`+
			`<div class="cal-chips">%s%s</div></div>`,
			Esc(label), len(rows), mine, targets, chips.String(), more)
	}
	return `<div class="cal">` + cards.String() + `</div>`
}

// calendarChip is one player on one day: yours are marked because that is the day you are
// exposed, and a rival's doubles as the button that arms the raid.
func calendarChip(row map[string]any) string {
	class := ""
	if truthy(row["is_mine"]) {
		class += " cal-mine"
	}
	if truthy(row["raid_scheduled"]) {
		class += " cal-armed"
	}
	raid := ""
	title := "Tuyo: ese dia queda expuesto"
	if !truthy(row["is_mine"]) {
		clause := number(row["clause"])
		max := int64(number(row["max_pay"]))
		if max == 0 {
			max = int64(clause * 1.2)
		}
		raid = fmt.Sprintf(` data-raid="%s" data-raid-name="%s" data-raid-max="%d"`+
			` data-raid-clause="%d"`, Esc(text(row["id"])), Esc(text(row["name"])),
			max, int64(clause))
		title = "Programar clausulazo"
	}
	armed := ""
	if truthy(row["raid_scheduled"]) {
		armed = "<span class=cal-armed-mark>armado</span>"
	}
	clause := asFloat(row["clause"])
	return fmt.Sprintf(`<button class="cal-chip%s" type="button"%s data-detail-alt="%s" `+
		`title="%s"><span class="crest crest-%s"></span>%s%s<b>%s</b></button>`,
		class, raid, Esc(text(row["id"])), title, Esc(text(row["team_id"])),
		Esc(text(row["name"])), armed, Esc(Money(clause)))
}

// SectionTable renders one of the page's tables by name. Same columns, same order, same
// section — the section matters because it decides whether a row says "mio".
func SectionTable(name string, rows []map[string]any) (string, error) {
	switch name {
	case "rivalsquad":
		// Inside a rival's own section the owner column would repeat his name on every row.
		columns := []Column{
			{"", whole, "cmp"},
			{"Jugador", whole, "player"},
			{"Frente a lo tuyo", whole, "vs_mine"},
			{"Cláusula", field("clause"), "money"},
			{"Se puede", whole, "clause_when"},
			{"x valor", field("clause_x"), "num"},
			{"Valor", field("value"), "money"},
			{"En venta", field("asking"), "money"},
			{"xPts/j", field("xpts"), "num"},
			{"Pts/M", field("points_value"), "mag"},
			{"Titular", field("start_probability"), "starts"},
			{"Proximo rival", func(row map[string]any) any {
				rival := text(row["next_rival"])
				if rival == "" {
					return nil
				}
				return crestOf(text(row["next_rival_id"])) + rival + " · " + Where(truthy(row["next_home"]))
			}, "text"},
		}
		return TableIn(columns, rows, "Sin jugadores", "", false), nil
	}
	return "", fmt.Errorf("seccion desconocida: %s", name)
}

// Table renders the sortable table the whole page is made of. The data-sort attribute is
// what makes a column sortable without shipping the data twice.
func Table(columns []Column, rows []map[string]any, empty string) string {
	return TableIn(columns, rows, empty, "", false)
}

// TableIn is Table with the section it belongs to and whether the filter bar acts on it.
func TableIn(columns []Column, rows []map[string]any, empty, section string,
	filterable bool) string {
	if len(rows) == 0 {
		return `<p class="empty">` + Esc(empty) + `</p>`
	}
	// A column nobody has data for is noise: drop it rather than ship an empty strip.
	kept := make([]Column, 0, len(columns))
	for _, column := range columns {
		if column.Kind == "spark" && !anyHistory(rows) {
			continue
		}
		kept = append(kept, column)
	}
	columns = kept
	var head strings.Builder
	for _, column := range columns {
		classes := make([]string, 0, 2)
		if NumericKinds[column.Kind] {
			classes = append(classes, "right")
		}
		if WideOnly(column) {
			classes = append(classes, "wide-only")
		}
		fmt.Fprintf(&head, `<th data-kind="%s"%s>%s</th>`, column.Kind, classAttr(classes),
			Esc(column.Header))
	}

	var body strings.Builder
	for _, row := range rows {
		classes := ""
		if truthy(row["is_me"]) {
			classes = ` class="row-me"`
		}
		attrs := ""
		if filterable {
			price := number(row["entry_cost"])
			if price == 0 {
				price = number(row["value"])
			}
			// The owner is searchable too: typing "cristian" has to bring up what cristian
			// holds rather than demanding you remember his players' names.
			searchable := strings.ToLower(text(row["name"]))
			for _, extra := range []string{text(row["owner"]), text(row["seller"])} {
				if extra != "" {
					searchable += " " + strings.ToLower(extra)
				}
			}
			// The route and whether the money is there, so a section can narrow itself to one
			// kind of move without asking the server for the same rows again.
			paid := "0"
			if truthy(row["affordable"]) {
				paid = "1"
			}
			attrs = fmt.Sprintf(` data-position="%s" data-price="%.0f" data-name="%s"`+
				` data-route="%s" data-afford="%s"`,
				Esc(text(row["position"])), price, Esc(searchable),
				Esc(text(row["route"])), paid)
		}
		body.WriteString("<tr" + classes + attrs + ">")
		for _, column := range columns {
			inner, sortKey := CellIn(column.Read(row), column.Kind, section)
			classes := make([]string, 0, 2)
			switch {
			case column.Kind == "pct":
				classes = append(classes, "bar")
			case NumericKinds[column.Kind]:
				classes = append(classes, "num")
			}
			if WideOnly(column) {
				classes = append(classes, "wide-only")
			}
			fmt.Fprintf(&body, `<td%s data-sort="%s">%s</td>`, classAttr(classes),
				Esc(sortKey), inner)
		}
		body.WriteString("</tr>")
	}
	// A table whose first column is a button pins that column: the scroll is horizontal and
	// the button is the reason the row is there.
	wrap := "table-wrap"
	if len(columns) > 0 && ActionKinds[columns[0].Kind] {
		wrap += " sticky-first"
	}
	return `<div class="` + wrap + `"><table class="sortable"><thead><tr>` + head.String() +
		"</tr></thead><tbody>" + body.String() + "</tbody></table></div>"
}

func classAttr(classes []string) string {
	if len(classes) == 0 {
		return ""
	}
	return ` class="` + strings.Join(classes, " ") + `"`
}

// The columns a phone does not draw. Hidden in CSS rather than dropped here: the sorter
// addresses a column by its index, and dropping one would sort by the wrong column.
var wideOnlyKinds = map[string]bool{"spark": true, "mag": true, "pct": true}

var wideOnlyHeaders = map[string]bool{"Pts 25/26": true, "Proximo rival": true}

// WideOnly is whether this column only exists on a wide screen.
func WideOnly(column Column) bool {
	return wideOnlyKinds[column.Kind] || wideOnlyHeaders[column.Header]
}

// ActionKinds are the cells that hold a button rather than a fact.
var ActionKinds = map[string]bool{"bid": true, "raid": true, "offer": true, "verdict": true,
	"verdict_raid": true, "cmp": true}

// CompareButton stacks a player into the comparator from a table, without opening his card
// first: in a table of everybody else's players, picking two to look at side by side is the
// whole point.
func CompareButton(row map[string]any) string {
	id := text(row["id"])
	if id == "" {
		return ""
	}
	return `<button class="cmp-add small" type="button" data-cmp="` + Esc(id) +
		`" data-cmp-name="` + Esc(text(row["name"])) +
		`" data-cmp-pos="` + Esc(text(row["position"])) +
		`" title="Añadir al comparador">+</button>`
}

// Cell returns (inner HTML, sort key) for one value. `section` only matters to the player
// cell, which drops the "mio" flag where every row is yours.
func Cell(value any, kind string) (string, string) {
	return CellIn(value, kind, "")
}

func CellIn(value any, kind string, section string) (string, string) {
	amount := asFloat(value)
	switch kind {
	case "star":
		row, _ := value.(map[string]any)
		key := "1"
		if truthy(row["starred"]) {
			key = "0"
		}
		return Star(row), key
	case "player":
		row, _ := value.(map[string]any)
		return PlayerCell(row, section), Esc(text(row["name"]))
	case "starts":
		return Starts(amount)
	case "ideal":
		// No ceiling is not zero: futbolfantasy has looked and sees no room at this price,
		// which is a verdict and reads as one.
		if amount == nil || *amount == 0 {
			return `<span class="pill-warning">sin margen</span>`, "0"
		}
		return Esc(Money(amount)), sortKey(amount)
	case "pct_plain":
		return Esc(Pct(amount)), sortKey(amount)
	case "ratio":
		return RatioBadge(amount, Paying), sortKey(amount)
	case "ratio_sell":
		return RatioBadge(amount, Offered), sortKey(amount)
	case "ratio_ask":
		return RatioBadge(amount, Asking), sortKey(amount)
	case "bid":
		row, _ := value.(map[string]any)
		listing, _ := row["market"].(map[string]any)
		return BidButton(row), fmt.Sprintf("%d", int64(number(listing["min_bid"])))
	case "raid":
		row, _ := value.(map[string]any)
		return RaidButton(row), sortKey(asFloat(row["clause"]))
	case "raise":
		row, _ := value.(map[string]any)
		return RaiseButton(row), sortKey(asFloat(row["pay"]))
	case "risk_pct":
		// A share, not a measured frequency, and the row carries who and why beside it.
		return RiskBar(number(value)), sortKey(asFloat(value))
	case "raise_verdict":
		row, _ := value.(map[string]any)
		return RaiseVerdict(row), Esc(text(row["verdict"]))
	case "cmp":
		row, _ := value.(map[string]any)
		return CompareButton(row), Esc(text(row["name"]))
	case "clause_when":
		// Whether he can be taken today, which is the only reading of a clause that changes
		// what you do. Shielded and locked are not the same "no".
		row, _ := value.(map[string]any)
		switch {
		case truthy(row["shielded"]):
			if until := text(row["shielded_until"]); until != "" {
				return `<span class="chip chip-warn">blindado <span data-deadline="` +
					Esc(until) + `">…</span></span>`, until
			}
			return `<span class="chip chip-warn">blindado</span>`, "zz"
		case truthy(row["clause_locked"]):
			if until := text(row["clause_locked_until"]); until != "" {
				return `<span class="chip chip-warn">en <span data-deadline="` + Esc(until) +
					`">…</span></span>`, until
			}
			return `<span class="chip chip-warn">bloqueada</span>`, "zy"
		case number(row["clause"]) == 0:
			return `<span class="muted">—</span>`, "zx"
		}
		return `<span class="chip chip-good">pagable ya</span>`, "0"
	case "vs_mine":
		// The reason a rival's player is interesting is what he would replace. Without this
		// the table is a census; with it, it is a shortlist.
		row, _ := value.(map[string]any)
		delta, who := asFloat(row["vs_mine"]), text(row["vs_who"])
		if delta == nil || who == "" {
			return `<span class="muted">—</span>`, "-999"
		}
		if *delta > 0 {
			return fmt.Sprintf(`<span class="vs-up">+%.2f</span> <span class="muted">sobre %s</span>`,
				*delta, Esc(who)), sortKey(delta)
		}
		return fmt.Sprintf(`<span class="muted">%.2f vs %s</span>`, *delta, Esc(who)),
			sortKey(delta)
	case "offer":
		row, _ := value.(map[string]any)
		return OfferButtons(row), sortKey(asFloat(row["offer_amount"]))
	case "offer_tally":
		// People and machine counted apart: five automatic bids are not five interested
		// managers, and a listing with one real offer is the one worth looking at.
		row, _ := value.(map[string]any)
		people, machine := 0, 0
		for _, offer := range rows(row["offers"]) {
			if truthy(offer["from_market"]) {
				machine++
			} else {
				people++
			}
		}
		if people == 0 && machine == 0 {
			return `<span class="muted">nadie</span>`, "0"
		}
		parts := []string{}
		if people > 0 {
			parts = append(parts, fmt.Sprintf(`<b>%d</b> de rivales`, people))
		}
		if machine > 0 {
			parts = append(parts,
				fmt.Sprintf(`<span class="from-market">%d del mercado</span>`, machine))
		}
		// Real offers dominate the sort: that is the column's whole purpose.
		return strings.Join(parts, " · "), fmt.Sprintf("%03d%03d", people, machine)

	case "best_offer":
		row, _ := value.(map[string]any)
		var best map[string]any
		for _, offer := range rows(row["offers"]) {
			if best == nil || number(offer["money"]) > number(best["money"]) {
				best = offer
			}
		}
		if best == nil {
			return Missing, "0"
		}
		amount := asFloat(best["money"])
		who := text(best["from"])
		if who == "" {
			who = "el mercado"
		}
		class := "from-rival"
		if truthy(best["from_market"]) {
			class = "from-market"
		}
		return Esc(Money(amount)) + ` <span class="` + class + `">· ` + Esc(who) + `</span>`,
			sortKey(amount)

	case "named":
		// A player named in a table that is not a player table: the feed, the scheduled raids.
		row, _ := value.(map[string]any)
		name := text(row["name"])
		id := text(row["player_id"])
		if id == "" {
			id = text(row["id"])
		}
		return PlayerLink(name, id), name

	case "always_edit":
		// The instruction was editable only from the player's card, which you had to know to
		// look for. Same two things the card offers: open it to set the amounts, or take the
		// instruction off.
		row, _ := value.(map[string]any)
		id := text(row["player_id"])
		if id == "" {
			id = text(row["id"])
		}
		if id == "" {
			return Missing, ""
		}
		return fmt.Sprintf(`<button class="raid-btn" type="button" data-detail="%s">Editar`+
			`</button> <button class="op danger" data-op="drop_always" data-op-player="%s" `+
			`data-op-name="%s" type="button">Quitar</button>`,
			Esc(id), Esc(id), Esc(text(row["name"]))), ""

	case "raid_edit":
		// A scheduled raid could be armed and then neither changed nor called off, which is the
		// one thing you want to do when the clause moves or you change your mind.
		row, _ := value.(map[string]any)
		id := text(row["player_id"])
		if id == "" {
			return Missing, ""
		}
		limit := int64(number(row["max_pay"]))
		common := fmt.Sprintf(`data-op-player="%s" data-op-name="%s"`, Esc(id),
			Esc(text(row["name"])))
		return fmt.Sprintf(`<button class="raid-btn" type="button" data-raid="%s" `+
			`data-name="%s" data-clause="%d" data-max="%d">Cambiar</button> `+
			`<button class="op danger" data-op="cancel_raid" %s type="button">Cancelar</button>`,
			Esc(id), Esc(text(row["name"])), int64(number(row["clause"])), limit, common), ""

	case "closes":
		row, _ := value.(map[string]any)
		stamp := text(row["closes"])
		if stamp == "" {
			return Missing, "0"
		}
		when, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			return Esc(stamp), stamp
		}
		return `<span data-deadline="` + Esc(stamp) + `">` + Esc(LeftUntil(stamp)) + `</span>`,
			fmt.Sprintf("%d", when.Unix())

	case "listing_until":
		row, _ := value.(map[string]any)
		listing := mapOf(row["market"])
		stamp := text(listing["expires"])
		if stamp == "" {
			return Missing, "0"
		}
		when, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			return Esc(stamp), stamp
		}
		return `<span data-deadline="` + Esc(stamp) + `">` + Esc(LeftUntil(stamp)) + `</span>`,
			fmt.Sprintf("%d", when.Unix())

	case "manager":
		// The name opens his squad: this table says how much he can spend, and the obvious next
		// question is what he already has.
		row, _ := value.(map[string]any)
		name := text(row["manager"])
		if name == "" {
			name = text(row["name"])
		}
		return ManagerLink(name, text(row["team_id"])), name
	case "seller":
		// Who is selling him, and with one click what else he has: the same question the
		// buying-power table answers, asked from the row where it comes up.
		row, _ := value.(map[string]any)
		listing := mapOf(row["market"])
		name := text(row["seller"])
		if name == "" {
			name = text(listing["seller"])
		}
		return ManagerLink(name, text(listing["seller_team_id"])), name
	case "owner":
		row, _ := value.(map[string]any)
		return ManagerLink(text(row["owner"]), text(row["owner_team_id"])), text(row["owner"])

	case "offer_from":
		// The machine's bid is named as such and greyed: it arrives every day whatever you
		// do, so it should never look like somebody made a decision about your player.
		row, _ := value.(map[string]any)
		who := text(row["offer_from"])
		if truthy(row["offer_from_market"]) {
			return `<span class="from-market" data-tip="Oferta automatica del juego: llega cada ` +
				`dia y caduca al cerrar el mercado">` + Esc(who) + `</span>`, "zzz " + who
		}
		// A person who offers is a person you can look up; the machine is not.
		if teamID := text(row["offer_from_team_id"]); teamID != "" {
			return `<span class="from-rival">` + ManagerLink(who, teamID) + `</span>`, who
		}
		return `<span class="from-rival">` + Esc(who) + `</span>`, who
	case "since":
		// How long the offer has been sitting there: an offer from ten minutes ago and one
		// about to expire read the same as a date and completely differently as an age.
		row, _ := value.(map[string]any)
		return Ago(text(row["offer_made"]))
	case "until":
		row, _ := value.(map[string]any)
		stamp := text(row["offer_expires"])
		if stamp == "" {
			return Missing, "0"
		}
		when, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			return Esc(stamp), stamp
		}
		return `<span data-deadline="` + Esc(stamp) + `">` + Esc(LeftUntil(stamp)) + `</span>`,
			fmt.Sprintf("%d", when.Unix())
	case "hours":
		row, _ := value.(map[string]any)
		hours := asFloat(row["hours_left"])
		if hours == nil {
			far := 1e9
			hours = &far
		}
		return Countdown(row), sortKey(hours)
	case "verdict":
		verdict := text(value)
		order := 0
		for index, name := range VerdictOrder {
			if name == verdict {
				order = index
			}
		}
		return VerdictBadge(verdict), fmt.Sprintf("%d", order)
	case "verdict_raid":
		row, _ := value.(map[string]any)
		// Sorted by the points per million you would get paying the clause, best first,
		// which means the sort key is its negative.
		return RaidVerdict(row), sortKey(negate(asFloat(row["ppm_at_clause"])))
	case "power":
		row, _ := value.(map[string]any)
		order := map[string]string{"justo": "0", "normal": "1", "holgado": "2"}
		return PowerBadge(row), order[text(row["power"])]
	case "status":
		// A pair: the label and the status that colours it. The label is always written,
		// so the pill is a second reading and never the only one.
		pair, _ := value.([]any)
		label, status := "", ""
		if len(pair) > 0 {
			label = text(pair[0])
		}
		if len(pair) > 1 {
			status = text(pair[1])
		}
		if status == "" {
			status = "neutral"
		}
		return fmt.Sprintf(`<span class="pill-%s">%s</span>`, status,
			Esc(strings.ReplaceAll(label, "_", " "))), label
	case "live_points":
		// A manager with nobody on the pitch has no figure in the standings, and a nought there
		// would read as a bad matchday rather than as no matchday at all.
		row, _ := value.(map[string]any)
		points := asFloat(row["points"])
		if !truthy(row["reported"]) {
			return `<span class="muted">sin puntos</span>`, "-1"
		}
		return fmt.Sprintf(`<b>%d</b>`, int64(number(points))), sortKey(points)

	case "waiting":
		// Nobody left is the most decisive thing a row can say, so it says it in words: his
		// afternoon is over whatever the rest of the league still has to play.
		row, _ := value.(map[string]any)
		waiting := int(number(row["waiting"]))
		if waiting == 0 {
			return `<span class="pill-neutral">cerrado</span>`, "0"
		}
		if text(row["source"]) == "techo" {
			return fmt.Sprintf(`%d <span class="muted" title="Sin alineacion legible: `+
				`los once mejores de su plantilla por jugar">techo</span>`, waiting),
				fmt.Sprintf("%d", waiting)
		}
		return fmt.Sprintf(`%d`, waiting), fmt.Sprintf("%d", waiting)

	case "projection":
		// The number and, when it disagrees with the order the table is sorted in, the seat it
		// would end in. That disagreement is the whole reason the column exists.
		row, _ := value.(map[string]any)
		projection := asFloat(row["projection"])
		seat := ""
		if now, then := int(number(row["points_rank"])), int(number(row["projection_rank"])); then != now {
			class, arrow := "up", "↑"
			if then > now {
				class, arrow = "down", "↓"
			}
			seat = fmt.Sprintf(` <span class="seat-%s">%s %dº</span>`, class, arrow, then)
		}
		if truthy(row["done"]) {
			// Nothing left to add: the projection is the result, and repeating it as a forecast
			// would invent a doubt that is not there.
			return fmt.Sprintf(`%s <span class="muted">final</span>%s`,
				Num(projection, 1), seat), sortKey(projection)
		}
		return Num(projection, 1) + seat, sortKey(projection)

	case "holes":
		// Zero holes is the good case and by far the common one, so it says "once completo"
		// rather than printing a nought that reads like a missing number. A hole is a slot
		// nobody can fill: eleven minus whatever his best legal formation seats.
		row, _ := value.(map[string]any)
		holes := int(number(row["holes"]))
		if holes == 0 {
			return `<span class="muted">once completo</span>`, "0"
		}
		return fmt.Sprintf(`<span class="pill-critical">%d en blanco</span>`, holes),
			fmt.Sprintf("%d", holes)
	case "list":
		items := asStrings(value)
		if len(items) == 0 {
			return Missing, ""
		}
		return Esc(strings.Join(items, ", ")), ""
	}
	switch kind {
	case "money":
		return Esc(Money(amount)), sortKey(amount)
	case "pct":
		return DivergingBar(amount, 12.0), sortKey(amount)
	case "num":
		return Esc(Num(amount, 2)), sortKey(amount)
	case "num1":
		return Esc(Num(amount, 1)), sortKey(amount)
	case "mag":
		return MagnitudeBar(amount, 1.0, 2), sortKey(amount)
	case "int":
		if amount == nil {
			return Missing, sortKey(amount)
		}
		return fmt.Sprintf("%d", int64(*amount)), sortKey(amount)
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
	return Esc(PyText(value)), Esc(PyText(value))
}

// PyFloat writes a float the way Python's str() does.
//
// Go's default (%v, fmt.Sprint) switches to scientific notation around ten million, which
// is the middle of the range every price in this game lives in: a value of 17761424.4 comes
// out as "1.7761424e+07". As a sort key that sorts wrongly while looking fine, and as
// visible text it is simply wrong. Python only goes scientific at an exponent of 16 or
// under -4, so those are the only cases that get it.
//
// One implementation, used everywhere a number becomes text, because the failure is
// invisible and would otherwise be reintroduced one call site at a time.
func PyFloat(amount float64) string {
	magnitude := math.Abs(amount)
	if magnitude >= 1e16 || (amount != 0 && magnitude < 1e-4) {
		return strconv.FormatFloat(amount, 'g', -1, 64)
	}
	if amount == math.Trunc(amount) {
		return fmt.Sprintf("%.1f", amount)
	}
	return strconv.FormatFloat(amount, 'f', -1, 64)
}

// PyText is any value as Python's str() would render it.
func PyText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case float64:
		return PyFloat(typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	}
	return fmt.Sprint(value)
}

// sortKey is the numeric sort value the browser reads, rendered as Python's str(float(x)).
//
// Not as Go's %v, which switches to scientific notation around ten million — right in the
// middle of the range every price in this game lives in. Python only goes scientific at an
// exponent of 16 or below -4, so those two cases are the only ones that get it.
func sortKey(number *float64) string {
	if number == nil {
		return PyFloat(0)
	}
	return PyFloat(*number)
}

func negate(number *float64) *float64 {
	if number == nil {
		zero := 0.0
		return &zero
	}
	flipped := -*number
	return &flipped
}

func anyHistory(rows []map[string]any) bool {
	for _, row := range rows {
		if len(asSeries(row["value_history"])) >= 5 {
			return true
		}
	}
	return false
}

// Rows reach here two ways: parsed from JSON, where every value is a plain float64 or
// string, and straight from the model, where absence is a nil pointer. Both have to read the
// same, and forgetting a pointer case is silent — a *float64 reads as zero and a whole table
// quietly loses its rows. It cost the actions table its four "vender" rows and three of its
// "fichar" ones before the page comparison caught it.
func asFloat(value any) *float64 {
	switch typed := value.(type) {
	case nil:
		return nil
	case float64:
		return &typed
	case *float64:
		return typed
	case *int:
		if typed == nil {
			return nil
		}
		converted := float64(*typed)
		return &converted
	case *bool:
		if typed == nil {
			return nil
		}
		converted := 0.0
		if *typed {
			converted = 1
		}
		return &converted
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

// versionTag is the build in plain sight: whether a deploy has landed is read at a glance, not
// hovered for.
func versionTag() string {
	if Build == "" {
		return ""
	}
	return `<span class="build-tag">` + Esc(Build) + `</span>`
}
