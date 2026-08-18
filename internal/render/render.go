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
	"strconv"
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

	team := text(row["team_short"])
	if team == "" {
		team = text(row["team"])
	}
	return fmt.Sprintf(`<span class="p-cell">%s`+
		`<button class="p-name" type="button" data-detail="%s">%s</button>`+
		`<span class="pos pos-%s">%s</span>`+
		`<span class="p-meta">%s</span>%s</span>`,
		badge, Esc(text(row["id"])), Esc(text(row["name"])), slug,
		Esc(text(row["position"])), Esc(team), strings.Join(flags, ""))
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
	case float64:
		return typed != 0
	case string:
		return typed != ""
	}
	return false
}

// Verdicts are the five recommendations, each with its glyph and status. The glyph is not
// decoration: it is what carries the meaning where the colour cannot be seen.
var Verdicts = map[string]struct{ Label, Icon, Status string }{
	"buy":     {"Fichar", "▲", "good"},
	"clause":  {"Clausulazo", "◆", "good"},
	"protect": {"Subir clausula", "!", "warning"},
	"sell":    {"Vender", "▼", "serious"},
	"out":     {"Baja", "✕", "critical"},
}

// VerdictOrder is the sort order, worst first, so a table sorted by the column reads as a
// severity list rather than alphabetically.
var VerdictOrder = []string{"out", "buy", "clause", "protect", "sell"}

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
		note = fmt.Sprintf(`<span class="pill-note">%s</span>`,
			Esc(fmt.Sprintf("%.1fx tu plantilla", *ratio)))
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
	bid := ""
	label := "Pujar"
	if existing := text(listing["my_bid_id"]); existing != "" {
		// Unquoted, as Python writes it: the value is an id and the attribute parses
		// either way, and a byte comparison does not forgive improvements.
		bid = " data-bid=" + existing
		label = "Mi puja"
	}
	return fmt.Sprintf(`<button class="bid" type="button" data-market="%s" `+
		`data-player="%s" data-name="%s" data-min="%d" data-ideal="%d" data-value="%d"%s>%s</button>`,
		Esc(marketID), Esc(text(row["id"])), Esc(text(row["name"])),
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
	return fmt.Sprintf(`<button class="op bid" data-op="accept_offer" %s type="button">Aceptar`+
		`</button> <button class="op danger" data-op="decline_offer" %s `+
		`type="button">Rechazar</button>`, common, common)
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
			where := "fuera"
			if truthy(row["next_home"]) {
				where = "casa"
			}
			return rival + " (" + where + ")"
		}, "text"},
		Column{"Pts 25/26", func(row map[string]any) any { return row["last_season_points"] }, "int"},
		Column{"Score", func(row map[string]any) any { return row["score"] }, "num"},
	)
	return append(columns, extra...)
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

// SectionTable renders one of the page's tables by name. Same columns, same order, same
// section — the section matters because it decides whether a row says "mio".
func SectionTable(name string, rows []map[string]any) (string, error) {
	switch name {
	case "plantilla":
		return TableIn(PlayerColumns(""), rows, "Sin datos", "plantilla", false), nil

	case "mercado":
		// The buying table puts what futbolfantasy still considers profitable right next
		// to what the player would cost.
		columns := insert(PlayerColumns("Puja minima"), 4,
			Column{"Puja max. rentable", field("ideal_bid"), "ideal"})
		return TableIn(columns, rows, "Sin datos", "mercado", true), nil

	case "misventas":
		// Yours, so no star and no "mio": what matters is what you are asking against what
		// he is worth.
		columns := []Column{
			{"Jugador", whole, "player"},
			{"Valor", field("value"), "money"},
			{"Pides", field("entry_cost"), "money"},
			{"Sobre valor", field("ask_ratio"), "ratio"},
			{"xPts/j", field("xpts"), "num"},
			{"Valor 7d", field("projected_pct"), "pct"},
		}
		return TableIn(columns, rows, "Sin datos", "misventas", false), nil

	case "seguimiento":
		return TableIn(PlayerColumns(""), rows, "Sin datos", "", false), nil

	case "ventas":
		columns := PlayerColumns("", Column{"Motivos", field("reasons"), "list"})
		return TableIn(columns, rows, "Sin datos", "ventas", false), nil

	case "rivales":
		// Who can buy today, which is a different table from who is winning: cash beats
		// points when the question is whether somebody can pay your clause tonight.
		columns := []Column{
			{"#", field("cash_position"), "int"},
			{"Manager", func(row map[string]any) any {
				if manager := text(row["manager"]); manager != "" {
					return manager
				}
				return row["name"]
			}, "text"},
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
		columns := []Column{
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
		return TableIn(columns, rows, "Sin datos", "", false), nil

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
			Column{"Techo futbolfantasy", field("ideal_bid"), "ideal"},
			Column{"Clausulazo", whole, "raid"})
		return TableIn(columns, rows,
			"Ninguna cláusula interesante se abre en los proximos 10 dias.", "", false), nil

	case "enventa":
		// What rivals are asking, next to what the player is worth: this is where the
		// fantasy prices show up, so the ratio sits right after the price.
		columns := insert(PlayerColumns("Pide"), 2,
			Column{"Vende", field("seller"), "text"})
		columns = insert(columns, 5, Column{"Sobre valor", field("ask_ratio"), "ratio"})
		columns = append(columns, Column{"", whole, "bid"})
		return TableIn(columns, rows, "Nadie ha puesto a nadie en venta", "", true), nil

	case "clausulas":
		columns := insert(PlayerColumns("Cláusula"), 1,
			Column{"Dueño", field("owner"), "text"})
		columns = insert(columns, 4, Column{"x valor", field("clause_premium"), "num"})
		columns = append(columns, Column{"Clausulazo", whole, "raid"})
		return TableIn(columns, rows, "Ninguna cláusula a tu alcance", "", false), nil

	case "ofertas":
		// Yours by definition, so no star: what matters is what they pay against what he
		// is worth, and when the offer dies.
		columns := []Column{
			{"Jugador", whole, "player"},
			{"Valor", field("value"), "money"},
			{"Pides", field("ask"), "money"},
			{"Te ofrecen", field("offer_amount"), "money"},
			{"Sobre su valor", field("vs_value"), "ratio_sell"},
			{"Ofertas", field("offer_count"), "int"},
			{"Caduca", func(row map[string]any) any {
				stamp := text(row["offer_expires"])
				if len(stamp) > 16 {
					stamp = stamp[:16]
				}
				return strings.ReplaceAll(stamp, "T", " ")
			}, "text"},
			{"xPts/j", field("xpts"), "num"},
			{"", whole, "offer"},
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
	return `<div class="table-wrap"><table class="sortable"><thead><tr>` + head.String() +
		"</tr></thead><tbody>" + body.String() + "</tbody></table></div>"
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
