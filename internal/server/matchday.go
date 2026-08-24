// Who had whom on a given matchday.
//
// Squads move constantly, so "3 tuyos juegan" on a matchday two weeks old is a lie told with
// today's squad. The league log carries every change of hands with its date, so the past is
// recoverable: this endpoint answers, for one matchday, what every manager owned when the ball
// rolled — and that is also the honest source for the counts in the calendar.
package server

import (
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
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

	// A past matchday is about who *played*, not who was owned. The eleven is only knowable from
	// the week-scoped lineup route, so it is read per manager — on demand, because this panel is
	// opened by hand and nobody needs thirteen extra requests on every rebuild.
	lineupTTL := 24 * time.Hour
	if week >= universe.Week.WeekNumber {
		// The current one can still change.
		lineupTTL = time.Minute
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
		entry := map[string]any{
			"user_id": user, "team_id": teamOfUser[user],
			"manager": fallback(nameOfUser[user], user),
			"is_me":   universe.MyTeamID != nil && teamOfUser[user] == *universe.MyTeamID,
			"players": len(squad), "playing": playing, "squad": squad,
		}
		if s.opts.Client != nil {
			if fielded, formation, points, err := s.lineupOf(teamOfUser[user], week,
				lineupTTL); err == nil && len(fielded) > 0 {
				entry["lineup"] = fielded
				entry["formation"] = formation
				entry["week_points"] = points
				// Who was owned but not played: the bench, which is the other half of the
				// decision.
				onPitch := map[string]bool{}
				for _, line := range fielded {
					for _, player := range line {
						onPitch[text(player["id"])] = true
					}
				}
				bench := []map[string]any{}
				for _, player := range squad {
					if !onPitch[text(player["id"])] {
						bench = append(bench, player)
					}
				}
				entry["bench"] = bench
			} else if err != nil {
				slog.Debug("lineup unavailable", "team", teamOfUser[user], "week", week,
					"reason", err.Error())
			}
		}
		managers = append(managers, entry)
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

// lineupOf is the eleven a team fielded that week, by line, plus its shape and what it scored.
func (s *Server) lineupOf(teamID string, week int,
	ttl time.Duration) (map[string][]map[string]any, []int, float64, error) {
	if teamID == "" {
		return nil, nil, 0, nil
	}
	payload, err := s.opts.Client.TeamLineupWeek(teamID, week, ttl)
	if err != nil {
		return nil, nil, 0, err
	}
	formation := mapOf(payload["formation"])
	known := map[string]map[string]any{}
	for _, row := range s.rows() {
		known[text(row["id"])] = row
	}

	shape := []int{}
	for _, value := range listAny(formation["tacticalFormation"]) {
		shape = append(shape, int(number(value)))
	}

	out := map[string][]map[string]any{}
	for _, line := range api.LineupLines {
		for _, slot := range listOf(formation[line]) {
			master := mapOf(slot["playerMaster"])
			id := text(master["id"])
			extra := known[id]
			out[line] = append(out[line], map[string]any{
				"id": id, "name": fallback(text(master["nickname"]), text(master["name"])),
				"position_id": int(number(master["positionId"])),
				"team_id":     text(master["teamId"]),
				"image":       text(nested(master, "images", "transparent", "256x256")),
				"points":      number(master["points"]),
				"team_short":  text(extra["team_short"]),
			})
		}
	}
	padLines(out, shape)

	return out, shape, number(payload["points"]), nil
}

// listAny reads a JSON array of anything, which is what a formation is.
func listAny(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return nil
}

// padLines opens an empty slot for every shirt the formation asks for and the payload does not
// bring. Without it a line short of a player comes back short, so the ten that did play spread
// themselves evenly over the pitch and the hole is invisible. The keeper is not in the shape.
func padLines(lines map[string][]map[string]any, shape []int) {
	want := map[string]int{"goalkeeper": 1}
	for index, line := range []string{"defender", "midfield", "striker"} {
		if index < len(shape) {
			want[line] = shape[index]
		}
	}
	for line, count := range want {
		for len(lines[line]) < count {
			lines[line] = append(lines[line], nil)
		}
	}
}

func playerOf(universe *model.Universe, id string) *model.Player {
	for index := range universe.Players {
		if universe.Players[index].ID == id {
			return &universe.Players[index]
		}
	}
	return nil
}
