package outcomes

import (
	"testing"
	"time"
)

// Telling the four endings apart is the whole point: a refusal says something about the rival,
// losing an auction says something about the price, and a listing that closed says nothing.
func TestClassify(t *testing.T) {
	when := time.Date(2026, 8, 19, 19, 0, 0, 0, time.UTC)
	offer := Pending{PlayerID: "1", Player: "De Galarreta", Amount: 13_600_000,
		Kind: "oferta", Seller: "JMjugon"}

	cases := []struct {
		name string
		now  Now
		want string
		who  string
	}{
		{"sigue viva: no hay final", Now{StillPending: true}, "", ""},
		{"ahora es mio: aceptada", Now{MineNow: true}, "aceptada", ""},
		{"se lo queda otro: perdida", Now{Owner: "Villaone"}, "perdida", "Villaone"},
		{"sigue anunciado y mi oferta no esta: rechazada", Now{Listed: true}, "rechazada", ""},
		{"ni anuncio ni dueno nuevo: caducada", Now{}, "caducada", ""},
	}
	for _, test := range cases {
		got := Classify(offer, test.now, when)
		if test.want == "" {
			if got != nil {
				t.Errorf("%s: esperaba ningun final, dio %q", test.name, got.Outcome)
			}
			continue
		}
		if got == nil {
			t.Errorf("%s: esperaba %q y no dio final", test.name, test.want)
			continue
		}
		if got.Outcome != test.want {
			t.Errorf("%s: dio %q, esperaba %q", test.name, got.Outcome, test.want)
		}
		if got.NewOwner != test.who {
			t.Errorf("%s: nuevo dueno %q, esperaba %q", test.name, got.NewOwner, test.who)
		}
		if got.Player != "De Galarreta" || got.Amount != 13_600_000 || got.Who != "JMjugon" {
			t.Errorf("%s: perdio los datos de la oferta: %+v", test.name, got)
		}
	}

	// A rival keeping him after refusing is still a refusal, not a loss: the owner has not changed.
	kept := Classify(offer, Now{Listed: true, Owner: "JMjugon"}, when)
	if kept.Outcome != "rechazada" {
		t.Errorf("el dueno de siempre sigue siendo el dueno: esperaba rechazada, dio %q",
			kept.Outcome)
	}
}
