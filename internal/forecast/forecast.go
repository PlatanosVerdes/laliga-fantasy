// Package forecast writes down what was expected of every saved eleven before the ball rolled,
// and what it then scored, so the xPts can be held to account.
//
// The forecast has to be kept from before kick-off: after it the model goes on moving with
// whatever the feed says, and asking it later what it expected answers a different question.
package forecast

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

// Pick is one player of one saved eleven on one matchday.
type Pick struct {
	Name    string  `json:"name"`
	TeamID  string  `json:"team_id"`
	XPts    float64 `json:"xpts"`
	Kickoff string  `json:"kickoff"`
	// Nil until the feed carries his row for that matchday.
	Points *float64 `json:"points"`
}

// Team is one manager's eleven on one matchday, keyed by player id.
type Team struct {
	Manager string           `json:"manager"`
	Players map[string]*Pick `json:"players"`
}

// Log is matchday number to team id to that team's eleven.
type Log map[int]map[string]*Team

// Load reads the log. A missing file is an empty log; a broken one is an error, so it is never
// overwritten by a fresh one.
func Load() (Log, error) {
	body, err := os.ReadFile(config.ForecastFile)
	if errors.Is(err, fs.ErrNotExist) {
		return Log{}, nil
	}
	if err != nil {
		return nil, err
	}
	log := Log{}
	if err := json.Unmarshal(body, &log); err != nil {
		return nil, err
	}
	return log, nil
}

func save(log Log) error {
	if err := os.MkdirAll(filepath.Dir(config.ForecastFile), 0o700); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(config.ForecastFile, blob, 0o600)
}

// Record brings the log up to date with the universe and writes it only when something changed.
// It returns the log as it now stands, even when writing it failed.
func Record(universe *model.Universe, now time.Time) (Log, error) {
	log, err := Load()
	if err != nil {
		return nil, err
	}
	before, _ := json.Marshal(log)
	Update(log, universe, now)
	after, err := json.Marshal(log)
	if err != nil || string(before) == string(after) {
		return log, err
	}
	return log, save(log)
}

// Update writes the current matchday's saved elevens into the log. A player's forecast is
// rewritten on every pass until his match kicks off and frozen from then on; his points are
// taken whenever the feed has them.
func Update(log Log, universe *model.Universe, now time.Time) {
	week := universe.Week.WeekNumber
	if week <= 0 || len(universe.Lineups) == 0 {
		return
	}
	kickoffs := kickoffsOf(universe, week)
	xpts := make(map[string]float64, len(universe.Players))
	for _, player := range universe.Players {
		xpts[player.ID] = player.XPts
	}
	if log[week] == nil {
		log[week] = map[string]*Team{}
	}

	for teamID, lineup := range universe.Lineups {
		team := log[week][teamID]
		if team == nil {
			team = &Team{Players: map[string]*Pick{}}
		}
		if name := managerOf(universe, teamID); name != "" {
			team.Manager = name
		}

		inLineup := map[string]bool{}
		for _, shirt := range lineup {
			inLineup[shirt.ID] = true
			kickoff := kickoffs[shirt.TeamID]
			pick := team.Players[shirt.ID]
			if pending(kickoff, now) {
				if pick == nil {
					pick = &Pick{}
					team.Players[shirt.ID] = pick
				}
				pick.Name, pick.TeamID, pick.Kickoff = shirt.Name, shirt.TeamID, kickoff
				pick.XPts = xpts[shirt.ID]
			}
			// First seen after kick-off: whatever the model says now is not what it expected.
			if pick == nil {
				continue
			}
			if shirt.Points != nil {
				points := *shirt.Points
				pick.Points = &points
			}
		}
		// Taken out of the eleven before his match: he was never part of the forecast.
		for id, pick := range team.Players {
			if !inLineup[id] && pending(pick.Kickoff, now) {
				delete(team.Players, id)
			}
		}
		if len(team.Players) > 0 {
			log[week][teamID] = team
		} else {
			delete(log[week], teamID)
		}
	}
	if len(log[week]) == 0 {
		delete(log, week)
	}
}

func pending(kickoff string, now time.Time) bool {
	when, err := time.Parse(time.RFC3339, kickoff)
	return err == nil && when.After(now)
}

// kickoffsOf is when each club plays on that matchday. The live fixture list is this matchday's
// by construction, so the tagged schedule is only the fallback.
func kickoffsOf(universe *model.Universe, week int) map[string]string {
	fixtures := universe.Fixtures
	if len(fixtures) == 0 {
		for _, fixture := range universe.Schedule {
			if fixture.Week == week {
				fixtures = append(fixtures, fixture)
			}
		}
	}
	out := map[string]string{}
	for _, fixture := range fixtures {
		out[fixture.LocalID] = fixture.Kickoff
		out[fixture.VisitorID] = fixture.Kickoff
	}
	return out
}

func managerOf(universe *model.Universe, teamID string) string {
	team := universe.LeagueTeams[teamID]
	if team == nil {
		return ""
	}
	if team.Manager != nil && *team.Manager != "" {
		return *team.Manager
	}
	if team.Name != nil {
		return *team.Name
	}
	return ""
}

// Line is one of my players: what was expected of him and what he made.
type Line struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Forecast float64  `json:"forecast"`
	Points   *float64 `json:"points"`
	Diff     *float64 `json:"diff"`
}

// Total is one manager's eleven on a matchday, forecast against result.
type Total struct {
	TeamID   string  `json:"team_id"`
	Manager  string  `json:"manager"`
	IsMe     bool    `json:"is_me"`
	Forecast float64 `json:"forecast"`
	Actual   float64 `json:"actual"`
	Counted  int     `json:"counted"`
}

// Week is how the forecast of one matchday went.
type Week struct {
	Week     int     `json:"week"`
	Complete bool    `json:"complete"`
	Managers []Total `json:"managers"`
	Mine     *Total  `json:"mine"`
	Players  []Line  `json:"players"`
	// The average distance between forecast and result per player, across the whole league.
	MeanAbsError float64 `json:"mean_abs_error"`
	Counted      int     `json:"counted"`
}

// Review is the last matchday that is over and, while it runs, the current one.
type Review struct {
	Last    *Week `json:"last"`
	Current *Week `json:"current"`
}

// Settled is how long after the last kick-off a matchday counts as played.
const Settled = 2 * time.Hour

// Summarize reviews the log against the universe's calendar. Nil when nothing was recorded.
func Summarize(log Log, universe *model.Universe, now time.Time) *Review {
	if len(log) == 0 {
		return nil
	}
	mine := ""
	if universe.MyTeamID != nil {
		mine = *universe.MyTeamID
	}
	weeks := make([]int, 0, len(log))
	for week := range log {
		weeks = append(weeks, week)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(weeks)))

	review := &Review{}
	for _, week := range weeks {
		complete := over(universe, week, now)
		if !complete {
			if week == universe.Week.WeekNumber && review.Current == nil {
				review.Current = summarizeWeek(week, log[week], mine, false)
			}
			continue
		}
		review.Last = summarizeWeek(week, log[week], mine, true)
		break
	}
	if review.Last == nil && review.Current == nil {
		return nil
	}
	return review
}

// over is whether every match of that matchday has been played.
func over(universe *model.Universe, week int, now time.Time) bool {
	if week < universe.Week.WeekNumber {
		return true
	}
	if week > universe.Week.WeekNumber {
		return false
	}
	latest := time.Time{}
	for _, kickoff := range kickoffsOf(universe, week) {
		when, err := time.Parse(time.RFC3339, kickoff)
		if err != nil {
			return false
		}
		if when.After(latest) {
			latest = when
		}
	}
	return !latest.IsZero() && !now.Before(latest.Add(Settled))
}

func summarizeWeek(week int, teams map[string]*Team, mine string, complete bool) *Week {
	out := &Week{Week: week, Complete: complete, Managers: []Total{}, Players: []Line{}}
	errorSum := 0.0
	ids := make([]string, 0, len(teams))
	for teamID := range teams {
		ids = append(ids, teamID)
	}
	sort.Strings(ids)

	for _, teamID := range ids {
		team := teams[teamID]
		total := Total{TeamID: teamID, Manager: team.Manager, IsMe: teamID == mine}
		for id, pick := range team.Players {
			// A played matchday with no row for him is a matchday he scored nothing in; while it
			// runs, only who already has points can be compared.
			points, known := 0.0, pick.Points != nil
			if known {
				points = *pick.Points
			}
			if known || complete {
				total.Forecast += pick.XPts
				total.Actual += points
				total.Counted++
				errorSum += math.Abs(points - pick.XPts)
				out.Counted++
			}
			if total.IsMe {
				line := Line{ID: id, Name: pick.Name, Forecast: pick.XPts, Points: pick.Points}
				if known || complete {
					scored := points
					diff := scored - pick.XPts
					line.Points, line.Diff = &scored, &diff
				}
				out.Players = append(out.Players, line)
			}
		}
		out.Managers = append(out.Managers, total)
		if total.IsMe {
			mineTotal := total
			out.Mine = &mineTotal
		}
	}
	if out.Counted > 0 {
		out.MeanAbsError = errorSum / float64(out.Counted)
	}
	sort.SliceStable(out.Players, func(one, two int) bool {
		if out.Players[one].Forecast != out.Players[two].Forecast {
			return out.Players[one].Forecast > out.Players[two].Forecast
		}
		return out.Players[one].ID < out.Players[two].ID
	})
	return out
}
