package render

import (
	"fmt"
	"sort"
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
	Notice   string      `json:"notice,omitempty"`
	// Voted is the ballot already sent this round: marked like the suggestion, a suggestion read
	// as a vote cast.
	Voted []string `json:"voted"`
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
	if live, ok := d.necroLive(); ok {
		return live, true
	}
	ranked := necroporra.Predict(d.Universe, d.NecroRoster, text(d.Universe["my_team_id"]))
	if len(ranked) == 0 {
		return View{}, false
	}
	picks := necroporra.Picks(ranked)
	selected := d.necroSelected(picks)
	voting := d.NecroOpen && d.NecroCanVote
	data := Necro{Facts: d.necroFacts(picks), Teams: []NecroTeam{}, CanVote: d.NecroCanVote,
		Open: d.NecroOpen, Gameweek: d.NecroGameweek, Notice: d.necroNotice(),
		Voted: append([]string{}, d.NecroChosen...)}
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

// necroLive is the round you already voted while it is played: the round's table first to
// last, and for every manager what the Constitution makes of his ballot, now and at the projected
// finish. One point if a pick ends last; an apology if a pick ends first, level at the top
// included; the denigration if a pick ends last and he ends first; breakfast for the last.
func (d Document) necroLive() (View, bool) {
	matchday := mapOf(d.Advice["matchday"])
	if d.NecroOpen || d.NecroLastRound == 0 || len(d.NecroLastPicks) == 0 ||
		int(number(matchday["week"])) != d.NecroLastRound {
		return View{}, false
	}
	managers := rows(matchday["managers"])
	if len(managers) == 0 {
		return View{}, false
	}
	ballots := map[string][]string{}
	for name, picks := range d.NecroBallots {
		ballots[necroKey(name)] = picks
	}
	me := ""
	for _, row := range managers {
		if truthy(row["is_me"]) {
			me = text(row["manager"])
		}
	}
	if _, ok := ballots[necroKey(me)]; !ok && me != "" {
		ballots[necroKey(me)] = d.NecroLastPicks
	}
	votedBy := map[string][]string{}
	for _, row := range managers {
		for _, pick := range ballots[necroKey(text(row["manager"]))] {
			votedBy[necroKey(pick)] = append(votedBy[necroKey(pick)], text(row["manager"]))
		}
	}

	shields := map[string]int{}
	for name, rounds := range d.NecroShields {
		shields[necroKey(name)] = rounds
	}
	now := necroStandings(managers, "points", shields)
	end := necroStandings(managers, "projection", shields)
	playing := truthy(matchday["live"])

	ordered := append([]map[string]any(nil), managers...)
	sort.SliceStable(ordered, func(one, two int) bool {
		return number(ordered[one]["points"]) > number(ordered[two]["points"])
	})
	teams := []NecroTeam{}
	for _, row := range ordered {
		name := text(row["manager"])
		key := necroKey(name)
		meta := fmt.Sprintf("previsto %dº", end.place[key])
		if waiting := int(number(row["waiting"])); waiting > 0 {
			meta += fmt.Sprintf(" · le faltan %d", waiting)
		}
		line := Row{Lead: []Seg{{T: fmt.Sprintf("%dº", now.place[key])}}, Name: name,
			Meta: []Seg{{T: meta}}, Value: esNum(number(row["points"]), 0), Note: []Seg{{T: "pts"}},
			Team: text(row["team_id"])}
		if picks := ballots[key]; len(picks) > 0 {
			kids := []Seg{{T: "vota a "}}
			for index, pick := range picks {
				if index > 0 {
					kids = append(kids, Seg{T: " y "})
				}
				kids = append(kids, Seg{T: pick, El: "b", C: "nb"})
			}
			line.Sub = append(line.Sub, Seg{Kids: kids})
		}
		if by := votedBy[key]; len(by) > 0 {
			line.Sub = append(line.Sub, Seg{Tip: strings.Join(by, ", "), Kids: []Seg{
				{T: "le votan "}, {T: fmt.Sprintf("%d", len(by)), El: "b", C: "nb"}}})
		}
		line.Chips = necroChips(key, ballots[key], now, playing, end, shields[key])
		for _, voter := range votedBy[key] {
			if necroKey(voter) == necroKey(me) {
				line.Chips = append([]Chip{{Icon: "point", C: "nc nc-voto", Tip: "tu voto"}},
					line.Chips...)
			}
		}
		if key == necroKey(me) {
			line.Tone, line.Name = "self", name+" (tú)"
			for _, voter := range votedBy[key] {
				line.Chips = append(line.Chips, Chip{T: "te vota " + voter, C: "acc"})
			}
		} else if containsKey(ballots[necroKey(me)], name) {
			line.Tone = "pick"
		}
		teams = append(teams, NecroTeam{Row: line})
	}

	state := "terminada"
	if playing {
		state = "en juego"
	}
	legend := []Seg{}
	for _, outcome := range []necroOutcome{necroDenigrate, necroPoint, necroApology, necroBreakfast} {
		legend = append(legend, Seg{Icon: outcome.icon, C: "nc " + outcome.class},
			Seg{T: " " + strings.SplitN(outcome.tip, ":", 2)[0] + "   "})
	}
	legend = append(legend, Seg{Icon: "shield", C: "nc nc-escudo"}, Seg{T: " protegido   "},
		Seg{Icon: "point", C: "nc nc-voto"}, Seg{T: " tu voto"})
	facts := [][]Seg{legend, {{T: "Relleno: ahora y al final · borde: solo ahora · " +
		"discontinuo: solo al final, si acaba como se prevé."}}}
	data := Necro{Facts: facts, Teams: teams, CanVote: d.NecroCanVote,
		Gameweek: d.NecroLastRound, Notice: d.necroNotice()}
	return View{Plain: true, Main: []Block{{Title: fmt.Sprintf("Necroporra · jornada %d %s",
			d.NecroLastRound, state), Count: count(len(teams)), Kind: "necro", Data: data,
			Sub: "De primero a último, con lo que la Constitución le da a cada uno."}}},
		true
}

// necroNotice is the banner for a Tebas page that could not be read: what is shown below it is
// then the forecast and the config's round, not the live game.
func (d Document) necroNotice() string {
	switch d.NecroProblem {
	case necroporra.ProblemLoggedOut:
		return "La sesión de Tebas ha caducado :( Hay que volver a copiar la cookie a necroporra.json."
	case necroporra.ProblemChanged:
		return "Tebas ha cambiado la página :( No consigo leer la Necroporra; lo de abajo puede no estar al día."
	}
	return ""
}

// necroStanding is one order of the round: each manager's place, and who holds each end.
type necroStanding struct {
	place                  map[string]int
	first, last, breakfast map[string]bool
}

func necroStandings(managers []map[string]any, key string, shields map[string]int) necroStanding {
	out := necroStanding{place: map[string]int{}, first: map[string]bool{}, last: map[string]bool{},
		breakfast: map[string]bool{}}
	top, bottom := 0.0, 0.0
	for index, row := range managers {
		value := number(row[key])
		if index == 0 || value > top {
			top = value
		}
		if index == 0 || value < bottom {
			bottom = value
		}
	}
	for _, row := range managers {
		name := necroKey(text(row["manager"]))
		place := 1
		for _, other := range managers {
			if number(other[key]) > number(row[key]) {
				place++
			}
		}
		out.place[name] = place
		out.first[name] = number(row[key]) == top
		out.last[name] = number(row[key]) == bottom
	}
	// Breakfast walks up from the bottom to the lowest score held by somebody unshielded, and
	// everyone unshielded on it brings it; with everyone shielded, the shields yield to the last.
	scores := []float64{}
	for _, row := range managers {
		scores = append(scores, number(row[key]))
	}
	sort.Float64s(scores)
	for _, score := range scores {
		for _, row := range managers {
			name := necroKey(text(row["manager"]))
			if number(row[key]) == score && shields[name] == 0 {
				out.breakfast[name] = true
			}
		}
		if len(out.breakfast) > 0 {
			return out
		}
	}
	for name, last := range out.last {
		out.breakfast[name] = last
	}
	return out
}

// necroOutcome is one thing the Constitution can hand a manager this round, drawn as an icon.
type necroOutcome struct{ icon, class, tip string }

var (
	necroApology   = necroOutcome{"bubble", "nc-perdon", "pide perdón: uno de sus dos va primero"}
	necroPoint     = necroOutcome{"target", "nc-punto", "+1: uno de sus dos va último"}
	necroDenigrate = necroOutcome{"flame", "nc-denigra", "puede denigrar: va primero y acertó el último"}
	necroBreakfast = necroOutcome{"cup", "nc-desayuno", "trae el desayuno"}
)

// necroChips is what one manager's ballot earns, one icon per outcome: solid when it holds now
// and at the projected finish, faded when only now, dashed when only at the finish.
func necroChips(who string, picks []string, now necroStanding, playing bool,
	end necroStanding, shielded int) []Chip {
	earns := func(order necroStanding) map[necroOutcome]bool {
		out := map[necroOutcome]bool{}
		for _, pick := range picks {
			if order.first[necroKey(pick)] {
				out[necroApology] = true
			}
			if order.last[necroKey(pick)] {
				out[necroPoint] = true
			}
		}
		if out[necroPoint] && order.first[who] {
			out[necroDenigrate] = true
		}
		if order.breakfast[who] {
			out[necroBreakfast] = true
		}
		return out
	}
	nowEarns, endEarns := earns(now), earns(end)
	if !playing {
		endEarns = nowEarns
	}
	var chips []Chip
	for _, outcome := range []necroOutcome{necroDenigrate, necroPoint, necroApology, necroBreakfast} {
		has, will := nowEarns[outcome], endEarns[outcome]
		switch {
		case has && will:
			chips = append(chips, Chip{Icon: outcome.icon, C: "nc " + outcome.class, Tip: outcome.tip})
		case has:
			chips = append(chips, Chip{Icon: outcome.icon, C: "nc fading " + outcome.class,
				Tip: outcome.tip + " (ahora; al final, previsto que no)"})
		case will:
			chips = append(chips, Chip{Icon: outcome.icon, C: "nc proj " + outcome.class,
				Tip: outcome.tip + " (previsto al final)"})
		}
	}
	if shielded > 0 {
		chips = append(chips, Chip{Icon: "shield", C: "nc nc-escudo",
			Tip: fmt.Sprintf("protegido del desayuno: %d ronda(s) más", shielded)})
	}
	return chips
}

func necroKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func containsKey(names []string, name string) bool {
	for _, candidate := range names {
		if necroKey(candidate) == necroKey(name) {
			return true
		}
	}
	return false
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
