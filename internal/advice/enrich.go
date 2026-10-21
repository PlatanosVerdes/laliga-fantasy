package advice

import (
	"errors"
	"log/slog"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

// EnrichBuckets fills in what only the per-player page knows: futbolfantasy's maximum
// profitable bid.
//
// Every shortlist in one pass, each player read at most once. The buckets overlap heavily — a
// market player is in bids_now and possibly in the watchlist and the raid list too — so
// enriching them one at a time meant tens of duplicated requests per refresh, which is how
// futbolfantasy came to answer 429.
func EnrichBuckets(buckets Row, limit int, ttl time.Duration) int {
	return EnrichBucketsReporting(buckets, limit, ttl, nil)
}

// EnrichBucketsReporting is EnrichBuckets telling report how many players of how many it has
// been through, for a page waiting on it. The stored ceiling is used while fresh, and a page
// that cannot be read falls back on the stored one, however old.
func EnrichBucketsReporting(buckets Row, limit int, _ time.Duration,
	report func(done, total int)) int {
	return EnrichBucketsWith(buckets, limit, func(ffID string) (Row, error) {
		ceiling, err := futbolfantasy.DetailFor(ffID)
		if ceiling.FetchedAt.IsZero() {
			return nil, err
		}
		return Row{"ideal_bid": ceiling.IdealBid}, err
	}, report)
}

// EnrichBucketsWith is EnrichBucketsReporting reading each page with fetch. A detail that comes
// with an error is still applied: it is the stored one, standing in for a read that failed.
func EnrichBucketsWith(buckets Row, limit int, fetch func(ffID string) (Row, error),
	report func(done, total int)) int {
	order, byPlayer := detailRows(buckets, limit)
	fetched := 0
	for index, ffID := range order {
		if report != nil {
			report(index, len(order))
		}
		detail, err := fetch(ffID)
		if detail != nil {
			for _, row := range byPlayer[ffID] {
				ApplyCeiling(row, number(detail["ideal_bid"]))
			}
		}
		if err != nil {
			var limited *httpx.RateLimited
			if errors.As(err, &limited) {
				// Being rate limited is an answer, not a glitch: stop for this cycle rather
				// than make it worse.
				slog.Warn("futbolfantasy rate limited: dejo de enriquecer este ciclo",
					"fetched", fetched, "pending", len(order)-fetched)
				break
			}
			continue
		}
		fetched++
	}
	if report != nil {
		report(len(order), len(order))
	}
	slog.Debug("detail enrichment", "unique_players", len(order), "fetched", fetched)
	return fetched
}

// DetailBuckets are the shortlists whose rows get the per-player page, in the order the page
// shows them: my squad, the market, the rivals' players, then the rest.
//
// The squad is in here because the profitable ceiling of a player you *own* is what tells you
// whether an offer is a good sale. Without it, "is this offer good?" can only be answered from
// price, which is half the question.
var DetailBuckets = []string{"squad", "my_listings", "offers", "sells", "bids_now", "asks",
	"raids", "upcoming_raids", "watchlist"}

// CandidateBuckets are the shortlists every judgement that compares signings reads its ceilings
// from: the plan, the buy verdicts and the stand-in for a sale.
var CandidateBuckets = []string{"bids_now", "asks"}

// PendingKey marks a row whose per-player page is still on its way, and AgeKey one drawn with
// a stored ceiling that is being read again, with how old it is.
const (
	PendingKey = "detail_pending"
	AgeKey     = "detail_age"
)

// detailRows is every player of the shortlists by futbolfantasy id, each once, in the order of
// DetailBuckets, with the rows his page is written onto. The buckets overlap heavily, so a
// player is fetched once for all of them.
func detailRows(buckets Row, limit int) ([]string, map[string][]Row) {
	byPlayer := map[string][]Row{}
	var order []string
	for _, name := range DetailBuckets {
		for index, row := range rowsOf(buckets[name]) {
			if index >= limit {
				break
			}
			ffID := text(row["ff_id"])
			if ffID == "" {
				continue
			}
			if _, seen := byPlayer[ffID]; !seen {
				order = append(order, ffID)
			}
			byPlayer[ffID] = append(byPlayer[ffID], row)
		}
	}
	return order, byPlayer
}

// Waiting is how many rows of the named buckets are still waiting for their page.
func Waiting(buckets Row, names ...string) int {
	waiting := 0
	for _, name := range names {
		for _, row := range rowsOf(buckets[name]) {
			if truthy(row[PendingKey]) {
				waiting++
			}
		}
	}
	return waiting
}

// ApplyCeiling writes the profitable ceiling onto a row. It is the only thing advice reads from
// the page: the season's high and low and the open injury stay in the store, where only the
// drawing reads them.
func ApplyCeiling(row Row, ideal float64) {
	delete(row, PendingKey)
	delete(row, AgeKey)
	row["ideal_bid"] = ideal
}
