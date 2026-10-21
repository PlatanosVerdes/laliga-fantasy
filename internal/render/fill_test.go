package render

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/advice"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
)

// fillWorld is a league small enough to read and big enough for every judgement the ceilings
// feed: a full squad with an offer on the table, a free market where one signing is under its
// ceiling and one over it, and a rival selling.
func fillWorld() map[string]any {
	var players []any
	add := func(player map[string]any) { players = append(players, player) }
	for index, position := range []float64{1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4} {
		id := "s" + string(rune('a'+index))
		player := map[string]any{"id": id, "ff_id": "ff-" + id, "name": "Mio " + id,
			"position_id": position, "is_mine": true, "available": true, "value": 5_000_000.0,
			"xpts": 3.0, "score": 1.0, "team_short": "RAY"}
		if index == 10 {
			player["xpts"], player["score"] = 1.0, 0.2
			player["market"] = map[string]any{"market_id": "own", "is_mine": true,
				"min_bid": 6_000_000.0, "expires": "2999-01-01T19:00:00+02:00"}
			player["offers"] = []any{map[string]any{"id": "o1", "money": 5_400_000.0,
				"from": "Rival", "expirationDate": "2999-01-01T19:00:00+02:00"}}
		}
		add(player)
	}
	free := func(id string, position, xpts, cost float64) {
		add(map[string]any{"id": id, "ff_id": "ff-" + id, "name": "Libre " + id,
			"position_id": position, "available": true, "value": cost, "xpts": xpts,
			"score": 1.5, "market": map[string]any{"market_id": "mk-" + id, "kind": "libre",
				"min_bid": cost, "bids": 0.0, "expires": "2999-01-01T19:00:00+02:00"}})
	}
	free("good", 4, 6.0, 4_000_000)
	free("dear", 4, 5.5, 9_000_000)
	free("mid", 3, 4.5, 3_000_000)
	add(map[string]any{"id": "rv", "ff_id": "ff-rv", "name": "Del rival", "position_id": 2.0,
		"available": true, "owner": "Rival", "value": 6_000_000.0, "xpts": 5.0, "score": 1.4,
		"clause": 7_000_000.0, "market": map[string]any{"market_id": "mk-rv", "kind": "venta",
			"seller": "Rival", "min_bid": 6_500_000.0, "expires": "2999-01-01T19:00:00+02:00"}})
	return map[string]any{"players": players, "my_team_id": "me"}
}

// ceilings is futbolfantasy for fillWorld: every page, slowly, recording the order asked.
type ceilings struct {
	mu     sync.Mutex
	asked  []string
	delay  time.Duration
	stored map[string]futbolfantasy.Detail
}

func (c *ceilings) fetch(ffID string) (futbolfantasy.Detail, error) {
	time.Sleep(c.delay)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, ffID)
	ideal := 0.0
	switch ffID {
	case "ff-good":
		ideal = 6_000_000
	case "ff-mid":
		ideal = 5_000_000
	case "ff-dear":
		ideal = 7_000_000
	case "ff-rv":
		ideal = 8_000_000
	case "ff-sk":
		ideal = 5_000_000
	}
	ceiling := futbolfantasy.Detail{IdealBid: ideal, FetchedAt: time.Now()}
	if c.stored == nil {
		c.stored = map[string]futbolfantasy.Detail{}
	}
	c.stored[ffID] = ceiling
	return ceiling, nil
}

func (c *ceilings) keep(ffID string) (futbolfantasy.Detail, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ceiling, ok := c.stored[ffID]
	return ceiling, ok
}

func (c *ceilings) page(ffID string) (map[string]any, error) {
	ceiling, err := c.fetch(ffID)
	return map[string]any{"ideal_bid": ceiling.IdealBid}, err
}

// drawn is the page's views the way the server draws them, the details applied by enrich.
func drawn(t *testing.T, enrich func(buckets map[string]any)) map[string]any {
	t.Helper()
	universe := fillWorld()
	buckets := advice.Recommend(universe, 20_000_000, 0, 15)
	enrich(buckets)
	_, views := Document{Universe: universe, Advice: buckets, Generated: "21/10/2026 09:00",
		Swaps: advice.ReadySwaps(universe, buckets, 20_000_000)}.Render()
	return views
}

func viewJSON(t *testing.T, view any) string {
	t.Helper()
	blob, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	return string(blob)
}

// Drawn before any page has landed, the panel is all there with every row waiting marked, and
// nothing judged from a ceiling it does not have.
func TestThePanelIsDrawnBeforeThePagesLand(t *testing.T) {
	source := &ceilings{delay: time.Hour}
	filler := &advice.Filler{Fetch: source.fetch, Stored: source.keep}
	views := drawn(t, func(buckets map[string]any) { filler.Enrich(buckets, 15) })

	decide := viewJSON(t, views["decidir"])
	if !strings.Contains(decide, "Esperando") || strings.Contains(decide, `"kind":"swap"`) ||
		strings.Contains(decide, "puja hasta") {
		t.Errorf("Decidir holds its cards until the ceilings are in: %s", decide)
	}
	if !strings.Contains(decide, "el plan espera a las fichas") {
		t.Errorf("the eleven says the plan is on its way: %s", decide)
	}
	buy := viewJSON(t, views["comprar"])
	if strings.Contains(buy, "de margen") || strings.Contains(buy, "sin margen") {
		t.Errorf("no margin is claimed for a ceiling still on its way: %s", buy)
	}
	if !strings.Contains(buy, `"pending":true`) || strings.Contains(buy, `"ideal":6000000`) {
		t.Errorf("the rows and their bid buttons say the page is coming: %s", buy)
	}
	sell := viewJSON(t, views["vender"])
	if !strings.Contains(sell, "el consejo espera a las fichas") || strings.Contains(sell, "recomendado") {
		t.Errorf("the offer is shown without a recommendation: %s", sell)
	}
}

// Filled in behind the page, the last draw is exactly the page that waited for every
// futbolfantasy page: no number, advice or plan differs.
func TestTheFilledPanelEqualsThePanelThatWaited(t *testing.T) {
	source := &ceilings{}
	waited := drawn(t, func(buckets map[string]any) {
		advice.EnrichBucketsWith(buckets, 15, source.page, nil)
	})

	over := make(chan struct{})
	source = &ceilings{}
	filler := &advice.Filler{Fetch: source.fetch, Stored: source.keep, Landed: func(batch advice.Batch) {
		if batch.Over {
			close(over)
		}
	}}
	drawn(t, func(buckets map[string]any) { filler.Enrich(buckets, 15) })
	select {
	case <-over:
	case <-time.After(5 * time.Second):
		t.Fatal("the fill never ended")
	}
	filled := drawn(t, func(buckets map[string]any) { filler.Enrich(buckets, 15) })

	if !strings.Contains(viewJSON(t, waited["decidir"]), `"kind":"swap"`) {
		t.Fatalf("the fixture should make the plan propose a signing: %s", viewJSON(t, waited["decidir"]))
	}
	for name := range waited {
		if got, want := viewJSON(t, filled[name]), viewJSON(t, waited[name]); got != want {
			t.Errorf("%s differs once filled:\n%s\nwant:\n%s", name, got, want)
		}
	}
	if len(filled) != len(waited) {
		t.Errorf("%d views, want %d", len(filled), len(waited))
	}
}

// Drawn with ceilings stored a day ago, the panel judges with them at once, each row saying how
// old its ceiling is, and nothing is held: the plan and the cards are there from the start.
func TestStoredCeilingsAreUsedWithTheirAge(t *testing.T) {
	source := &ceilings{delay: time.Hour}
	for _, id := range []string{"ff-good", "ff-mid", "ff-dear", "ff-rv"} {
		ceiling, _ := (&ceilings{}).fetch(id)
		ceiling.FetchedAt = time.Now().Add(-26 * time.Hour)
		if source.stored == nil {
			source.stored = map[string]futbolfantasy.Detail{}
		}
		source.stored[id] = ceiling
	}
	filler := &advice.Filler{Fetch: source.fetch, Stored: source.keep}
	views := drawn(t, func(buckets map[string]any) { filler.Enrich(buckets, 15) })
	decide, buy := viewJSON(t, views["decidir"]), viewJSON(t, views["comprar"])
	if strings.Contains(decide, "Esperando") || !strings.Contains(decide, `"kind":"swap"`) {
		t.Errorf("stored ceilings are judged with: %s", decide)
	}
	if !strings.Contains(buy, `"ceiling_age":"hace 26 h"`) || !strings.Contains(buy, `"ideal_age":"hace 26 h"`) ||
		!strings.Contains(buy, "de margen") {
		t.Errorf("each row and its bid say how old the ceiling is: %s", buy)
	}
}
