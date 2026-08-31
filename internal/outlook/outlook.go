// Package outlook is how the next matchday looks for every squad in the league.
//
// The tables next door answer who is rich and who owns whom. This one answers a different
// question, and the one a league is actually read for: who is about to have a bad weekend. A
// manager two points ahead of you with three starters injured and Barcelona away is not two
// points ahead of you.
//
// Everything it needs is already in the world: points per matchday, the odds of starting, the
// injury and suspension lists, the fixture list, and a strength for every club. The only thing
// added here is the arithmetic that puts them on one scale, which is the best legal eleven each
// manager can field on that matchday against what the same eleven would be worth with everybody
// fit. The gap between the two is the news.
//
// Two judgements are worth writing down. Formations are the rule, so only eleven players in a
// legal shape score, and a squad that cannot fill one carries the empty slots as zeros rather
// than being quietly excused. And a rival's real lineup is not readable before kick-off, so
// what is measured is the best eleven he *could* field: he might do worse, never better.
package outlook

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/eleven"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

type Row = map[string]any

// DoubtOdds is what a doubt is worth: the same 0.55 the model already applies to a doubtful
// player. A doubt discounted two different ways in two places is a doubt nobody can explain.
const DoubtOdds = model.DoubtfulXPts

// Hard is where an opponent starts being worth naming, on the 0..1 club scale.
const Hard = 0.75

// Worst is how many managers the page puts on the podium.
const Worst = 3

// Gone is the status of a player who is no longer in LaLiga: owned, unplayable, and never
// coming back. Not an injury, and the one status no verdict overrides.
const Gone = "out_of_league"

// lines are the positions in the order a squad reads.
var lines = []int{eleven.Keeper, eleven.Defender, eleven.Midfielder, eleven.Striker}

// seat is one owned player as the next matchday sees him.
type seat struct {
	id       string
	name     string
	position int
	// expected is what he is worth on that matchday, absences discounted; healthy is the same
	// number with every absence cleared. Both already carry his fixture and his odds of
	// starting, so the only difference between them is fitness.
	expected float64
	healthy  float64
	// fit is the odds he plays at all: 1 available, 0 out, in between a doubt.
	fit float64
	// plays is whether his club has a fixture that matchday at all.
	plays    bool
	opponent string
	strength float64
	home     bool
	// why he is not at full, in words, and empty when he is.
	why string
}

type fixture struct {
	week      int
	localID   string
	visitorID string
	local     string
	visitor   string
	kickoff   time.Time
	dated     bool
}

type match struct {
	opponent string
	strength float64
	home     bool
}

// Week is the next matchday still to be decided: the earliest one with a kick-off ahead of now.
//
// Neither of the two numbers the API offers answers this. weekNumber is still called the
// current matchday for hours after the last ball of it was kicked, and nextWeek is the one
// after that. The fixture list is the only thing that knows, so the fixture list decides.
func Week(universe Row, now time.Time) int {
	best := 0
	for _, entry := range calendar(universe) {
		if !entry.dated || !entry.kickoff.After(now) {
			continue
		}
		if best == 0 || entry.week < best {
			best = entry.week
		}
	}
	if best > 0 {
		return best
	}
	week := mapOf(universe["week"])
	if next := int(number(week["nextWeek"])); next > 0 {
		return next
	}
	return int(number(week["weekNumber"]))
}

// calendar is every fixture we know about, from the full schedule when it is there and from this
// matchday's live fixtures when it is not. Those carry no matchday of their own, because they are
// always the current one.
func calendar(universe Row) []fixture {
	current := int(number(mapOf(universe["week"])["weekNumber"]))
	rows := rowsOf(universe["schedule"])
	if len(rows) == 0 {
		rows = rowsOf(universe["fixtures"])
	}
	out := make([]fixture, 0, len(rows))
	for _, row := range rows {
		entry := fixture{
			week:      int(number(row["week"])),
			localID:   text(row["local_id"]),
			visitorID: text(row["visitor_id"]),
			local:     text(row["local"]),
			visitor:   text(row["visitor"]),
		}
		if entry.week == 0 {
			entry.week = current
		}
		if when, err := time.Parse(time.RFC3339, text(row["kickoff"])); err == nil {
			entry.kickoff, entry.dated = when, true
		}
		out = append(out, entry)
	}
	return out
}

// matches is who each club faces on that matchday, and how good they are. A club missing from
// the map does not play, which is not the same as facing an easy opponent.
func matches(universe Row, week int, strength map[string]float64) map[string]match {
	out := map[string]match{}
	for _, entry := range calendar(universe) {
		if entry.week != week {
			continue
		}
		out[entry.localID] = match{opponent: entry.visitor,
			strength: strength[entry.visitorID], home: true}
		out[entry.visitorID] = match{opponent: entry.local,
			strength: strength[entry.localID], home: false}
	}
	return out
}

func strengths(universe Row) map[string]float64 {
	switch typed := universe["team_strength"].(type) {
	case map[string]float64:
		return typed
	case map[string]any:
		out := make(map[string]float64, len(typed))
		for key, value := range typed {
			out[key] = number(value)
		}
		return out
	}
	return map[string]float64{}
}

// absenceWeek reads the matchday out of futbolfantasy's verdict: "Duda para la jornada 3",
// "Baja confirmada para la jornada 3", "Disponible para la jornada 3".
var absenceWeek = regexp.MustCompile(`jornada\s+(\d+)`)

// fitness is the odds a player takes the pitch on that matchday, and why not when he does not.
//
// The API only ever says injured / doubtful / sanctioned, with no notion of *until when*, and
// it lags: a player back in training is still flagged for days. futbolfantasy publishes a
// verdict per matchday (available, a doubt, or confirmed out), so when it names the matchday
// being asked about, it wins. That is the whole reason the scrape exists.
func fitness(player Row, week int) (float64, string) {
	status := strings.ToLower(text(player["status"]))
	// Out of the league is not an injury and never clears: he is dead weight in the squad, not
	// a player having a bad week, so nothing overrides it.
	if status == Gone {
		return 0, "fuera de LaLiga"
	}

	absence := mapOf(player["absence"])
	verdict := strings.ToLower(strings.TrimSpace(text(absence["until"])))
	kind := strings.ToLower(text(absence["kind"]))
	if verdict != "" {
		if found := absenceWeek.FindStringSubmatch(verdict); found != nil {
			named, _ := strconv.Atoi(found[1])
			switch {
			case named > week:
				return 0, fmt.Sprintf("vuelve en la J%d", named)
			case named == week:
				switch {
				case strings.HasPrefix(verdict, "disponible"):
					return 1, ""
				case strings.HasPrefix(verdict, "duda"):
					return DoubtOdds, "duda para esta jornada"
				default:
					return 0, reasonOf(absence, kind)
				}
			}
			// A verdict about a matchday already played says nothing about this one: fall
			// through to the status, which at least is current.
		} else {
			// No matchday named is always one of the long ones: "Baja hasta enero".
			return 0, reasonOf(absence, kind)
		}
	}

	switch status {
	case "injured":
		return 0, reasonOf(absence, "lesionado")
	case "sanctioned", "suspended":
		return 0, reasonOf(absence, "sancionado")
	case "doubtful":
		return DoubtOdds, "duda"
	}
	return 1, ""
}

// reasonOf prefers futbolfantasy's words to a status: "Rotura de ligamento cruzado" is a red
// badge you can act on, "lesionado" is only a red badge.
func reasonOf(absence Row, fallback string) string {
	if reason := text(absence["reason"]); reason != "" {
		return strings.ToLower(reason)
	}
	if fallback == "" {
		return "no disponible"
	}
	return fallback
}

// read is one player measured for that matchday.
//
// The points are rebuilt rather than taken from the row's xPts on purpose: xPts carries whatever
// fixture futbolfantasy called his next one, and every player here has to be measured against
// the *same* matchday. It is the model's own formula and the model's own constants, with only the
// fixture and the fitness swapped.
func read(player Row, week int, fixtures map[string]match) seat {
	entry := seat{
		id:       text(player["id"]),
		name:     text(player["name"]),
		position: int(number(player["position_id"])),
	}
	game, plays := fixtures[text(player["team_id"])]
	entry.plays = plays
	if plays {
		entry.opponent, entry.strength, entry.home = game.opponent, game.strength, game.home
	}

	fit, why := fitness(player, week)
	entry.fit, entry.why = fit, why

	confidence := number(player["confidence"])
	if confidence == 0 {
		confidence = 1
	}
	if plays {
		strength, home := game.strength, game.home
		entry.healthy = number(player["base_week"]) *
			model.Availability(optional(player["start_probability"])) *
			model.FixtureFactor(&strength, &home) * confidence
	} else if entry.why == "" {
		entry.why = "su equipo no juega"
	}
	// A player out of the league is worth nothing even in the eleven where everybody is fit: he
	// is not injured, he is gone. Leaving him his points there would inflate the ceiling of
	// whoever is stuck with him and file permanent dead weight as this week's bad news.
	if strings.ToLower(text(player["status"])) == Gone {
		entry.healthy = 0
	}
	entry.expected = entry.healthy * fit
	return entry
}

// pick is the best legal eleven by one measure, and how many of the eleven slots it fills.
//
// Formations come from the eleven package because they are the rule: the shape that seats the
// most players wins and points break the tie, so a squad that can field eleven never scores
// itself as ten. An absent player is not left out of the pitch the way the lineup helper leaves
// him out, because the game lets you line up an injured man and a manager with no cover does
// exactly that. He is simply worth nearly nothing, which is what the zero in his row says.
func pick(squad []seat, worth func(seat) float64) ([]seat, float64, int) {
	byPosition := map[int][]seat{}
	for _, player := range squad {
		byPosition[player.position] = append(byPosition[player.position], player)
	}
	for _, players := range byPosition {
		sort.SliceStable(players, func(one, two int) bool {
			if worth(players[one]) != worth(players[two]) {
				return worth(players[one]) > worth(players[two])
			}
			return players[one].id < players[two].id
		})
	}

	var best []seat
	bestSlots, bestTotal := -1, 0.0
	for _, shape := range eleven.Shapes {
		chosen := make([]seat, 0, 11)
		total := 0.0
		for _, position := range lines {
			players := byPosition[position]
			for index := 0; index < shape.Need[position] && index < len(players); index++ {
				chosen = append(chosen, players[index])
				total += worth(players[index])
			}
		}
		if len(chosen) > bestSlots || (len(chosen) == bestSlots && total > bestTotal) {
			best, bestSlots, bestTotal = chosen, len(chosen), total
		}
	}
	return best, bestTotal, bestSlots
}

func expectedOf(player seat) float64 { return player.expected }
func healthyOf(player seat) float64  { return player.healthy }

// League is one row per manager, the one who looks worst first.
func League(universe Row, now time.Time) []Row {
	teams := mapOf(universe["league_teams"])
	if len(teams) < 2 {
		return nil
	}
	week := Week(universe, now)
	fixtures := matches(universe, week, strengths(universe))
	if len(fixtures) == 0 {
		// No fixture list for that matchday means every squad would score zero, which is a
		// missing feed and not a forecast.
		return nil
	}
	mine := text(universe["my_team_id"])

	squads := map[string][]seat{}
	for _, player := range rowsOf(universe["players"]) {
		if owner := text(player["owner_team_id"]); owner != "" {
			squads[owner] = append(squads[owner], read(player, week, fixtures))
		}
	}

	out := make([]Row, 0, len(teams))
	for key, value := range teams {
		team := mapOf(value)
		if team == nil {
			continue
		}
		id := text(team["team_id"])
		if id == "" {
			id = key
		}
		if len(squads[id]) == 0 {
			continue
		}
		out = append(out, summarise(team, id, id == mine, squads[id], week))
	}

	sort.SliceStable(out, func(one, two int) bool {
		first, second := number(out[one]["xpts"]), number(out[two]["xpts"])
		if first != second {
			return first < second
		}
		return text(out[one]["team_id"]) < text(out[two]["team_id"])
	})
	for index, row := range out {
		row["rank"] = index + 1
		row["worst"] = index < Worst
	}
	return out
}

// summarise is one manager's matchday: the number that ranks him, the numbers that explain it,
// and the same thing said in words.
func summarise(team Row, teamID string, isMine bool, squad []seat, week int) Row {
	starters, expected, slots := pick(squad, expectedOf)
	_, ceiling, _ := pick(squad, healthyOf)

	away, hard := 0, map[string]float64{}
	strength, factors := 0.0, 0.0
	forced, doubts := []string{}, []string{}
	air := 0.0
	for _, player := range starters {
		if !player.home && player.plays {
			away++
		}
		if player.plays {
			strength += player.strength
			home := player.home
			factors += model.FixtureFactor(&player.strength, &home)
			if player.strength >= Hard {
				hard[player.opponent] = player.strength
			}
		}
		// A slot he has to fill with somebody who will not play: out, or at a club with no
		// fixture that matchday. Either way it is a shirt on an empty slot.
		if player.fit == 0 || !player.plays {
			forced = append(forced, named(player))
		}
		// Doubts are counted on the eleven and not on the squad, because a doubt sitting deep
		// enough on the bench that nobody would field him is not what the matchday rests on.
		if player.fit > 0 && player.fit < 1 {
			doubts = append(doubts, player.name)
			air += player.healthy * (1 - player.fit)
		}
	}
	if len(starters) > 0 {
		strength /= float64(len(starters))
		factors /= float64(len(starters))
	}

	// Absences, unlike doubts, are counted over the whole squad: an absent player is absent from
	// the eleven, so counting him there would count nothing at all. What he *would* have been
	// worth is the number, and `lost` beside it says whether the bench covered him. A manager with
	// three injuries and a deep bench has three injuries and no problem.
	outs := []string{}
	lostToOuts := 0.0
	for _, player := range squad {
		if player.fit == 0 && player.healthy > 0 {
			outs = append(outs, player.name)
			lostToOuts += player.healthy
		}
	}

	manager := text(team["manager"])
	if manager == "" {
		manager = text(team["name"])
	}
	if manager == "" {
		manager = teamID
	}

	row := Row{
		"team_id": teamID, "manager": manager, "is_me": isMine,
		"position": team["position"], "points": team["points"],
		"week": week,
		// xpts is the ranking number: the best legal eleven he could field, this matchday,
		// with his absences and his fixtures in it.
		"xpts": expected, "ceiling": ceiling, "lost": math.Max(0, ceiling-expected),
		"air": air, "out_cost": lostToOuts,
		"squad": len(squad), "slots": slots, "holes": max(0, 11-slots),
		"outs": len(outs), "out_names": outs,
		"doubts": len(doubts), "doubt_names": doubts,
		// forced_names carry the reason in brackets: they are the two or three names where
		// *why* he is not playing is the whole point, and they are never a long list.
		"forced": len(forced), "forced_names": forced,
		"away": away, "opponent_strength": strength,
		// The same fixture as a percentage off neutral, which is the only reading of it that
		// needs no scale explained: minus three per cent is three per cent of his points.
		"fixture_pct": (factors - 1) * 100,
		"hard":        names(hard),
	}
	row["reasons"] = reasons(row)
	return row
}

// names are the opponents worth naming, hardest first.
func names(hard map[string]float64) []string {
	out := make([]string, 0, len(hard))
	for name := range hard {
		out = append(out, name)
	}
	sort.SliceStable(out, func(one, two int) bool {
		if hard[out[one]] != hard[out[two]] {
			return hard[out[one]] > hard[out[two]]
		}
		return out[one] < out[two]
	})
	return out
}

// reasons is the row said out loud, in the order that decides a matchday: who is missing, what
// it costs, who might still fail, what the fixtures do, and whether he can even field eleven.
func reasons(row Row) []string {
	out := []string{}
	if holes := int(number(row["holes"])); holes > 0 {
		out = append(out, fmt.Sprintf("no llega al once: %s en blanco",
			plural(holes, "hueco", "huecos")))
	}
	if outs := int(number(row["outs"])); outs > 0 {
		out = append(out, fmt.Sprintf("%s (%s) · %.1f xPts fuera",
			plural(outs, "baja", "bajas"), join(asWords(row["out_names"]), 3),
			number(row["out_cost"])))
	}
	if forced := int(number(row["forced"])); forced > 0 {
		out = append(out, fmt.Sprintf("tiene que alinear a %s",
			join(asWords(row["forced_names"]), 3)))
	}
	if doubts := int(number(row["doubts"])); doubts > 0 {
		out = append(out, fmt.Sprintf("%s (%s) · %.1f xPts en el aire",
			plural(doubts, "duda", "dudas"), join(asWords(row["doubt_names"]), 3),
			number(row["air"])))
	}
	if hard := asWords(row["hard"]); len(hard) > 0 {
		out = append(out, "rivales duros: "+join(hard, 3))
	}
	if shift := number(row["fixture_pct"]); shift <= -1 {
		out = append(out, fmt.Sprintf("el calendario le resta un %.0f%%", -shift))
	}
	if away := int(number(row["away"])); away >= 7 {
		out = append(out, fmt.Sprintf("%d de sus once juegan fuera", away))
	}
	return out
}

// named is a player and, in brackets, why he will not play.
func named(player seat) string {
	if player.why == "" {
		return player.name
	}
	return player.name + " (" + player.why + ")"
}

func plural(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", count, many)
}

// join names at most `limit` of them and counts the rest, because a card is not a list.
func join(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s y %d mas", strings.Join(items[:limit], ", "), len(items)-limit)
}
