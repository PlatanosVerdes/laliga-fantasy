package render

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// --- Cláusulas -------------------------------------------------------------------------

func (d Document) clauseView() string { return shell("v-clausulas", "clausulas", "clausulas") }

// ClauseData is Cláusulas: which of my clauses to raise and for how much, the clausulazos I have
// scheduled, the clauses worth paying today and the cheap ones, and the next week of unlocks.
func (d Document) ClauseData() View {
	clauses, _, _ := d.buyOptions()
	gains := d.gains("clausula")
	sort.SliceStable(clauses, func(one, two int) bool {
		return gains[text(clauses[one]["id"])] > gains[text(clauses[two]["id"])]
	})
	window := d.window()
	payable := []Row{}
	listed := map[string]bool{}
	for _, item := range clauses {
		gain := gains[text(item["id"])]
		if truthy(item["affordable"]) && gain > MinShownGain && len(payable) < 3 {
			listed[text(item["id"])] = true
			payable = append(payable, d.buyLine(item, "clausula", gain, window))
		}
	}
	main := []Block{d.raiseBlock(), {Title: "Pagables hoy que mejoran tu once", Rows: payable,
		Empty: "Ninguna cláusula pagable mejora tu once.",
		Links: []Act{gotoAct("Todas en Comprar →", "comprar")}}}
	if cheap := d.cheapBlock(listed); cheap != nil {
		main = append(main, *cheap)
	}
	return View{Main: main, Aside: []Block{d.plannedBlock(), d.calendarBlock()}}
}

// raiseBlock is the clauses worth raising, the ones about to open first, and folded under them
// the rest with why not.
func (d Document) raiseBlock() Block {
	var worth, rest []map[string]any
	for _, raise := range rows(d.Raise["rows"]) {
		switch text(raise["verdict"]) {
		case "sube", "no te llega":
			if number(raise["pay"]) > 0 && d.raiseWanted(raise) {
				worth = append(worth, raise)
				continue
			}
		}
		rest = append(rest, raise)
	}
	sort.SliceStable(worth, func(one, two int) bool {
		first, second := truthy(worth[one]["clause_locked"]), truthy(worth[two]["clause_locked"])
		if first != second {
			return !first
		}
		return number(worth[one]["xi_drop"]) > number(worth[two]["xi_drop"])
	})
	lines := []Row{}
	for _, raise := range worth {
		opens := "abierta ya"
		if truthy(raise["clause_locked"]) {
			opens = "se abre " + esDay(text(raise["clause_locked_until"]))
		}
		meta := opens
		if threat := text(raise["top_threat"]); threat != "" {
			meta += " · " + threat + " tiene " + esMoney(number(raise["top_threat_cash"]))
		}
		if risk := number(raise["risk"]); risk > 0 {
			meta += fmt.Sprintf(" · riesgo %.0f %%", risk*100)
		}
		deadline, label := d.raiseDeadline(raise)
		pay := number(raise["pay"])
		kind := "primary"
		if text(raise["verdict"]) == "no te llega" {
			kind = "ghost"
		}
		line := playerLine(raise)
		line.Value = esMoney(number(raise["target_clause"]))
		line.Note = []Seg{{T: fmt.Sprintf("hoy %s · −%s xPts si se va",
			esMoney(number(raise["clause"])), esNum(number(raise["xi_drop"]), 1))}}
		line.Why, line.Chips = meta, clockChip(deadline, label)
		line.Acts = []Act{act("Subir · pagas "+esMoney(pay), kind, "raise", "raise",
			map[string]any{"id": text(raise["id"]), "name": text(raise["name"]),
				"pay": int64(pay), "slot": text(raise["player_team_id"]),
				"clause": int64(number(raise["clause"])),
				"target": int64(number(raise["target_clause"]))})}
		if !truthy(raise["clause_locked"]) {
			line.Tone = "critical"
		}
		lines = append(lines, line)
	}
	block := Block{Title: "Recomendaciones", Count: count(len(lines)), Rows: lines,
		Empty: "Ninguna cláusula tuya merece subirse ahora."}
	if len(rest) > 0 {
		others := []Row{}
		for _, raise := range rest {
			verdict, why := text(raise["verdict"]), text(raise["why"])
			if !d.raiseWanted(raise) {
				verdict = "déjalo ir"
				why = fmt.Sprintf("su cláusula ya es %s su valor: si se lo llevan, es buena venta",
					strings.TrimSuffix(ratioNote(number(raise["clause"])/number(raise["value"])),
						" su valor"))
			}
			line := playerLine(raise)
			line.Note, line.Why = []Seg{{T: verdict}}, why
			if pay := number(raise["pay"]); pay > 0 {
				line.Value, line.Note = esMoney(pay), []Seg{{T: verdict + " · subirla"}}
			}
			others = append(others, line)
		}
		block.Folds = []Fold{{Summary: fmt.Sprintf("%d que no merece la pena subir", len(rest)),
			Rows: others}}
	}
	return block
}

// plannedBlock is every clausulazo I have scheduled, standing or not, and folded under them what
// became of the ones no longer armed.
func (d Document) plannedBlock() Block {
	byID := d.playersByID()
	lines := []Row{}
	for _, raid := range d.Raids {
		player := byID[text(raid["player_id"])]
		if player == nil {
			player = map[string]any{"id": raid["player_id"], "name": raid["name"],
				"owner": raid["owner"]}
		}
		clause, limit := number(raid["clause"]), number(raid["max_pay"])
		standing := raidsStandingDown[text(raid["action"])]
		line := playerLine(player)
		why := text(raid["why"])
		if limit > 0 && clause > limit {
			line.Tags = []Seg{{T: "⚠ pasa tu límite", C: "tg tg-warn"}}
			why = "no se pagará: su cláusula pasa tu límite"
			line.Tone = "warn"
		}
		switch {
		case standing:
			mark := outcomeSeg(text(raid["action"]), text(raid["why"]))
			line.Chips = []Chip{{Icon: mark.Icon, C: mark.C, Tip: mark.Tip},
				{T: strings.ReplaceAll(text(raid["action"]), "_", " "), C: "warn"}}
			line.Tone = "warn"
		case truthy(player["clause_locked"]):
			line.Chips = clockChip(text(player["clause_locked_until"]), "se abre")
		}
		line.Acts = []Act{act("Cambiar límite", "ghost", "raid-btn", "raid", map[string]any{
			"id": text(raid["player_id"]), "name": text(raid["name"]), "max": int64(limit),
			"clause": int64(clause)})}
		if standing {
			line.Acts = append(line.Acts, act("Cancelar", "ghost", "op", "cancel_raid",
				map[string]any{"player_id": text(raid["player_id"]), "name": text(raid["name"])}))
		}
		line.Value, line.Note, line.Why = "≤ "+esMoney(limit),
			[]Seg{{T: "cláusula " + esMoney(clause)}}, why
		lines = append(lines, line)
	}
	block := Block{Title: "Clausulazos programados", Count: count(len(lines)),
		Sub:  "se pagan solos al abrirse, si siguen por debajo de tu límite",
		Rows: lines, Empty: "Ninguno programado: se programan desde la ficha de un rival."}
	if len(d.Orders) > 0 {
		history := []Row{}
		for _, order := range d.Orders {
			outcome := text(order["outcome"])
			what := outcome
			if why := text(order["why"]); why != "" {
				what += " · " + why
			}
			history = append(history, outcomeLine(text(order["player_id"]), text(order["player"]),
				outcome, text(order["why"]), what, shortDate(text(order["at"])),
				number(order["amount"])))
		}
		block.Folds = []Fold{{Summary: fmt.Sprintf("Historial de tus órdenes · %d", len(history)),
			Rows: history, Scroll: 320}}
	}
	return block
}

// cheapBlock is the clauses that return more points per million than the squad already does:
// rarely for the eleven, but the cheap way to fill the bench.
func (d Document) cheapBlock(seen map[string]bool) *Block {
	var found []map[string]any
	for _, item := range append(rows(d.Advice["raids"]), rows(d.Advice["upcoming_raids"])...) {
		id := text(item["id"])
		verdict := text(item["verdict"])
		if seen[id] || (verdict != "chollo" && verdict != "renta") || truthy(item["shielded"]) {
			continue
		}
		seen[id] = true
		found = append(found, item)
	}
	if len(found) == 0 {
		return nil
	}
	sort.SliceStable(found, func(one, two int) bool {
		return number(found[one]["vs_market"]) > number(found[two]["vs_market"])
	})
	lines := []Row{}
	for _, item := range found {
		line := playerLine(item)
		line.Chips = []Chip{{T: "pagable", C: "ok"}}
		if stamp := text(item["unlock_at"]); stamp != "" {
			if when, ok := parseStamp(stamp); ok && when.After(time.Now()) {
				line.Chips = clockChip(stamp, "se abre su cláusula")
			}
		}
		line.Value = esMoney(number(item["clause"]))
		line.Note = []Seg{{T: fmt.Sprintf("%s · %s pts/M de tu plantilla", text(item["verdict"]),
			esRatio(number(item["vs_market"])))}}
		line.Acts = raidActs(item, "mb mb-ghost raid-btn")
		lines = append(lines, line)
	}
	return &Block{Title: "Baratas que rentan", Count: count(len(lines)), Rows: lines, Scroll: 420,
		Sub: "más puntos por millón que tu plantilla: para el banquillo"}
}

// CalendarDay is one day of clauses opening: mine to watch, and the rivals' within reach that
// would add most to the eleven.
type CalendarDay struct {
	Label  string        `json:"label"`
	Mine   []CalendarHit `json:"mine"`
	Theirs []CalendarHit `json:"theirs"`
	Reach  int           `json:"reach"`
}

type CalendarHit struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Gain string `json:"gain,omitempty"`
}

func (d Document) calendarBlock() Block {
	now := time.Now()
	horizon := now.Add(7 * 24 * time.Hour)
	squad := rows(d.Advice["squad"])
	_, base := bestElevenOf(squad)
	cash := number(d.Advice["budget"])
	type day struct {
		label        string
		mine, theirs []map[string]any
	}
	days := map[string]*day{}
	for _, player := range rows(d.Universe["players"]) {
		if text(player["owner"]) == "" || !truthy(player["clause_locked"]) {
			continue
		}
		when, ok := parseStamp(text(player["clause_locked_until"]))
		if !ok || !when.After(now) || when.After(horizon) {
			continue
		}
		key := when.Local().Format("2006-01-02")
		if days[key] == nil {
			days[key] = &day{label: esDay(text(player["clause_locked_until"]))}
		}
		if truthy(player["is_mine"]) {
			days[key].mine = append(days[key].mine, player)
		} else if number(player["clause"]) <= cash {
			days[key].theirs = append(days[key].theirs, player)
		}
	}
	keys := make([]string, 0, len(days))
	for key := range days {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []CalendarDay{}
	for _, key := range keys {
		entry := days[key]
		item := CalendarDay{Label: entry.label, Mine: []CalendarHit{}, Theirs: []CalendarHit{},
			Reach: len(entry.theirs)}
		for _, player := range entry.mine {
			item.Mine = append(item.Mine, CalendarHit{ID: text(player["id"]),
				Name: text(player["name"])})
		}
		type gained struct {
			player map[string]any
			gain   float64
		}
		var best []gained
		for _, player := range entry.theirs {
			_, with := bestElevenOf(append(append([]map[string]any{}, squad...), player))
			if gain := with - base; gain > 1 {
				best = append(best, gained{player, gain})
			}
		}
		sort.SliceStable(best, func(one, two int) bool { return best[one].gain > best[two].gain })
		for index, hit := range best {
			if index == 3 {
				break
			}
			item.Theirs = append(item.Theirs, CalendarHit{ID: text(hit.player["id"]),
				Name: text(hit.player["name"]), Gain: esSigned(hit.gain)})
		}
		out = append(out, item)
	}
	return Block{Title: "Próximos 7 días", Kind: "calendar", Data: out,
		Empty: "Ninguna cláusula se abre en los próximos 7 días."}
}
