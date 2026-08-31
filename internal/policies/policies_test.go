package policies

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func amount(value float64) *float64 { return &value }

// An instruction that outlives the sale it authorised is a row the page can never clear, so
// what counts as sold matters, and so does what does not.
func TestSold(t *testing.T) {
	armed := map[string]Policy{
		"1": {ID: "1", Name: "Camavinga", AlwaysList: true, AutoSell: true},
		"2": {ID: "2", Name: "Yuri", AlwaysList: true},
		"3": {ID: "3", Name: "Youssef", Raid: true, MaxPay: amount(1_200_000)},
		"4": {ID: "4", Name: "Isi", MinPrice: amount(5_000_000)},
		"5": {ID: "5", Name: "Outside the universe", AlwaysList: true},
	}
	mine := map[string]bool{"1": false, "2": true, "3": false, "4": false}

	got := Sold(mine, armed)
	want := []string{"1", "4"}
	if len(got) != len(want) {
		t.Fatalf("Sold() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sold() = %v, want %v", got, want)
		}
	}
}

func TestForgetKeepsTheRaid(t *testing.T) {
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	if err := Save(map[string]Policy{
		"1": {ID: "1", Name: "Camavinga", AlwaysList: true, AutoSell: true},
		"2": {ID: "2", Name: "Youssef", AlwaysList: true, MinPrice: amount(2_000_000),
			Raid: true, MaxPay: amount(1_200_000)},
		"3": {ID: "3", Name: "Yuri", AlwaysList: true},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	gone, err := Forget("1", "2", "9")
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if len(gone) != 2 || gone[0] != "Camavinga" || gone[1] != "Youssef" {
		t.Fatalf("gone = %v, want [Camavinga Youssef]", gone)
	}

	left, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, still := left["1"]; still {
		t.Fatal("a sell-only instruction survived the sale")
	}
	if !left["3"].AlwaysList {
		t.Fatal("an untouched instruction was dropped")
	}
	raid := left["2"]
	if !raid.Raid || raid.MaxPay == nil || *raid.MaxPay != 1_200_000 {
		t.Fatalf("the raid did not survive: %+v", raid)
	}
	if raid.AlwaysList || raid.AutoSell || raid.MinPrice != nil {
		t.Fatalf("the sell side survived on the raid: %+v", raid)
	}
}

// The bug this rule exists for, and the one that made the page lie: the market feed keeps
// handing over a listing after its own expiration date, so a player nobody could see on sale in
// the app was reported as "ya listado" and never relisted. The rule lived in the serve loop, so
// only the half of the code that acts could see it; the page read the raw feed.
func TestAnExpiredListingIsNoListing(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	player := Row{"id": "2348", "name": "M. Aguado", "is_mine": true, "position_id": 2.0,
		"player_team_id": "47115712", "value": 5_437_025.0, "offers": []Row{},
		"market": Row{"market_id": "23688094", "min_bid": 5_437_025.0,
			"expires": now.Add(-3 * time.Hour).Format(time.RFC3339)}}
	armed := map[string]Policy{"2348": {ID: "2348", AlwaysList: true}}

	plan := Plan([]Row{player}, armed, now)
	if len(plan) != 1 {
		t.Fatalf("%d acciones, want 1", len(plan))
	}
	if action := text(plan[0]["action"]); action != "poner_en_venta" {
		t.Fatalf("action = %q, want poner_en_venta: el anuncio ya habia caducado", action)
	}
	// The slot, or the call reaches the API with playerId "" and comes back 400.
	if slot := text(plan[0]["player_team_id"]); slot != "47115712" {
		t.Errorf("player_team_id = %q, want la ficha de la plantilla", slot)
	}
	if why := text(plan[0]["why"]); !strings.Contains(why, "caduco") {
		t.Errorf("why = %q: tiene que decir que el anuncio caduco, no que no existia", why)
	}
}

// A listing still inside its own dates is a listing, and the reason now says until when: "ya
// listado" was a claim about the market with no date on it, which is exactly the claim that
// turned out to be wrong and could not be checked.
func TestALiveListingSaysUntilWhen(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	player := Row{"id": "2348", "name": "M. Aguado", "is_mine": true, "position_id": 2.0,
		"value": 5_437_025.0, "offers": []Row{},
		"market": Row{"market_id": "23688094", "min_bid": 5_437_025.0,
			"expires": now.Add(48 * time.Hour).Format(time.RFC3339)}}
	armed := map[string]Policy{"2348": {ID: "2348", AlwaysList: true}}

	plan := Plan([]Row{player}, armed, now)
	if len(plan) != 1 || text(plan[0]["action"]) != "ninguna" {
		t.Fatalf("plan = %v, want una sola fila sin accion", plan)
	}
	why := text(plan[0]["why"])
	for _, want := range []string{"listado a 5.437.025", "hasta el"} {
		if !strings.Contains(why, want) {
			t.Errorf("why = %q, falta %q", why, want)
		}
	}
}

// Two things that are not an expired advert: one with no date at all, and one expiring this very
// instant. Reading either as dead would relist a player who is already on sale.
func TestAListingWithoutADateIsBelieved(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	base := func(market Row) Row {
		return Row{"id": "1", "name": "p", "is_mine": true, "position_id": 2.0,
			"value": 10_000_000.0, "offers": []Row{}, "market": market}
	}
	armed := map[string]Policy{"1": {ID: "1", AlwaysList: true}}

	for name, market := range map[string]Row{
		"sin fecha":      {"market_id": "m", "min_bid": 10_000_000.0},
		"fecha ilegible": {"market_id": "m", "min_bid": 10_000_000.0, "expires": "mañana"},
		"caduca ahora":   {"market_id": "m", "min_bid": 10_000_000.0, "expires": now.Format(time.RFC3339)},
	} {
		plan := Plan([]Row{base(market)}, armed, now)
		if len(plan) != 1 || text(plan[0]["action"]) != "ninguna" {
			t.Errorf("%s: action = %q, want ninguna: sigue anunciado",
				name, text(plan[0]["action"]))
		}
	}
}

// A player who was never on the market and one whose advert ran out both need listing, and the
// price is the same, but they are not the same news and the reason has to tell them apart.
func TestNeverListedAndExpiredReadDifferently(t *testing.T) {
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	armed := map[string]Policy{"1": {ID: "1", AlwaysList: true}}
	bare := Row{"id": "1", "name": "p", "is_mine": true, "position_id": 2.0,
		"value": 10_000_000.0, "offers": []Row{}}

	fresh := Plan([]Row{bare}, armed, now)
	if why := text(fresh[0]["why"]); !strings.Contains(why, "no esta en el mercado") {
		t.Errorf("why = %q, want que no esta en el mercado", why)
	}
	if amount := number(fresh[0]["amount"]); amount != 10_000_000 {
		t.Errorf("amount = %v, want su valor", amount)
	}
}

// The automatic sale that made a hole. Eleven players, five defenders, an offer over the
// threshold: the old floor per position said two defenders were spare, so it sold one and left
// ten, which no formation fields.
func TestAutoSellStopsShortOfBreakingTheEleven(t *testing.T) {
	squad := []Row{}
	for index, position := range []int{1, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4} {
		squad = append(squad, Row{"id": string(rune('a' + index)), "name": "p",
			"is_mine": true, "position_id": position, "value": 10_000_000.0})
	}
	// The defender with a good offer on the table and permission to sell at that price.
	seller := squad[1]
	seller["offers"] = []Row{{"id": "o1", "money": 12_000_000.0}}
	seller["market"] = Row{"market_id": "m1", "min_bid": 11_000_000.0}

	armed := map[string]Policy{
		text(seller["id"]): {ID: text(seller["id"]), AlwaysList: true, AutoSell: true},
	}
	plan := Plan(squad, armed, time.Now())
	if len(plan) != 1 {
		t.Fatalf("%d actions, want one", len(plan))
	}
	if action := text(plan[0]["action"]); action != "avisar" {
		t.Fatalf("action = %q, want avisar: selling him leaves ten players", action)
	}
	if room := SquadRoom(squad, 2); room != 0 {
		t.Errorf("SquadRoom(defender) = %d, want 0 with eleven players", room)
	}
}

// Two sales in one pass. Each one on its own leaves a legal eleven, so measured against the
// squad this pass started with both were cleared, and between them they left ten: a cycle
// performs up to three operations. The second one has to see the squad the first one leaves.
func TestASecondSaleSeesTheSquadTheFirstOneLeaves(t *testing.T) {
	// Twelve players, six defenders: exactly one to spare.
	squad := []Row{}
	for index, position := range []int{1, 2, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4} {
		squad = append(squad, Row{"id": string(rune('a' + index)), "name": "p",
			"is_mine": true, "position_id": position, "value": 10_000_000.0})
	}
	armed := map[string]Policy{}
	for _, index := range []int{1, 2} {
		player := squad[index]
		player["offers"] = []Row{{"id": "o", "money": 12_000_000.0}}
		player["market"] = Row{"market_id": "m", "min_bid": 11_000_000.0}
		armed[text(player["id"])] = Policy{ID: text(player["id"]), AlwaysList: true,
			AutoSell: true}
	}

	plan := Plan(squad, armed, time.Now())
	sales := 0
	for _, action := range plan {
		if text(action["action"]) == "aceptar_oferta" {
			sales++
		}
	}
	if sales != 1 {
		t.Fatalf("%d sales authorised, want 1: the second leaves ten players", sales)
	}
}
