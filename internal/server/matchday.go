// Who had whom on a given matchday.
//
// Squads move constantly, so "3 tuyos juegan" on a matchday two weeks old is a lie told with
// today's squad. The league log carries every change of hands with its date, so the past is
// recoverable: this endpoint answers, for one matchday, what every manager owned when the ball
// rolled — and that is also the honest source for the counts in the calendar.
package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

func (s *Server) matchday(writer http.ResponseWriter, request *http.Request) {
	week, err := strconv.Atoi(strings.TrimPrefix(request.URL.Path, "/api/matchday/"))
	if err != nil || week <= 0 {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "jornada no valida"})
		return
	}
	universe := s.state.Universe()
	if universe == nil {
		s.json(writer, http.StatusServiceUnavailable, map[string]any{"error": "generando"})
		return
	}

	// The matchday starts with its first kick-off: that is the instant the squads counted.
	kickoff := time.Time{}
	teamsPlaying := map[string]bool{}
	for _, fixture := range universe.Schedule {
		if fixture.Week != week {
			continue
		}
		teamsPlaying[fixture.LocalID] = true
		teamsPlaying[fixture.VisitorID] = true
		if when, err := time.Parse(time.RFC3339, fixture.Kickoff); err == nil {
			if kickoff.IsZero() || when.Before(kickoff) {
				kickoff = when
			}
		}
	}
	if kickoff.IsZero() {
		s.json(writer, http.StatusNotFound, map[string]any{"error": "no tengo esa jornada"})
		return
	}

	userOfTeam := map[string]string{}
	nameOfUser := map[string]string{}
	teamOfUser := map[string]string{}
	for teamID, team := range universe.LeagueTeams {
		if team == nil || team.UserID == "" {
			continue
		}
		userOfTeam[teamID] = team.UserID
		teamOfUser[team.UserID] = teamID
		name := teamID
		if team.Manager != nil && *team.Manager != "" {
			name = *team.Manager
		} else if team.Name != nil && *team.Name != "" {
			name = *team.Name
		}
		nameOfUser[team.UserID] = name
	}

	// Today's owners in user space, which is what the log speaks.
	today := map[string]string{}
	for _, player := range universe.Players {
		if player.OwnerTeamID == nil {
			continue
		}
		if user := userOfTeam[*player.OwnerTeamID]; user != "" {
			today[player.ID] = user
		}
	}
	owners := model.OwnershipAt(universe.Activity, today, kickoff)

	byName := map[string]string{}
	for _, player := range universe.Players {
		byName[player.ID] = player.Name
	}

	squads := map[string][]map[string]any{}
	for playerID, user := range owners {
		player := playerOf(universe, playerID)
		if player == nil {
			continue
		}
		squads[user] = append(squads[user], map[string]any{
			"id": player.ID, "name": player.Name, "position": player.Position,
			"position_id": player.PositionID, "team_id": player.TeamID,
			"team_short": player.TeamShort, "value": player.Value, "image": player.Image,
			"xpts": player.XPts, "played": teamsPlaying[player.TeamID],
			"season_points": player.SeasonPoints,
		})
	}

	managers := make([]map[string]any, 0, len(squads))
	for user, squad := range squads {
		sort.SliceStable(squad, func(one, two int) bool {
			return number(squad[one]["value"]) > number(squad[two]["value"])
		})
		playing := 0
		for _, player := range squad {
			if truthy(player["played"]) {
				playing++
			}
		}
		managers = append(managers, map[string]any{
			"user_id": user, "team_id": teamOfUser[user],
			"manager": fallback(nameOfUser[user], user),
			"is_me":   universe.MyTeamID != nil && teamOfUser[user] == *universe.MyTeamID,
			"players": len(squad), "playing": playing, "squad": squad,
		})
	}
	// Mine first, then by how many of theirs played: that is the comparison being made.
	sort.SliceStable(managers, func(one, two int) bool {
		if truthy(managers[one]["is_me"]) != truthy(managers[two]["is_me"]) {
			return truthy(managers[one]["is_me"])
		}
		return number(managers[one]["playing"]) > number(managers[two]["playing"])
	})

	s.json(writer, http.StatusOK, map[string]any{
		"week": week, "kickoff": kickoff.Format(time.RFC3339),
		"reconstructed": true, "managers": managers,
	})
}

func playerOf(universe *model.Universe, id string) *model.Player {
	for index := range universe.Players {
		if universe.Players[index].ID == id {
			return &universe.Players[index]
		}
	}
	return nil
}
