package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/necroporra"
)

// necroporraSection ranks the league from weakest to strongest for the side game: who finishes
// the round last. The signal is each manager's best eleven by expected points, the lineup he
// is likeliest to field, since the bench does not score.
//
// Every row is a toggle: you pick the two you vote (your own team is left out, you cannot vote
// for yourself). The suggestion is the two weakest and comes pre-marked, as does your current
// vote. The gameweek, deadline and current vote are read live, not written by hand.
func (d Document) necroporraSection() string {
	if d.NecroGameweek == 0 {
		return ""
	}
	ranked := necroporra.Predict(d.Universe, d.NecroRoster, text(d.Universe["my_team_id"]))
	if len(ranked) == 0 {
		return ""
	}
	picks := necroporra.Picks(ranked)
	selected := d.necroSelected(picks)
	body := d.necroFacts(picks) + d.necroTable(ranked, selected) + d.necroActions(selected)
	return mkSection("necroporra", "necroporra", "Necroporra",
		`<div class="mk-box">`+body+`</div>`,
		fmt.Sprintf("Quién acaba último la jornada %d. Tu mejor once estimado, de más flojo a "+
			"más fuerte. Marca los dos que votas.", d.NecroGameweek), len(ranked))
}

// necroSelected is what comes pre-ticked: your current vote if you have one, otherwise the
// suggestion.
func (d Document) necroSelected(picks []necroporra.Ranked) map[string]bool {
	selected := map[string]bool{}
	if len(d.NecroChosen) > 0 {
		for _, id := range d.NecroChosen {
			selected[id] = true
		}
		return selected
	}
	for _, pick := range picks {
		selected[pick.NecroID] = true
	}
	return selected
}

func (d Document) necroFacts(picks []necroporra.Ranked) string {
	var facts strings.Builder
	if d.NecroDeadline != "" {
		fmt.Fprintf(&facts, `<p class="lead">Ronda abierta: <b>%s</b></p>`, Esc(d.NecroDeadline))
	}
	if len(d.NecroChosen) > 0 {
		fmt.Fprintf(&facts, `<p class="lead">Tu voto actual: <b>%s</b></p>`,
			Esc(d.necroNames(d.NecroChosen)))
	}
	if len(picks) == 2 {
		fmt.Fprintf(&facts, `<p class="lead">Sugerencia: <b>%s</b></p>`,
			Esc(picks[0].Name+" y "+picks[1].Name))
	}
	return facts.String()
}

// necroActions is the send control under the list, aligned right like the rest of the panel's
// row actions.
func (d Document) necroActions(selected map[string]bool) string {
	if !d.NecroCanVote {
		return `<p class="mk-empty">Sin sesión de la necroporra configurada: solo predicción.</p>`
	}
	disabled := " disabled"
	if countSelected(selected) == 2 {
		disabled = ""
	}
	attrs := fmt.Sprintf(` data-gw="%d"%s`, d.NecroGameweek, disabled)
	vote := button("Enviar", "primary", "necro-vote", attrs)
	return `<div class="necro-actions"><span class="necro-msg"></span>` + vote + `</div>`
}

func (d Document) necroTable(ranked []necroporra.Ranked, selected map[string]bool) string {
	items := make([]string, 0, len(ranked))
	for index, team := range ranked {
		line := ListRow{
			Lead:  fmt.Sprintf(`<span class="rank-dot">%d</span>`, index+1),
			Name:  Esc(team.Name),
			Meta:  fmt.Sprintf("%.0f pts temporada", team.SeasonPoints),
			Value: decimal(team.Predicted),
			Note:  "once est.",
		}
		if team.NecroID != "" && d.NecroCanVote {
			on := selected[team.NecroID]
			class, label, pressed := "necro-pick", "votar", "false"
			if on {
				class, label, pressed = "necro-pick on", "voto ✓", "true"
				line.Tone = "pick"
			}
			line.Action = button(label, "ghost", class,
				fmt.Sprintf(` data-id="%s" data-name="%s" aria-pressed="%s"`,
					Esc(team.NecroID), Esc(team.Name), pressed))
		}
		items = append(items, line.HTML())
	}
	return rowList(items)
}

// necroReminder is the nudge on the Decidir tab, shown only while the round is open and you
// have not voted. When the server runs on auto it says it will cast the two weakest for you
// near the close; otherwise it points at the Necroporra tab. NecroPreview forces it even once
// you have voted, for a local look at the card.
func (d Document) necroReminder() string {
	if d.NecroGameweek == 0 || !d.NecroCanVote {
		return ""
	}
	if len(d.NecroChosen) >= 2 && !d.NecroPreview {
		return ""
	}
	when := ""
	if !d.NecroClosesAt.IsZero() {
		when = fmt.Sprintf(` cierra en <b data-deadline="%s"></b>`,
			d.NecroClosesAt.Format(time.RFC3339))
	}
	tail := `Vótalos en la pestaña <a href="#necroporra">Necroporra</a>.`
	if d.Mode == "auto" {
		picks := necroporra.Picks(necroporra.Predict(d.Universe, d.NecroRoster,
			text(d.Universe["my_team_id"])))
		if len(picks) == 2 {
			tail = fmt.Sprintf(`Si no votas, se enviarán los dos peores (<b>%s</b>) 2 h antes `+
				`del cierre.`, Esc(picks[0].Name+" y "+picks[1].Name))
		}
	}
	return fmt.Sprintf(`<div class="necro-remind warn"><b>Necroporra J%d</b>: aún no has `+
		`votado%s. %s</div>`, d.NecroGameweek, when, tail)
}

// necroNames turns a list of tebasfury ids into their manager names, for the "current vote"
// line. An id with no name in the roster is shown as itself.
func (d Document) necroNames(ids []string) string {
	byID := make(map[string]string, len(d.NecroRoster))
	for name, id := range d.NecroRoster {
		byID[id] = name
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, fallbackText(byID[id], id))
	}
	return strings.Join(names, ", ")
}

func countSelected(selected map[string]bool) int {
	count := 0
	for _, on := range selected {
		if on {
			count++
		}
	}
	return count
}

// decimal is one place with the Spanish comma, matching the rest of the page.
func decimal(value float64) string {
	return strings.Replace(fmt.Sprintf("%.1f", value), ".", ",", 1)
}
