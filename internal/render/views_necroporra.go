package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/necroporra"
)

// NecroTeam is one team of the side game's list: its row, and the ballot id the toggle sends.
type NecroTeam struct {
	Row Row    `json:"row"`
	ID  string `json:"id,omitempty"`
	On  bool   `json:"on,omitempty"`
}

// Necro is the side game's list: who finishes the round last, the facts above it and whether
// this server may vote.
type Necro struct {
	Facts    [][]Seg     `json:"facts"`
	Teams    []NecroTeam `json:"teams"`
	CanVote  bool        `json:"can_vote"`
	Open     bool        `json:"open"`
	Gameweek int         `json:"gameweek"`
}

// NecroData ranks the league from weakest to strongest for the side game: who finishes the round
// last. The signal is each manager's best eleven by expected points, the lineup he is likeliest
// to field, since the bench does not score.
//
// Every row is a toggle: you pick the two you vote (your own team is left out, you cannot vote
// for yourself). The suggestion is the two weakest and comes pre-marked, as does your current
// vote. The gameweek, deadline and current vote are read live, not written by hand. Between
// rounds the list stays as a forecast without toggles, under your vote in the last round.
func (d Document) NecroData() (View, bool) {
	if d.NecroGameweek == 0 && d.NecroLastRound == 0 {
		return View{}, false
	}
	ranked := necroporra.Predict(d.Universe, d.NecroRoster, text(d.Universe["my_team_id"]))
	if len(ranked) == 0 {
		return View{}, false
	}
	picks := necroporra.Picks(ranked)
	selected := d.necroSelected(picks)
	voting := d.NecroOpen && d.NecroCanVote
	data := Necro{Facts: d.necroFacts(picks), Teams: []NecroTeam{}, CanVote: d.NecroCanVote,
		Open: d.NecroOpen, Gameweek: d.NecroGameweek}
	for index, team := range ranked {
		row := Row{Lead: []Seg{{T: fmt.Sprintf("%d", index+1)}}, Name: team.Name,
			Meta:  []Seg{{T: fmt.Sprintf("%.0f pts temporada", team.SeasonPoints)}},
			Value: esNum(team.Predicted, 1), Note: []Seg{{T: "once est."}}}
		item := NecroTeam{Row: row}
		if team.NecroID != "" && voting {
			item.ID, item.On = team.NecroID, selected[team.NecroID]
		}
		data.Teams = append(data.Teams, item)
	}
	round := fmt.Sprintf("la jornada %d", d.NecroGameweek)
	if !d.NecroOpen {
		round = "la próxima jornada"
	}
	sub := "Quién acaba último " + round + ". Tu mejor once estimado, de más flojo a más fuerte."
	if voting {
		sub += " Marca los dos que votas."
	}
	return View{Plain: true, Main: []Block{{Title: "Necroporra", Count: count(len(ranked)),
		Sub: sub, Kind: "necro", Data: data}}}, true
}

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

func (d Document) necroFacts(picks []necroporra.Ranked) [][]Seg {
	facts := [][]Seg{}
	if !d.NecroOpen {
		facts = append(facts, []Seg{{T: "Votación cerrada hasta que nombren la siguiente ronda."}})
		if d.NecroLastRound > 0 && len(d.NecroLastPicks) > 0 {
			facts = append(facts, []Seg{{T: fmt.Sprintf("Tu voto en la jornada %d: ", d.NecroLastRound)},
				{T: strings.Join(d.NecroLastPicks, " y "), El: "b"}})
		}
	}
	if d.NecroOpen && d.NecroDeadline != "" {
		facts = append(facts, []Seg{{T: "Ronda abierta: "}, {T: d.NecroDeadline, El: "b"}})
	}
	if d.NecroOpen && len(d.NecroChosen) > 0 {
		facts = append(facts, []Seg{{T: "Tu voto actual: "},
			{T: d.necroNames(d.NecroChosen), El: "b"}})
	}
	if len(picks) == 2 {
		facts = append(facts, []Seg{{T: "Sugerencia: "},
			{T: picks[0].Name + " y " + picks[1].Name, El: "b"}})
	}
	return facts
}

// NecroRemind is the nudge on the Decidir tab while the round is open and you have not voted.
type NecroRemind struct {
	Week     int    `json:"week"`
	ClosesAt string `json:"closes_at,omitempty"`
	Tail     []Seg  `json:"tail"`
}

// necroReminder is the nudge, shown only while the round is open and you have not voted. On auto
// it says it will cast the two weakest for you near the close; otherwise it points at the
// Necroporra tab. NecroPreview forces it even once you have voted, for a local look at the card.
func (d Document) necroReminder() *NecroRemind {
	if !d.NecroOpen || d.NecroGameweek == 0 || !d.NecroCanVote {
		return nil
	}
	if len(d.NecroChosen) >= 2 && !d.NecroPreview {
		return nil
	}
	remind := &NecroRemind{Week: d.NecroGameweek,
		Tail: []Seg{{T: "Vótalos en la pestaña "}, {T: "Necroporra", Href: "#necroporra"}, {T: "."}}}
	if !d.NecroClosesAt.IsZero() {
		remind.ClosesAt = d.NecroClosesAt.Format(time.RFC3339)
	}
	if d.Mode == "auto" {
		picks := necroporra.Picks(necroporra.Predict(d.Universe, d.NecroRoster,
			text(d.Universe["my_team_id"])))
		if len(picks) == 2 {
			remind.Tail = []Seg{{T: "Si no votas, se enviarán los dos peores ("},
				{T: picks[0].Name + " y " + picks[1].Name, El: "b"}, {T: ") 2 h antes del cierre."}}
		}
	}
	return remind
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
