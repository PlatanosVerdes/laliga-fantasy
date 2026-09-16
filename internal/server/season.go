// How the table got to where it is.
//
// The league publishes today's classification and nothing else: position and previousPosition,
// one matchday of memory. So the past is rebuilt from what each manager fielded each week, which
// is the same arithmetic the game does — checked against the standing, where the running total
// lands on teamPoints for all thirteen.
package server

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// A finished matchday never scores again, so its points are kept for as long as the process
// lives and only the newest week costs requests.
type season struct {
	mu     sync.Mutex
	points map[string]float64
}

func (s *Server) weekPoints(teamID string, week int) (float64, bool) {
	key := teamID + "/" + strconv.Itoa(week)
	s.season.mu.Lock()
	if points, known := s.season.points[key]; known {
		s.season.mu.Unlock()
		return points, true
	}
	s.season.mu.Unlock()

	payload, err := s.opts.Client.TeamLineupWeek(teamID, week, 24*time.Hour)
	if err != nil {
		return 0, false
	}
	points := number(payload["points"])
	s.season.mu.Lock()
	if s.season.points == nil {
		s.season.points = map[string]float64{}
	}
	s.season.points[key] = points
	s.season.mu.Unlock()
	return points, true
}

// placeByWeek writes down where each manager stood once each matchday was in, level on points
// being level. A week nobody could be read for leaves a hole rather than a place, so the line
// breaks there instead of dropping to last.
func placeByWeek(managers []map[string]any, weeks int) {
	for at := 0; at < weeks; at++ {
		for _, manager := range managers {
			var place any
			if mine := manager["total"].([]any)[at]; mine != nil {
				position := 1
				for _, other := range managers {
					if above := other["total"].([]any)[at]; above != nil &&
						number(above) > number(mine) {
						position++
					}
				}
				place = position
			}
			places, _ := manager["place"].([]any)
			manager["place"] = append(places, place)
		}
	}
}

func (s *Server) seasonTable(writer http.ResponseWriter, request *http.Request) {
	universe := s.state.Universe()
	if universe == nil || s.opts.Client == nil {
		s.json(writer, http.StatusServiceUnavailable, map[string]any{"error": "generando"})
		return
	}
	// The live one is half a matchday and would draw a place nobody holds yet.
	last := universe.Week.WeekNumber - 1
	if last < 1 {
		s.json(writer, http.StatusOK, map[string]any{"weeks": []int{}, "managers": []any{}})
		return
	}
	weeks := make([]int, 0, last)
	for week := 1; week <= last; week++ {
		weeks = append(weeks, week)
	}

	managers := []map[string]any{}
	for teamID, team := range universe.LeagueTeams {
		if team == nil {
			continue
		}
		name := teamID
		if team.Manager != nil && *team.Manager != "" {
			name = *team.Manager
		} else if team.Name != nil && *team.Name != "" {
			name = *team.Name
		}
		scored, running := []any{}, []any{}
		total := 0.0
		for _, week := range weeks {
			points, ok := s.weekPoints(teamID, week)
			if !ok {
				scored, running = append(scored, nil), append(running, nil)
				continue
			}
			total += points
			scored, running = append(scored, points), append(running, total)
		}
		managers = append(managers, map[string]any{
			"team_id": teamID, "manager": name,
			"is_me":  universe.MyTeamID != nil && teamID == *universe.MyTeamID,
			"points": scored, "total": running, "season": total,
		})
	}

	placeByWeek(managers, len(weeks))
	// By the season's total, and by name when they are level: the map they came out of has no
	// order, and the labels down the right of the chart cannot change place between refreshes.
	sort.SliceStable(managers, func(one, two int) bool {
		if first, second := number(managers[one]["season"]),
			number(managers[two]["season"]); first != second {
			return first > second
		}
		return text(managers[one]["manager"]) < text(managers[two]["manager"])
	})

	s.json(writer, http.StatusOK, map[string]any{"weeks": weeks, "managers": managers})
}
