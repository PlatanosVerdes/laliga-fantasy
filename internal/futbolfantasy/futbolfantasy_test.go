package futbolfantasy

import "testing"

// The ceiling's own page links the player, and that link is the one to follow: a slug made from
// a short name ("Roberto") points at somebody else or at nobody.
func TestDetailCarriesThePageItBelongsTo(t *testing.T) {
	page := `<div class="analytics-nombre"><a href="https://www.futbolfantasy.com/jugadores/roberto-fernandez" ` +
		`class="jugador">Roberto</a></div><script>parsePujaIdeal( 56800000 )</script>`
	detail := ParseDetail(page)
	url, _ := detail["ff_url"].(*string)
	if url == nil || *url != "https://www.futbolfantasy.com/jugadores/roberto-fernandez" {
		t.Fatalf("ff_url = %v", detail["ff_url"])
	}
	if detail["ideal_bid"] != 56800000 {
		t.Errorf("ideal_bid = %v", detail["ideal_bid"])
	}
	if none := ParseDetail(`<script>parsePujaIdeal(1)</script>`)["ff_url"].(*string); none != nil {
		t.Errorf("no link on the page, no link: %v", *none)
	}
}
