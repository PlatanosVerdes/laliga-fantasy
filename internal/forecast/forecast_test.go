package forecast

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

var saturday = time.Date(2026, 10, 3, 16, 0, 0, 0, time.UTC)

func useTempFile(t *testing.T) {
	t.Helper()
	previous := config.ForecastFile
	config.ForecastFile = filepath.Join(t.TempDir(), "forecasts.json")
	t.Cleanup(func() { config.ForecastFile = previous })
}

func points(value float64) *float64 { return &value }

// Two clubs: 10 plays at 16:00 and 20 at 21:00 on the same Saturday.
func world(xpts map[string]float64, lineup []model.LineupPlayer) *model.Universe {
	mine := "a"
	name := "Ana"
	players := []model.Player{}
	for id, value := range xpts {
		players = append(players, model.Player{ID: id, XPts: value})
	}
	return &model.Universe{
		Week: api.Week{WeekNumber: 7},
		Fixtures: []model.Fixture{
			{LocalID: "10", VisitorID: "11", Kickoff: saturday.Format(time.RFC3339)},
			{LocalID: "20", VisitorID: "21",
				Kickoff: saturday.Add(5 * time.Hour).Format(time.RFC3339)},
		},
		MyTeamID:    &mine,
		LeagueTeams: map[string]*model.LeagueTeam{"a": {TeamID: "a", Manager: &name}},
		Players:     players,
		Lineups:     map[string][]model.LineupPlayer{"a": lineup},
	}
}

// The forecast that counts is the last one before his kick-off: until then it follows the model,
// and from then on nothing the model says can touch it.
func TestTheForecastFreezesAtKickOff(t *testing.T) {
	useTempFile(t)
	lineup := []model.LineupPlayer{{ID: "1", Name: "Uno", TeamID: "10"}}

	if _, err := Record(world(map[string]float64{"1": 5}, lineup), saturday.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := Record(world(map[string]float64{"1": 6}, lineup), saturday.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	log, err := Record(world(map[string]float64{"1": 1}, lineup), saturday.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got := log[7]["a"].Players["1"].XPts; got != 6 {
		t.Errorf("forecast = %v, want 6: el ultimo antes de empezar", got)
	}
	reread, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := reread[7]["a"].Players["1"].XPts; got != 6 {
		t.Errorf("en disco = %v, want 6", got)
	}
	if got := reread[7]["a"].Manager; got != "Ana" {
		t.Errorf("manager = %q, want Ana", got)
	}
}

// Points arrive while the match is being played and keep moving until it is over.
func TestPointsFollowTheFeed(t *testing.T) {
	useTempFile(t)
	lineup := []model.LineupPlayer{{ID: "1", Name: "Uno", TeamID: "10"}}
	Record(world(map[string]float64{"1": 5}, lineup), saturday.Add(-time.Hour))

	lineup[0].Points = points(2)
	Record(world(map[string]float64{"1": 5}, lineup), saturday.Add(time.Hour))
	lineup[0].Points = points(9)
	log, _ := Record(world(map[string]float64{"1": 5}, lineup), saturday.Add(2*time.Hour))
	if got := log[7]["a"].Players["1"].Points; got == nil || *got != 9 {
		t.Errorf("points = %v, want 9", got)
	}
	// A pass where the feed has no row for him does not wipe what it said before.
	lineup[0].Points = nil
	log, _ = Record(world(map[string]float64{"1": 5}, lineup), saturday.Add(3*time.Hour))
	if got := log[7]["a"].Players["1"].Points; got == nil || *got != 9 {
		t.Errorf("points = %v, want que sigan los 9", got)
	}
}

// It runs on every rebuild, so the same world twice must not rewrite the file.
func TestRecordIsIdempotent(t *testing.T) {
	useTempFile(t)
	lineup := []model.LineupPlayer{{ID: "1", Name: "Uno", TeamID: "10", Points: points(3)},
		{ID: "2", Name: "Dos", TeamID: "20"}}
	universe := world(map[string]float64{"1": 5, "2": 4}, lineup)
	now := saturday.Add(-time.Hour)

	Record(universe, now)
	first, err := os.ReadFile(config.ForecastFile)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(config.ForecastFile, stamp, stamp)
	Record(universe, now)
	second, _ := os.ReadFile(config.ForecastFile)
	if string(first) != string(second) {
		t.Error("el mismo mundo dos veces ha cambiado el fichero")
	}
	if info, _ := os.Stat(config.ForecastFile); !info.ModTime().Equal(stamp) {
		t.Error("sin cambios no se escribe el fichero")
	}
}

// Somebody first seen after his kick-off has no forecast from before it, and one taken out of the
// eleven before his match was never part of it.
func TestOnlyWhatWasForecastBeforeKickOffIsKept(t *testing.T) {
	useTempFile(t)
	Record(world(map[string]float64{"1": 5, "2": 4},
		[]model.LineupPlayer{{ID: "2", Name: "Dos", TeamID: "20"}}), saturday.Add(-time.Hour))
	log, _ := Record(world(map[string]float64{"1": 5, "2": 4},
		[]model.LineupPlayer{{ID: "1", Name: "Uno", TeamID: "10"}}), saturday.Add(time.Hour))
	if _, seen := log[7]["a"]; seen {
		t.Errorf("ni el que entro tarde ni el que salio antes de jugar: %+v", log[7]["a"])
	}
}

func near(one, two float64) bool { return math.Abs(one-two) < 1e-9 }

// A matchday over counts every man in the eleven, and one with no row scored nothing.
func TestSummaryOfAFinishedMatchday(t *testing.T) {
	log := Log{
		7: {
			"a": {Manager: "Ana", Players: map[string]*Pick{
				"1": {Name: "Uno", XPts: 5, Points: points(8)},
				"2": {Name: "Dos", XPts: 3, Points: points(1)},
				"3": {Name: "Tres", XPts: 2},
			}},
			"b": {Manager: "Bea", Players: map[string]*Pick{
				"4": {Name: "Cuatro", XPts: 4, Points: points(4)},
			}},
		},
	}
	universe := world(nil, nil)
	universe.Week.WeekNumber = 8
	review := Summarize(log, universe, saturday)
	if review == nil || review.Last == nil {
		t.Fatal("la J7 ya se jugo y tiene previsiones")
	}
	last := review.Last
	if last.Week != 7 || last.Mine == nil {
		t.Fatalf("last = %+v", last)
	}
	if !near(last.Mine.Forecast, 10) || !near(last.Mine.Actual, 9) {
		t.Errorf("mio = previsto %v real %v, want 10 y 9", last.Mine.Forecast, last.Mine.Actual)
	}
	// |8-5| + |1-3| + |0-2| + |4-4| over four players.
	if !near(last.MeanAbsError, 7.0/4) {
		t.Errorf("error medio = %v, want 1.75", last.MeanAbsError)
	}
	if len(last.Players) != 3 || last.Players[0].Name != "Uno" || !near(*last.Players[0].Diff, 3) {
		t.Errorf("mis jugadores = %+v", last.Players)
	}
	if review.Current != nil {
		t.Error("la J8 no tiene nada apuntado")
	}
}

// While the matchday runs only who already has points can be compared, and the last kick-off two
// hours gone is when it counts as over.
func TestSummaryOfTheMatchdayInPlay(t *testing.T) {
	log := Log{7: {"a": {Manager: "Ana", Players: map[string]*Pick{
		"1": {Name: "Uno", XPts: 5, Points: points(8)},
		"2": {Name: "Dos", XPts: 3},
	}}}}
	universe := world(nil, nil)

	review := Summarize(log, universe, saturday.Add(5*time.Hour))
	if review.Last != nil || review.Current == nil {
		t.Fatalf("la J7 todavia se juega: %+v", review)
	}
	if mine := review.Current.Mine; !near(mine.Forecast, 5) || !near(mine.Actual, 8) {
		t.Errorf("en juego = %+v, want solo el que ya puntuo", mine)
	}

	review = Summarize(log, universe, saturday.Add(7*time.Hour))
	if review.Last == nil || !review.Last.Complete {
		t.Fatal("dos horas despues del ultimo partido la jornada esta jugada")
	}
}

func TestNothingRecordedIsNoReview(t *testing.T) {
	if got := Summarize(Log{}, world(nil, nil), saturday); got != nil {
		t.Errorf("sin nada apuntado no hay repaso: %+v", got)
	}
}
