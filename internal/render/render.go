// Package render builds the page. A port of fantasy/report.py, and the primitives come
// first: every table on the page is these formatters repeated, so if a number is spelled
// differently here than there, every section differs and the diff is useless.
//
// Each one is compared against its Python original over a table of inputs, including the
// edges that look like nothing and are not — 999,500 rounds to "1.000K" in a naive
// implementation and must read "1.00M".
package render

import (
	"encoding/json"
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
			`title="%s"></span>`, Esc(teamID), Esc(team), Esc(team))
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
			`<span class="flag-muted" title="Sin historico: estimado por precio">est.</span>`)
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
			`<span class="flag-hold" title="Norma de la liga: recien fichado, `+
				`no se puede vender hasta el %s">🔒</span>`, Esc(until)))
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

// KPI is a widget. `meter` (0..1) draws where you sit in the league for that number,
// because a figure like "79.76M" only means something next to the other twelve.
type KPI struct {
	Label string
	Value string
	Hint  string
	Rank  string
	Meter *float64
	Status string
	Tab    string
	// Deadline turns the value into a live countdown: the server renders the first value and
	// stamps the instant, the browser keeps it honest every second.
	Deadline string
	// Notes are extra facts, one per line under the hint.
	Notes []string
}

func Widget(kpi KPI) string {
	stamp := ""
	if kpi.Deadline != "" {
		stamp = ` data-deadline="` + Esc(kpi.Deadline) + `" data-plain="1"`
	}
	parts := []string{
		`<span class="kpi-label">` + Esc(kpi.Label) + `</span>`,
		`<span class="kpi-value"` + stamp + `>` + Esc(kpi.Value) + `</span>`,
	}
	if kpi.Rank != "" {
		status := kpi.Status
		if status == "" {
			status = "neutral"
		}
		parts = append(parts, fmt.Sprintf(`<span class="kpi-rank pill-%s">%s</span>`,
			status, Esc(kpi.Rank)))
	}
	if kpi.Meter != nil {
		width := math.Max(2.0, math.Min(100.0, *kpi.Meter*100))
		parts = append(parts, `<span class="kpi-meter" role="presentation">`+
			fmt.Sprintf(`<span class="kpi-meter-fill" style="width:%.0f%%"></span></span>`, width))
	}
	if kpi.Hint != "" {
		parts = append(parts, `<span class="kpi-hint">`+Esc(kpi.Hint)+`</span>`)
	}
	// Several short facts read as a list, not as a sentence with dots: one line each.
	for _, line := range kpi.Notes {
		parts = append(parts, `<span class="kpi-note">`+Esc(line)+`</span>`)
	}
	if kpi.Tab != "" {
		// A widget that states a fact should take you to where the fact is explained.
		return fmt.Sprintf(`<button class="kpi kpi-link" type="button" data-goto="%s">%s</button>`,
			Esc(kpi.Tab), strings.Join(parts, ""))
	}
	return `<div class="kpi">` + strings.Join(parts, "") + `</div>`
}

// RankOf is where a number sits among the league's: (label, 0..1 position, status).
func RankOf(value float64, others []float64) (string, float64, string) {
	total := len(others)
	if total == 0 {
		return "", 0, ""
	}
	position := 1
	for _, other := range others {
		if other > value {
			position++
		}
	}
	share := 1 - float64(position-1)/math.Max(1, float64(total-1))
	third := max(1, total/3)
	status := "neutral"
	if position <= third {
		status = "good"
	} else if position > total-third {
		status = "critical"
	}
	return fmt.Sprintf("%dº de %d", position, total), share, status
}

// Tabs are the page's groups. Rendered only when there is a session, because a chip
// that jumps nowhere is worse than no chip.
const Tabs = `<div class="tabs" id="tabs" role="tablist">` +
	`<button class="tab" role="tab" data-tab="decidir" aria-selected="false" type="button">Decidir</button>` +
	`<button class="tab" role="tab" data-tab="mercado" aria-selected="false" type="button">Mercado</button>` +
	`<button class="tab" role="tab" data-tab="clausulas" aria-selected="false" type="button">Cláusulas</button>` +
	`<button class="tab" role="tab" data-tab="plantilla" aria-selected="false" type="button">Plantilla</button>` +
	`<button class="tab" role="tab" data-tab="partidos" aria-selected="false" type="button">Partidos<`+
	`/button>`+
	`<button class="tab" role="tab" data-tab="liga" aria-selected="false" type="button">Liga</button>` +
	`<button class="tab" role="tab" data-tab="ranking" aria-selected="false" type="button">Ranking</button></div>`

// Header is the title, when it was generated, the widgets and the tabs. The live dot starts
// off: a static file is honest about not being live, and the script turns it on when the
// push channel connects.
func Header(generated, leagueName string, week int, kpis []string, withTabs bool) string {
	league := ""
	if leagueName != "" {
		league = ` · liga <strong>` + Esc(leagueName) + `</strong>`
	}
	tabs := ""
	if withTabs {
		tabs = Tabs
	}
	return `<header><h1>LaLiga Fantasy · panel de decisiones</h1>` +
		`<p>` + Esc(generated) + league +
		fmt.Sprintf(` · jornada %d</p>`, week) +
		`<span class="live"><span id="live-dot" class="live-off"></span>` +
		`<span id="live-stamp">estatico</span></span>` +
		`</header>` +
		`<div class="kpis">` + strings.Join(kpis, "") + `</div>` + tabs
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
const Favicon = `<link rel="icon" type="image/png" href="data:image/png;base64,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">`

func Page(css, js, crestCSS, header, body, footer, modal, drawer string) string {
	return `<title>Fantasy</title>` + Favicon + `<style>` + css + crestCSS + `</style>` +
		`<div class="wrap">` + header + body + footer + `</div>` +
		modal + drawer +
		`<script>` + js + `</script>`
}

// SwapPlan draws the plan as what it is: pairs. Who leaves on the left, who arrives on the
// right, and under each pair the two numbers that decide it — points won and euros moved.
//
// Deliberately not a table. A table invites comparing rows; this is meant to be read once,
// top to bottom, and acted on.
func SwapPlan(plan map[string]any) string {
	moves := rows(plan["moves"])
	if len(moves) == 0 {
		return `<p class="empty">Ningun cambio del mercado de hoy mejora tu once sin ` +
			`pagar de mas. No mover tambien es un plan.</p>`
	}

	var out strings.Builder
	before, after := asFloat(plan["xpts_before"]), asFloat(plan["xpts_after"])
	cashBefore, cashAfter := asFloat(plan["cash_before"]), asFloat(plan["cash_after"])
	fmt.Fprintf(&out, `<div class="plan-head">`+
		`<span class="plan-total"><b>%s</b> xPts <span class="plan-arrow">→</span> `+
		`<b class="up">%s</b></span>`+
		`<span class="plan-total">Caja <b>%s</b> <span class="plan-arrow">→</span> `+
		`<b>%s</b></span>`+
		`<span class="plan-count">%d cambio%s</span></div>`,
		Num(before, 2), Num(after, 2), Money(cashBefore), Money(cashAfter),
		len(moves), map[bool]string{true: "", false: "s"}[len(moves) == 1])

	out.WriteString(`<div class="plan">`)
	for _, move := range moves {
		leaving, arriving := mapOf(move["out"]), mapOf(move["in"])
		gain, net := asFloat(move["gain"]), asFloat(move["net"])
		netClass, netText := "down", "+"+Money(net)
		if net != nil && *net <= 0 {
			positive := -*net
			netClass, netText = "up", "-"+Money(&positive)
		}
		fmt.Fprintf(&out, `<div class="plan-move">`+
			`<div class="plan-side out">%s<span class="plan-why">%s</span>`+
			`<span class="plan-money">%s · %s</span></div>`+
			`<div class="plan-mid"><span class="plan-arrow">→</span>`+
			`<span class="plan-gain up">+%s xPts</span>`+
			`<span class="plan-net %s">%s</span>%s</div>`+
			`<div class="plan-side in">%s<span class="plan-why">%s titular · %s xPts</span>`+
			`<span class="plan-money">cuesta %s</span></div></div>`,
			planCard(leaving), Esc(text(move["why"])),
			Esc(Money(asFloat(move["sale"]))), Esc(text(move["sale_note"])),
			Num(gain, 2), netClass, Esc(netText), orderNote(move),
			planCard(arriving), starts(asFloat(arriving["start_probability"])),
			Num(asFloat(arriving["xpts"]), 2),
			Esc(Money(asFloat(move["cost"]))))
	}
	out.WriteString(`</div>`)

	for _, warning := range plan["warnings"].([]string) {
		fmt.Fprintf(&out, `<p class="plan-warn">⚠ %s</p>`, Esc(warning))
	}
	return out.String()
}

// starts is the probability as plain text: inside the plan the pill would compete with the
// numbers that matter.
func starts(value *float64) string {
	if value == nil {
		return "sin dato de"
	}
	return fmt.Sprintf("%.0f%%", *value)
}

// orderNote is the warning that a swap has a right order. It only appears where it matters: a
// position you are already at the minimum in, where selling first leaves the eleven illegal
// until the signing lands, and the signing might not land.
func orderNote(move map[string]any) string {
	note := text(move["order"])
	if note == "" {
		return ""
	}
	return `<span class="plan-order" title="` + Esc(note) + `">primero ficha</span>`
}

// planCard is one player inside the plan: crest, name and position, nothing else.
func planCard(player map[string]any) string {
	slug := positionSlug[int(number(player["position_id"]))]
	if slug == "" {
		slug = "ent"
	}
	return fmt.Sprintf(`<span class="plan-who">%s<button class="p-name" type="button" `+
		`data-detail="%s">%s</button><span class="pos pos-%s">%s</span></span>`,
		crestOf(text(player["team_id"])), Esc(text(player["id"])),
		Esc(text(player["name"])), slug, Esc(text(player["position"])))
}

// HouseRules prints the pact. Only the hold rule changes what the tool proposes; the rest
// are here because a rule nobody can read is a rule nobody follows — and because the page is
// where you look before deciding.
func HouseRules(holdDays int, exceptions string, notes []string) string {
	var out strings.Builder
	out.WriteString(`<ul class="rules">`)
	if holdDays > 0 {
		line := fmt.Sprintf("Un jugador fichado o clausulado <strong>no se puede vender "+
			"durante %d dias</strong>. Vale para toda la liga, asi que un rival tampoco "+
			"puede venderte a quien acaba de fichar: a ese solo se llega por clausula.",
			holdDays)
		if exceptions != "" {
			line += " Excepciones acordadas: " + Esc(exceptions) + "."
		}
		fmt.Fprintf(&out, `<li class="rule-live"><span class="rule-tag">se aplica</span>%s</li>`,
			line)
	}
	for _, note := range notes {
		fmt.Fprintf(&out, `<li><span class="rule-tag rule-social">acuerdo</span>%s</li>`,
			Esc(note))
	}
	out.WriteString(`</ul>`)
	return out.String()
}

// Feed is the league's movements: who signed and sold, and for how much.
//
// Lineup changes are the bulk of the log and say nothing about the market, so they are
// dropped before anything is counted.
func Feed(events []map[string]any) string {
	var moves []map[string]any
	for _, event := range events {
		if !strings.Contains(text(event["kind"]), "alinea") {
			moves = append(moves, event)
		}
	}
	if len(moves) == 0 {
		return `<p class="empty">Hay actividad en la liga, pero solo cambios de alineacion: ` +
			`ninguna compra ni venta todavia.</p>`
	}

	withAmount := make([]map[string]any, 0, len(moves))
	for _, event := range moves {
		if amount := asFloat(event["amount"]); amount != nil && *amount != 0 {
			withAmount = append(withAmount, event)
		}
	}
	sort.SliceStable(withAmount, func(i, j int) bool {
		return number(withAmount[i]["amount"]) > number(withAmount[j]["amount"])
	})
	if len(withAmount) > 12 {
		withAmount = withAmount[:12]
	}

	var blocks strings.Builder
	// Two lists only earn their space once the log is long enough for them to differ.
	if len(moves) > 8 && len(withAmount) > 0 {
		blocks.WriteString(`<h3 class="kpi-label">Operaciones mas grandes</h3><div class="feed">`)
		for _, event := range withAmount {
			blocks.WriteString(FeedRow(event))
		}
		blocks.WriteString(`</div><h3 class="kpi-label" style="margin-top:20px">Lo ultimo</h3>`)
	}
	blocks.WriteString(`<div class="feed">`)
	for index, event := range moves {
		if index >= 20 {
			break
		}
		blocks.WriteString(FeedRow(event))
	}
	blocks.WriteString(`</div>`)
	return blocks.String()
}

// FeedRow is one movement. The amount alone does not say whether it was a steal or a panic
// buy, so the player's value on that same day travels with it.
func FeedRow(event map[string]any) string {
	player, buyer, seller := text(event["player"]), text(event["buyer"]), text(event["seller"])
	var body string
	switch {
	case player != "" && buyer != "" && seller != "":
		body = fmt.Sprintf(`<strong>%s</strong>: %s &rarr; %s`, Esc(player), Esc(seller), Esc(buyer))
	case player != "" && buyer != "":
		body = fmt.Sprintf(`<strong>%s</strong> &rarr; %s`, Esc(player), Esc(buyer))
	case player != "" && seller != "":
		body = fmt.Sprintf(`<strong>%s</strong>, vendido por %s`, Esc(player), Esc(seller))
	case player != "":
		body = fmt.Sprintf(`<strong>%s</strong>`, Esc(player))
	default:
		// Nobody named: dump what came, so an event shape we do not know yet is visible
		// rather than an empty row.
		fallback := buyer
		if fallback == "" {
			fallback = seller
		}
		if fallback == "" {
			blob, _ := json.Marshal(event["raw"])
			fallback = string(blob)
			if len(fallback) > 110 {
				fallback = fallback[:110]
			}
		}
		body = Esc(fallback)
	}

	amount := Missing
	if value := asFloat(event["amount"]); value != nil && *value != 0 {
		amount = Money(value)
	}

	extra := ""
	if then := asFloat(event["value_then"]); then != nil && *then != 0 {
		premium := 1.0
		if value := asFloat(event["premium"]); value != nil {
			premium = *value
		}
		status := "neutral"
		switch {
		case premium >= 1.25:
			status = "critical"
		case premium >= 1.08:
			status = "warning"
		case premium <= 0.98:
			status = "good"
		}
		extra = fmt.Sprintf(`<span class="feed-then">valia <b>%s</b></span>`+
			`<span class="pill-%s">%.2fx</span>`, Esc(Money(then)), status, premium)
	}

	date := strings.ReplaceAll(text(event["date"]), "T", " ")
	if len(date) > 16 {
		date = date[:16]
	}
	return fmt.Sprintf(`<div class="feed-row"><span class="feed-date">%s</span>`+
		`<span class="feed-kind">%s</span><span class="feed-body">%s</span>`+
		`<span class="feed-amount">%s</span>%s</div>`,
		Esc(date), Esc(text(event["kind"])), body, Esc(amount), extra)
}

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
		// "5.0x tu plantilla" no dice de que: el titulo lo escribe entero, con las dos
		// cifras que se estan comparando.
		ppm := number(row["ppm_at_clause"])
		explain := fmt.Sprintf("Pagando su clausula sacas %.2f pts/M; la mediana de tu "+
			"plantilla es %.2f pts/M, asi que rinde %.1f veces mas por cada millon.",
			ppm, ppm / *ratio, *ratio)
		note = fmt.Sprintf(`<span class="pill-note" title="%s">%s</span>`,
			Esc(explain), Esc(fmt.Sprintf("%.1fx pts/M de tu plantilla", *ratio)))
	}
	return fmt.Sprintf(`<span class="pill-%s">%s</span>`, status, Esc(verdict)) + note
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
		bid = ` data-bid="` + Esc(existing) + `"`
		// The amount is the point: "mi puja" told you nothing you could act on.
		label = "Tu puja"
		if amount := asFloat(listing["my_bid"]); amount != nil {
			label = "Tu puja " + Money(amount)
		}
	}
	return fmt.Sprintf(`<button class="bid" type="button" data-market="%s" `+
		`data-operation="%s" `+
		`data-player="%s" data-name="%s" data-min="%d" data-ideal="%d" data-value="%d"%s>%s</button>`,
		Esc(marketID), Esc(operation), Esc(text(row["id"])), Esc(text(row["name"])),
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

// RatioBadge is a price against market value, named as well as coloured.
//
// The same multiple means opposite things depending on which side of it you are: paying
// 1.30x is expensive, being *paid* 1.30x is a gift. `selling` flips the scale, and the
// name is written out so the colour is never the only reading.
func RatioBadge(ratio *float64, selling bool) string {
	if ratio == nil {
		return Missing
	}
	value := *ratio
	var status, label string
	if selling {
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
	} else {
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
	"money": true, "num": true, "int": true, "pct": true, "pct_plain": true,
	"mag": true, "ideal": true, "starts": true,
}

// Column is a header, how to read the value out of a row, and how to render it.
type Column struct {
	Header string
	Read   func(map[string]any) any
	Kind   string
}

// Section wraps a rendered body with its heading, an optional count badge and an optional
// note. The body arrives already rendered, so which section a row belongs to has to be
// passed down to the table rather than read back out here.
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

// PlayerColumns are the columns every player table shares. `costLabel` adds the price
// column that only the buying tables want, and `extra` appends the ones a section adds.
func PlayerColumns(costLabel string, extra ...Column) []Column {
	columns := []Column{
		{"★", func(row map[string]any) any { return row }, "star"},
		{"Jugador", func(row map[string]any) any { return row }, "player"},
		{"Valor", func(row map[string]any) any { return row["value"] }, "money"},
	}
	if costLabel != "" {
		columns = append(columns,
			Column{costLabel, func(row map[string]any) any { return row["entry_cost"] }, "money"})
	}
	columns = append(columns,
		Column{"xPts/j", func(row map[string]any) any { return row["xpts"] }, "num"},
		Column{"Pts/M", func(row map[string]any) any { return row["points_value"] }, "mag"},
		Column{"Titular", func(row map[string]any) any { return row["start_probability"] }, "starts"},
		Column{"Valor 7d", func(row map[string]any) any { return row["projected_pct"] }, "pct"},
		Column{"Historico", func(row map[string]any) any { return row["value_history"] }, "spark"},
		Column{"Proximo rival", func(row map[string]any) any {
			rival := text(row["next_rival"])
			if rival == "" {
				return nil
			}
			return rival + " " + Where(truthy(row["next_home"]))
		}, "text"},
		Column{"Pts 25/26", func(row map[string]any) any { return row["last_season_points"] }, "int"},
		Column{"Score", func(row map[string]any) any { return row["score"] }, "num"},
	)
	return append(columns, extra...)
}

// Where is home or away in one glyph: a house or a plane. Two words repeated on every row
// were reading as noise, and the glyph carries a title so it is not colour-alone reasoning.
func Where(home bool) string {
	if home {
		return "🏠"
	}
	return "✈️"
}

// insert puts a column at a position, which is how the page adds a section's own column
// in the middle of the shared ones rather than at the end.
func insert(columns []Column, at int, column Column) []Column {
	out := make([]Column, 0, len(columns)+1)
	out = append(out, columns[:at]...)
	out = append(out, column)
	return append(out, columns[at:]...)
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
}

var months = []string{"", "ene", "feb", "mar", "abr", "may", "jun",
	"jul", "ago", "sep", "oct", "nov", "dic"}
var weekdays = []string{"lun", "mar", "mie", "jue", "vie", "sab", "dom"}

// Calendar lays the clause unlocks out by day.
//
// A table sorts; a calendar answers a different question — *when does the league open up* —
// and at the start of a season the answer is dramatic: everything on the same day. That is
// worth seeing as a shape rather than reading as 28 rows.
// finishedMatch is the API's matchState for a played match. Kept here rather than importing the
// scheduler for one number: this package renders and depends on nothing of ours.
// See schedule.FinishedMatch, which has to agree.
const finishedMatch = 7

var monthNames = []string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio",
	"agosto", "septiembre", "octubre", "noviembre", "diciembre"}

var dayNames = []string{"dom", "lun", "mar", "mie", "jue", "vie", "sab"}

// MatchCalendar is the fixture list ahead, grouped by month and matchday. Your own teams are
// marked, because the only reason to read a fixture list here is to see who your players face.
func MatchCalendar(fixtures []map[string]any, mine map[string]int) string {
	if len(fixtures) == 0 {
		return `<p class="empty">Sin calendario disponible.</p>`
	}

	type group struct {
		week  int
		when  time.Time
		rows  []map[string]any
	}
	order := []string{}
	weeks := map[string]*group{}
	for _, fixture := range fixtures {
		kickoff, err := time.Parse(time.RFC3339, text(fixture["kickoff"]))
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%d", int(number(fixture["week"])))
		if _, seen := weeks[key]; !seen {
			weeks[key] = &group{week: int(number(fixture["week"])), when: kickoff}
			order = append(order, key)
		}
		if kickoff.Before(weeks[key].when) {
			weeks[key].when = kickoff
		}
		weeks[key].rows = append(weeks[key].rows, fixture)
	}

	var out strings.Builder
	out.WriteString(`<div class="months">`)
	month := ""
	for _, key := range order {
		block := weeks[key]
		label := fmt.Sprintf("%s %d", monthNames[int(block.when.Month())-1], block.when.Year())
		if label != month {
			if month != "" {
				out.WriteString(`</div>`)
			}
			month = label
			fmt.Fprintf(&out, `<div class="month"><h4>%s</h4>`, Esc(label))
		}

		yours, played := 0, 0
		var matches strings.Builder
		for _, fixture := range block.rows {
			local, visitor := text(fixture["local_id"]), text(fixture["visitor_id"])
			count := mine[local] + mine[visitor]
			yours += count
			classes := "match"
			if count > 0 {
				classes += " match-mine"
			}
			// A played match is history: it stays, because the run of fixtures reads better
			// whole, but it steps back so the eye lands on what is still to come.
			done := int(number(fixture["state"])) == finishedMatch
			if done {
				classes += " match-done"
				played++
			}
			kickoff, _ := time.Parse(time.RFC3339, text(fixture["kickoff"]))
			stamp := fmt.Sprintf("%s %02d:%02d", dayNames[int(kickoff.Weekday())],
				kickoff.Hour(), kickoff.Minute())
			// Once it is played the hour stops mattering and the result starts: same slot,
			// better answer.
			if done {
				if home, away := asFloat(fixture["local_score"]), asFloat(fixture["visitor_score"]); home != nil && away != nil {
					stamp = fmt.Sprintf("%.0f - %.0f", *home, *away)
				}
			}
			tag := ""
			if count > 0 {
				tag = fmt.Sprintf(`<span class="match-mine-tag">%d tuyo%s</span>`,
					count, plural(count))
			}
			fmt.Fprintf(&matches, `<div class="%s"><span class="match-side">%s%s</span>`+
				`<span class="match-vs">–</span><span class="match-side">%s%s</span>`+
				`<span class="match-when">%s</span>%s</div>`,
				classes, crestOf(local), Esc(text(fixture["local"])),
				crestOf(visitor), Esc(text(fixture["visitor"])), Esc(stamp), tag)
		}

		note := ""
		if yours > 0 {
			note = fmt.Sprintf(`<span class="j-mine">%d de los tuyos juegan</span>`, yours)
		}
		// A whole matchday behind us dims with its matches; one still running does not, because
		// the half that has not kicked off is the part being read.
		wrap := "jornada"
		if played == len(block.rows) {
			wrap += " jornada-done"
			note = `<span class="j-done">jugada</span>`
		}
		fmt.Fprintf(&out, `<div class="%s"><div class="j-head"><span class="j-num">J%d</span>`+
			`<span class="j-when">desde %s %d %s</span>%s</div>%s</div>`,
			wrap, block.week, dayNames[int(block.when.Weekday())], block.when.Day(),
			monthNames[int(block.when.Month())-1][:3], note, matches.String())
	}
	if month != "" {
		out.WriteString(`</div>`)
	}
	out.WriteString(`</div>`)
	return out.String()
}

func crestOf(teamID string) string {
	if _, known := Crests[teamID]; !known {
		return ""
	}
	return `<span class="crest crest-` + Esc(teamID) + `"></span>`
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func Calendar(entries []map[string]any, spending float64) string {
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
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
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

// clauseColumns are shared by both clause tables: mine and the rivals'.
func clauseColumns() []Column {
	return []Column{
		{"Se abre en", whole, "hours"},
		{"Fecha", func(row map[string]any) any {
			stamp := text(row["unlock_at"])
			if len(stamp) > 16 {
				stamp = stamp[:16]
			}
			return strings.ReplaceAll(stamp, "T", " ")
		}, "text"},
		{"Jugador", whole, "player"},
		{"Valor", field("value"), "money"},
		{"Cláusula", field("clause"), "money"},
		{"xPts/j", field("xpts"), "num"},
		{"Score", field("score"), "num"},
	}
}

// columnsFor is the column spec of a section, for the callers that need the columns rather
// than the rendered table — the actions section prepends a legend to its own.
func columnsFor(name string) []Column {
	switch name {
	case "acciones":
		return []Column{
			{"Que hacer", field("verdict"), "verdict"},
			{"★", whole, "star"},
			{"Jugador", whole, "player"},
			{"Motivo", field("why"), "text"},
			{"Coste", field("entry_cost"), "money"},
			{"Valor", field("value"), "money"},
			{"xPts/j", field("xpts"), "num"},
			{"Pts/M", field("points_value"), "mag"},
			{"Valor 7d", field("projected_pct"), "pct"},
		}
	}
	return nil
}

// SectionTable renders one of the page's tables by name. Same columns, same order, same
// section — the section matters because it decides whether a row says "mio".
func SectionTable(name string, rows []map[string]any) (string, error) {
	switch name {
	case "plantilla":
		return TableIn(PlayerColumns(""), rows, "Sin datos", "plantilla", false), nil

	case "mercado":
		// The buying table puts what futbolfantasy still considers profitable right next
		// to what the player would cost, and ends with how many rivals are already bidding
		// and the button to join them.
		columns := insert(PlayerColumns("Puja minima"), 4,
			Column{"Puja max. rentable", field("ideal_bid"), "ideal"})
		columns = append(columns,
			Column{"Pujas", func(row map[string]any) any {
				listing, _ := row["market"].(map[string]any)
				return listing["bids"]
			}, "int"})
		// First column, not last: the table scrolls sideways and a button that scrolls out
		// of view is a button that does not exist.
		columns = insert(columns, 0, Column{"", whole, "bid"})
		return TableIn(columns, rows, "El mercado libre esta vacio ahora mismo", "mercado",
			true), nil

	case "misventas":
		// Yours, so no star and no "mio": what matters is what you are asking, what he is
		// worth, and whether anybody has actually put money on the table.
		columns := []Column{
			{"Jugador", whole, "player"},
			{"Pides", field("entry_cost"), "money"},
			{"Valor", field("value"), "money"},
			{"Sobre valor", field("ask_ratio"), "ratio"},
			{"Ofertas", whole, "offer_tally"},
			{"Mejor", whole, "best_offer"},
			{"Cierra", whole, "listing_until"},
			{"xPts/j", field("xpts"), "num"},
			{"Valor 7d", field("projected_pct"), "pct"},
		}
		return TableIn(columns, rows, "Sin datos", "misventas", false), nil

	case "seguimiento":
		return TableIn(PlayerColumns(""), rows, "Sin datos", "", false), nil

	case "ventas":
		columns := PlayerColumns("", Column{"Motivos", field("reasons"), "list"})
		return TableIn(columns, rows, "Sin datos", "ventas", false), nil

	case "siempre":
		// The plan, not the state: what the standing instructions would do on the next
		// cycle, with the two numbers that decide it beside each row.
		columns := []Column{
			{"Jugador", field("name"), "text"},
			{"Accion", func(row map[string]any) any {
				return strings.ReplaceAll(text(row["action"]), "_", " ")
			}, "text"},
			{"Importe", field("amount"), "money"},
			{"Precio minimo", field("policy_min_price"), "money"},
			// Not an empty cell when there is no threshold: a blank would read as "no
			// limit", which is the opposite of what it means.
			{"Acepto desde", func(row map[string]any) any {
				if above := asFloat(row["policy_accept_above"]); above != nil && *above != 0 {
					return Money(above)
				}
				return "no vendo solo"
			}, "text"},
			{"Motivo", field("why"), "text"},
			{"Resultado", func(row map[string]any) any {
				if result := text(row["result"]); result != "" {
					return result
				}
				return "pendiente"
			}, "text"},
		}
		return TableIn(columns, rows, "Sin datos", "siempre", false), nil

	case "programados":
		columns := []Column{
			{"Jugador", field("name"), "text"},
			{"Dueño", field("owner"), "text"},
			{"Cláusula", field("clause"), "money"},
			{"Mi limite", field("max_pay"), "money"},
			{"Estado", func(row map[string]any) any {
				action := text(row["action"])
				return []any{action, raidPlanStatus[action]}
			}, "status"},
			{"Motivo", field("why"), "text"},
		}
		return TableIn(columns, rows, "Sin datos", "", false), nil

	case "rivales":
		// Who can buy today, which is a different table from who is winning: cash beats
		// points when the question is whether somebody can pay your clause tonight.
		columns := []Column{
			{"#", field("cash_position"), "int"},
			{"Manager", whole, "manager"},
			{"Poder de compra", whole, "power"},
			{"Puntos", field("points"), "int"},
			{"Jugadores", field("players"), "int"},
			{"Valor plantilla", field("squad_value"), "money"},
			{"Neto en fichajes", field("net_flow"), "money"},
			{"Caja estimada", field("estimated_cash"), "money"},
			{"Suma de cláusulas", field("clause_total"), "money"},
		}
		return TableIn(columns, rows, "Sin datos", "", false), nil

	case "acciones":
		// The one table that says what to do rather than what is true, so the verdict is
		// the first column and the reason sits right next to the name.
		return TableIn(columnsFor("acciones"), rows, "Sin datos", "", false), nil

	case "vencimientos":
		// Yours, and the clock is the subject: when the lock falls, anyone with the cash
		// can pay. So the countdown is the first column, not an afterthought.
		return TableIn(clauseColumns(), rows,
			"Ninguna se desbloquea en los proximos 10 dias.", "vencimientos", false), nil

	case "proximas":
		// The other side of the same clock. "¿Renta?" goes first because the useful
		// question is not when it opens but whether it is worth paying.
		columns := insert(clauseColumns(), 3, Column{"Dueño", field("owner"), "text"})
		columns = insert(columns, 0, Column{"¿Renta?", whole, "verdict_raid"})
		columns = append(columns,
			Column{"x valor", field("clause_premium"), "num"},
			Column{"Pts/M pagando", field("ppm_at_clause"), "mag"},
			Column{"Techo futbolfantasy", field("ideal_bid"), "ideal"})
		columns = insert(columns, 0, Column{"Clausulazo", whole, "raid"})
		return TableIn(columns, rows,
			"Ninguna cláusula interesante se abre en los proximos 10 dias.", "", false), nil

	case "enventa":
		// What rivals are asking, next to what the player is worth: this is where the
		// fantasy prices show up, so the ratio sits right after the price.
		columns := insert(PlayerColumns("Pide"), 2,
			Column{"Vende", field("seller"), "text"})
		columns = insert(columns, 5, Column{"Sobre valor", field("ask_ratio"), "ratio"})
		columns = insert(columns, 0, Column{"", whole, "bid"})
		return TableIn(columns, rows, "Nadie ha puesto a nadie en venta", "", true), nil

	case "clausulas":
		columns := insert(PlayerColumns("Cláusula"), 1,
			Column{"Dueño", field("owner"), "text"})
		columns = insert(columns, 4, Column{"x valor", field("clause_premium"), "num"})
		columns = insert(columns, 0, Column{"Clausulazo", whole, "raid"})
		return TableIn(columns, rows, "Ninguna cláusula a tu alcance", "", false), nil

	case "ofertas":
		// One row per offer, and the first thing it says is who: a rival's offer and the
		// game's daily automatic bid are the same money and completely different news.
		columns := []Column{
			{"", whole, "offer"},
			{"Quien", whole, "offer_from"},
			{"Jugador", whole, "player"},
			{"Te ofrecen", field("offer_amount"), "money"},
			{"Valor", field("value"), "money"},
			{"Pides", field("ask"), "money"},
			{"Sobre su valor", field("vs_value"), "ratio_sell"},
			{"Ofrecida", whole, "since"},
			{"Caduca", whole, "until"},
			{"xPts/j", field("xpts"), "num"},
		}
		return TableIn(columns, rows, "Sin datos", "ofertas", false), nil

	case "riesgo":
		// Not "who is good" but "who can be taken from you today", which is why the count
		// of rivals who could pay is a column of its own.
		columns := []Column{
			{"Jugador", whole, "player"},
			{"Valor", field("value"), "money"},
			{"Cláusula", field("clause"), "money"},
			{"x valor", field("clause_margin"), "num"},
			{"Rivales que pueden", field("threats"), "int"},
			{"El mas rico", field("top_threat"), "text"},
			{"xPts/j", field("xpts"), "num"},
			{"Score", field("score"), "num"},
		}
		return TableIn(columns, rows, "Sin exposicion relevante", "riesgo", false), nil
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
		class := ""
		if NumericKinds[column.Kind] {
			class = ` class="right"`
		}
		fmt.Fprintf(&head, `<th data-kind="%s"%s>%s</th>`, column.Kind, class,
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
			attrs = fmt.Sprintf(` data-position="%s" data-price="%.0f" data-name="%s"`,
				Esc(text(row["position"])), price,
				Esc(strings.ToLower(text(row["name"]))))
		}
		body.WriteString("<tr" + classes + attrs + ">")
		for _, column := range columns {
			inner, sortKey := CellIn(column.Read(row), column.Kind, section)
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
	// A table whose first column is a button pins that column: the scroll is horizontal and
	// the button is the reason the row is there.
	wrap := "table-wrap"
	if len(columns) > 0 && ActionKinds[columns[0].Kind] {
		wrap += " sticky-first"
	}
	return `<div class="` + wrap + `"><table class="sortable"><thead><tr>` + head.String() +
		"</tr></thead><tbody>" + body.String() + "</tbody></table></div>"
}

// ActionKinds are the cells that hold a button rather than a fact.
var ActionKinds = map[string]bool{"bid": true, "raid": true, "offer": true, "verdict": true,
	"verdict_raid": true}

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
		return RatioBadge(amount, false), sortKey(amount)
	case "ratio_sell":
		return RatioBadge(amount, true), sortKey(amount)
	case "bid":
		row, _ := value.(map[string]any)
		listing, _ := row["market"].(map[string]any)
		return BidButton(row), fmt.Sprintf("%d", int64(number(listing["min_bid"])))
	case "raid":
		row, _ := value.(map[string]any)
		return RaidButton(row), sortKey(asFloat(row["clause"]))
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
		teamID := text(row["team_id"])
		if teamID == "" {
			return Esc(name), name
		}
		return fmt.Sprintf(`<button class="p-name" type="button" data-manager="%s">%s</button>`,
			Esc(teamID), Esc(name)), name

	case "offer_from":
		// The machine's bid is named as such and greyed: it arrives every day whatever you
		// do, so it should never look like somebody made a decision about your player.
		row, _ := value.(map[string]any)
		who := text(row["offer_from"])
		if truthy(row["offer_from_market"]) {
			return `<span class="from-market" title="Oferta automatica del juego: llega cada ` +
				`dia y caduca al cerrar el mercado">` + Esc(who) + `</span>`, "zzz " + who
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
