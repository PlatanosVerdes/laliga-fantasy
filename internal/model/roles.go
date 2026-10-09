package model

import "github.com/PlatanosVerdes/laliga-fantasy/internal/matching"

// MatchRoles says whose each hierarchy row is: by futbolfantasy's own id when the market
// already matched him, and by name and club the way the market is matched otherwise.
func MatchRoles(players []map[string]any, market map[string]map[string]any,
	roles []map[string]any, teamIndex map[string]map[string]any) map[string]map[string]any {
	byFFID := map[string]string{}
	for id, row := range market {
		if ffID := text(row["ff_id"]); ffID != "" {
			byFFID[ffID] = id
		}
	}
	out := map[string]map[string]any{}
	var rest []map[string]any
	for _, role := range roles {
		if id, ok := byFFID[text(role["ff_id"])]; ok && text(role["ff_id"]) != "" {
			out[id] = role
			continue
		}
		rest = append(rest, map[string]any{"ff_name": role["ff_name"], "ff_team": role["team"],
			"ff_team_id": role["team"], "role": role})
	}
	var open []map[string]any
	for _, player := range players {
		if _, done := out[text(player["id"])]; !done {
			open = append(open, player)
		}
	}
	matched, _ := matching.MatchMarket(open, rest, teamIndex)
	for id, row := range matched {
		if role, ok := row["role"].(map[string]any); ok {
			out[id] = role
		}
	}
	return out
}
