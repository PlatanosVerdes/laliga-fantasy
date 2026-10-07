package model

import (
	"log/slog"
	"sort"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
)

// LineupPlayer is one shirt of the eleven a manager saved for the matchday in play.
type LineupPlayer struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	TeamID     string `json:"team_id"`
	PositionID int    `json:"position_id"`
	// Nil until the feed carries a row for that matchday: not scored yet is not zero points.
	Points *float64 `json:"points"`
}

// loadLineups is every manager's saved eleven for one matchday, keyed by team id. A team whose
// lineup cannot be read is left out, and the readers fall back to what they did without it.
func loadLineups(client *api.Client, teams map[string]*LeagueTeam,
	week int) map[string][]LineupPlayer {
	out := map[string][]LineupPlayer{}
	for teamID := range teams {
		payload, err := client.TeamLineupWeek(teamID, week, 5*time.Minute)
		if err != nil {
			slog.Debug("lineup unavailable", "team", teamID, "week", week,
				"reason", err.Error())
			continue
		}
		out[teamID] = LineupOf(payload, week)
	}
	return out
}

// LineupOf reads the shirts of a week-scoped lineup payload, line by line.
func LineupOf(payload map[string]any, week int) []LineupPlayer {
	formation, _ := payload["formation"].(map[string]any)
	out := []LineupPlayer{}
	for _, line := range api.LineupLines {
		for _, slot := range rowsOf(formation[line]) {
			master, _ := slot["playerMaster"].(map[string]any)
			id := text(master["id"])
			if id == "" {
				continue
			}
			player := LineupPlayer{ID: id, Name: label(master), TeamID: text(master["teamId"]),
				PositionID: int(number(master["positionId"]))}
			if points, ok := PointsIn(master, week); ok {
				player.Points = &points
			}
			out = append(out, player)
		}
	}
	return out
}

// PointsIn is what a player master scored on one matchday, and whether the feed has a row for it.
func PointsIn(master map[string]any, week int) (float64, bool) {
	for _, row := range OneRowPerWeek(rowsOf(master["lastStats"])) {
		if int(number(row["weekNumber"])) == week {
			return number(row["totalPoints"]), true
		}
	}
	return 0, false
}

// OneRowPerWeek is the season as it was played: one row per matchday, oldest first. The live
// matchday can come back several times and only one copy is the match, so the copies are told
// apart by minutes and not by points, because a matchday on the bench is a real row of zeros. The
// feed does not promise an order either: Espart came back as J2, J1, J3.
func OneRowPerWeek(stats []map[string]any) []map[string]any {
	best := map[float64]map[string]any{}
	for _, stat := range stats {
		week := number(stat["weekNumber"])
		if current, seen := best[week]; seen && !fuller(stat, current) {
			continue
		}
		best[week] = stat
	}
	out := make([]map[string]any, 0, len(best))
	for _, stat := range best {
		out = append(out, stat)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return number(out[i]["weekNumber"]) < number(out[j]["weekNumber"])
	})
	return out
}

func fuller(candidate, current map[string]any) bool {
	if played, before := minutesIn(candidate), minutesIn(current); played != before {
		return played > before
	}
	return number(current["totalPoints"]) == 0 && number(candidate["totalPoints"]) != 0
}

// Every stat is a pair: what he did and what it scored.
func minutesIn(stat map[string]any) float64 {
	stats, _ := stat["stats"].(map[string]any)
	pair, ok := stats["mins_played"].([]any)
	if !ok || len(pair) == 0 {
		return 0
	}
	return number(pair[0])
}

func rowsOf(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok {
				out = append(out, row)
			}
		}
		return out
	}
	return nil
}
