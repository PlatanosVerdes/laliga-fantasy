// The rival's squad, from the world we already hold.
//
// It needs no new request: every player carries who owns him and the standings carry the totals,
// so "what does cristian1206 actually have" was always answerable and simply had nowhere to be
// asked. It matters for the two decisions that involve somebody else — whether to pay a clause,
// and whether an offer of his is worth taking — because both depend on what else he owns.
package server

import (
	"net/http"
	"sort"
	"strings"
)

func (s *Server) manager(writer http.ResponseWriter, request *http.Request) {
	teamID := strings.TrimPrefix(request.URL.Path, "/api/manager/")
	universe := s.state.Universe()
	if universe == nil {
		s.json(writer, http.StatusServiceUnavailable, map[string]any{"error": "generando"})
		return
	}
	team := universe.LeagueTeams[teamID]
	if team == nil {
		s.json(writer, http.StatusNotFound, map[string]any{"error": "no conozco ese equipo"})
		return
	}

	rows := s.rows()
	squad := make([]map[string]any, 0, 16)
	listed, locked := 0, 0
	for _, player := range rows {
		owner := text(player["owner_team_id"])
		if owner != teamID {
			continue
		}
		listing := mapOf(player["market"])
		if text(listing["market_id"]) != "" {
			listed++
		}
		if truthy(player["clause_locked"]) {
			locked++
		}
		squad = append(squad, map[string]any{
			"id": player["id"], "name": player["name"], "position": player["position"],
			"position_id": player["position_id"], "team_id": player["team_id"],
			"team_short": player["team_short"], "image": player["image"],
			"value": player["value"], "xpts": player["xpts"],
			"points_value": player["points_value"], "season_points": player["season_points"],
			"projected_pct": player["projected_pct"], "start_probability": player["start_probability"],
			"status": player["status"], "available": player["available"],
			"clause": player["clause"], "clause_locked": player["clause_locked"],
			"clause_locked_until": player["clause_locked_until"],
			"shielded":            player["shielded"],
			"sale_locked":         player["sale_locked"], "hold_until": player["hold_until"],
			"market": listing, "score": player["score"],
		})
	}
	// Dearest first: that is the order a rival's squad is read in, because the expensive ones
	// are the ones whose clause hurts and whose sale would fund him.
	sort.SliceStable(squad, func(one, two int) bool {
		return number(squad[one]["value"]) > number(squad[two]["value"])
	})

	name := teamID
	if team.Manager != nil && *team.Manager != "" {
		name = *team.Manager
	} else if team.Name != nil && *team.Name != "" {
		name = *team.Name
	}
	total := 0.0
	for _, player := range squad {
		total += number(player["xpts"])
	}

	s.json(writer, http.StatusOK, map[string]any{
		"team_id": teamID, "manager": name, "team_name": team.Name,
		"points": team.Points, "position": team.Position,
		"squad_value": team.SquadValue, "clause_total": team.ClauseTotal,
		"estimated_cash": team.EstimatedCash, "cash_is_estimate": team.CashIsEstimate,
		"net_flow": team.NetFlow, "players": len(squad),
		"listed": listed, "clauses_locked": locked, "xpts_total": total,
		"squad": squad,
	})
}
