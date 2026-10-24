package render

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// --- Liga ------------------------------------------------------------------------------

// The three sections of Liga, each a view of its own so a link can still land on one.
func (d Document) leagueShells() []SectionView {
	out := []SectionView{}
	if d.liveBlock() != nil {
		out = append(out, shell("directo", "liga", "directo"))
	}
	out = append(out, shell("evolucion", "liga", "evolucion"), shell("movimientos", "liga",
		"movimientos"))
	if d.HoldDays > 0 || len(d.RuleNotes) > 0 {
		out = append(out, shell("normas", "liga", "normas"))
	}
	return out
}

func (d Document) leagueViews() map[string]any {
	views := map[string]any{
		// Only the frame: every manager's season is thirteen lineups per matchday, so the
		// chart asks /api/season when it is opened rather than on every rebuild.
		"evolucion": View{Plain: true, Main: []Block{{Title: "La liga jornada a jornada",
			Kind: "season"}}},
		"movimientos": View{Plain: true, Main: []Block{d.feedBlock()}},
	}
	if live := d.liveBlock(); live != nil {
		views["directo"] = View{Plain: true, Main: []Block{*live}}
	}
	if d.HoldDays > 0 || len(d.RuleNotes) > 0 {
		rules := houseRules(d.HoldDays, d.HoldExceptions, d.RuleNotes)
		views["normas"] = View{Plain: true, Main: []Block{{Title: "Normas de la liga",
			Count: count(len(rules)), Kind: "rules", Data: rules}}}
	}
	return views
}

// liveBlock is the matchday in play as a table of the moment: what each manager has scored so
// far, who he has left, and where he would finish.
func (d Document) liveBlock() *Block {
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
		return number(managers[one]["points_rank"]) < number(managers[two]["points_rank"])
	})
	lines := []Row{}
	for _, manager := range managers {
		now, finish := int(number(manager["points_rank"])), int(number(manager["projection_rank"]))
		line := Row{Lead: []Seg{{T: fmt.Sprintf("%dº", now)}}, Name: text(manager["manager"]),
			Team: text(manager["team_id"]), Value: fmt.Sprintf("%.0f pts", number(manager["points"])),
			Meta: metaSegs(fmt.Sprintf("%dº en la general · %.0f pts",
				int(number(manager["position"])), number(manager["season_points"]))),
			Sub: metaSegs(stillToPlay(manager))}
		if !started {
			line.Lead, line.Value = nil, ""
		}
		if started && truthy(matchday["live"]) {
			line.Note = []Seg{{T: fmt.Sprintf("acabaría %dº con %s", finish,
				esNum(number(manager["projection"]), 1))}}
		}
		if truthy(manager["is_me"]) {
			line.Tone = "accent me"
		}
		if started && now <= 3 {
			line.Tone += fmt.Sprintf(" podium p%d", now)
		}
		lines = append(lines, line)
	}
	title, sub := fmt.Sprintf("Cómo va la J%d", week), ""
	switch {
	case !truthy(matchday["live"]):
		title = fmt.Sprintf("Cómo acabó la J%d", week)
	case !started:
		title, sub = fmt.Sprintf("La J%d", week), "aún no ha empezado"
	default:
		sub = fmt.Sprintf("%d de %d partidos jugados", played, int(number(matchday["matches"])))
	}
	return &Block{Title: title, Sub: sub, Rows: lines, ListC: "board"}
}

// Rule is one line of the league's pact: whether the tool enforces it or it is a social
// agreement, and its words.
type Rule struct {
	Tag   string `json:"tag"`
	Class string `json:"class"`
	Line  []Seg  `json:"line"`
	Live  bool   `json:"live,omitempty"`
}

// houseRules is the pact. Only the hold rule changes what the tool proposes; the rest are here
// because a rule nobody can read is a rule nobody follows.
func houseRules(holdDays int, exceptions string, notes []string) []Rule {
	out := []Rule{}
	if holdDays > 0 {
		line := []Seg{{T: "Un jugador fichado o clausulado "},
			{T: fmt.Sprintf("no se puede vender durante %d dias", holdDays), El: "strong"},
			{T: ". Vale para toda la liga, asi que un rival tampoco puede venderte a quien " +
				"acaba de fichar: a ese solo se llega por clausula."}}
		if exceptions != "" {
			line = append(line, Seg{T: " Excepciones acordadas: " + exceptions + "."})
		}
		out = append(out, Rule{Tag: "se aplica", Class: "rule-tag", Line: line, Live: true})
	}
	for _, note := range notes {
		out = append(out, Rule{Tag: "acuerdo", Class: "rule-tag rule-social",
			Line: []Seg{{T: note}}})
	}
	return out
}

// FeedRow is one movement of the league: when, what, who to whom, for how much and what the
// player was worth that day. Mark says whether it concerns me.
type FeedLine struct {
	Date   string `json:"date"`
	Kind   string `json:"kind"`
	Body   []Seg  `json:"body"`
	Amount string `json:"amount"`
	Size   int64  `json:"size"`
	Mark   string `json:"mark,omitempty"`
	Quiet  bool   `json:"quiet,omitempty"`
	Then   string `json:"then,omitempty"`
	Pill   string `json:"pill,omitempty"`
	Ratio  string `json:"ratio,omitempty"`
}

type Feed struct {
	Lines    []FeedLine `json:"lines"`
	Sortable bool       `json:"sortable"`
}

// feedBlock is the league's movements, newest first: who signed and sold, and for how much.
// Lineup changes are the bulk of the log and say nothing about the market, so they are left out.
func (d Document) feedBlock() Block {
	events := rows(d.Universe["activity"])
	if len(events) == 0 {
		return Block{Title: "Movimientos de la liga", Kind: "empty",
			Empty: "Sin movimientos todavia. Si la liga ya tiene actividad y esto sigue vacio, " +
				"la respuesta del API ha cambiado de forma: probe activity la vuelca cruda."}
	}
	teams := d.managerTeams()
	marks, me := d.feedMarks()
	var moves []map[string]any
	counted := 0
	for _, event := range events {
		if strings.Contains(text(event["kind"]), "alinea") {
			continue
		}
		// Counted only when drawn: the header said 4 over 2 lines.
		if !feedHidden[int(number(event["type_id"]))] {
			counted++
			moves = append(moves, event)
		}
	}
	if len(moves) == 0 {
		return Block{Title: "Movimientos de la liga", Count: count(counted), Kind: "empty",
			Empty: "Ninguna compra ni venta todavia."}
	}
	return Block{Title: "Movimientos de la liga", Count: count(counted), Kind: "feed",
		Data: feedOf(moves, teams, marks, me)}
}

func feedOf(moves []map[string]any, teams, marks map[string]string, me string) Feed {
	feed := Feed{Sortable: len(moves) > 8, Lines: []FeedLine{}}
	for _, event := range moves {
		feed.Lines = append(feed.Lines, feedLine(event, teams, marks, me))
	}
	return feed
}

func playerSeg(name, id string) Seg {
	if id == "" {
		return Seg{T: name}
	}
	return Seg{T: name, C: "p-name", Pid: id}
}

func managerSeg(name, teamID string) Seg {
	if name == "" {
		return Seg{T: Missing}
	}
	if teamID == "" {
		return Seg{T: name}
	}
	return Seg{T: name, C: "p-name", Team: teamID}
}

// feedLine is one movement. The amount alone does not say whether it was a steal or a panic
// buy, so the player's value on that same day travels with it.
func feedLine(event map[string]any, teams, marks map[string]string, me string) FeedLine {
	name := text(event["player"])
	player := Seg{}
	if name != "" {
		player = playerSeg(name, text(event["player_id"]))
	}
	buyer := managerSeg(text(event["buyer"]), teams[buyerUser(event)])
	seller := managerSeg(text(event["seller"]), teams[sellerUser(event)])
	if int(number(event["type_id"])) == ShieldType {
		return FeedLine{Date: feedDate(event), Kind: "blindaje", Quiet: true,
			Body: []Seg{player, {T: " · blindado por "},
				managerSeg(text(event["actor"]), teams[text(event["user1"])])}}
	}
	hasBuyer, hasSeller := text(event["buyer"]) != "", text(event["seller"]) != ""
	strong := Seg{El: "strong", Kids: []Seg{player}}
	var body []Seg
	switch {
	case text(event["kind"]) == "recompensa" && hasBuyer:
		week := ""
		if raw := mapOf(event["raw"]); raw != nil {
			if matchday := number(raw["weekNumber"]); matchday > 0 {
				week = fmt.Sprintf(" por la jornada %.0f", matchday)
			}
		}
		body = []Seg{buyer, {T: " cobra" + week, C: "feed-then"}}
	case name != "" && hasBuyer && hasSeller:
		body = []Seg{strong, {T: ": "}, seller, {T: " → "}, buyer}
	case name != "" && hasBuyer:
		body = []Seg{strong, {T: " → "}, buyer}
	case name != "" && hasSeller:
		body = []Seg{strong, {T: ", vendido por "}, seller}
	case name != "":
		body = []Seg{strong}
	default:
		// Nobody named: dump what came, so an event shape we do not know yet is visible
		// rather than an empty row.
		fallback := fallbackText(text(event["buyer"]), text(event["seller"]))
		if fallback == "" {
			blob, _ := json.Marshal(event["raw"])
			fallback = string(blob)
			if len(fallback) > 110 {
				fallback = fallback[:110]
			}
		}
		body = []Seg{{T: fallback}}
	}
	line := FeedLine{Date: feedDate(event), Body: body, Amount: Missing}
	if value := asFloat(event["amount"]); value != nil && *value != 0 {
		line.Amount = Money(value)
	}
	if then := asFloat(event["value_then"]); then != nil && *then != 0 {
		premium := 1.0
		if value := asFloat(event["premium"]); value != nil {
			premium = *value
		}
		status := "neutral"
		switch {
		case premium >= 1.25:
			status = "critical"
		case premium >= 1.08:
			status = "warning"
		case premium <= 0.98:
			status = "good"
		}
		line.Then, line.Pill, line.Ratio = Money(then), "pill-"+status,
			fmt.Sprintf("%.2fx", premium)
	}
	// The matchday prize is not an operation: sorted by size it would bury the signings.
	if text(event["kind"]) != "recompensa" {
		line.Size = int64(math.Round(math.Abs(number(event["amount"]))))
	}
	kind := text(event["kind"])
	if truthy(event["clausulazo"]) {
		kind = "clausulazo"
	}
	if strings.HasPrefix(kind, "tipo ") {
		kind = "movimiento"
	}
	line.Kind = kind
	line.Mark = marks[text(event["player_id"])]
	if me != "" && (text(event["buyer"]) == me || text(event["seller"]) == me) {
		line.Mark = "mine"
	}
	return line
}
