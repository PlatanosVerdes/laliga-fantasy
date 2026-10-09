package render

import (
	"encoding/json"
	"strings"
	"testing"
)

// Vender as data carries every decision the HTML shows, so the browser's rows cannot drift
// from the server's: the same offers, the same verdict, the hold rule already worded.
func TestSellDataMatchesTheView(t *testing.T) {
	document := decidingDocument()
	squad := rows(document.Advice["squad"])
	squad[1]["sale_locked"], squad[1]["hold_until"] = true, "2999-01-04T10:00:00+02:00"
	document.Advice["squad"] = squad
	data := document.SellData()
	html := document.sellView()
	if len(data.Offers) != strings.Count(html, `data-op="accept_offer"`) {
		t.Fatalf("offers: %d in the data, %d in the page", len(data.Offers),
			strings.Count(html, `data-op="accept_offer"`))
	}
	for _, offer := range data.Offers {
		if offer.Why == "" || offer.Player["id"] == "" || offer.OfferID == "" {
			t.Errorf("an offer without its verdict or ids: %+v", offer)
		}
	}
	locked := 0
	for _, item := range data.Rest {
		if item.LockedWhy != "" {
			locked++
			if item.Player["locked_until"] == nil {
				t.Errorf("the row's tags lose the padlock: %v", item.Player)
			}
		}
	}
	if locked == 0 {
		t.Error("the hold rule did not reach the data")
	}
	blob, err := json.Marshal(data)
	if err != nil || strings.Contains(string(blob), "source") {
		t.Errorf("the JSON carries only what the rows draw: %v %.200s", err, blob)
	}
}
