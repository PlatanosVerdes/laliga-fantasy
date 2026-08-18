package advice

import (
	"errors"
	"log/slog"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

// EnrichBuckets fills in what only the per-player page knows: futbolfantasy's maximum
// profitable bid, the value history behind the sparkline, and the season's high and low.
//
// Every shortlist in one pass, each player fetched at most once. The buckets overlap heavily
// — a market player is in bids_now and possibly in the watchlist and the raid list too — so
// enriching them one at a time meant tens of duplicated requests per refresh, which is how
// futbolfantasy came to answer 429.
func EnrichBuckets(buckets Row, limit int, ttl time.Duration) int {
	// The squad is in here because the profitable ceiling of a player you *own* is what tells
	// you whether an offer is a good sale. Without it, "is this offer good?" can only be
	// answered from price, which is half the question.
	names := []string{"bids_now", "asks", "watchlist", "raids", "upcoming_raids", "squad",
		"my_listings", "offers", "sells"}

	byPlayer := map[string][]Row{}
	var order []string
	for _, name := range names {
		rows := rowsOf(buckets[name])
		for index, row := range rows {
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

	fetched := 0
	for _, ffID := range order {
		detail, err := futbolfantasy.PlayerDetail(ffID, ttl)
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
		for _, row := range byPlayer[ffID] {
			ApplyDetail(row, detail)
		}
	}
	slog.Debug("detail enrichment", "unique_players", len(order), "fetched", fetched)
	return fetched
}

// ApplyDetail writes the detail onto a row.
func ApplyDetail(row Row, detail Row) {
	row["ideal_bid"] = detail["ideal_bid"]
	row["max_value"] = detail["max_value"]
	row["min_value"] = detail["min_value"]
	row["max_date"] = detail["max_date"]
	row["injury_marks"] = detail["injury_marks"]

	entry := number(row["entry_cost"])
	if entry == 0 {
		entry = number(row["value"])
	}
	ideal := number(detail["ideal_bid"])
	if ideal != 0 {
		headroom := ideal - entry
		row["bid_headroom"] = headroom
	} else {
		row["bid_headroom"] = nil
	}
	row["profitable"] = ideal != 0 && entry != 0 && ideal >= entry

	history := rowsOf(detail["history"])
	if len(history) == 0 {
		history = rowsOf(detail["prev_season_history"])
	}
	values := make([]any, 0, len(history))
	for _, point := range history {
		values = append(values, point["value"])
	}
	// The last sixty points: the sparkline is 74 pixels wide and a longer series only makes
	// it noisier.
	if len(values) > 60 {
		values = values[len(values)-60:]
	}
	row["value_history"] = values
}
