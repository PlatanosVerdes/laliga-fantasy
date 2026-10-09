package futbolfantasy

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// The team hierarchies: futbolfantasy's editors sort every squad into Clave, Importantes,
// Rotación, Revulsivos, Reservas and Descartes, with a line on why and an arrow when a player
// moved. One page per club.

// HierarchyTTL keeps a day's pages for six hours: the editors revise them a couple of times a
// week, around the matchdays.
const HierarchyTTL = 6 * time.Hour

// Roles are the categories in their order, best first, keyed by the icon the page draws.
var Roles = []struct{ Key, Label string }{
	{"clave", "Clave"}, {"importante", "Importante"}, {"rotacion", "Rotación"},
	{"revulsivo", "Revulsivo"}, {"reserva", "Reserva"}, {"descarte", "Descarte"},
}

// RoleRank is a category's place in Roles, from 1, or 0 when it is not one.
func RoleRank(key string) int {
	for index, role := range Roles {
		if role.Key == key {
			return index + 1
		}
	}
	return 0
}

var (
	teamLink       = regexp.MustCompile(`/laliga/equipos/([a-z0-9-]+)"`)
	roleHeader     = regexp.MustCompile(`uploads/images/([a-z]+)\d*\.png">\s*([^<]+?)\s*</header>`)
	roleEntry      = regexp.MustCompile(`<div class="elemento `)
	rolePlayerLink = regexp.MustCompile(`href="https://www\.futbolfantasy\.com/jugadores/([a-z0-9-]+)[^"]*" class="jugador">\s*([^<]+?)\s*</a>`)
	roleFicha      = regexp.MustCompile(`/jugadores/ficha/(\d+)\.png`)
	roleNoteFull   = regexp.MustCompile(`data-text="([^"]*)"`)
	roleNoteShort  = regexp.MustCompile(`(?s)class="observaciones[^"]*">\s*(.*?)\s*(?:<a |</div>)`)
	roleChange     = regexp.MustCompile(`jer-change[^"]*fa-caret-(up|down)`)
	roleStatus     = regexp.MustCompile(`uploads/images/([a-z]+)_box_min\.png`)
)

// TeamSlugs are the clubs of the league, read off any page that carries the team menu.
func TeamSlugs(page string) []string {
	seen := map[string]bool{}
	var out []string
	for _, found := range teamLink.FindAllStringSubmatch(page, -1) {
		if !seen[found[1]] {
			seen[found[1]] = true
			out = append(out, found[1])
		}
	}
	sort.Strings(out)
	return out
}

// ParseHierarchy is one row per player of a club's hierarchy page: his futbolfantasy id and
// page, the category, the editors' note, and whether he went up or down.
func ParseHierarchy(page, team string) []map[string]any {
	headers := roleHeader.FindAllStringSubmatchIndex(page, -1)
	starts := roleEntry.FindAllStringIndex(page, -1)
	teamURL := strings.ReplaceAll(config.FFHierarchyURL, "{team}", team)
	var out []map[string]any
	for index, start := range starts {
		end := len(page)
		if index+1 < len(starts) {
			end = starts[index+1][0]
		}
		// A player belongs to the category whose section he is in: the last header before
		// him, with no section closing in between.
		key, label := "", ""
		for _, header := range headers {
			if header[0] > start[0] {
				break
			}
			key, label = "", ""
			if !strings.Contains(page[header[0]:start[0]], "</section>") {
				key = strings.TrimSuffix(page[header[2]:header[3]], "s")
				label = page[header[4]:header[5]]
			}
		}
		if close := strings.Index(page[start[0]:end], "</section>"); close >= 0 {
			end = start[0] + close
		}
		chunk := page[start[0]:end]
		link := rolePlayerLink.FindStringSubmatch(chunk)
		if link == nil || RoleRank(key) == 0 {
			continue
		}
		note := ""
		if found := roleNoteFull.FindStringSubmatch(chunk); found != nil {
			note = found[1]
		} else if found := roleNoteShort.FindStringSubmatch(chunk); found != nil {
			note = tagStripper.ReplaceAllString(found[1], "")
		}
		note = strings.TrimSpace(spaceRun.ReplaceAllString(html.UnescapeString(note), " "))
		row := map[string]any{
			"ff_id": group(roleFicha, chunk, 1), "ff_name": html.UnescapeString(link[2]),
			"slug": link[1], "url": strings.ReplaceAll(config.FFPlayerURL, "{slug}", link[1]),
			"team": team, "team_url": teamURL,
			"key": key, "label": Roles[RoleRank(key)-1].Label, "category": label,
			"rank": RoleRank(key), "note": note,
			"change": group(roleChange, chunk, 1), "status": group(roleStatus, chunk, 1),
		}
		out = append(out, row)
	}
	return out
}

// Hierarchies are every club's hierarchy. The clubs come from the injured page's team menu,
// which the absences read anyway; a club whose page fails is just missing.
func Hierarchies(ttl time.Duration) []map[string]any {
	menu, err := fetch(config.FFInjuredURL, 3*time.Hour, "ff_absences")
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, team := range TeamSlugs(menu) {
		page, err := fetch(strings.ReplaceAll(config.FFHierarchyURL, "{team}", team), ttl,
			"ff_hierarchy")
		if err != nil {
			continue
		}
		out = append(out, ParseHierarchy(page, team)...)
	}
	return out
}
