// Comparing players side by side.
//
// The drawer answers "how good is he", but a signing is always "instead of whom": el que entra
// se paga con el que sale, y esa pregunta necesita dos columnas. Sale del mundo que ya tenemos,
// asi que comparar no cuesta ninguna peticion a LaLiga.
package server

import (
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
)

// A table nobody can read at a glance compares nothing.
const compareMax = 8

func (s *Server) compare(writer http.ResponseWriter, request *http.Request) {
	if s.state.Universe() == nil {
		s.json(writer, http.StatusServiceUnavailable, map[string]any{"error": "generando"})
		return
	}
	rows := s.rows()
	byID := make(map[string]map[string]any, len(rows))
	mine := make([]map[string]any, 0, 26)
	for _, row := range rows {
		byID[text(row["id"])] = row
		if truthy(row["is_mine"]) {
			mine = append(mine, map[string]any{
				"id": row["id"], "name": row["name"], "position": row["position"],
				"position_id": row["position_id"], "value": row["value"],
				"xpts": row["xpts"], "score": row["score"], "image": row["image"],
			})
		}
	}
	// Best first: whoever asks for "mis MED" wants the ones that actually set the bar, and the
	// tray has room for a handful, not for the whole squad.
	sort.SliceStable(mine, func(one, two int) bool {
		return number(mine[one]["score"]) > number(mine[two]["score"])
	})

	seen := map[string]bool{}
	players := make([]map[string]any, 0, compareMax)
	for _, id := range strings.Split(request.URL.Query().Get("ids"), ",") {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || byID[id] == nil || len(players) >= compareMax {
			continue
		}
		seen[id] = true
		players = append(players, compareRow(byID[id]))
	}

	// The profitable ceiling lives on futbolfantasy, one page per player, and a column that
	// arrives late is a column nobody reads. Parallel and cached: usually no request at all.
	var wait sync.WaitGroup
	for _, player := range players {
		ffID := text(player["ff_id"])
		if player["ideal_bid"] != nil || ffID == "" {
			continue
		}
		wait.Add(1)
		go func(target map[string]any, ffID string) {
			defer wait.Done()
			if detail, err := futbolfantasy.PlayerDetail(ffID, futbolfantasy.DetailTTL); err == nil {
				target["ideal_bid"] = number(detail["ideal_bid"])
			}
		}(player, ffID)
	}
	wait.Wait()

	s.json(writer, http.StatusOK, map[string]any{"players": players, "mine": mine})
}

// compareRow keeps the same field names the tables and the drawer use, so one number cannot
// read differently depending on where it is shown.
func compareRow(row map[string]any) map[string]any {
	out := map[string]any{"market": mapOf(row["market"])}
	for _, key := range []string{
		"id", "name", "position", "position_id", "team", "team_short", "team_id", "image",
		"ff_id", "value", "xpts", "points_value", "score", "rank", "position_rank",
		"season_points", "season_avg", "last_season_points", "start_probability",
		"next_rival", "next_home", "next_week", "projected_pct", "ideal_bid",
		"clause", "clause_locked", "clause_locked_until", "shielded",
		"sale_locked", "hold_until", "status", "available",
		"is_mine", "owner", "owner_team_id",
	} {
		if value, present := row[key]; present {
			out[key] = value
		}
	}
	return out
}
