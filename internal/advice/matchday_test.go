package advice

import (
	"testing"
	"time"
)

var kickoffs = time.Date(2026, 8, 31, 18, 0, 0, 0, time.UTC)

// A league of two managers and two matches: one already kicked off, one still to come.
func board(teams []Row, players []Row, fixtures []Row) Row {
	byID := Row{}
	for _, entry := range teams {
		byID[text(entry["team_id"])] = entry
	}
	asAny := make([]any, 0, len(players))
	for _, player := range players {
		asAny = append(asAny, player)
	}
	asFixtures := make([]any, 0, len(fixtures))
	for _, fixture := range fixtures {
		asFixtures = append(asFixtures, fixture)
	}
	return Row{
		"week":         Row{"weekNumber": 3.0, "closingWeekDate": "2026-09-01T03:00:00+02:00"},
		"my_team_id":   "a",
		"league_teams": byID,
		"players":      asAny,
		"fixtures":     asFixtures,
	}
}

func match(local, visitor string, kickoff time.Time, state int) Row {
	return Row{"local_id": local, "visitor_id": visitor,
		"local": "C" + local, "visitor": "C" + visitor,
		"kickoff": kickoff.Format(time.RFC3339), "state": float64(state)}
}

func manager(id, name string, live any) Row {
	return Row{"team_id": id, "manager": name, "live_points": live, "points": 100.0}
}

func owned(id, club, owner string, xpts float64, available bool) Row {
	return Row{"id": id, "name": "j" + id, "team_id": club, "owner_team_id": owner,
		"xpts": xpts, "available": available, "position_id": 3.0}
}

func find(managers []Row, name string) Row {
	for _, row := range managers {
		if text(row["manager"]) == name {
			return row
		}
	}
	return nil
}

// The calendar is cached for hours, so its match states go stale: a game that kicked off an hour
// ago can still be reported as pending. The kick-off time is the one thing that cannot go stale,
// so it is the only thing this reads.
func TestPendingIsJudgedByKickOffAndNotByTheState(t *testing.T) {
	now := kickoffs
	universe := board(
		[]Row{manager("a", "Ana", 30.0)},
		[]Row{
			owned("1", "10", "a", 5, true), // played an hour ago, state still says pending
			owned("2", "20", "a", 7, true), // kicks off in an hour
		},
		[]Row{
			match("10", "11", now.Add(-time.Hour), 1),
			match("20", "21", now.Add(time.Hour), 1),
		})

	result := Matchday(universe, now)
	if got := int(number(result["played"])); got != 1 {
		t.Errorf("played = %d, want 1: uno ya empezo aunque el estado diga que no", got)
	}
	ana := find(rowsOf(result["managers"]), "Ana")
	if got := int(number(ana["waiting"])); got != 1 {
		t.Fatalf("waiting = %d, want 1", got)
	}
	if got := number(ana["to_come"]); got != 7 {
		t.Errorf("to_come = %v, want 7: solo el que no ha empezado", got)
	}
	if got := number(ana["projection"]); got != 37 {
		t.Errorf("projection = %v, want 37", got)
	}
}

// The reason the section exists. The league's live table is the number everybody quotes at each
// other, and halfway through a matchday it says the wrong thing: the manager behind on points
// with men still to play is not behind.
func TestTheLiveOrderAndTheEndingOrderCanDisagree(t *testing.T) {
	now := kickoffs
	universe := board(
		[]Row{manager("a", "Ana", 48.0), manager("b", "Bea", 39.0)},
		[]Row{
			owned("1", "10", "a", 1.3, true),
			owned("2", "20", "b", 8.0, true),
			owned("3", "20", "b", 7.1, true),
		},
		[]Row{
			match("10", "11", now.Add(time.Hour), 1),
			match("20", "21", now.Add(2*time.Hour), 1),
		})

	managers := rowsOf(Matchday(universe, now)["managers"])
	// Sorted the way the league sorts it: by what is on the board.
	if got := text(managers[0]["manager"]); got != "Ana" {
		t.Fatalf("primera fila %q, want Ana: la tabla va por puntos", got)
	}
	ana, bea := find(managers, "Ana"), find(managers, "Bea")
	if int(number(ana["points_rank"])) != 1 || int(number(bea["points_rank"])) != 2 {
		t.Errorf("puestos por puntos = %v y %v, want 1 y 2",
			ana["points_rank"], bea["points_rank"])
	}
	if int(number(ana["projection_rank"])) != 2 || int(number(bea["projection_rank"])) != 1 {
		t.Errorf("puestos por proyeccion = %v y %v, want 2 y 1: Bea tiene mas por jugar",
			ana["projection_rank"], bea["projection_rank"])
	}
}

// A man who is injured or suspended has a match ahead of him and is not going to play in it, so
// counting him as pending would promise points nobody is going to score.
func TestAnAbsentPlayerIsNotWaitingToPlay(t *testing.T) {
	now := kickoffs
	universe := board(
		[]Row{manager("a", "Ana", 20.0)},
		[]Row{
			owned("1", "20", "a", 6, true),
			owned("2", "20", "a", 0, false),
		},
		[]Row{match("20", "21", now.Add(time.Hour), 1)})

	ana := find(rowsOf(Matchday(universe, now)["managers"]), "Ana")
	if got := int(number(ana["waiting"])); got != 1 {
		t.Errorf("waiting = %d, want 1: el lesionado no cuenta", got)
	}
	if names := ana["waiting_names"].([]string); len(names) != 1 || names[0] != "j1" {
		t.Errorf("waiting_names = %v, want solo j1", names)
	}
}

// Nobody left to play is the most decisive thing a row can say, and it is not the same as having
// scored nothing: his afternoon is over whatever the rest of the league still has to play.
func TestAManagerWithNobodyLeftIsDone(t *testing.T) {
	now := kickoffs
	universe := board(
		[]Row{manager("a", "Ana", 30.0), manager("b", "Bea", 10.0)},
		[]Row{
			owned("1", "10", "a", 5, true),
			owned("2", "20", "b", 5, true),
		},
		[]Row{
			match("10", "11", now.Add(-time.Hour), 7),
			match("20", "21", now.Add(time.Hour), 1),
		})

	managers := rowsOf(Matchday(universe, now)["managers"])
	ana, bea := find(managers, "Ana"), find(managers, "Bea")
	if !truthy(ana["done"]) {
		t.Error("Ana no tiene a nadie por jugar: su jornada esta cerrada")
	}
	if truthy(bea["done"]) {
		t.Error("Bea todavia tiene un partido")
	}
	if got := number(ana["projection"]); got != number(ana["points"]) {
		t.Errorf("projection = %v, want los puntos que ya tiene", got)
	}
}

// The standings carry no figure at all for a manager with nothing on the pitch, and a nought
// there would read as a bad matchday rather than as no matchday.
func TestNoFigureIsNotZeroPoints(t *testing.T) {
	now := kickoffs
	universe := board(
		[]Row{manager("a", "Ana", nil)},
		[]Row{owned("1", "20", "a", 4, true)},
		[]Row{match("20", "21", now.Add(time.Hour), 1)})

	ana := find(rowsOf(Matchday(universe, now)["managers"]), "Ana")
	if truthy(ana["reported"]) {
		t.Error("reported tendria que ser falso: el juego no da cifra")
	}
	if got := number(ana["points"]); got != 0 {
		t.Errorf("points = %v, want 0 para poder sumar", got)
	}
}

// Only eleven score, so however deep the squad the ceiling stops at eleven, and it takes the best
// eleven rather than the first eleven the feed happened to list.
func TestTheCeilingStopsAtEleven(t *testing.T) {
	now := kickoffs
	players := make([]Row, 0, 15)
	for index := 0; index < 15; index++ {
		players = append(players, owned(string(rune('a'+index)), "20", "a", float64(index+1), true))
	}
	universe := board([]Row{manager("a", "Ana", 0.0)}, players,
		[]Row{match("20", "21", now.Add(time.Hour), 1)})

	ana := find(rowsOf(Matchday(universe, now)["managers"]), "Ana")
	if got := int(number(ana["waiting"])); got != 11 {
		t.Fatalf("waiting = %d, want 11", got)
	}
	// The best eleven of 1..15 is 5..15, which is 110.
	if got := number(ana["to_come"]); got != 110 {
		t.Errorf("to_come = %v, want 110: los once mejores", got)
	}
}

// No calendar for the matchday is a missing feed, not a matchday where nobody plays.
func TestNoFixturesIsNoBoard(t *testing.T) {
	universe := board([]Row{manager("a", "Ana", 10.0)},
		[]Row{owned("1", "20", "a", 4, true)}, nil)
	if got := Matchday(universe, kickoffs); got != nil {
		t.Errorf("Matchday = %v, want nada sin calendario", got)
	}
}
