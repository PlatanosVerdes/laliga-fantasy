package render

import (
	"fmt"
	"strings"
)

// ListRow is the panel's one list row: a lead (face or place), who with a second line, the
// figure that decides with its note, a chip and the buttons. The list it sits in decides
// whether it draws wide or compact, so the same markup works in a main column and an aside.
type ListRow struct {
	Lead, Name, Tag, Meta, Sub, Value, Note, Chip, Action, Tone, Attrs string
}

func (r ListRow) HTML() string {
	var who strings.Builder
	who.WriteString(`<span class="rwho"><span class="rname"><b>` + r.Name + `</b>` + r.Tag +
		`</span>`)
	for _, line := range []string{r.Meta, r.Sub} {
		if line != "" {
			who.WriteString(`<span class="meta">` + line + `</span>`)
		}
	}
	who.WriteString(`</span>`)
	value := ""
	if r.Value != "" || r.Note != "" {
		value = `<span class="rval">`
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

// row is a player as a list row.
func row(player map[string]any, meta, value, note, chip, action, tone string) string {
	return rowWith(player, "", meta, value, note, chip, action, tone)
}

// rowWith is row with extra attributes on the line, which is what the filter bar reads.
func rowWith(player map[string]any, attrs, meta, value, note, chip, action, tone string) string {
	line := playerRow(player)
	line.Meta, line.Value, line.Note, line.Chip, line.Action, line.Tone = meta, value, note, chip,
		action, tone
	line.Attrs += attrs
	return line.HTML()
}

// playerRow is the row's player half: face, name with the shield, position.
func playerRow(player map[string]any) ListRow {
	return ListRow{Lead: face(player, "sm"), Name: shieldName(player), Tag: posTag(player),
		Attrs: ` data-pid="` + Esc(text(player["id"])) + `"`}
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
