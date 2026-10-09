package render

import (
	"fmt"
	"sort"
	"strings"
)

// --- Ranking ---------------------------------------------------------------------------

// RankingData is Ranking: the best by xPts with whether my reach gets to each, the bargains,
// the best of each line and the most xPts per million.
func (d Document) RankingData(players []map[string]any) View {
	available := make([]map[string]any, 0, len(players))
	for _, player := range players {
		if truthy(player["available"]) {
			available = append(available, player)
		}
	}
	top := func(less func(one, two map[string]any) bool, keep func(map[string]any) bool,
		limit int) []map[string]any {
		out := []map[string]any{}
		for _, player := range available {
			if keep == nil || keep(player) {
				out = append(out, player)
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
		if len(out) > limit {
			out = out[:limit]
		}
		return out
	}
	byScore := top(func(one, two map[string]any) bool {
		return number(one["score"]) > number(two["score"])
	}, nil, 80)
	byXPts := top(func(one, two map[string]any) bool {
		return number(one["xpts"]) > number(two["xpts"])
	}, nil, 80)
	byValue := top(func(one, two map[string]any) bool {
		return number(one["points_value"]) > number(two["points_value"])
	}, func(player map[string]any) bool { return number(player["value"]) > 0 }, 40)
	return d.rankingView(byScore, byXPts, byValue)
}

func (d Document) rankingView(byScore, byXPts, byValue []map[string]any) View {
	reach, _ := d.crackReach()
	best := make([]Row, 0, len(byXPts))
	for _, player := range byXPts {
		best = append(best, bestLine(player, reach))
	}
	main := []Block{
		{Title: "Los mejores", Count: count(len(byXPts)), Rows: best, Scroll: 560},
		{Title: "Chollos", Count: count(len(byScore)), Rows: rankLines(byScore), Scroll: 560,
			Lead: "puntos por millón, titularidad y valor al alza: para el banquillo barato"},
	}
	byLine := []Row{}
	for _, positionID := range []int{1, 2, 3, 4} {
		var items []Row
		for _, player := range byXPts {
			if int(number(player["position_id"])) == positionID && len(items) < 3 {
				items = append(items, rankLine(player))
			}
		}
		if len(items) == 0 {
			continue
		}
		slug := positionSlug[positionID]
		byLine = append(byLine, Row{HeadSegs: []Seg{{T: strings.ToUpper(slug), C: "pos pos-" + slug}}})
		byLine = append(byLine, items...)
	}
	aside := []Block{{Title: "Los mejores por posición", Rows: byLine}}
	if len(byValue) > 0 {
		aside = append(aside, Block{Title: "Más xPts por millón", Count: count(len(byValue)),
			Rows: rankLines(byValue), Scroll: 420, Lead: "lo que manda cuando vas justo de caja"})
	}
	return View{Filters: true, Main: main, Aside: aside}
}

// starAct follows or stops following a player.
func starAct(player map[string]any) Act {
	return Act{Label: "☆", Class: "star", Do: "star", Tip: "Marcar como favorito",
		Args: map[string]any{"id": text(player["id"]), "name": text(player["name"]),
			"on": truthy(player["starred"])}}
}

func rankLine(player map[string]any) Row {
	line := playerLine(player)
	line.Find = findOf(player, number(player["value"]))
	line.Value = esNum(number(player["xpts"]), 1) + " xPts"
	line.Note = []Seg{{T: fmt.Sprintf("%s pts/M · %s", esNum(number(player["points_value"]), 2),
		esMoney(number(player["value"])))}}
	line.Acts = []Act{starAct(player), cmpAct(player, "cmp-add small")}
	return line
}

func rankLines(players []map[string]any) []Row {
	out := make([]Row, 0, len(players))
	for _, player := range players {
		out = append(out, rankLine(player))
	}
	return out
}

// bestLine is a player of "Los mejores": his price today and whether my reach gets there.
func bestLine(player map[string]any, reach float64) Row {
	price, how := crackPrice(player)
	line := rankLine(player)
	note := how
	switch {
	case truthy(player["is_mine"]):
		note = "vale " + esMoney(number(player["value"]))
	case price > 0:
		note = esMoney(price) + " · " + how
	}
	line.Note = []Seg{{T: note}}
	switch {
	case truthy(player["is_mine"]):
	case price > 0 && reach >= price:
		line.Chips = []Chip{{T: "te llega", C: "ok"}}
	case price > 0:
		line.Chips = []Chip{{T: "te faltan " + esMoney(price-reach), C: "warn"}}
	}
	return line
}
