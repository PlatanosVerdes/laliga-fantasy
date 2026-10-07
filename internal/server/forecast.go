package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/forecast"
)

// forecastWeek answers one matchday's forecast against what each eleven scored.
func (s *Server) forecastWeek(writer http.ResponseWriter, request *http.Request) {
	week, err := strconv.Atoi(strings.TrimPrefix(request.URL.Path, "/api/forecast/"))
	if err != nil || week <= 0 {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "jornada no valida"})
		return
	}
	universe := s.state.Universe()
	if universe == nil {
		s.json(writer, http.StatusServiceUnavailable, map[string]any{"error": "generando"})
		return
	}
	log, err := forecast.Load()
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	review := forecast.ReviewWeek(log, universe, week, time.Now())
	if review == nil {
		s.json(writer, http.StatusNotFound,
			map[string]any{"error": "no guarde la prevision de esa jornada"})
		return
	}
	s.json(writer, http.StatusOK, review)
}

// addForecasts puts beside each matchday what was expected of him that day, from whichever
// eleven he was in, and adds the matchday in play when it has a forecast and no points yet.
func addForecasts(id string, weeks []map[string]any) []map[string]any {
	log, err := forecast.Load()
	if err != nil || len(log) == 0 {
		return weeks
	}
	expected := map[int]float64{}
	for week, teams := range log {
		for _, team := range teams {
			if pick, ok := team.Players[id]; ok {
				expected[week] = pick.XPts
			}
		}
	}
	seen := map[int]bool{}
	for _, row := range weeks {
		week := int(number(row["week"]))
		seen[week] = true
		if xpts, ok := expected[week]; ok {
			row["forecast"] = xpts
		}
	}
	for week, xpts := range expected {
		if !seen[week] {
			weeks = append(weeks, map[string]any{"week": float64(week), "points": nil,
				"forecast": xpts})
		}
	}
	sort.SliceStable(weeks, func(one, two int) bool {
		return number(weeks[one]["week"]) < number(weeks[two]["week"])
	})
	return weeks
}
