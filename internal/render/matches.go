package render

import (
	"fmt"
	"sort"
	"strings"
)

// --- Partidos --------------------------------------------------------------------------

func (d Document) matchesView() SectionView { return shell("v-partidos", "partidos", "partidos") }

// FixtureLine is one match of the matchday with the players of mine in it.
type FixtureLine struct {
	When       string        `json:"when"`
	Local      string        `json:"local"`
	Visitor    string        `json:"visitor"`
	LocalID    string        `json:"local_id"`
	VisitorID  string        `json:"visitor_id"`
	LocalCrest bool          `json:"local_crest"`
	VisitCrest bool          `json:"visitor_crest"`
	Score      string        `json:"score,omitempty"`
	Players    []FixtureChip `json:"players"`
}

// FixtureChip is a player as a chip: face, name and xPts in its colour, grey on the bench.
type FixtureChip struct {
	Player map[string]any `json:"player"`
	XPts   string         `json:"xpts"`
	Class  string         `json:"class"`
}

type Fixtures struct {
	Lines []FixtureLine `json:"lines"`
	Idle  []string      `json:"idle,omitempty"`
}

// MatchdayCard is the three figures of the matchday: when the lineup closes, the last match and
// what my eleven is expected to score.
type MatchdayCard struct {
	Deadline  string `json:"deadline"`
	First     string `json:"first"`
	Last      string `json:"last"`
	LastWhen  string `json:"last_when"`
	LastMine  int    `json:"last_mine"`
	Total     string `json:"total"`
	Average   string `json:"average,omitempty"`
	FirstWhen string `json:"first_when"`
}

// History is what Previsto vs real needs to draw: the matchday and my planned total; the
// points per matchday come from /api/season.
type History struct {
	Week    int     `json:"week"`
	Planned float64 `json:"planned"`
	Hit     string  `json:"hit,omitempty"`
}

type ComingWeek struct {
	Label   string        `json:"label"`
	Day     string        `json:"day"`
	Matches []ComingMatch `json:"matches"`
	Yours   string        `json:"yours"`
}

type ComingMatch struct {
	LocalID    string `json:"local_id"`
	VisitorID  string `json:"visitor_id"`
	Local      string `json:"local"`
	Visitor    string `json:"visitor"`
	LocalCrest bool   `json:"local_crest"`
	VisitCrest bool   `json:"visitor_crest"`
	Count      int    `json:"count"`
}

func crestKnown(teamID string) bool {
	_, known := Crests[teamID]
	return known
}

// MatchesData is Partidos: my players match by match, how the matchday would end, the three
// figures of the matchday, forecast against result, and the weeks ahead.
func (d Document) MatchesData() View {
	fixtures := rows(d.Universe["fixtures"])
	sort.SliceStable(fixtures, func(one, two int) bool {
		return text(fixtures[one]["kickoff"]) < text(fixtures[two]["kickoff"])
	})
	squad := rows(d.Advice["squad"])
	choice, total := bestElevenOf(squad)
	starter := map[string]bool{}
	for _, id := range choice.IDs() {
		starter[id] = true
	}
	byTeam := map[string][]map[string]any{}
	for _, player := range squad {
		byTeam[text(player["team_id"])] = append(byTeam[text(player["team_id"])], player)
	}
	mineIn := func(fixture map[string]any) []map[string]any {
		out := append(append([]map[string]any{}, byTeam[text(fixture["local_id"])]...),
			byTeam[text(fixture["visitor_id"])]...)
		sort.SliceStable(out, func(one, two int) bool {
			return number(out[one]["xpts"]) > number(out[two]["xpts"])
		})
		return out
	}
	week := mapOf(d.Universe["week"])
	weekNumber := int(number(week["weekNumber"]))
	teams := mapOf(d.Universe["league_teams"])
	me := mapOf(teams[text(d.Universe["my_team_id"])])

	var main []Block
	if len(fixtures) == 0 {
		main = append(main, Block{Kind: "empty",
			Empty: "Sin partidos de esta jornada en el calendario."})
	} else {
		first, last := fixtures[0], fixtures[len(fixtures)-1]
		match := func(fixture map[string]any) string {
			return text(fixture["local"]) + " – " + text(fixture["visitor"])
		}
		card := MatchdayCard{Deadline: text(first["kickoff"]), First: match(first),
			FirstWhen: esWhen(text(first["kickoff"])), LastWhen: esHour(text(last["kickoff"])),
			Last: match(last), LastMine: len(mineIn(last)), Total: esNum(total, 1)}
		if weekNumber > 1 {
			card.Average = fmt.Sprintf("media real %s pts/jornada",
				esNum(number(me["points"])/float64(weekNumber-1), 1))
		}
		lines := Fixtures{Lines: []FixtureLine{}}
		for _, fixture := range fixtures {
			mine := mineIn(fixture)
			if len(mine) == 0 {
				lines.Idle = append(lines.Idle, match(fixture)+" "+esHour(text(fixture["kickoff"])))
				continue
			}
			line := FixtureLine{When: esHour(text(fixture["kickoff"])),
				Local: text(fixture["local"]), Visitor: text(fixture["visitor"]),
				LocalID: text(fixture["local_id"]), VisitorID: text(fixture["visitor_id"]),
				LocalCrest: crestKnown(text(fixture["local_id"])),
				VisitCrest: crestKnown(text(fixture["visitor_id"])), Players: []FixtureChip{}}
			for _, player := range mine {
				class := xptsClass(number(player["xpts"]))
				if !starter[text(player["id"])] {
					class += " bench"
				}
				line.Players = append(line.Players, FixtureChip{Player: RowPlayer(player),
					XPts: esNum(number(player["xpts"]), 1), Class: class})
			}
			if fixture["local_score"] != nil && fixture["visitor_score"] != nil {
				line.Score = fmt.Sprintf("%d-%d", int(number(fixture["local_score"])),
					int(number(fixture["visitor_score"])))
			}
			lines.Lines = append(lines.Lines, line)
		}
		main = append(main, Block{Title: fmt.Sprintf("Tus jugadores en la J%d", weekNumber),
			Sub: "en gris, los del banquillo", Kind: "fixtures", Data: lines})
		if board := d.boardBlock(); board != nil {
			main = append(main, *board)
		}
		main = append(main, Block{Kind: "matchday", Data: card})
	}
	aside := []Block{{Title: "Previsto vs real", Kind: "history",
		Data: History{Week: weekNumber, Planned: total, Hit: d.forecastHit()}}}
	if coming := d.comingBlock(weekNumber); coming != nil {
		aside = append(aside, *coming)
	}
	return View{Main: main, Aside: aside}
}

// boardBlock is how the matchday would end: every manager's saved eleven played out, with
// where each stands now and who is still to play.
func (d Document) boardBlock() *Block {
	matchday := mapOf(d.Advice["matchday"])
	managers := rows(matchday["managers"])
	if len(managers) == 0 {
		return nil
	}
	week := int(number(matchday["week"]))
	played := int(number(matchday["played"]))
	started := played > 0
	for _, manager := range managers {
		if number(manager["points"]) != 0 {
			started = true
		}
	}
	sort.SliceStable(managers, func(one, two int) bool {
		return number(managers[one]["projection_rank"]) < number(managers[two]["projection_rank"])
	})
	lines := []Row{}
	for _, manager := range managers {
		finish, now := int(number(manager["projection_rank"])), int(number(manager["points_rank"]))
		names := asStrings(manager["waiting_names"])
		waiting := int(number(manager["waiting"]))
		left := "ya no le queda nadie"
		if waiting > 0 {
			shown := names
			if len(shown) > 4 {
				shown = shown[:4]
			}
			left = "le quedan " + counted(waiting, "jugador", "jugadores")
			if len(shown) > 0 {
				left += ": " + strings.Join(shown, ", ")
				if len(names) > len(shown) {
					left += fmt.Sprintf(" +%d", len(names)-len(shown))
				}
			}
		}
		meta := fmt.Sprintf("%.0f pts · ahora %dº", number(manager["points"]), now)
		if !started {
			meta = fmt.Sprintf("%.0f pts en la temporada", number(manager["season_points"]))
		}
		if text(manager["source"]) == "techo" {
			left += " · sin su alineación: cuento su mejor once"
		}
		line := Row{Lead: []Seg{{T: fmt.Sprintf("%dº", finish)}}, Name: text(manager["manager"]),
			Meta: metaSegs(meta), Sub: metaSegs(left), Team: text(manager["team_id"]),
			Value: esNum(number(manager["projection"]), 1) + " pts",
			Note: []Seg{{T: fmt.Sprintf("%.0f + %s por sumar", number(manager["points"]),
				esNum(number(manager["to_come"]), 1))}}}
		if started && finish != now {
			if finish < now {
				line.Chips = []Chip{{T: fmt.Sprintf("▲ %d", now-finish), C: "ok"}}
			} else {
				line.Chips = []Chip{{T: fmt.Sprintf("▼ %d", finish-now), C: "soon"}}
			}
		}
		if truthy(manager["is_me"]) {
			line.Tone = "accent me"
		}
		if finish <= 3 {
			line.Tone += fmt.Sprintf(" podium p%d", finish)
		}
		lines = append(lines, line)
	}
	title := fmt.Sprintf("Cómo acabaría la J%d", week)
	sub := ""
	if !truthy(matchday["live"]) {
		title = fmt.Sprintf("Cómo acabó la J%d", week)
	} else if started {
		sub = fmt.Sprintf("%d de %d partidos jugados", played, int(number(matchday["matches"])))
	}
	return &Block{Title: title, Sub: sub, Rows: lines, ListC: "board"}
}

// forecastHit is how the last matchday played compared with its forecast, mine and the
// league's error per player.
func (d Document) forecastHit() string {
	last := mapOf(mapOf(d.Universe["forecast_review"])["last"])
	if number(last["counted"]) == 0 {
		return ""
	}
	line := fmt.Sprintf("Así acertó la previsión en la J%d: ", int(number(last["week"])))
	if mine := mapOf(last["mine"]); number(mine["counted"]) > 0 {
		forecast, actual := number(mine["forecast"]), number(mine["actual"])
		line += fmt.Sprintf("previsto %s · real %s (%s). ", esNum(forecast, 1),
			esNum(actual, 1), esSigned(actual-forecast))
	}
	return line + fmt.Sprintf("En la liga falló %s puntos por jugador, de media.",
		esNum(number(last["mean_abs_error"]), 1))
}

// comingBlock is the fixture list after this matchday: how many of mine play each week and
// where, plus any match of a past week still to be played.
func (d Document) comingBlock(current int) *Block {
	mine := map[string]int{}
	for _, player := range rows(d.Advice["squad"]) {
		mine[text(player["team_id"])]++
	}
	type week struct {
		label, kickoff string
		yours          int
		matches        []ComingMatch
	}
	weeks := map[int]*week{}
	var order []int
	for _, fixture := range rows(d.Universe["schedule"]) {
		round := int(number(fixture["week"]))
		if int(number(fixture["state"])) == FinishedMatch || round == current {
			continue
		}
		key := round
		if round < current {
			key = 0
		}
		entry := weeks[key]
		if entry == nil {
			entry = &week{label: fmt.Sprintf("J%d", round), kickoff: text(fixture["kickoff"])}
			if key == 0 {
				entry.label = "Aplazados"
			}
			weeks[key] = entry
			order = append(order, key)
		}
		if text(fixture["kickoff"]) < entry.kickoff {
			entry.kickoff = text(fixture["kickoff"])
		}
		local, visitor := text(fixture["local_id"]), text(fixture["visitor_id"])
		count := mine[local] + mine[visitor]
		entry.yours += count
		if count > 0 || key == 0 {
			entry.matches = append(entry.matches, ComingMatch{LocalID: local, VisitorID: visitor,
				Local: text(fixture["local"]), Visitor: text(fixture["visitor"]),
				LocalCrest: crestKnown(local), VisitCrest: crestKnown(visitor), Count: count})
		}
	}
	sort.Ints(order)
	out := []ComingWeek{}
	shown := 0
	for _, key := range order {
		entry := weeks[key]
		if key > 0 {
			if shown == 3 {
				continue
			}
			shown++
		}
		matches := entry.matches
		if matches == nil {
			matches = []ComingMatch{}
		}
		out = append(out, ComingWeek{Label: entry.label, Day: esDay(entry.kickoff),
			Matches: matches, Yours: counted(entry.yours, "tuyo", "tuyos")})
	}
	if len(out) == 0 {
		return nil
	}
	return &Block{Title: "Próximas jornadas", Kind: "weeks", Data: out,
		Note: "cada partido con cuántos tuyos juegan"}
}

// myFinish is the place my saved eleven would finish the matchday in, or 0.
func (d Document) myFinish() (int, int) {
	matchday := mapOf(d.Advice["matchday"])
	for _, manager := range rows(matchday["managers"]) {
		if truthy(manager["is_me"]) {
			return int(number(matchday["week"])), int(number(manager["projection_rank"]))
		}
	}
	return 0, 0
}
