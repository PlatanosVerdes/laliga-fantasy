package outlook

import (
	"math"
	"testing"
	"time"
)

var when = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

// Four clubs, two of them extreme: 1 is the best in the league and 2 the worst, so a fixture
// against either one moves the points as far as the model ever moves them.
var clubs = map[string]any{"1": 0.9, "2": 0.1, "3": 0.5, "4": 0.5}

// world is a league of two managers with a matchday 3 still to come and a matchday 2 already
// played, which is the shape every one of these tests needs.
func world(players ...Row) Row {
	return Row{
		"week":          Row{"weekNumber": 3.0, "nextWeek": 4.0},
		"team_strength": clubs,
		"my_team_id":    "a",
		"schedule": []any{
			fixtureRow(2, "1", "2", when.Add(-7*24*time.Hour)),
			fixtureRow(2, "3", "4", when.Add(-7*24*time.Hour)),
			fixtureRow(3, "1", "2", when.Add(24*time.Hour)),
			fixtureRow(3, "3", "4", when.Add(25*time.Hour)),
		},
		"league_teams": Row{
			"a": Row{"team_id": "a", "manager": "Ana", "position": 1.0, "points": 50.0},
			"b": Row{"team_id": "b", "manager": "Bea", "position": 2.0, "points": 40.0},
		},
		"players": rows(players),
	}
}

func fixtureRow(week int, local, visitor string, kickoff time.Time) Row {
	return Row{"week": float64(week), "local_id": local, "visitor_id": visitor,
		"local": "CLUB" + local, "visitor": "CLUB" + visitor,
		"kickoff": kickoff.Format(time.RFC3339)}
}

func rows(items []Row) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

// player is a fit starter of a club that plays: 85% odds of starting is exactly the model's
// baseline, so his points are his weekly base times his fixture and nothing else.
func player(id string, position int, club, owner string, base float64, extra Row) Row {
	row := Row{"id": id, "name": "j" + id, "position_id": float64(position),
		"team_id": club, "owner_team_id": owner, "base_week": base, "confidence": 1.0,
		"start_probability": 85.0, "status": "ok", "available": true}
	for key, value := range extra {
		row[key] = value
	}
	return row
}

// squad is a legal eleven of one club, plus whatever extras a test adds.
func squad(club, owner string, base float64) []Row {
	positions := []int{1, 2, 2, 2, 2, 2, 3, 3, 3, 4, 4}
	out := make([]Row, 0, len(positions))
	for index, position := range positions {
		out = append(out, player(owner+string(rune('a'+index)), position, club, owner, base, nil))
	}
	return out
}

func find(forecast []Row, manager string) Row {
	for _, row := range forecast {
		if text(row["manager"]) == manager {
			return row
		}
	}
	return nil
}

// The matchday asked about is the next one with a kick-off ahead. Neither number the API
// publishes says that: weekNumber is still 3 for hours after the last ball of matchday 3.
func TestWeekIsTheNextMatchdayStillToBePlayed(t *testing.T) {
	if got := Week(world(), when); got != 3 {
		t.Errorf("Week = %d, want 3", got)
	}
	// Mid-matchday: one game of 3 gone, one still to come. It is still matchday 3.
	live := world()
	live["schedule"] = []any{
		fixtureRow(3, "1", "2", when.Add(-2*time.Hour)),
		fixtureRow(3, "3", "4", when.Add(2*time.Hour)),
	}
	if got := Week(live, when); got != 3 {
		t.Errorf("con la jornada en juego Week = %d, want 3", got)
	}
	// Nothing left ahead: fall back to what the API calls the next one.
	over := world()
	over["schedule"] = []any{fixtureRow(3, "1", "2", when.Add(-2*time.Hour))}
	if got := Week(over, when); got != 4 {
		t.Errorf("Week = %d, want 4 (nextWeek)", got)
	}
	// No calendar at all, and the live fixtures carry no matchday of their own.
	bare := Row{"week": Row{"weekNumber": 3.0, "nextWeek": 4.0},
		"fixtures": []any{fixtureRow(0, "1", "2", when.Add(time.Hour))}}
	if got := Week(bare, when); got != 3 {
		t.Errorf("Week = %d, want 3 from the live fixtures", got)
	}
}

// The API only says injured, and says it for days after a player is back in training.
// futbolfantasy publishes a verdict for the exact matchday, so when it names that matchday it
// wins, which is the whole reason the page scrapes it.
func TestTheVerdictForThisMatchdayBeatsTheStatus(t *testing.T) {
	cases := []struct {
		name   string
		status string
		until  string
		want   float64
	}{
		{"disponible pese al estado", "injured", "Disponible para la jornada 3", 1},
		{"duda para esta jornada", "injured", "Duda para la jornada 3", DoubtOdds},
		{"baja confirmada", "ok", "Baja confirmada para la jornada 3", 0},
		{"vuelve mas adelante", "injured", "Disponible para la jornada 5", 0},
		{"baja larga sin jornada", "injured", "Baja hasta enero", 0},
		{"veredicto de una jornada ya jugada", "injured", "Duda para la jornada 2", 0},
		{"sin veredicto, sano", "ok", "", 1},
		{"sin veredicto, duda del API", "doubtful", "", DoubtOdds},
		{"sancionado", "suspended", "", 0},
	}
	for _, test := range cases {
		row := Row{"status": test.status}
		if test.until != "" {
			row["absence"] = Row{"until": test.until, "kind": "lesionado"}
		}
		got, why := fitness(row, 3)
		if got != test.want {
			t.Errorf("%s: fitness = %v, want %v", test.name, got, test.want)
		}
		if got == 1 && why != "" {
			t.Errorf("%s: un jugador entero no necesita explicacion, dijo %q", test.name, why)
		}
		if got < 1 && why == "" {
			t.Errorf("%s: falta el motivo", test.name)
		}
	}
}

// Out of the league is not a bad week and never clears, so no verdict overrides it. And it must
// not inflate the healthy eleven either, or a squad of dead weight would look like a squad with
// injuries.
func TestOutOfTheLeagueIsNeverExcused(t *testing.T) {
	row := Row{"status": "out_of_league",
		"absence": Row{"until": "Disponible para la jornada 3"}}
	if got, why := fitness(row, 3); got != 0 || why != "fuera de LaLiga" {
		t.Errorf("fitness = %v (%q), want 0 fuera de LaLiga", got, why)
	}
}

// A player who has left LaLiga is owned, unplayable and never coming back. He is a hole in the
// eleven, not an injury: he must not be filed as a baja, and he must not lift the healthy eleven
// of the manager stuck with him, which would read as a bad week for good.
func TestAPlayerOutOfTheLeagueIsAHoleAndNotAnAbsence(t *testing.T) {
	gone := squad("1", "a", 4)
	gone[10] = player("a-gone", 4, "1", "a", 4, Row{"status": Gone, "available": false})
	ana := find(League(world(append(gone, squad("3", "b", 4)...)...), when), "Ana")
	if got := int(number(ana["outs"])); got != 0 {
		t.Errorf("outs = %d, want 0: no es una baja de esta semana", got)
	}
	if got := number(ana["lost"]); got != 0 {
		t.Errorf("lost = %v, want 0: sano o no, no juega nunca", got)
	}
	if got := int(number(ana["forced"])); got != 1 {
		t.Errorf("forced = %d, want 1: tiene que alinearlo igual", got)
	}
	if number(ana["ceiling"]) != number(ana["xpts"]) {
		t.Errorf("ceiling %v y xpts %v tendrian que coincidir",
			number(ana["ceiling"]), number(ana["xpts"]))
	}
}

// Only eleven score, and only in a formation that exists. Nine players field nine, and the two
// slots nobody can fill are two zeros, not an eleven quietly measured as nine.
func TestASquadTooSmallCarriesItsHoles(t *testing.T) {
	nine := squad("1", "a", 4)[:9]
	forecast := League(world(append(nine, squad("3", "b", 4)...)...), when)
	ana := find(forecast, "Ana")
	if got := int(number(ana["slots"])); got != 9 {
		t.Errorf("slots = %d, want 9", got)
	}
	if got := int(number(ana["holes"])); got != 2 {
		t.Errorf("holes = %d, want 2", got)
	}
	if got := number(ana["xpts"]); got >= number(find(forecast, "Bea")["xpts"]) {
		t.Errorf("nueve jugadores puntuan %v, no menos que once", got)
	}
	if reasons := asWords(ana["reasons"]); len(reasons) == 0 ||
		!contains(reasons, "no llega al once") {
		t.Errorf("reasons = %v, want the holes said out loud", reasons)
	}
}

// An absence a bench covers costs nothing, and one it cannot cover costs the player. Both are
// the same injury, so the number that separates them is the point of the section.
func TestWhatAnAbsenceCostsDependsOnTheBench(t *testing.T) {
	bare := squad("1", "a", 4)
	bare[10] = player("a-hurt", 4, "1", "a", 4, Row{"status": "injured", "available": false})
	tight := League(world(append(bare, squad("3", "b", 4)...)...), when)
	ana := find(tight, "Ana")
	if got := int(number(ana["outs"])); got != 1 {
		t.Fatalf("outs = %d, want 1", got)
	}
	if got := number(ana["lost"]); got <= 0.5 {
		t.Errorf("lost = %v, want most of a striker: nobody covers him", got)
	}

	deep := squad("1", "a", 4)
	deep[10] = player("a-hurt", 4, "1", "a", 4, Row{"status": "injured", "available": false})
	deep = append(deep, player("a-cover", 4, "1", "a", 4, nil))
	covered := find(League(world(append(deep, squad("3", "b", 4)...)...), when), "Ana")
	if got := int(number(covered["outs"])); got != 1 {
		t.Fatalf("outs = %d, want 1: sigue lesionado", got)
	}
	if got := number(covered["lost"]); got > 0.001 {
		t.Errorf("lost = %v, want 0: el banquillo lo tapa", got)
	}
	if got := int(number(covered["forced"])); got != 0 {
		t.Errorf("forced = %d, want 0: no tiene que alinearlo", got)
	}
}

// A club with no fixture that matchday scores nothing, which is not the same as facing an easy
// opponent. It is a different sentence, so it gets one.
func TestAClubThatDoesNotPlayScoresNothing(t *testing.T) {
	// Club 9 is in nobody's calendar.
	idle := squad("9", "a", 4)
	forecast := League(world(append(idle, squad("3", "b", 4)...)...), when)
	ana := find(forecast, "Ana")
	if got := number(ana["xpts"]); got != 0 {
		t.Errorf("xpts = %v, want 0: su equipo no juega", got)
	}
	if got := number(ana["ceiling"]); got != 0 {
		t.Errorf("ceiling = %v, want 0: sanos o no, no hay partido", got)
	}
}

// The same squad against the best club in the league and against the worst are not the same
// matchday, which is the fixture half of the forecast doing its job.
func TestTheHarderFixtureRanksWorse(t *testing.T) {
	// Ana's players are at club 2, so they visit club 1, the strongest; Bea's are at club 3
	// and face club 4, an average one.
	forecast := League(world(append(squad("2", "a", 4), squad("3", "b", 4)...)...), when)
	ana, bea := find(forecast, "Ana"), find(forecast, "Bea")
	if number(ana["xpts"]) >= number(bea["xpts"]) {
		t.Errorf("Ana %v vs Bea %v: visitar al mejor club tiene que costar puntos",
			number(ana["xpts"]), number(bea["xpts"]))
	}
	if got := number(ana["fixture_pct"]); got >= 0 {
		t.Errorf("fixture_pct = %v, want negativo", got)
	}
	if got := number(ana["rank"]); got != 1 {
		t.Errorf("rank = %v, want 1: es el que peor pinta", got)
	}
	if !truthy(ana["worst"]) {
		t.Error("el primero de la lista tiene que ir marcado como uno de los peores")
	}
	if names := asWords(ana["hard"]); len(names) != 1 || names[0] != "CLUB1" {
		t.Errorf("hard = %v, want [CLUB1]", names)
	}
	// The away trip is counted, not just the opponent.
	if got := int(number(ana["away"])); got != 11 {
		t.Errorf("away = %d, want 11", got)
	}
}

// A doubt is not a hole and not a full player: he is discounted, and what the discount is worth
// is written down as its own number so the card can say "en el aire".
func TestADoubtIsPointsInTheAirRatherThanOut(t *testing.T) {
	shaky := squad("1", "a", 4)
	shaky[10] = player("a-doubt", 4, "1", "a", 4,
		Row{"absence": Row{"until": "Duda para la jornada 3", "kind": "lesionado"}})
	ana := find(League(world(append(shaky, squad("3", "b", 4)...)...), when), "Ana")
	if got := int(number(ana["doubts"])); got != 1 {
		t.Fatalf("doubts = %d, want 1", got)
	}
	if got := int(number(ana["outs"])); got != 0 {
		t.Errorf("outs = %d, want 0: una duda no es una baja", got)
	}
	air := number(ana["air"])
	if air <= 0 {
		t.Errorf("air = %v, want lo que se juega con la duda", air)
	}
	// What is in the air is exactly the part of him the discount takes away.
	if lost := number(ana["lost"]); math.Abs(lost-air) > 0.001 {
		t.Errorf("lost = %v y air = %v: tendrian que ser lo mismo con una sola duda", lost, air)
	}
}

// A doubt with cover behind him is not what the matchday rests on: the eleven simply does not
// include him, so counting his points as being in the air would invent a risk nobody is taking.
func TestADoubtOnTheBenchIsNotAtStake(t *testing.T) {
	deep := squad("1", "a", 4)
	deep = append(deep, player("a-doubt", 4, "1", "a", 4,
		Row{"absence": Row{"until": "Duda para la jornada 3", "kind": "lesionado"}}))
	ana := find(League(world(append(deep, squad("3", "b", 4)...)...), when), "Ana")
	if got := int(number(ana["doubts"])); got != 0 {
		t.Errorf("doubts = %d, want 0: la duda se queda en el banquillo", got)
	}
	if got := number(ana["air"]); got != 0 {
		t.Errorf("air = %v, want 0", got)
	}
}

func TestOnlyTheWorstThreeGoOnThePodium(t *testing.T) {
	teams := Row{}
	var players []Row
	// Five managers, each a little better than the last.
	for index := 0; index < 5; index++ {
		id := string(rune('a' + index))
		teams[id] = Row{"team_id": id, "manager": "M" + id, "position": float64(index + 1)}
		players = append(players, squad("3", id, 2+float64(index))...)
	}
	universe := world(players...)
	universe["league_teams"] = teams
	forecast := League(universe, when)
	if len(forecast) != 5 {
		t.Fatalf("managers = %d, want 5", len(forecast))
	}
	for index, row := range forecast {
		if want := index < Worst; truthy(row["worst"]) != want {
			t.Errorf("%s: worst = %v, want %v", text(row["manager"]), row["worst"], want)
		}
		if got := int(number(row["rank"])); got != index+1 {
			t.Errorf("%s: rank = %d, want %d", text(row["manager"]), got, index+1)
		}
	}
	if got := text(forecast[0]["manager"]); got != "Ma" {
		t.Errorf("el peor es %q, want Ma", got)
	}
}

func contains(items []string, needle string) bool {
	for _, item := range items {
		if len(item) >= len(needle) && item[:len(needle)] == needle {
			return true
		}
	}
	return false
}

func truthy(value any) bool {
	asBool, ok := value.(bool)
	return ok && asBool
}
