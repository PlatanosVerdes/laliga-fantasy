package render

import (
	"fmt"
	"regexp"
	"strings"
)

// ListRow is the panel's one list row: a lead (face or place), who with a second line, the
// figure that decides with its note, a chip and the buttons. The list it sits in decides
// whether it draws wide or compact, so the same markup works in a main column and an aside.
// A player's second line is Tags: his basics as small tags, never prose. Meta and Sub are the
// text lines of the rows that are not players.
type ListRow struct {
	Lead, Name, Tag, Tags, Meta, Sub, Value, Note, Why, Chip, Action, Tone, Attrs string
}

func (r ListRow) HTML() string {
	var who strings.Builder
	who.WriteString(`<span class="rwho"><span class="rname"><b>` + r.Name + `</b>` + r.Tag +
		`</span>`)
	for _, line := range []string{r.Meta, r.Sub} {
		if line != "" {
			who.WriteString(metaLine(line))
		}
	}
	who.WriteString(`</span>`)
	// The tags are the row's own grid item, so a narrow list can let them run under the figures.
	if r.Tags != "" {
		who.WriteString(`<span class="tags">` + r.Tags + `</span>`)
	}
	value := ""
	if r.Value != "" || r.Note != "" {
		value = `<span class="rval">`
		if r.Why != "" {
			value = `<span class="rval" title="` + r.Why + `">`
		}
		if r.Value != "" {
			value += `<b>` + r.Value + `</b>`
		}
		if r.Note != "" {
			value += `<span class="rnote">` + r.Note + `</span>`
		}
		value += `</span>`
	}
	action := ""
	if r.Action != "" {
		action = `<span class="ract">` + r.Action + `</span>`
	}
	class := strings.TrimSpace("r " + r.Tone)
	tail := `<span class="rtail"><span class="rchip">` + r.Chip + `</span>` + action + `</span>`
	return `<li class="` + class + `"` + r.Attrs + `>` + r.Lead + who.String() + value + tail +
		`</li>`
}

// row is a player as a list row; extra are tags of his that only this list has.
func row(player map[string]any, extra, value, note, chip, action, tone string) string {
	return rowWith(player, "", extra, value, note, chip, action, tone)
}

// rowWith is row with extra attributes on the line, which is what the filter bar reads.
func rowWith(player map[string]any, attrs, extra, value, note, chip, action, tone string) string {
	line := playerRow(player)
	line.Tags += extra
	line.Value, line.Note, line.Chip, line.Action, line.Tone = value, note, chip, action, tone
	line.Attrs += attrs
	return line.HTML()
}

// playerRow is the row's player half: face, name with the shield, position.
func playerRow(player map[string]any) ListRow {
	return ListRow{Lead: face(player, "sm"), Name: shieldName(player), Tag: posTag(player),
		Tags: playerTags(player), Attrs: ` data-pid="` + Esc(text(player["id"])) + `"`}
}

// teamRow is a manager as a list row: his place where a face would be.
func teamRow(teamID, place, name, meta, sub, value, note, chip, action, tone string) string {
	return ListRow{Lead: `<span class="rank-dot">` + place + `</span>`, Name: Esc(name),
		Meta: meta, Sub: sub, Value: value, Note: note, Chip: chip, Action: action, Tone: tone,
		Attrs: ` data-team="` + Esc(teamID) + `"`}.HTML()
}

// filterAttrs are what the filter bar reads off a row: position, price, and the words a search
// matches (name, team, owner or seller).
func filterAttrs(player map[string]any, price float64) string {
	words := []string{text(player["name"]), text(player["team_short"]), text(player["team"]),
		text(player["owner"]), text(mapOf(player["market"])["seller"])}
	return fmt.Sprintf(` data-position="%s" data-price="%.0f" data-name="%s" data-find="%s"`,
		Esc(text(player["position"])), price, Esc(strings.ToLower(text(player["name"]))),
		Esc(strings.ToLower(strings.Join(filterEmpty(words), " "))))
}

func rowList(items []string) string {
	return `<ul class="rows">` + strings.Join(items, "") + `</ul>`
}

// scrollList is a whole list in a box of fixed height that scrolls inside, instead of a fold.
func scrollList(items []string, height int) string {
	return fmt.Sprintf(`<div class="scrollbox" style="max-height:%dpx">`, height) +
		rowList(items) + `</div>`
}

// tg is the one small tag of the panel: same height, padding and font in every list and in
// the card's header.
func tg(content, class string) string {
	return `<span class="tg ` + class + `">` + content + `</span>`
}

// playerTags are a player's basics, always in this order: club, owner, role, starting odds,
// and then only the states that apply (held, shielded, moved down, injured or in doubt).
func playerTags(player map[string]any) string {
	var out strings.Builder
	if team := text(player["team_short"]); team != "" {
		out.WriteString(tg(crestOf(text(player["team_id"]))+Esc(team), "tg-team"))
	}
	owner := text(player["owner"])
	switch {
	case truthy(player["is_mine"]):
		owner = "tuyo"
	case owner == "":
		owner = fallbackText(text(mapOf(player["market"])["seller"]), "libre")
	}
	out.WriteString(tg(Esc(owner), "tg-owner"))
	out.WriteString(roleChip(player))
	if starts := asFloat(player["start_probability"]); starts != nil {
		out.WriteString(tg(fmt.Sprintf("titular %.0f %%", *starts), "tg-starts"))
	}
	if truthy(player["is_mine"]) && truthy(player["sale_locked"]) {
		out.WriteString(`<span class="tg tg-warn" title="no se puede vender hasta el ` +
			Esc(esWhen(text(player["hold_until"]))) + `">🔒 hasta ` +
			Esc(esDay(text(player["hold_until"]))) + `</span>`)
	}
	if truthy(player["shielded"]) {
		out.WriteString(tg("🛡", "tg-info"))
	}
	out.WriteString(roleDrop(player))
	if ring, _, reason := health(player); ring != "" {
		class := "tg-warn"
		if ring == "out" {
			class = "tg-bad"
		}
		out.WriteString(tg(Esc(reason), class))
	}
	return out.String()
}

// roleChip is futbolfantasy's category for him in his club, in the colour of their own icon.
func roleChip(player map[string]any) string {
	role := mapOf(player["role"])
	key := text(role["key"])
	if key == "" {
		return ""
	}
	return `<span class="tg role role-` + Esc(key) + `" title="` + Esc(text(role["note"])) + `">` +
		Esc(text(role["label"])) + `</span>`
}

// roleDrop is the warning when the editors just moved him down, or nothing.
func roleDrop(player map[string]any) string {
	role := mapOf(player["role"])
	if text(role["change"]) != "down" {
		return ""
	}
	return `<span class="tg tg-warn" title="` + Esc(text(role["note"])) + `">bajó a ` +
		Esc(text(role["label"])) + `</span>`
}

// metaChip is a chip inside a row's second line: the role, a warning, the eleven mark.
var metaChip = regexp.MustCompile(`<span class="(?:role|mk-chip|xi-mark)[^"]*"[^>]*>[^<]*</span>`)

// metaLine lays a second line out so the chips are never cut: the first item (his team, as a
// rule), the chips, then the rest of the text, which is the only part that shortens.
func metaLine(line string) string {
	chips := strings.Join(metaChip.FindAllString(line, -1), "")
	var parts []string
	for _, part := range strings.Split(metaChip.ReplaceAllString(line, ""), " · ") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return `<span class="meta">` + chips + `</span>`
	}
	tail := ""
	if len(parts) > 1 {
		tail = `<span class="mtail">` + strings.Join(parts[1:], " · ") + `</span>`
	}
	return `<span class="meta"><span class="mhead">` + parts[0] + `</span>` + chips + tail +
		`</span>`
}
