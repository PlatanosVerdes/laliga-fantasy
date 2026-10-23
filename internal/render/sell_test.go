package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func asJSON(value any) string {
	blob, _ := json.Marshal(value)
	return string(blob)
}

func rowOf(block Block, id string) *Row {
	if index := indexOf(block, id); index >= 0 {
		return &block.Rows[index]
	}
	return nil
}

func indexOf(block Block, id string) int {
	for index, row := range block.Rows {
		if row.Player != nil && row.Player["id"] == id {
			return index
		}
	}
	return -1
}

func headAt(block Block, prefix string) int {
	for index, row := range block.Rows {
		if strings.HasPrefix(row.Head, prefix) {
			return index
		}
	}
	return -1
}

// Every offer comes with both buttons, Aceptar first, and only the recommended one filled, with
// the reason as its tooltip.
func TestSellOffersRecommendOneButton(t *testing.T) {
	block := decidingDocument().offersBlock()
	if len(block.Rows) == 0 {
		t.Fatal("no offers")
	}
	for _, row := range block.Rows {
		if len(row.Acts) != 2 || row.Acts[0].Label != "Aceptar" || row.Acts[1].Label != "Rechazar" {
			t.Fatalf("Aceptar then Rechazar: %+v", row.Acts)
		}
		filled := 0
		for _, button := range row.Acts {
			if strings.Contains(button.Class, "mb-primary") {
				filled++
				if !strings.HasPrefix(button.Tip, "recomendado: ") {
					t.Errorf("the filled one says why: %+v", button)
				}
			}
			if button.Do != "op" || button.Args["offer_id"] == "" {
				t.Errorf("both run the two-step operation on the offer: %+v", button)
			}
		}
		if filled != 1 {
			t.Errorf("one recommendation per offer: %+v", row.Acts)
		}
	}
}

// The hold stops taking an offer, never listing: a fit player says when he can be sold, one out
// injured that he can be sold now.
func TestTheHoldRuleNeverStopsAListing(t *testing.T) {
	d := Document{HoldExceptions: "lesiones, hablandolo antes"}
	fit := map[string]any{"id": "1", "sale_locked": true, "available": true, "hold_until": "2026-10-15"}
	if sell, chip := d.sellAct(fit); sell.Off || chip == nil || !strings.HasPrefix(chip.T, "acepta desde") {
		t.Errorf("a fit player in his hold could not be listed: %+v %+v", sell, chip)
	}
	hurt := map[string]any{"id": "2", "sale_locked": true, "available": false, "hold_until": "2026-10-15"}
	sell, chip := d.sellAct(hurt)
	if sell.Off || chip == nil || chip.T != "excepción: baja" {
		t.Errorf("an injured player in his hold was not offered for sale: %+v %+v", sell, chip)
	}
}

// An offer can be taken only against a listing, and within the hold only if he is out.
func TestAcceptBlockedMatchesWhatTheServerRefuses(t *testing.T) {
	for _, c := range []struct {
		name    string
		row     map[string]any
		blocked bool
	}{
		{"listed", map[string]any{"market_id": "m"}, false},
		{"not listed", map[string]any{"market_id": ""}, true},
		{"in the hold, fit", map[string]any{"market_id": "m", "sale_locked": true, "available": true}, true},
		{"in the hold, injured", map[string]any{"market_id": "m", "sale_locked": true, "available": false}, false},
	} {
		if got := AcceptBlocked(c.row) != ""; got != c.blocked {
			t.Errorf("%s: blocked = %v, want %v", c.name, got, c.blocked)
		}
	}
}

// The lot buttons carry exactly the offers their rows would answer: the recommended ones for
// Aceptar, every answerable one for Rechazar.
func TestTheLotButtonsMatchTheRows(t *testing.T) {
	block := decidingDocument().offersBlock()
	recommended, answerable := 0, 0
	for _, row := range block.Rows {
		if strings.Contains(row.Acts[0].Class, "mb-primary") {
			recommended++
		}
		if row.Acts[1].Args["market_id"] != "" && !row.Acts[1].Off {
			answerable++
		}
	}
	items := map[string]int{}
	for _, link := range block.Links {
		if link.Do != "bulk" {
			t.Fatalf("a lot button that is not a lot: %+v", link)
		}
		items[link.Args["op"].(string)] = len(link.Args["items"].([]map[string]any))
	}
	if want := map[bool]int{true: recommended, false: 0}[recommended > 1]; items["accept_offer"] != want {
		t.Errorf("accept lot has %d offers, the rows recommend %d", items["accept_offer"], recommended)
	}
	if want := map[bool]int{true: answerable, false: 0}[answerable > 1]; items["decline_offer"] != want {
		t.Errorf("decline lot has %d offers, %d can be answered", items["decline_offer"], answerable)
	}
}

// Two good offers for spare players go in one Aceptar lot, with the cash and what the eleven
// loses without both.
func TestTwoRecommendedOffersMakeAnAcceptLot(t *testing.T) {
	squad := []any{}
	for i, pos := range []float64{1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 2, 3} {
		squad = append(squad, map[string]any{"id": fmt.Sprint(100 + i), "name": fmt.Sprint("P", i),
			"position_id": pos, "available": true, "is_mine": true, "xpts": 6.0, "value": 10e6})
	}
	spare := func(id string) map[string]any {
		return map[string]any{"id": id, "name": "Spare " + id, "position_id": 4.0, "available": true,
			"is_mine": true, "xpts": 0.5, "value": 2e6}
	}
	squad = append(squad, spare("1"), spare("2"))
	offer := func(id string) map[string]any {
		return map[string]any{"id": id, "name": "Spare " + id, "market_id": "m" + id, "offer_id": "o" + id,
			"offer_amount": 3e6, "value": 2e6, "vs_value": 1.5, "position_id": 4.0}
	}
	d := Document{Advice: map[string]any{"squad": squad, "offers": []any{offer("1"), offer("2")}}}
	var lot *Act
	for i, link := range d.offersBlock().Links {
		if link.Args["op"] == "accept_offer" {
			lot = &d.offersBlock().Links[i]
		}
	}
	if lot == nil {
		t.Fatalf("no accept lot: %+v", d.offersBlock().Links)
	}
	if got := len(lot.Args["items"].([]map[string]any)); got != 2 || lot.Args["cash"] != int64(6e6) {
		t.Errorf("lot of %d for %v, want 2 for 6000000", got, lot.Args["cash"])
	}
}
