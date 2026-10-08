package render

import (
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
)

func TestOrderCardsDeadlineThenImpact(t *testing.T) {
	now := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	cards := []Card{
		{Key: "late", Deadline: "2026-10-08T21:00:00Z", Weight: 9},
		{Key: "none", Weight: 50},
		{Key: "small", Deadline: "2026-10-08T19:10:00Z", Weight: 1},
		{Key: "big", Deadline: "2026-10-08T19:40:00Z", Weight: 3},
		{Key: "gone", Deadline: "2026-10-08T17:00:00Z", Weight: 99},
	}
	var keys []string
	for _, card := range orderCards(cards, now) {
		keys = append(keys, card.Key)
	}
	if got := strings.Join(keys, ","); got != "big,small,late,none" {
		t.Errorf("order = %s, want big,small,late,none", got)
	}
}

func TestOrderCardsKeepsOneCardPerPlayer(t *testing.T) {
	now := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	cards := []Card{
		{Kind: "raise", Key: "own:7", Deadline: "2026-10-08T21:00:00Z", Weight: 5},
		{Kind: "offer", Key: "own:7", Deadline: "2026-10-08T19:00:00Z", Weight: 0.3},
	}
	out := orderCards(cards, now)
	if len(out) != 1 || out[0].Kind != "offer" {
		t.Fatalf("selling him and defending him cannot both stay: %+v", out)
	}
}

// A document with one offer worth taking, one raise in the clause plan and two market listings
// the actions table backs: one that pays for itself at the squad's rate and one that does not.
func decidingDocument() Document {
	squad := []any{
		map[string]any{"id": "7", "name": "Unai Lopez", "position": "MED", "position_id": 3.0,
			"is_mine": true, "available": true, "value": 20_000_000.0, "xpts": 2.5,
			"team_short": "RAY", "image": "https://example.test/7.png",
			"market": map[string]any{"market_id": "m7"},
			"offers": []any{map[string]any{"id": "o1", "money": 21_700_000.0,
				"expirationDate": "2999-01-01T19:00:00+02:00"}}},
		map[string]any{"id": "8", "name": "O. Rey", "position": "MED", "position_id": 3.0,
			"is_mine": true, "available": true, "value": 2_000_000.0, "xpts": 3.3},
		map[string]any{"id": "10", "name": "Medio", "position": "MED", "position_id": 3.0,
			"is_mine": true, "available": true, "value": 2_000_000.0, "xpts": 2.0},
		map[string]any{"id": "11", "name": "Otro medio", "position": "MED", "position_id": 3.0,
			"is_mine": true, "available": true, "value": 2_000_000.0, "xpts": 2.0},
		map[string]any{"id": "12", "name": "Medio suplente", "position": "MED",
			"position_id": 3.0, "is_mine": true, "available": true, "value": 1_000_000.0,
			"xpts": 1.0},
		map[string]any{"id": "13", "name": "Ángel Pérez", "position": "MED", "position_id": 3.0,
			"is_mine": true, "available": true, "value": 36_000_000.0, "xpts": 7.0,
			"market": map[string]any{"market_id": "m13"}},
		map[string]any{"id": "9", "name": "Portero", "position": "POR", "position_id": 1.0,
			"is_mine": true, "available": true, "value": 5_000_000.0, "xpts": 6.0},
	}
	// Enough of the other lines that selling a midfielder still leaves a legal eleven.
	for index, position := range []float64{2, 2, 2, 2, 2, 4, 4, 4} {
		squad = append(squad, map[string]any{"id": "f" + string(rune('a'+index)),
			"name": "Relleno", "position_id": position, "is_mine": true, "available": true,
			"value": 1_000_000.0, "xpts": 0.5})
	}
	listing := func(id string) map[string]any {
		return map[string]any{"market_id": "mk" + id, "kind": "libre", "min_bid": 1.0,
			"expires": "2999-01-01T19:00:00+02:00"}
	}
	cheap := map[string]any{"id": "20", "name": "Barato", "position": "DEL", "position_id": 4.0,
		"xpts": 5.0, "value": 2_000_000.0, "entry_cost": 2_000_000.0, "affordable": true,
		"ideal_bid": 3_000_000.0, "available": true, "market": listing("20")}
	dear := map[string]any{"id": "21", "name": "Caro", "position": "DEL", "position_id": 4.0,
		"xpts": 1.0, "value": 50_000_000.0, "entry_cost": 50_000_000.0, "affordable": true,
		"ideal_bid": 60_000_000.0, "available": true, "market": listing("21")}
	bargain := func(row map[string]any, gain float64) map[string]any {
		return merge(row, map[string]any{"route": "puja libre", "xi_gain": gain})
	}
	return Document{
		Universe: map[string]any{"players": squad},
		Advice: map[string]any{
			"budget": 30_000_000.0, "squad_ppm_benchmark": 0.5,
			"squad":    squad,
			"bids_now": []any{cheap, dear},
			"offers": []any{merge(squad[0].(map[string]any), map[string]any{
				"offer_id": "o1", "offer_amount": 21_700_000.0, "worth_taking": true,
				"offer_expires": "2999-01-01T19:00:00+02:00", "market_id": "m7",
				"vs_value": 1.085, "offer_from": "el mercado"}),
				merge(squad[5].(map[string]any), map[string]any{
					"offer_id": "o2", "offer_amount": 37_600_000.0, "worth_taking": true,
					"offer_expires": "2999-01-01T19:00:00+02:00", "market_id": "m13",
					"vs_value": 1.04, "offer_from": "cristian"})},
		},
		Money: map[string]any{"bargains": []any{bargain(cheap, 2.0), bargain(dear, 1.0)}},
		Raise: map[string]any{"rows": []any{merge(squad[1].(map[string]any), map[string]any{
			"in_plan": true, "pay": 2_000_000.0, "target_clause": 24_200_000.0,
			"clause": 3_500_000.0, "top_threat": "JMjugon", "top_threat_cash": 66_200_000.0,
			"threats": 8.0, "tempted": 3.0, "xi_drop": 2.7, "risk": 1.0,
			"player_team_id": "s8"})}},
		Window: &schedule.Window{Open: true, ClosesAt: soonClose},
	}
}

// soonClose is the clause window shutting within the hours a raise card looks ahead.
var soonClose = time.Now().Add(3 * time.Hour).Format(time.RFC3339)

func TestDecisionCardsComeFromTheAdvice(t *testing.T) {
	cards := decidingDocument().decisionCards(time.Now())
	kinds := map[string]Card{}
	for _, card := range cards {
		kinds[card.Kind+":"+text(card.Player["id"])] = card
	}
	offer, ok := kinds["offer:7"]
	if !ok {
		t.Fatalf("the offer worth taking has no card: %+v", cards)
	}
	if offer.Big != "21,7M" || !strings.Contains(offer.Button, `data-op="accept_offer"`) ||
		!strings.Contains(offer.Button, `data-op-offer="o1"`) {
		t.Errorf("offer card = %q / %s", offer.Big, offer.Button)
	}
	raise, ok := kinds["raise:8"]
	if !ok {
		t.Fatal("the raise in the plan has no card")
	}
	if !strings.Contains(raise.Why[0], "JMjugon tiene 66,2M") ||
		!strings.Contains(raise.Button, `class="raise dcard-go"`) ||
		!strings.Contains(raise.Button, `data-raise-pay="2000000"`) {
		t.Errorf("raise card = %v / %s", raise.Why, raise.Button)
	}
	if raise.Deadline != soonClose {
		t.Errorf("an open clause is defended until the window shuts, got %q", raise.Deadline)
	}
	if _, ok := kinds["offer:13"]; ok {
		t.Error("selling the best player of the eleven with no replacement is not a decision to push")
	}
	if _, ok := kinds["sign:20"]; !ok {
		t.Error("a backed signing that pays for itself should have a card")
	}
	if _, ok := kinds["sign:21"]; ok {
		t.Error("one xPts for 50M is under the squad's rate and the plan turns it down")
	}
	if !strings.Contains(kinds["sign:20"].Button, `data-operation="bid"`) {
		t.Errorf("free market signing should bid: %s", kinds["sign:20"].Button)
	}
}

func TestDecideSectionRendersOneSwappableSection(t *testing.T) {
	html := decidingDocument().decideSection()
	if !strings.HasPrefix(html, `<section id="ahora"`) ||
		strings.Count(html, "<section") != 1 || strings.Count(html, "</section>") != 1 {
		t.Fatalf("the live refresh splits on sections, so there must be exactly one: %.200s", html)
	}
	for _, want := range []string{`data-pid="7"`, "Qué hacer ahora", "Tu once",
		`data-deadline="2999-01-01T19:00:00+02:00"`, `data-goto="clausulas"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestSpanishNumbers(t *testing.T) {
	cases := map[string]string{
		esMoney(21_724_064): "21,7M", esMoney(757_000): "757K", esMoney(-1_100_000): "−1,1M",
		esNum(-0.04, 1): "0,0", esNum(-2.7, 1): "−2,7", esSigned(5.25): "+5,3",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestElevenAsideShowsThePlan(t *testing.T) {
	document := decidingDocument()
	squad := rows(document.Advice["squad"])
	signing := rows(document.Advice["bids_now"])[0]
	document.Swaps = map[string]any{"moves": []any{map[string]any{
		"out": squad[len(squad)-2], "in": signing, "gain": 4.5, "cost": 2_000_000.0}}}
	html := document.elevenAside()
	for _, want := range []string{"si haces el plan", `class="from"`, `is-new`, `data-pid="20"`} {
		if !strings.Contains(html, want) {
			t.Errorf("plan aside misses %s", want)
		}
	}
	if strings.Contains(decidingDocument().elevenAside(), "si haces el plan") {
		t.Error("without moves the aside is today's eleven")
	}
}

// Pedrosa: 3,1M for a 3,2M player is not a sale worth a card, unless the advice wants him out
// anyway and it is close to his value.
func TestOfferCardOnlyWhenItPays(t *testing.T) {
	document := decidingDocument()
	offers := rows(document.Advice["offers"])
	offers[0]["offer_amount"] = 19_400_000.0
	document.Advice["offers"] = []any{offers[0], offers[1]}
	has := func(document Document) bool {
		for _, card := range document.decisionCards(time.Now()) {
			if card.Kind == "offer" && text(card.Player["id"]) == "7" {
				return true
			}
		}
		return false
	}
	if has(document) {
		t.Error("×0,97 su valor no es una venta que empujar")
	}
	document.Advice["sells"] = []any{map[string]any{"id": "7", "reasons": []any{"no juega"}}}
	offers[0]["offer_amount"] = 19_700_000.0
	if !has(document) {
		t.Error("si el consejo ya quiere venderlo, ×0,97 basta")
	}
	if note, class := ratioWords(0.97); note != "×0,97 su valor" || class != "down" {
		t.Errorf("ratio: %q %q", note, class)
	}
}

// Unai: a clause at 1,27x his value is already a good sale, and Agoumé's lock lifts in 11 days.
func TestRaiseCardsOnlyWhenWorthItAndSoon(t *testing.T) {
	document := decidingDocument()
	raises := rows(document.Raise["rows"])
	cheap := merge(raises[0], map[string]any{"id": "10", "name": "Medio", "value": 2_000_000.0,
		"clause": 2_600_000.0, "xpts": 2.0})
	late := merge(raises[0], map[string]any{"clause_locked": true,
		"clause_locked_until": time.Now().Add(11 * 24 * time.Hour).Format(time.RFC3339)})
	for _, row := range []map[string]any{cheap, late} {
		document.Raise = map[string]any{"rows": []any{row}}
		for _, card := range document.decisionCards(time.Now()) {
			if card.Kind == "raise" {
				t.Errorf("no tendría que haber carta para %v", row["name"])
			}
		}
	}
	// O. Rey is among the three best outfield players, so his 1,75x clause is still defended.
	document.Raise = map[string]any{"rows": []any{raises[0]}}
	found := false
	for _, card := range document.decisionCards(time.Now()) {
		if card.Kind == "raise" {
			found = strings.Contains(card.Impact, "retrasa tu crack 2,0M")
		}
	}
	if !found {
		t.Error("un titular clave se defiende, y la carta dice cuánto retrasa el crack")
	}
}

func TestCashBeforeSpendingAtTheSameHour(t *testing.T) {
	now := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	cards := orderCards([]Card{
		{Key: "spend", Deadline: "2026-10-08T21:00:00Z", Weight: 9, Cash: -5e6},
		{Key: "cash", Deadline: "2026-10-08T21:10:00Z", Weight: 1, Cash: 3e6},
	}, now)
	if cards[0].Key != "cash" {
		t.Errorf("a la misma hora, primero lo que trae caja: %v", cards[0].Key)
	}
}

func TestCrackBoxNamesTheBestIDoNotHave(t *testing.T) {
	document := decidingDocument()
	players := rows(document.Universe["players"])
	crack := map[string]any{"id": "99", "name": "Raphinha", "position_id": 4.0, "xpts": 9.5,
		"available": true, "owner": "Rival", "clause": 200_000_000.0}
	document.Universe["players"] = append(toAny(players), crack)
	box := document.crackBox()
	if !strings.Contains(box, "Raphinha") || !strings.Contains(box, "200,0M") ||
		!strings.Contains(box, "te faltan") {
		t.Errorf("objetivo: %s", box)
	}
}

func toAny(rows []map[string]any) []any {
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	return out
}

// Á. Valles: the keeper of the best eleven, sold with nobody to replace him, is never on the
// list, whatever the advice says about his points per million; a reserve with a reason is.
func TestSellListKeepsTheBestElevenUnlessItBarelyNotices(t *testing.T) {
	document := decidingDocument()
	document.Advice["sells"] = []any{
		map[string]any{"id": "9", "available": true, "reasons": []any{"pocos puntos por millon"}},
		map[string]any{"id": "12", "available": true, "reasons": []any{"no juega"}},
	}
	got := document.sellCandidates()
	if got["9"] || !got["12"] {
		t.Errorf("candidatos: %v", got)
	}
	document.Advice["sells"] = []any{map[string]any{"id": "9", "available": false,
		"reasons": []any{"lesionado"}}}
	if !document.sellCandidates()["9"] {
		t.Error("el que no puede jugar sí se puede vender")
	}
}

// The reach counts the cash, offers that pay and reserves free to sell: not the eleven.
func TestCrackReachIsWhatCanBeRaisedToday(t *testing.T) {
	document := decidingDocument()
	crack := map[string]any{"id": "99", "name": "Raphinha", "position_id": 4.0, "xpts": 9.5,
		"available": true, "owner": "Rival", "clause": 200_000_000.0}
	document.Universe["players"] = append(toAny(rows(document.Universe["players"])), crack)
	box := document.crackBox()
	if strings.Contains(box, "Portero") || !strings.Contains(box, "Unai Lopez") {
		t.Errorf("alcance: %s", box)
	}
}
