package advice

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

// shortlists is one player per kind of shortlist, each with his own futbolfantasy id, plus a
// market player who is also on the watchlist.
func shortlists() Row {
	row := func(id string, extra Row) Row {
		out := Row{"id": id, "ff_id": "ff" + id, "value": 10_000_000.0}
		for key, value := range extra {
			out[key] = value
		}
		return out
	}
	return Row{
		"watchlist":      []any{row("9", nil), row("5", nil)},
		"raids":          []any{row("7", Row{"entry_cost": 30_000_000.0})},
		"bids_now":       []any{row("5", Row{"entry_cost": 4_000_000.0})},
		"asks":           []any{row("6", Row{"entry_cost": 12_000_000.0})},
		"squad":          []any{row("1", nil), row("2", nil)},
		"offers":         []any{row("2", Row{"offer_amount": 11_000_000.0})},
		"upcoming_raids": []any{row("8", nil)},
	}
}

// pages is a futbolfantasy that answers every player, slowly, remembers who it was asked for and
// in which order, and keeps what it answered the way the ceiling store does.
type pages struct {
	mu     sync.Mutex
	asked  []string
	delay  time.Duration
	fail   map[string]error
	stored map[string]futbolfantasy.Detail
}

func (p *pages) fetch(ffID string) (futbolfantasy.Detail, error) {
	time.Sleep(p.delay)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, ffID)
	if err := p.fail[ffID]; err != nil {
		return futbolfantasy.Detail{}, err
	}
	ceiling := futbolfantasy.Detail{IdealBid: 20_000_000, FetchedAt: time.Now()}
	if p.stored == nil {
		p.stored = map[string]futbolfantasy.Detail{}
	}
	p.stored[ffID] = ceiling
	return ceiling, nil
}

func (p *pages) keep(ffID string) (futbolfantasy.Detail, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ceiling, ok := p.stored[ffID]
	return ceiling, ok
}

// row is fetch the way the synchronous enrichment reads it.
func (p *pages) row(ffID string) (Row, error) {
	ceiling, err := p.fetch(ffID)
	if err != nil {
		return nil, err
	}
	return Row{"ideal_bid": ceiling.IdealBid}, nil
}

func (p *pages) filler() *Filler { return &Filler{Fetch: p.fetch, Stored: p.keep} }

func (p *pages) order() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.asked...)
}

// filled runs a filler over buckets until its pass is over, and returns its batches.
func filled(t *testing.T, filler *Filler, buckets Row) []Batch {
	t.Helper()
	var mu sync.Mutex
	var batches []Batch
	over := make(chan struct{})
	filler.Landed = func(batch Batch) {
		mu.Lock()
		batches = append(batches, batch)
		mu.Unlock()
		if batch.Over {
			close(over)
		}
	}
	if filler.Enrich(buckets, 15) == 0 {
		t.Fatal("nothing to fill")
	}
	select {
	case <-over:
	case <-time.After(5 * time.Second):
		t.Fatal("the fill never ended")
	}
	for running := true; running; {
		filler.mu.Lock()
		running = filler.running
		filler.mu.Unlock()
	}
	mu.Lock()
	defer mu.Unlock()
	return batches
}

// The page is drawn with what is stored: a fresh ceiling as it is, an old one with its age and
// read again, a player never read marked as waiting. Nobody is fetched before Enrich returns.
func TestEnrichAppliesWhatIsStoredAndQueuesTheRest(t *testing.T) {
	source := &pages{delay: 50 * time.Millisecond, stored: map[string]futbolfantasy.Detail{
		"ff1": {IdealBid: 15_000_000, FetchedAt: time.Now()},
		"ff2": {IdealBid: 11_000_000, FetchedAt: time.Now().Add(-50 * time.Hour)},
	}}
	filler := source.filler()
	buckets := shortlists()
	started := time.Now()
	if queued := filler.Enrich(buckets, 15); queued != 6 {
		t.Errorf("%d queued, want 6: every one but the fresh one", queued)
	}
	if spent := time.Since(started); spent > 30*time.Millisecond {
		t.Errorf("Enrich waited for a page: %v", spent)
	}
	squad := rowsOf(buckets["squad"])
	if squad[0]["ideal_bid"] != 15_000_000.0 || squad[0][PendingKey] != nil || squad[0][AgeKey] != nil {
		t.Errorf("the fresh ceiling is applied as it is: %v", squad[0])
	}
	if squad[1]["ideal_bid"] != 11_000_000.0 || squad[1][AgeKey] != "hace 2 días" || squad[1][PendingKey] != nil {
		t.Errorf("the old one is shown with its age while it is read again: %v", squad[1])
	}
	market := rowsOf(buckets["bids_now"])[0]
	if !truthy(market[PendingKey]) || market["ideal_bid"] != nil {
		t.Errorf("a player never read waits, with no ceiling at all: %v", market)
	}
	if Waiting(buckets, CandidateBuckets...) != 2 {
		t.Errorf("both market candidates wait")
	}
}

// What the page shows first is fetched first: my squad, then the market, then the rivals'
// players, then the rest. Every batch names its players and the last one says it is over.
func TestTheFillFollowsThePageAndReportsEachBatch(t *testing.T) {
	source := &pages{delay: 5 * time.Millisecond}
	filler := source.filler()
	filler.Gap = time.Nanosecond
	buckets := shortlists()
	batches := filled(t, filler, buckets)
	want := []string{"ff1", "ff2", "ff5", "ff6", "ff7", "ff8", "ff9"}
	if got := source.order(); !reflect.DeepEqual(got, want) {
		t.Errorf("fetched %v, want %v", got, want)
	}
	var players []string
	for _, batch := range batches {
		players = append(players, batch.Players...)
	}
	// A player in two shortlists is named once per page.
	if !reflect.DeepEqual(players, []string{"1", "2", "5", "6", "7", "8", "9"}) {
		t.Errorf("batches named %v", players)
	}
	last := batches[len(batches)-1]
	if !last.Over || last.Done != 7 || last.Total != 7 || len(batches) != 7 {
		t.Errorf("one batch per page with no gap, the last one over: %+v", batches)
	}

	// The next draw finds every page in hand and waits for nothing.
	again := shortlists()
	if waiting := filler.Enrich(again, 15); waiting != 0 || Waiting(again, DetailBuckets...) != 0 {
		t.Errorf("%d still waiting after the fill", waiting)
	}
}

// The first page is shown as soon as it lands; after it, a gap between batches keeps a fast fill
// from drawing the page once per player.
func TestBatchesAreSpacedByTheGap(t *testing.T) {
	source := &pages{delay: time.Millisecond}
	filler := source.filler()
	filler.Gap = time.Hour
	batches := filled(t, filler, shortlists())
	if len(batches) != 2 || len(batches[0].Players) != 1 || len(batches[1].Players) != 6 {
		t.Errorf("the first page, then the rest at the end: %+v", batches)
	}
}

// Once everything has landed the rows are exactly what the synchronous enrichment writes, so
// the page drawn last is the page the panel always drew.
func TestAFilledPageEqualsTheOneThatWaited(t *testing.T) {
	waited := shortlists()
	EnrichBucketsWith(waited, 15, (&pages{}).row, nil)

	source := &pages{}
	filler := source.filler()
	filled(t, filler, shortlists())
	drawn := shortlists()
	filler.Enrich(drawn, 15)
	if !reflect.DeepEqual(waited, drawn) {
		t.Errorf("filled:\n%v\nwaited:\n%v", drawn, waited)
	}
}

// A 429 stops the pass, as it always stopped the enrichment: the rest is left alone for a while
// and drawn the way a failed fetch always was, with no ceiling and nothing waiting.
func TestARateLimitStopsTheFillAndLeavesTheRestAlone(t *testing.T) {
	source := &pages{fail: map[string]error{"ff5": &httpx.RateLimited{}}}
	filler := source.filler()
	batches := filled(t, filler, shortlists())
	if got := source.order(); !reflect.DeepEqual(got, []string{"ff1", "ff2", "ff5"}) {
		t.Errorf("asked %v after the 429", got)
	}
	if last := batches[len(batches)-1]; last.Done != 7 || last.Total != 7 {
		t.Errorf("the pass counts the ones it gave up on: %+v", last)
	}
	again := shortlists()
	if waiting := filler.Enrich(again, 15); waiting != 0 {
		t.Errorf("%d queued again right after a 429", waiting)
	}
	market := rowsOf(again["bids_now"])[0]
	if truthy(market[PendingKey]) || market["ideal_bid"] != nil {
		t.Errorf("drawn as a failed fetch: %v", market)
	}
	if squad := rowsOf(again["squad"])[0]; squad["ideal_bid"] != 20_000_000.0 {
		t.Errorf("what landed before the 429 stays: %v", squad)
	}

	filler.Retry, filler.Landed = time.Nanosecond, nil
	if waiting := filler.Enrich(shortlists(), 15); waiting != 5 {
		t.Errorf("after the pause the rest is asked for again: %d", waiting)
	}
}

// Any other failure leaves that one player alone and goes on with the next.
func TestAFailedPageDoesNotStopTheOthers(t *testing.T) {
	source := &pages{fail: map[string]error{"ff2": errors.New("404")}}
	filler := source.filler()
	filled(t, filler, shortlists())
	if got := len(source.order()); got != 7 {
		t.Errorf("asked %d pages, want all 7", got)
	}
}

// An old ceiling that cannot be read again is kept, with its age, and not asked for again until
// the pause is over; one read again loses its age.
func TestAnOldCeilingStaysWhenItCannotBeReadAgain(t *testing.T) {
	old := futbolfantasy.Detail{IdealBid: 11_000_000, FetchedAt: time.Now().Add(-30 * time.Hour)}
	source := &pages{fail: map[string]error{"ff2": errors.New("timeout")},
		stored: map[string]futbolfantasy.Detail{"ff1": old, "ff2": old}}
	filler := source.filler()
	filled(t, filler, shortlists())
	again := shortlists()
	filler.Enrich(again, 15)
	squad := rowsOf(again["squad"])
	if squad[0][AgeKey] != nil || squad[0]["ideal_bid"] != 20_000_000.0 {
		t.Errorf("read again, it is drawn as new: %v", squad[0])
	}
	if squad[1][AgeKey] != "hace 30 h" || squad[1]["ideal_bid"] != 11_000_000.0 {
		t.Errorf("unreadable, the stored one stays with its age: %v", squad[1])
	}
}

// The synchronous enrichment applies the stored ceiling a failed read hands back.
func TestTheWaitingEnrichmentKeepsTheStoredCeiling(t *testing.T) {
	buckets := shortlists()
	EnrichBucketsWith(buckets, 15, func(ffID string) (Row, error) {
		return Row{"ideal_bid": 9.0}, errors.New("timeout")
	}, nil)
	if got := rowsOf(buckets["asks"])[0]["ideal_bid"]; got != 9.0 {
		t.Errorf("the stored ceiling is not applied: %v", got)
	}
}
