package model

import (
	"log/slog"
	"sort"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
)

// EnrichActivityValues adds what each player was worth the day he changed hands.
//
// The amount alone does not say whether it was a bargain or a panic buy. The market-value
// history is a daily series per player, so the value on the event's own date turns "paid 92M"
// into "paid 92M for somebody worth 78M". Only the biggest trades are looked up: it is one
// request each, and the small ones tell no story.
func EnrichActivityValues(client *api.Client, events []Event, limit int) {
	type sized struct {
		index  int
		amount float64
	}
	var candidates []sized
	for index, event := range events {
		if event.Amount != nil && *event.Amount != 0 && event.PlayerID != nil {
			candidates = append(candidates, sized{index, *event.Amount})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].amount > candidates[j].amount
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	enriched := 0
	for _, candidate := range candidates {
		event := &events[candidate.index]
		history, err := client.PlayerMarketValue(*event.PlayerID, 12*time.Hour)
		if err != nil {
			slog.Debug("value history unavailable", "player_id", *event.PlayerID,
				"reason", err.Error())
			continue
		}
		day := event.Date
		if len(day) > 10 {
			day = day[:10]
		}
		if len(history) == 0 || day == "" {
			continue
		}

		// The series is daily: take the last point at or before the trade, and the first
		// point when the trade predates the series.
		point := history[0]
		found := false
		for _, item := range history {
			stamp := text(item["date"])
			if len(stamp) > 10 {
				stamp = stamp[:10]
			}
			if stamp <= day {
				point, found = item, true
			}
		}
		_ = found
		value := number(point["marketValue"])
		if value == 0 {
			continue
		}
		event.ValueThen = &value
		premium := *event.Amount / value
		event.Premium = &premium
		absolute := *event.Amount - value
		event.PremiumAbs = &absolute
		enriched++
	}
	slog.Info("activity values enriched", "enriched", enriched)
}
