package render

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The tabs' first screens: compact rows, cards and an aside, the way the approved mockup draws
// them. Every figure is read off the advice layer or the universe.

// MinShownGain is the least a signing has to add to the best eleven to be listed: under a third
// of a point it does not show on the pitch.
const MinShownGain = 0.3

// ViewTabs are the tabs that open with a view, in page order.
var ViewTabs = []string{"decidir", "comprar", "vender", "clausulas", "plantilla", "partidos"}

// health is the ring a face wears: red for a confirmed absence, yellow for a doubt.
func health(player map[string]any) (ring, glyph, reason string) {
	absence := mapOf(player["absence"])
	kind, status := text(absence["kind"]), text(player["status"])
	reason = text(absence["reason"])
	severity, known := absence["severity"].(float64)
	pick := func(fallback string) string {
		if reason != "" {
			return reason
		}
		return fallback
	}
	switch {
	case status == "suspended" || status == "sanctioned" || kind == "sancionado":
		return "out", "card", pick("Sancionado")
	case status == "injured" || (kind == "lesionado" && known && severity == 0):
		return "out", "cross", pick("Lesionado")
	case status == "doubtful" || kind == "lesionado" || kind == "duda":
		if kind == "lesionado" {
			glyph = "cross"
		}
		return "doubt", glyph, pick("Duda")
	}
	return "", "", ""
}

func initials(name string) string {
	out := ""
	for _, word := range strings.Fields(strings.ReplaceAll(name, ".", " ")) {
		out += strings.ToUpper(string([]rune(word)[:1]))
		if len([]rune(out)) == 2 {
			break
		}
	}
	return out
}

func ratioClass(ratio float64) string {
	if ratio >= 1 {
		return "up"
	}
	return "down"
}

func esRatio(ratio float64) string {
	return strings.Replace(fmt.Sprintf("%.2f", ratio), ".", ",", 1) + "x"
}

var esShortDays = []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}

// esDay is "jue 8 oct".
func esDay(stamp string) string {
	when, ok := parseStamp(stamp)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s %d %s", esShortDays[int(when.Weekday())], when.Day(),
		months[int(when.Month())])
}

// esHour is "vie 21:00".
func esHour(stamp string) string {
	when, ok := parseStamp(stamp)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s %02d:%02d", esShortDays[int(when.Weekday())], when.Hour(), when.Minute())
}

func (d Document) playersByID() map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, player := range rows(d.Universe["players"]) {
		out[text(player["id"])] = player
	}
	for _, player := range rows(d.Advice["squad"]) {
		out[text(player["id"])] = player
	}
	return out
}

// Views are every tab's screen.
func (d Document) Views() []SectionView {
	if len(d.Advice) == 0 {
		return nil
	}
	var out []SectionView
	for _, section := range []SectionView{d.decideSection(), d.buyView(), d.sellView(),
		d.clauseView(), LineupShell, d.squadView(), d.matchesView(), d.rivalsView()} {
		if section.ID != "" {
			out = append(out, section)
		}
	}
	return out
}

// --- Comprar ---------------------------------------------------------------------------

// worthItsPrice is whether the price is one to recommend: within futbolfantasy's ceiling on the
// market, and for a clause the advice's own verdict that it returns more per million than the
// squad does.
func (d Document) worthItsPrice(item map[string]any, route string) bool {
	if route != "clausula" {
		ceiling := number(item["ideal_bid"])
		return ceiling > 0 && ceiling >= number(item["entry_cost"])
	}
	for _, raid := range append(rows(d.Advice["raids"]), rows(d.Advice["upcoming_raids"])...) {
		if text(raid["id"]) == text(item["id"]) {
			verdict := text(raid["verdict"])
			return verdict == "chollo" || verdict == "renta"
		}
	}
	return false
}

// buyingPower is today's cash, and what a market bid can reach with the debt the league allows
// over the squad's value.
func (d Document) buyingPower() (cash, reach float64) {
	cash = number(d.Advice["budget"])
	squad := 0.0
	for _, player := range rows(d.Advice["squad"]) {
		squad += number(player["value"])
	}
	return cash, cash + squad*d.MaxDebtPct/100
}

type windowState struct {
	known, open   bool
	closes, opens string
}

func (d Document) window() *windowState {
	window, ok := d.clauseWindow()
	return &windowState{known: ok, open: ok && window.Open, closes: window.ClosesAt,
		opens: window.OpensAt}
}

// gains is what each candidate of a route adds to the best eleven.
func (d Document) gains(route string) map[string]float64 {
	out := map[string]float64{}
	for _, item := range rows(d.Money["bargains"]) {
		if text(item["route"]) == route {
			out[text(item["id"])] = math.Max(out[text(item["id"])], number(item["xi_gain"]))
		}
	}
	return out
}

// buyOptions are the three routes' candidates: the clauses priced by the advice layer, and
// every listing of the market and of the rivals.
func (d Document) buyOptions() (clauses, offers, free []map[string]any) {
	for _, item := range rows(d.Money["bargains"]) {
		if text(item["route"]) == "clausula" {
			clauses = append(clauses, item)
		}
	}
	listed := func(source any) []map[string]any {
		var out []map[string]any
		for _, item := range rows(source) {
			if !truthy(item["is_mine"]) && text(mapOf(item["market"])["market_id"]) != "" {
				out = append(out, item)
			}
		}
		return out
	}
	return clauses, listed(d.Advice["asks"]), listed(d.Advice["bids_now"])
}

func shortDate(stamp string) string {
	if when, ok := parseStamp(stamp); ok {
		return when.Format("02/01")
	}
	return ""
}

// benchOf is the squad outside its best eleven, least useful first.
func (d Document) benchOf() []map[string]any {
	squad := rows(d.Advice["squad"])
	choice, _ := bestElevenOf(squad)
	in := map[string]bool{}
	for _, id := range choice.IDs() {
		in[id] = true
	}
	out := []map[string]any{}
	for _, player := range squad {
		if !in[text(player["id"])] {
			out = append(out, player)
		}
	}
	sort.SliceStable(out, func(one, two int) bool {
		return number(out[one]["xpts"]) < number(out[two]["xpts"])
	})
	return out
}
