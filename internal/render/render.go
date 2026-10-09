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

// Positions carry a colour on the page, chosen by Jorge: keeper orange, defence lilac,
// midfield turquoise, attack yellow. Only the slug matters here; the hues live in the CSS.
var positionSlug = map[int]string{1: "por", 2: "def", 3: "med", 4: "del", 5: "ent"}

// The API reports status in English; the page is in Spanish.
var statusLabels = map[string]string{
	"injured": "lesionado", "doubtful": "duda", "sanctioned": "sancionado",
	"suspended": "sancionado", "out_of_league": "fuera de la liga",
	"unknown": "sin datos",
}

// Crests holds the team badges as data URIs, keyed by team id. Filled by the caller,
// because the images come from the API and this package does not fetch.
var Crests = map[string]string{}

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

// Build is the version of the binary serving this page, stamped at compile time. Empty renders
// nothing: a page built by hand should not claim a version it does not have.
var Build string

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

// feedHidden are the log's per-matchday and league-wide notices (types 7 and 10): no money and no
// player moves, so they are not movements.
var feedHidden = map[int]bool{7: true, 10: true}

// ShieldType is the log's shield bought (see model.ShieldType): it moves no money.
const ShieldType = 4

// VerdictOrder is the sort order, worst first, so a table sorted by the column reads as a
// severity list rather than alphabetically.
// Offers first after the emergencies: they are the only rows with somebody else's money on a
// clock, and the clock runs whether or not the page was open.
var VerdictOrder = []string{"out", "cash", "buy", "bidding", "clause", "protect", "sell",
	"cash_blocked"}

func feedDate(event map[string]any) string {
	date := strings.ReplaceAll(text(event["date"]), "T", " ")
	if len(date) > 16 {
		date = date[:16]
	}
	return date
}

// buyerUser and sellerUser read the two sides of an event. cash == -1 means user1 paid, which
// loadActivity already used to fill buyer and seller, so the mapping has to match it.
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

// Where is home or away, in words.
func Where(home bool) string {
	if home {
		return "en casa"
	}
	return "fuera"
}

var months = []string{"", "ene", "feb", "mar", "abr", "may", "jun",
	"jul", "ago", "sep", "oct", "nov", "dic"}

// FinishedMatch is the API's matchState for a played match. Kept here rather than importing the
// scheduler for one number: this package renders and depends on nothing of ours.
// See schedule.FinishedMatch, which has to agree.
const FinishedMatch = 7

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
