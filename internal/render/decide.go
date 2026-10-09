package render

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// --- Decidir ---------------------------------------------------------------------------

func (d Document) decideSection() SectionView {
	if len(d.Advice) == 0 {
		return SectionView{}
	}
	return shell("ahora", "decidir", "decidir")
}

// CardView is a decision card as the browser draws it.
type CardView struct {
	Kind      string         `json:"kind"`
	Tone      string         `json:"tone"`
	Player    map[string]any `json:"player"`
	In        map[string]any `json:"in,omitempty"`
	Meta      string         `json:"meta"`
	Verb      string         `json:"verb"`
	Big       string         `json:"big"`
	BigNote   string         `json:"big_note,omitempty"`
	NoteClass string         `json:"note_class,omitempty"`
	Chips     []Chip         `json:"chips,omitempty"`
	Why       []string       `json:"why"`
	Buttons   []Act          `json:"buttons"`
	Impact    string         `json:"impact"`
}

type Cards struct {
	// Remind is the side game's nudge, above the cards while its round is open and unvoted.
	Remind *NecroRemind `json:"remind,omitempty"`
	Cards  []CardView   `json:"cards"`
	Rest   string       `json:"rest,omitempty"`
	Empty  string       `json:"empty,omitempty"`
}

// DecideData is "Qué hacer ahora": the cards to act on, and beside them the eleven they are
// about, the crack worth aiming at and the way to every other list.
func (d Document) DecideData() View {
	cards := d.decisionCards(time.Now())
	if len(cards) > MaxCards {
		cards = cards[:MaxCards]
	}
	data := Cards{Cards: []CardView{}, Remind: d.necroReminder()}
	if len(cards) == 0 {
		data.Empty = "Nada que decidir ahora: ninguna oferta que cobrar, ninguna cláusula que " +
			"subir y ningún fichaje que mejore tu once a su precio."
	} else {
		cash := number(d.Advice["budget"])
		for _, card := range cards {
			data.Cards = append(data.Cards, cardView(card))
			cash += card.Cash
		}
		count := []string{"", "la", "las dos", "las tres", "las cuatro", "las cinco"}[len(cards)]
		data.Rest = fmt.Sprintf("Haciendo %s te quedan %s en caja. Lo demás puede esperar.", count,
			esMoney(cash))
		if len(cards) == 1 {
			data.Rest = fmt.Sprintf("Haciéndolo te quedan %s en caja. Lo demás puede esperar.",
				esMoney(cash))
		}
	}
	aside := []Block{}
	if eleven := d.elevenBlock(); eleven != nil {
		aside = append(aside, *eleven)
		if crack := d.crackBlock(); crack != nil {
			aside = append(aside, *crack)
		}
		aside = append(aside, Block{Kind: "more", Data: []Act{
			gotoAct("Cláusulas", "clausulas"), gotoAct("Comprar", "comprar"),
			gotoAct("Vender", "vender")}})
	}
	return View{Main: []Block{{Kind: "cards", Data: data}}, Aside: aside}
}

func cardView(card Card) CardView {
	player := card.Player
	view := CardView{Kind: card.Kind, Tone: fallbackText(card.Tone, "muted"),
		Player: RowPlayer(player), Verb: card.Verb, Big: card.Big, BigNote: card.BigNote,
		NoteClass: card.NoteClass, Why: card.Why, Buttons: card.Buttons, Impact: card.Impact,
		Meta: fmt.Sprintf("%s · %s xPts", text(player["team_short"]),
			esNum(number(player["xpts"]), 1))}
	if card.In != nil {
		view.In = RowPlayer(card.In)
	}
	view.Chips = append(clockChip(card.Deadline, card.DeadlineLabel),
		clockChip(card.Deadline2, card.DeadlineLabel2)...)
	if view.Why == nil {
		view.Why = []string{}
	}
	if view.Buttons == nil {
		view.Buttons = []Act{}
	}
	return view
}

// ElevenChip is a player of the eleven: face, name, xPts in its colour, marked when he is a
// signing the plan brings.
type ElevenChip struct {
	Player map[string]any `json:"player"`
	XPts   string         `json:"xpts"`
	Class  string         `json:"class"`
}

type ElevenLine struct {
	Pos     string       `json:"pos"`
	Players []ElevenChip `json:"players"`
}

type Eleven struct {
	From     string       `json:"from,omitempty"`
	To       string       `json:"to"`
	Gain     string       `json:"gain,omitempty"`
	Lines    []ElevenLine `json:"lines"`
	Fresh    bool         `json:"fresh,omitempty"`
	Warnings []Seg        `json:"warnings,omitempty"`
	Week     int          `json:"week,omitempty"`
	Place    int          `json:"place,omitempty"`
}

// elevenBlock is the best eleven the squad can field, or the one the plan leaves.
func (d Document) elevenBlock() *Block {
	squad := rows(d.Advice["squad"])
	if len(squad) == 0 {
		return nil
	}
	byID := map[string]map[string]any{}
	for _, player := range squad {
		byID[text(player["id"])] = player
	}
	_, now := bestElevenOf(squad)
	planned, arriving := planSquad(squad, rows(d.Swaps["moves"]))
	for _, player := range planned {
		byID[text(player["id"])] = player
	}
	choice, total := bestElevenOf(planned)
	shape := choice.Shape.Name
	data := Eleven{To: esNum(total, 1) + " xPts", Lines: []ElevenLine{}}
	if len(arriving) > 0 {
		shape += " si haces el plan"
		data.From, data.Gain, data.Fresh = esNum(now, 1), esSigned(total-now), true
	}
	lines := byLines(byID, choice.Attack, choice.Middle, choice.Defence, choice.Keeper)
	for index, line := range lines {
		out := ElevenLine{Pos: []string{"del", "med", "def", "por"}[index], Players: []ElevenChip{}}
		for _, player := range line {
			class := xptsClass(number(player["xpts"]))
			if arriving[text(player["id"])] {
				class += " is-new"
			}
			out.Players = append(out.Players, ElevenChip{Player: RowPlayer(player),
				XPts: esNum(number(player["xpts"]), 1), Class: class})
		}
		data.Lines = append(data.Lines, out)
	}
	data.Warnings = d.planWarnings()
	// The starters futbolfantasy's editors just moved down in their club.
	for _, line := range lines {
		for _, player := range line {
			role := mapOf(player["role"])
			if text(role["change"]) != "down" {
				continue
			}
			note := ""
			if text(role["note"]) != "" {
				note = ": " + text(role["note"])
			}
			data.Warnings = append(data.Warnings, Seg{Pid: text(player["id"]),
				T: fmt.Sprintf("⚠ %s bajó a %s en su equipo%s", text(player["name"]),
					text(role["label"]), note)})
		}
	}
	data.Week, data.Place = d.myFinish()
	return &Block{Title: "Tu once", Sub: shape, Kind: "eleven", Data: data}
}

// planWarnings are the plan's warnings about the eleven itself: a squad that cannot field
// eleven, or a position with nobody to spare.
func (d Document) planWarnings() []Seg {
	var out []Seg
	for _, warning := range asStrings(d.Swaps["warnings"]) {
		out = append(out, Seg{T: "⚠ " + warning})
	}
	return out
}

// byLines turns a choice into its four lines of players, attack first, best first.
func byLines(byID map[string]map[string]any, attack, middle, defence []string,
	keeper string) [][]map[string]any {
	pick := func(ids []string) []map[string]any {
		out := []map[string]any{}
		for _, id := range ids {
			if player := byID[id]; player != nil {
				out = append(out, player)
			}
		}
		sort.SliceStable(out, func(one, two int) bool {
			return number(out[one]["xpts"]) > number(out[two]["xpts"])
		})
		return out
	}
	return [][]map[string]any{pick(attack), pick(middle), pick(defence), pick([]string{keeper})}
}

// crackBlock is the goal: the three best players I do not have, what each costs today, how far
// my cash and my sales reach, and what the cheapest would add to my eleven.
func (d Document) crackBlock() *Block {
	squad := rows(d.Advice["squad"])
	if len(squad) == 0 {
		return nil
	}
	var others []map[string]any
	for _, player := range rows(d.Universe["players"]) {
		if !truthy(player["is_mine"]) && truthy(player["available"]) {
			others = append(others, player)
		}
	}
	sort.SliceStable(others, func(one, two int) bool {
		return number(others[one]["xpts"]) > number(others[two]["xpts"])
	})
	// The ones who would actually improve my eleven: a keeper below mine is no crack.
	_, base := bestElevenOf(squad)
	gains := map[string]float64{}
	var picked []map[string]any
	for _, player := range others {
		if len(picked) == 3 {
			break
		}
		_, with := bestElevenOf(append(append([]map[string]any{}, squad...), player))
		if with-base > 0.05 {
			gains[text(player["id"])] = with - base
			picked = append(picked, player)
		}
	}
	if len(picked) == 0 {
		return nil
	}
	reach, sold := d.crackReach()
	lines := []Row{}
	var cheapest map[string]any
	cheapestPrice := 0.0
	for _, player := range picked {
		price, how := crackPrice(player)
		value := "no se puede"
		if price > 0 {
			value = esMoney(price)
			if cheapest == nil || price < cheapestPrice {
				cheapest, cheapestPrice = player, price
			}
		}
		line := playerLine(player)
		line.Value, line.Note, line.Why = esNum(number(player["xpts"]), 1)+" xPts",
			[]Seg{{T: value}}, how
		lines = append(lines, line)
	}
	note := "Con tu caja llegas a " + esMoney(reach)
	if len(sold) > 0 {
		names := sold
		if len(names) > 4 {
			names = append(names[:4:4], fmt.Sprintf("%d más", len(sold)-4))
		}
		note = fmt.Sprintf("Con tu caja y vendiendo %s llegas a %s", strings.Join(names, ", "),
			esMoney(reach))
	}
	if cheapest != nil {
		if gap := cheapestPrice - reach; gap > 0 {
			note += fmt.Sprintf("; a %s te faltan %s", text(cheapest["name"]), esMoney(gap))
		} else {
			note += fmt.Sprintf("; %s te llega", text(cheapest["name"]))
		}
		note += fmt.Sprintf(" (%s xPts a tu once).", esSigned(gains[text(cheapest["id"])]))
	} else {
		note += "; hoy ninguno se puede fichar."
	}
	return &Block{Title: "Objetivo: un crack", Rows: lines, Note: note}
}
