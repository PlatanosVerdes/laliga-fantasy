package render

import (
	"encoding/json"
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
