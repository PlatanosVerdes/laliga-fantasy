package model

import (
	"math"
	"sort"
	"time"
)

// ClauseCalendar is when each locked clause opens up.
//
// Two sides of the same date: your own players become raidable the moment their lock lifts,
// and a rival's player becomes raidable *by you*. Both are only actionable if you know the
// date is coming, which is why this has a section of its own rather than a column.
func ClauseCalendar(players []Player, horizonHours float64) map[string]any {
	now := time.Now().UTC()
	mine, rivals := []map[string]any{}, []map[string]any{}

	for _, player := range players {
		if player.Owner == nil || *player.Owner == "" || player.ClauseUntil == nil {
			continue
		}
		unlock, err := parseTime(*player.ClauseUntil)
		if err != nil {
			continue
		}
		hours := unlock.Sub(now).Hours()
		row := playerRow(player)
		row["unlock_at"] = unlock.Format(time.RFC3339)
		row["hours_left"] = hours
		row["unlocked"] = hours <= 0
		if player.IsMine {
			mine = append(mine, row)
		} else {
			rivals = append(rivals, row)
		}
	}

	// Soonest first, and within the same hour the better player first: every clause in a
	// league tends to open at the same instant, so the hour alone is usually a tie.
	byHourThenWorth := func(rows []map[string]any) {
		sort.SliceStable(rows, func(i, j int) bool {
			li := math.Round(number(rows[i]["hours_left"]))
			lj := math.Round(number(rows[j]["hours_left"]))
			if li != lj {
				return li < lj
			}
			return number(rows[i]["score"]) > number(rows[j]["score"])
		})
	}
	byHourThenWorth(mine)
	byHourThenWorth(rivals)

	soon := func(rows []map[string]any) []map[string]any {
		out := []map[string]any{}
		for _, row := range rows {
			if number(row["hours_left"]) <= horizonHours {
				out = append(out, row)
			}
		}
		return out
	}

	var next any
	if len(mine) > 0 {
		next = mine[0]["unlock_at"]
	}
	return map[string]any{
		"mine": mine, "rivals": rivals,
		"mine_soon": soon(mine), "rivals_soon": soon(rivals),
		"next_unlock": next,
	}
}
