// Package futbolfantasy scrapes futbolfantasy.com. A port of fantasy/futbolfantasy.py.
//
// Three sources, in order of value:
//   - /analytics/laliga-fantasy/mercado — one request, every player, value deltas over
//     1/2/3/7/14/30 days, trend, acceleration, next fixture and the odds of starting it;
//   - /analytics/laliga-fantasy/mercado/detalle/{id} — per player: full value history,
//     season max/min and the site's "puja maxima rentable";
//   - /jugadores/{slug} — per matchday role, points and injury markers.
//
// This is the most fragile code in the project, because it reads someone else's HTML. It is
// also, for that reason, the most worth comparing: the parsers are pure functions of a page,
// so the same cached HTML goes through both implementations and the JSON they produce is
// compared field by field.
package futbolfantasy

import (
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

const Hour = time.Hour

// futbolfantasy is somebody's site, not an API we are entitled to hammer: keep a floor
// between requests that actually leave this process. Cache hits do not count.
const MinInterval = 400 * time.Millisecond

var (
	throttle    sync.Mutex
	lastRequest time.Time
)

func fetch(url string, ttl time.Duration, tag string) (string, error) {
	if cached, ok := httpx.Cached(url, tag, ttl); ok {
		return cached, nil
	}
	throttle.Lock()
	if wait := MinInterval - time.Since(lastRequest); wait > 0 && !lastRequest.IsZero() {
		time.Sleep(wait)
	}
	lastRequest = time.Now()
	throttle.Unlock()

	return httpx.Fetch(httpx.Request{URL: url, Headers: config.FFHeaders(), TTL: ttl,
		Tag: tag, Timeout: 60 * time.Second})
}

// num parses both raw JS numbers from data-* attributes ("-0.0739") and rendered Spanish
// text ("1.715.638", "8,1"). A comma is the tell for the latter: without it a dot is a
// decimal point, with it the dots are thousands separators.
func num(text string) *float64 {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.Contains(text, ",") {
		text = strings.ReplaceAll(text, ".", "")
		text = strings.ReplaceAll(text, ",", ".")
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil
	}
	return &value
}

func intOf(text string) *int {
	value, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return nil
	}
	return &value
}

// group returns the nth capture of the first match, or "".
func group(pattern *regexp.Regexp, text string, n int) string {
	found := pattern.FindStringSubmatch(text)
	if len(found) <= n {
		return ""
	}
	return found[n]
}

// --- the market page --------------------------------------------------------------------

var (
	teamSelect = regexp.MustCompile(`(?s)<select[^>]*name="equipo"[^>]*>(.*?)</select>`)
	teamOption = regexp.MustCompile(`<option[^>]*value="(\d+)"[^>]*>([^<]+)</option>`)
)

// ParseTeamMap reads the team filter's dropdown, which is where the site's own team ids and
// names are written down.
func ParseTeamMap(page string) map[string]string {
	block := teamSelect.FindStringSubmatch(page)
	if block == nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for _, pair := range teamOption.FindAllStringSubmatch(block[1], -1) {
		if pair[1] == "0" {
			continue
		}
		out[pair[1]] = strings.TrimSpace(html.UnescapeString(pair[2]))
	}
	return out
}

var attrKeys = []string{
	"id", "nombre", "posicion", "equipo", "valor", "valor1", "valor2", "valor3",
	"valor7", "valor14", "valor30", "tendencia", "aceleracion",
	"diferencia1", "diferencia2", "diferencia3", "diferencia7", "diferencia14",
	"diferencia30", "diferencia-pct1", "diferencia-pct2", "diferencia-pct3",
	"diferencia-pct7", "diferencia-pct14", "diferencia-pct30",
}

var (
	attrPatterns = func() map[string]*regexp.Regexp {
		out := make(map[string]*regexp.Regexp, len(attrKeys))
		for _, key := range attrKeys {
			out[key] = regexp.MustCompile(`data-` + regexp.QuoteMeta(key) + `="([^"]*)"`)
		}
		return out
	}()
	displayName = regexp.MustCompile(`class="player-name"><span[^>]*>([^<]+)<`)
	fixtureInfo = regexp.MustCompile(`title="Jornada (\d+)[^"]*?Pr[^"]*?rival: ([^("]+)\(([^)]+)\)"`)
	probability = regexp.MustCompile(`class="prob-\d+"[^>]*>(\d+)%<`)
	trendIcon   = regexp.MustCompile(`<i class="fas fa-[^"]*"\s+data-tooltip="([^"]+)"`)
	streakInfo  = regexp.MustCompile(`fa-caret-(up|down)[^>]*></i>\s*(\d+)<span`)
)

// ParseMarket is one row per player, keyed by futbolfantasy's own id.
func ParseMarket(page string) []map[string]any {
	teams := ParseTeamMap(page)
	chunks := strings.Split(page, `class="elemento_jugador`)
	rows := make([]map[string]any, 0, len(chunks))

	for _, chunk := range chunks[1:] {
		attrs := map[string]string{}
		for key, pattern := range attrPatterns {
			if found := pattern.FindStringSubmatch(chunk); found != nil {
				attrs[key] = found[1]
			}
		}
		if attrs["id"] == "" || attrs["nombre"] == "" {
			continue
		}

		row := map[string]any{
			"ff_id":         attrs["id"],
			"ff_name":       html.UnescapeString(attrs["nombre"]),
			"display_name":  nilString(strings.TrimSpace(html.UnescapeString(group(displayName, chunk, 1)))),
			"position":      nilString(attrs["posicion"]),
			"ff_team_id":    nilString(attrs["equipo"]),
			"ff_team":       nilString(teams[attrs["equipo"]]),
			"value":         num(attrs["valor"]),
			"trend_score":   num(attrs["tendencia"]),
			"acceleration":  num(attrs["aceleracion"]),
			"next_week":     nil,
			"next_rival":    nil,
			"next_home":     nil,
			"start_probability": nil,
			"trend_label":   nilString(html.UnescapeString(group(trendIcon, chunk, 1))),
			"streak_days":   nil,
			"streak_dir":    nil,
		}

		if found := fixtureInfo.FindStringSubmatch(chunk); found != nil {
			row["next_week"] = intOf(found[1])
			rival := strings.TrimSpace(html.UnescapeString(found[2]))
			row["next_rival"] = &rival
			// "casa" or "fuera": only the first three letters are load-bearing.
			home := strings.HasPrefix(strings.ToLower(strings.TrimSpace(found[3])), "cas")
			row["next_home"] = &home
		}
		if found := probability.FindStringSubmatch(chunk); found != nil {
			row["start_probability"] = intOf(found[1])
		}
		if found := streakInfo.FindStringSubmatch(chunk); found != nil {
			row["streak_days"] = intOf(found[2])
			direction := found[1]
			row["streak_dir"] = &direction
		}

		for _, window := range []int{1, 2, 3, 7, 14, 30} {
			suffix := strconv.Itoa(window)
			row["value_"+suffix+"d_ago"] = num(attrs["valor"+suffix])
			row["delta_"+suffix+"d"] = num(attrs["diferencia"+suffix])
			row["pct_"+suffix+"d"] = num(attrs["diferencia-pct"+suffix])
		}
		rows = append(rows, row)
	}
	return rows
}

func Market(ttl time.Duration) ([]map[string]any, error) {
	page, err := fetch(config.FFMarketURL, ttl, "ff_market")
	if err != nil {
		return nil, err
	}
	return ParseMarket(page), nil
}

// --- the per-player detail page ---------------------------------------------------------

var (
	idealBidCall = regexp.MustCompile(`parsePujaIdeal\(\s*(\d+)\s*\)`)
	injuryMark   = regexp.MustCompile(`xMin:\s*'([^']+)'`)
	seriesPair   = regexp.MustCompile(`\{"date":"([^"]+)","value":(\d+)\}`)
)

// ParseDetail reads the numbers the page leaves in its own JavaScript.
func ParseDetail(page string) map[string]any {
	jsNumber := func(name string) *float64 {
		pattern := regexp.MustCompile(`\b` + name + `\s*=\s*(-?\d+(?:\.\d+)?)`)
		found := pattern.FindStringSubmatch(page)
		if found == nil {
			return nil
		}
		value, err := strconv.ParseFloat(found[1], 64)
		if err != nil {
			return nil
		}
		return &value
	}
	jsString := func(name string) *string {
		pattern := regexp.MustCompile(`\b` + name + `\s*=\s*"([^"]*)"`)
		found := pattern.FindStringSubmatch(page)
		if found == nil {
			return nil
		}
		return &found[1]
	}
	jsSeries := func(name string) []map[string]any {
		pattern := regexp.MustCompile(`(?s)\b` + name + `\s*=\s*(\[.*?\]);`)
		found := pattern.FindStringSubmatch(page)
		if found == nil {
			return []map[string]any{}
		}
		out := []map[string]any{}
		for _, pair := range seriesPair.FindAllStringSubmatch(found[1], -1) {
			value, _ := strconv.Atoi(pair[2])
			out = append(out, map[string]any{
				// The dates arrive JSON-escaped inside the script.
				"date": strings.ReplaceAll(pair[1], `\/`, "/"), "value": value})
		}
		return out
	}

	ideal := 0
	if found := idealBidCall.FindStringSubmatch(page); found != nil {
		ideal, _ = strconv.Atoi(found[1])
	}

	seen := map[string]bool{}
	var injuries []string
	for _, found := range injuryMark.FindAllStringSubmatch(page, -1) {
		if !seen[found[1]] {
			seen[found[1]] = true
			injuries = append(injuries, found[1])
		}
	}
	sort.Strings(injuries)
	if injuries == nil {
		injuries = []string{}
	}

	history := jsSeries("player_chartjs")
	if len(history) == 0 {
		// A player recreated this season has no current curve, only last season's.
		history = jsSeries("player_chartjs_prev")
	}

	return map[string]any{
		"ideal_bid":           ideal,
		"max_value":           jsNumber("max_valor"),
		"min_value":           jsNumber("min_valor"),
		"max_date":            jsString("max_date"),
		"min_date":            jsString("min_date"),
		"history":             history,
		"prev_season_history": jsSeries("player_chartjs_prev"),
		"injury_marks":        injuries,
	}
}

// DetailTTL is how long a player's futbolfantasy page is trusted.
//
// It used to be a day, which was fine while the profitable ceiling was one more column. It is
// not fine now that the ceiling decides who the plan proposes and what the guard warns about:
// futbolfantasy recomputes it when a price moves, prices move every night at the market close,
// and a number that decides cannot be a day old. It cost a real confusion — the plan kept
// proposing a defender whose ceiling had already been withdrawn.
//
// Four hours: the evening recomputation is picked up the same night, and the scraping stays
// about a sixth of what a two-hour TTL would cost, which matters because their rate limit is
// real and the code has to stop for a whole cycle when it is hit.
const DetailTTL = 4 * time.Hour

func PlayerDetail(ffID string, ttl time.Duration) (map[string]any, error) {
	page, err := fetch(strings.ReplaceAll(config.FFDetailURL, "{ff_id}", ffID), ttl, "ff_detail")
	if err != nil {
		return nil, err
	}
	return ParseDetail(page), nil
}

// --- the per-player page ----------------------------------------------------------------

const GameKey = "laliga-fantasy"

var (
	tagStripper  = regexp.MustCompile(`<[^>]+>`)
	spaceRun     = regexp.MustCompile(`\s+`)
	pointsCell   = regexp.MustCompile(`(?s)<td class="data points[^"]*">(.*?)</td>`)
	pointsSpan   = regexp.MustCompile(`(?s)<span class="([^"]*)">\s*([-\d.,]*)\s*</span>`)
	headingOne   = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	roleAttr     = regexp.MustCompile(`data-posicion-laliga-fantasy="([^"]*)"`)
	weekCell     = regexp.MustCompile(`jorn-td">\s*(\d+)`)
	teamImage    = regexp.MustCompile(`<img class="img"[^>]*alt="([^"]+)"`)
	scoreCell    = regexp.MustCompile(`class="score (won|lost|draw)">([\d]+-[\d]+)<`)
	minuteOut    = regexp.MustCompile(`title="Salida"[^>]*>\s*(\d+)`)
)

func cell(chunk, css string) *string {
	pattern := regexp.MustCompile(`(?s)<td class="[^"]*\b` + regexp.QuoteMeta(css) +
		`\b[^"]*">(.*?)</td>`)
	found := pattern.FindStringSubmatch(chunk)
	if found == nil {
		return nil
	}
	text := tagStripper.ReplaceAllString(found[1], " ")
	text = strings.TrimSpace(spaceRun.ReplaceAllString(html.UnescapeString(text), " "))
	return nilString(text)
}

// gamePoints is the score for one fantasy game inside the row's points cell.
//
// The cell carries one span per fantasy game — nineteen of them — and each span's class is a
// quality band plus the game key, e.g. "... very-high laliga-fantasy". Only the span whose
// class contains the game key is that game's score; the visible `relevo` column is a
// different metric entirely, and reading it gave Raphinha 55 points instead of 179.
func gamePoints(row string) *float64 {
	found := pointsCell.FindStringSubmatch(row)
	if found == nil {
		return nil
	}
	for _, span := range pointsSpan.FindAllStringSubmatch(found[1], -1) {
		for _, class := range strings.Fields(span[1]) {
			if class == GameKey {
				return num(span[2])
			}
		}
	}
	return nil
}

// splitRows is the one place Python uses a lookahead, which RE2 does not support:
// re.split(r'<tr class="(?=plegado plegable)'). Splitting on the literal and keeping the
// pieces that start with it does the same job.
func splitRows(page string) []string {
	parts := strings.Split(page, `<tr class="`)
	out := make([]string, 0, len(parts))
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, "plegado plegable") {
			out = append(out, part)
		}
	}
	return out
}

func ParsePlayerPage(page string) map[string]any {
	name := (*string)(nil)
	if found := headingOne.FindStringSubmatch(page); found != nil {
		clean := strings.TrimSpace(spaceRun.ReplaceAllString(
			tagStripper.ReplaceAllString(found[1], ""), " "))
		name = nilString(clean)
	}

	matches := []map[string]any{}
	for _, chunk := range splitRows(page) {
		role := roleAttr.FindStringSubmatch(chunk)
		if role == nil {
			continue
		}
		// The main row ends at the first </tr>; what follows is its stats breakdown.
		row := strings.Split(chunk, "</tr>")[0]

		teams := []string{}
		for index, found := range teamImage.FindAllStringSubmatch(row, -1) {
			if index >= 2 {
				break
			}
			teams = append(teams, found[1])
		}

		entry := map[string]any{
			"week":       nil,
			"role":       role[1],
			"played":     role[1] != "NoConvocado",
			"home":       strings.Contains(chunk, `data-local="1"`),
			"teams":      teams,
			"result":     nil,
			"outcome":    nil,
			"minute_out": nil,
			"injured":    strings.Contains(strings.ToLower(row), "lesionado"),
			"points":     gamePoints(row),
			"sofascore":  nil,
		}
		if found := weekCell.FindStringSubmatch(row); found != nil {
			entry["week"] = intOf(found[1])
		}
		if found := scoreCell.FindStringSubmatch(row); found != nil {
			entry["result"], entry["outcome"] = &found[2], &found[1]
		}
		if found := minuteOut.FindStringSubmatch(row); found != nil {
			entry["minute_out"] = intOf(found[1])
		}
		if value := cell(row, "sofascore"); value != nil {
			entry["sofascore"] = num(*value)
		}
		matches = append(matches, entry)
	}

	played, total := 0, 0.0
	for _, match := range matches {
		points, ok := match["points"].(*float64)
		if match["played"] == true && ok && points != nil {
			played++
			total += *points
		}
	}
	out := map[string]any{
		"name": name, "matches": matches, "games_played": played,
		"total_points": total, "avg_points": nil,
	}
	if played > 0 {
		average := total / float64(played)
		out["avg_points"] = &average
	}
	return out
}

func PlayerPage(slug string, ttl time.Duration) (map[string]any, error) {
	page, err := fetch(strings.ReplaceAll(config.FFPlayerURL, "{slug}", slug), ttl, "ff_player")
	if err != nil {
		return nil, err
	}
	return ParsePlayerPage(page), nil
}

// --- the absence pages ------------------------------------------------------------------

var (
	absenceName     = regexp.MustCompile(`class="jugador"[^>]*>([^<]+)</a>`)
	absenceSlug     = regexp.MustCompile(`/jugadores/([a-z0-9-]+)"`)
	absenceReason   = regexp.MustCompile(`<span class="(?:lesion|sancion)">\s*([^<]+?)\s*</span>`)
	absenceSince    = regexp.MustCompile(`fa-calendar"></i>\s*([^<]+?)\s*</span>`)
	absenceUntil    = regexp.MustCompile(`<span class="gravedad-\d+">\s*([^<]+?)\s*</span>`)
	absenceSeverity = regexp.MustCompile(`gravedad-(\d+)`)
)

// ParseAbsences is one entry per absent player, with the reason in words.
//
// The LaLiga API only says injured / doubtful / sanctioned; the reason lives here — "Rotura
// de ligamento cruzado" — along with how long it has been going on and when he is expected
// back. That is the difference between a red badge and a red badge you can act on.
func ParseAbsences(page, kind string) []map[string]any {
	out := []map[string]any{}
	for _, chunk := range strings.Split(page, `<div class="elemento `)[1:] {
		classes := strings.SplitN(chunk, `"`, 2)[0]
		name := absenceName.FindStringSubmatch(chunk)
		if name == nil {
			continue
		}
		entry := map[string]any{
			"name": strings.TrimSpace(html.UnescapeString(name[1])),
			"slug": nilString(group(absenceSlug, chunk, 1)),
			// A doubt is its own kind, whichever page it was found on.
			"kind":     kind,
			"reason":   nilString(html.UnescapeString(group(absenceReason, chunk, 1))),
			"since":    nilString(html.UnescapeString(group(absenceSince, chunk, 1))),
			"until":    nilString(html.UnescapeString(group(absenceUntil, chunk, 1))),
			"severity": nil,
		}
		if strings.Contains(classes, "duda") {
			entry["kind"] = "duda"
		}
		if found := absenceSeverity.FindStringSubmatch(chunk); found != nil {
			entry["severity"] = intOf(found[1])
		}
		out = append(out, entry)
	}
	return out
}

// Absences are the injured and the suspended, from the two pages that list them.
func Absences(ttl time.Duration) []map[string]any {
	out := []map[string]any{}
	for _, source := range []struct{ URL, Kind string }{
		{config.FFInjuredURL, "lesionado"},
		{config.FFSuspendedURL, "sancionado"},
	} {
		page, err := fetch(source.URL, ttl, "ff_absences")
		if err != nil {
			// One page missing is a page missing, not a reason to lose the other.
			continue
		}
		out = append(out, ParseAbsences(page, source.Kind)...)
	}
	return out
}

func nilString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
