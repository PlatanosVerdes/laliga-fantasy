package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// --- Rivales ---------------------------------------------------------------------------

func (d Document) rivalsView() SectionView { return shell("v-rivales", "rivales", "rivales") }

// Reach is which of my players a rival's estimated cash reaches, for the popup his chip opens.
type Reach struct {
	Title string `json:"title"`
	Note  []Seg  `json:"note"`
	Rows  []Row  `json:"rows"`
	Empty string `json:"empty,omitempty"`
}

// RivalsData is Equipos: each rival with his cash, whom it reaches and how far, the popups of
// who exactly, and who looks worst for the matchday. Your own team sits in its place in the
// table, to read the rest against, with nothing to reach.
func (d Document) RivalsData() View {
	rivals := rows(d.Advice["rivals"])
	if len(rivals) == 0 {
		return View{Main: []Block{}}
	}
	sort.SliceStable(rivals, func(one, two int) bool {
		return number(rivals[one]["position"]) < number(rivals[two]["position"])
	})
	squad := rows(d.Advice["squad"])
	// Who each rival is the main threat to, read off the clause plan.
	threatens := map[string][]string{}
	threatOf := map[string]string{}
	for _, raise := range rows(d.Raise["rows"]) {
		if who := text(raise["top_threat"]); who != "" {
			threatOf[text(raise["id"])] = who
			if !truthy(raise["clause_locked"]) && !truthy(raise["shielded"]) {
				threatens[who] = append(threatens[who], text(raise["name"]))
			}
		}
	}
	reaches := map[string]Reach{}
	lines := []Row{}
	for _, team := range rivals {
		cash := number(team["estimated_cash"])
		manager := fallbackText(text(team["manager"]), text(team["name"]))
		if truthy(team["is_me"]) {
			lines = append(lines, d.myTeamLine(team, manager, cash))
			continue
		}
		var top map[string]any
		reach := 0
		for _, player := range squad {
			clause := number(player["clause"])
			if clause <= 0 || clause > cash || truthy(player["shielded"]) {
				continue
			}
			if !truthy(player["clause_locked"]) {
				reach++
			}
			if top == nil || clause > number(top["clause"]) {
				top = player
			}
		}
		pays := "no le llega a ninguna cláusula tuya"
		if top != nil {
			lock := ""
			if truthy(top["clause_locked"]) {
				lock = " 🔒"
			}
			pays = fmt.Sprintf("le llega hasta %s %s%s", text(top["name"]),
				esMoney(number(top["clause"])), lock)
		}
		teamID := text(team["team_id"])
		label, class, tone := "no llega", "done", ""
		switch {
		case len(threatens[manager]) > 0:
			label, class, tone = fmt.Sprintf("amenaza a %d", len(threatens[manager])), "soon",
				"critical"
		case reach > 0:
			label, class = fmt.Sprintf("llega a %d", reach), "warn"
		}
		reaches[teamID] = reachOf(manager, cash, squad, threatOf)
		place := "—"
		if position := number(team["position"]); position > 0 {
			place = fmt.Sprintf("%.0fº", position)
			if position <= 3 {
				tone += fmt.Sprintf(" podium p%.0f", position)
			}
		}
		lines = append(lines, Row{Lead: []Seg{{T: place}}, Name: manager, Team: teamID,
			Meta: metaSegs(fmt.Sprintf("%.0f pts · %.0f jugadores · plantilla %s",
				number(team["points"]), number(team["players"]),
				esMoney(number(team["squad_value"])))),
			Sub: metaSegs(pays), Value: esMoney(cash),
			Note: []Seg{{T: "caja estimada",
				Tip: "Reconstruida del historial de traspasos: es una estimación"}},
			Chips: []Chip{{T: label, C: class + " reach-btn", Tip: "A quién de los tuyos le llega",
				Do: "reach", Args: map[string]any{"team_id": teamID}}},
			Acts: []Act{act("Ver plantilla", "ghost", "", "goto",
				map[string]any{"target": "rival-" + teamID})},
			Tone: strings.TrimSpace(tone)})
	}
	view := View{Main: []Block{{Title: "Equipos", Count: count(len(lines)), Rows: lines,
		Data: reaches}}}
	if outlook := d.outlookBlock(); outlook != nil {
		view.Aside = []Block{*outlook}
	}
	return view
}

// myTeamLine is your row among the rivals: the same numbers, your real cash, and a link to your
// squad instead of the reach chip, which only means something for somebody else's money.
func (d Document) myTeamLine(team map[string]any, manager string, cash float64) Row {
	place := "—"
	if position := number(team["position"]); position > 0 {
		place = fmt.Sprintf("%.0fº", position)
	}
	return Row{Lead: []Seg{{T: place}}, Name: manager + " (tú)", Team: text(team["team_id"]),
		Meta: metaSegs(fmt.Sprintf("%.0f pts · %.0f jugadores · plantilla %s",
			number(team["points"]), number(team["players"]), esMoney(number(team["squad_value"])))),
		Value: esMoney(cash), Note: []Seg{{T: "tu caja"}},
		Acts: []Act{act("Ver plantilla", "ghost", "", "goto",
			map[string]any{"target": "plantilla"})},
		Tone: "accent me"}
}

// reachOf is which of my players a rival's cash reaches: best first, with the state of each
// clause and whether the plan names him as the top threat.
func reachOf(manager string, cash float64, squad []map[string]any,
	threatOf map[string]string) Reach {
	var reached []map[string]any
	cheapest := 0.0
	for _, player := range squad {
		clause := number(player["clause"])
		if clause <= 0 {
			continue
		}
		if cheapest == 0 || clause < cheapest {
			cheapest = clause
		}
		if clause <= cash {
			reached = append(reached, player)
		}
	}
	sort.SliceStable(reached, func(one, two int) bool {
		return number(reached[one]["xpts"]) > number(reached[two]["xpts"])
	})
	out := Reach{Title: "Al alcance de " + manager, Rows: []Row{},
		Note: []Seg{{T: "Caja de " + manager + ": "}, {T: esMoney(cash), El: "b"},
			{T: " · estimada del historial de traspasos"}}}
	for _, player := range reached {
		line := playerLine(player)
		switch {
		case truthy(player["shielded"]):
			line.Chips = []Chip{{T: "🛡 hasta " + esWhen(text(player["shielded_until"])), C: "done"}}
		case truthy(player["clause_locked"]):
			line.Chips = clockChip(text(player["clause_locked_until"]), "se abre su cláusula")
		default:
			line.Chips = []Chip{{T: "pagable", C: "ok"}}
		}
		if threatOf[text(player["id"])] == manager {
			line.Tags = []Seg{{T: "amenaza", C: "tg tg-bad"}}
		}
		line.Value = esNum(number(player["xpts"]), 1) + " xPts"
		line.Note = []Seg{{T: "cláusula " + esMoney(number(player["clause"]))}}
		out.Rows = append(out.Rows, line)
	}
	if len(out.Rows) == 0 {
		out.Empty = "No le llega a ninguno: tu cláusula más barata es " + esMoney(cheapest) + "."
	}
	return out
}

func (d Document) outlookBlock() *Block {
	forecast := rows(d.Advice["outlook"])
	if len(forecast) == 0 {
		return nil
	}
	week := int(number(forecast[0]["week"]))
	lines := []Row{}
	for index, row := range forecast {
		if index == 3 {
			break
		}
		why := ""
		if reasons := asStrings(row["reasons"]); len(reasons) > 0 {
			why = reasons[0]
		}
		lines = append(lines, Row{Lead: []Seg{{T: fmt.Sprintf("%dº", index+1)}},
			Name: text(row["manager"]), Meta: metaSegs(why), Team: text(row["team_id"]),
			Value: esNum(number(row["xpts"]), 1) + " xPts",
			Note:  []Seg{{T: fmt.Sprintf("%.0fº de la liga", number(row["position"]))}}})
	}
	return &Block{Title: fmt.Sprintf("Quién pinta peor la J%d", week), Rows: lines}
}

// DataTable is a sortable table: its columns, and per row the cells with the key each sorts by.
type DataTable struct {
	Cols   []Col  `json:"cols"`
	Rows   []TRow `json:"rows"`
	Sticky bool   `json:"sticky,omitempty"`
}

type Col struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Num   bool   `json:"num,omitempty"`
	Wide  bool   `json:"wide,omitempty"`
}

type TRow struct {
	Me    bool    `json:"me,omitempty"`
	Cells []TCell `json:"cells"`
}

type TCell struct {
	Segs []Seg  `json:"segs,omitempty"`
	Acts []Act  `json:"acts,omitempty"`
	Sort string `json:"sort"`
	C    string `json:"c,omitempty"`
}

// RivalPick is the picker of the rival squads, one at a time.
type RivalPick struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// rivalViews is one view per rival, each with his squad whole, behind the picker that shows
// one at a time. Grouped by manager and not by player on purpose: "what does this one have" is
// how a league is actually read.
func (d Document) rivalViews(players []map[string]any) ([]SectionView, map[string]any) {
	teams := mapOf(d.Universe["league_teams"])
	if teams == nil {
		return nil, nil
	}
	myTeamID := text(d.Universe["my_team_id"])

	// Your best in each position: the bar every rival's player is read against.
	best := map[string]map[string]any{}
	for _, player := range players {
		if !truthy(player["is_mine"]) {
			continue
		}
		position := text(player["position"])
		if current, seen := best[position]; !seen ||
			number(player["xpts"]) > number(current["xpts"]) {
			best[position] = player
		}
	}
	squads := map[string][]map[string]any{}
	for _, player := range players {
		owner := text(player["owner_team_id"])
		if owner == "" || owner == myTeamID || truthy(player["is_mine"]) {
			continue
		}
		row := make(map[string]any, len(player)+4)
		for key, value := range player {
			row[key] = value
		}
		if value := number(player["value"]); value > 0 {
			if clause := number(player["clause"]); clause > 0 {
				row["clause_x"] = clause / value
			}
		}
		// What he is listed at, if he is: a player already on sale is reachable without
		// paying his clause, and that changes the answer completely.
		if listing := mapOf(player["market"]); listing != nil {
			if asking := number(listing["min_bid"]); asking > 0 {
				row["asking"] = asking
			}
		}
		if mine := best[text(player["position"])]; mine != nil {
			row["vs_mine"] = number(player["xpts"]) - number(mine["xpts"])
			row["vs_who"] = mine["name"]
		}
		squads[owner] = append(squads[owner], row)
	}
	if len(squads) == 0 {
		return nil, nil
	}
	ordered := make([]map[string]any, 0, len(teams))
	for _, value := range teams {
		team := mapOf(value)
		if team == nil || text(team["team_id"]) == myTeamID ||
			len(squads[text(team["team_id"])]) == 0 {
			continue
		}
		ordered = append(ordered, team)
	}
	sort.SliceStable(ordered, func(one, two int) bool {
		first, second := number(ordered[one]["position"]), number(ordered[two]["position"])
		if first != second && first > 0 && second > 0 {
			return first < second
		}
		return number(ordered[one]["points"]) > number(ordered[two]["points"])
	})
	views := map[string]any{}
	picks := []RivalPick{}
	shells := []SectionView{shell("rivalpick", "rivales", "rivalpick")}
	for _, team := range ordered {
		teamID := text(team["team_id"])
		squad := squads[teamID]
		// Read like a squad: keeper, defence, midfield, attack, and the best of each line first.
		sort.SliceStable(squad, func(one, two int) bool {
			first, second := number(squad[one]["position_id"]), number(squad[two]["position_id"])
			if first != second {
				return first < second
			}
			return number(squad[one]["xpts"]) > number(squad[two]["xpts"])
		})
		manager := fallbackText(fallbackText(text(team["manager"]), text(team["name"])), teamID)
		label := manager
		if position := number(team["position"]); position > 0 {
			label = fmt.Sprintf("%.0fº · %s", position, manager)
		}
		picks = append(picks, RivalPick{Value: "rival-" + teamID,
			Label: fmt.Sprintf("%s · %d jugadores", label, len(squad))})
		views["rival-"+teamID] = View{Plain: true,
			Main: []Block{rivalSquadBlock(team, label, squad)}}
		shells = append(shells, shell("rival-"+teamID, "rivales", "rival-"+teamID))
	}
	picks = append(picks, RivalPick{Value: "all", Label: "todos a la vez"})
	views["rivalpick"] = View{Plain: true, Main: []Block{{Title: "Plantillas rivales",
		Count: count(len(ordered)), Kind: "rivalpick", Data: picks}}}
	return shells, views
}

// rivalSquadBlock is one rival's squad as compact rows, the full table folded under it.
func rivalSquadBlock(team map[string]any, title string, squad []map[string]any) Block {
	lines := []Row{}
	upgrades, payable := 0, 0
	for _, player := range squad {
		if number(player["vs_mine"]) > 0 {
			upgrades++
		}
		if !truthy(player["clause_locked"]) && !truthy(player["shielded"]) &&
			number(player["clause"]) > 0 {
			payable++
		}
		line := playerLine(player)
		line.Note = []Seg{{T: "cláusula " + esMoney(number(player["clause"]))}}
		if vs, ok := player["vs_mine"]; ok {
			class := "down"
			if number(vs) > 0 {
				class = "up"
			}
			line.Note = append(line.Note, Seg{T: " · "}, Seg{T: esSigned(number(vs)), C: class})
			line.Why = "frente a " + text(player["vs_who"])
		}
		switch {
		case truthy(player["shielded"]):
			line.Chips = []Chip{{T: "🛡 blindado", C: "done"}}
		case truthy(player["clause_locked"]):
			line.Chips = clockChip(text(player["clause_locked_until"]), "se libera su cláusula")
		case number(player["asking"]) > 0:
			line.Chips = []Chip{{T: "en venta " + esMoney(number(player["asking"]))}}
		case number(player["clause"]) > 0:
			line.Chips = []Chip{{T: "pagable", C: "ok"}}
		}
		line.Acts = []Act{cmpAct(player, "cmp-add small")}
		if raid := raidActs(player, "mb mb-ghost raid-btn"); !raid[0].Text {
			line.Acts = append(line.Acts, raid...)
		}
		line.Value = esNum(number(player["xpts"]), 1) + " xPts"
		lines = append(lines, line)
	}
	sub := fmt.Sprintf("%.0f pts · caja estimada %s · %d jugadores · %s · %s",
		number(team["points"]), esMoney(number(team["estimated_cash"])), len(squad),
		counted(upgrades, "mejora a los tuyos", "mejoran a los tuyos"),
		counted(payable, "pagable ya", "pagables ya"))
	return Block{Title: title, Sub: sub, Rows: lines, Scroll: 560, Kind: "squad",
		Data: rivalTable(squad)}
}

// cmpAct puts a player in the comparator, or takes him out.
func cmpAct(player map[string]any, class string) Act {
	return Act{Label: "+", Class: class, Do: "cmp", Tip: "Añadir al comparador",
		Args: map[string]any{"id": text(player["id"]), "name": text(player["name"]),
			"pos": text(player["position"])}}
}

// rivalTable is the full table of a rival's squad, every column the rows leave out.
func rivalTable(squad []map[string]any) DataTable {
	cols := []Col{{Label: "", Kind: "cmp"}, {Label: "Jugador", Kind: "player"},
		{Label: "Frente a lo tuyo", Kind: "vs_mine"}, {Label: "Cláusula", Kind: "money", Num: true},
		{Label: "Se puede", Kind: "clause_when"}, {Label: "x valor", Kind: "num", Num: true},
		{Label: "Valor", Kind: "money", Num: true}, {Label: "En venta", Kind: "money", Num: true},
		{Label: "xPts/j", Kind: "num", Num: true}, {Label: "Pts/M", Kind: "mag", Num: true, Wide: true},
		{Label: "Titular", Kind: "starts", Num: true}, {Label: "Proximo rival", Kind: "text", Wide: true}}
	table := DataTable{Cols: cols, Rows: []TRow{}, Sticky: true}
	money := func(key string, row map[string]any) TCell {
		amount := asFloat(row[key])
		return TCell{Segs: []Seg{{T: Money(amount)}}, Sort: sortKey(amount), C: "num"}
	}
	num := func(key string, row map[string]any) TCell {
		amount := asFloat(row[key])
		return TCell{Segs: []Seg{{T: Num(amount, 2)}}, Sort: sortKey(amount), C: "num"}
	}
	for _, row := range squad {
		cells := []TCell{
			{Acts: []Act{cmpAct(row, "cmp-add small")}, Sort: text(row["name"])},
			{Segs: []Seg{playerCell(row)}, Sort: text(row["name"])},
			vsMineCell(row), money("clause", row), clauseWhenCell(row), num("clause_x", row),
			money("value", row), money("asking", row), num("xpts", row), magCell(row),
			startsCell(asFloat(row["start_probability"])), nextRivalCell(row),
		}
		table.Rows = append(table.Rows, TRow{Me: truthy(row["is_me"]), Cells: cells})
	}
	return table
}

// playerCell is PlayerCell as data: crest, name that opens the card, position, the team when
// there is no crest, and the flags that change what the row means.
func playerCell(row map[string]any) Seg {
	teamID := text(row["team_id"])
	var kids []Seg
	_, known := Crests[teamID]
	if known {
		kids = append(kids, Seg{C: "crest crest-" + teamID, Tip: text(row["team"])})
	}
	slug := positionSlug[int(number(row["position_id"]))]
	if slug == "" {
		slug = "ent"
	}
	kids = append(kids, Seg{T: text(row["name"]), C: "p-name", Pid: text(row["id"])},
		Seg{T: text(row["position"]), C: "pos pos-" + slug})
	if !known {
		if team := fallbackText(text(row["team_short"]), text(row["team"])); team != "" {
			kids = append(kids, Seg{T: team, C: "p-meta"})
		}
	}
	if !truthy(row["available"]) {
		label := fallbackText(statusLabels[text(row["status"])], text(row["status"]))
		kids = append(kids, Seg{T: label, C: "flag-critical"})
	} else if text(row["status"]) == "doubtful" {
		kids = append(kids, Seg{T: "duda", C: "flag-warning"})
	}
	if truthy(row["prior_based"]) {
		kids = append(kids, Seg{T: "est.", C: "flag-muted", Tip: "Sin historico: estimado por precio"})
	}
	if truthy(row["is_mine"]) {
		kids = append(kids, Seg{T: "mio", C: "flag-mine"})
	}
	if truthy(row["sale_locked"]) {
		until := text(row["hold_until"])
		if len(until) > 10 {
			until = until[:10]
		}
		kids = append(kids, Seg{T: "🔒", C: "flag-hold",
			Tip: "Norma de la liga: recien fichado, no se puede vender hasta el " + until})
	}
	return Seg{C: "p-cell", Kids: kids}
}

func vsMineCell(row map[string]any) TCell {
	delta, who := asFloat(row["vs_mine"]), text(row["vs_who"])
	if delta == nil || who == "" {
		return TCell{Segs: []Seg{{T: Missing, C: "muted"}}, Sort: "-999"}
	}
	if *delta > 0 {
		return TCell{Segs: []Seg{{T: fmt.Sprintf("+%.2f", *delta), C: "vs-up"}, {T: " "},
			{T: "sobre " + who, C: "muted"}}, Sort: sortKey(delta)}
	}
	return TCell{Segs: []Seg{{T: fmt.Sprintf("%.2f vs %s", *delta, who), C: "muted"}},
		Sort: sortKey(delta)}
}

// clauseWhenCell is whether he can be taken today. Shielded and locked are not the same "no".
func clauseWhenCell(row map[string]any) TCell {
	switch {
	case truthy(row["shielded"]):
		if until := text(row["shielded_until"]); until != "" {
			return TCell{Segs: []Seg{{C: "chip chip-warn", Kids: []Seg{{T: "blindado "},
				{Until: until}}}}, Sort: until}
		}
		return TCell{Segs: []Seg{{T: "blindado", C: "chip chip-warn"}}, Sort: "zz"}
	case truthy(row["clause_locked"]):
		if until := text(row["clause_locked_until"]); until != "" {
			return TCell{Segs: []Seg{{C: "chip chip-warn", Kids: []Seg{{T: "en "},
				{Until: until}}}}, Sort: until}
		}
		return TCell{Segs: []Seg{{T: "bloqueada", C: "chip chip-warn"}}, Sort: "zy"}
	case number(row["clause"]) == 0:
		return TCell{Segs: []Seg{{T: Missing, C: "muted"}}, Sort: "zx"}
	}
	return TCell{Segs: []Seg{{T: "pagable ya", C: "chip chip-good"}}, Sort: "0"}
}

// magCell is points per million as a bar: the figure and how far it goes on a scale of one.
func magCell(row map[string]any) TCell {
	amount := asFloat(row["points_value"])
	if amount == nil {
		return TCell{Segs: []Seg{{C: "bar-cell", Kids: []Seg{{T: Missing, C: "bar-num"}}}},
			Sort: sortKey(amount), C: "num wide-only"}
	}
	value := math.Max(0, *amount)
	width := math.Min(100, value*100)
	return TCell{Segs: []Seg{{C: "bar-cell",
		Tip: fmt.Sprintf("%.3f puntos esperados por millon", value),
		Kids: []Seg{{T: fmt.Sprintf("%.2f", value), C: "bar-num"},
			{C: "mag-track", Kids: []Seg{{C: "mag-fill", Style: fmt.Sprintf("width:%.1f%%", width)}}}}}},
		Sort: sortKey(amount), C: "num wide-only"}
}

func startsCell(value *float64) TCell {
	if value == nil {
		return TCell{Segs: []Seg{{T: Missing}}, Sort: "-1", C: "num"}
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
	return TCell{Segs: []Seg{{T: fmt.Sprintf("%d%%", share), C: "pill-" + status}},
		Sort: fmt.Sprintf("%d", share), C: "num"}
}

func nextRivalCell(row map[string]any) TCell {
	rival := text(row["next_rival"])
	if rival == "" {
		return TCell{Segs: []Seg{{T: Missing}}, C: "wide-only"}
	}
	var segs []Seg
	if crestKnown(text(row["next_rival_id"])) {
		segs = append(segs, Seg{C: "crest crest-" + text(row["next_rival_id"])})
	}
	words := rival + " · " + Where(truthy(row["next_home"]))
	return TCell{Segs: append(segs, Seg{T: words}), Sort: words, C: "wide-only"}
}
