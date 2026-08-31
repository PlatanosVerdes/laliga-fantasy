package advice

import (
	"sort"
	"time"
)

// Matchday is how the matchday in play is going, for the whole league.
//
// The thing a scoreboard cannot say on its own: forty-eight points with everybody played and
// thirty-nine with six men still to kick off are not the same afternoon, and a table of the
// moment claims they are. In a real matchday the league's own order and the order it will finish
// in were completely different, so every row carries what is still to come beside what is
// already scored.
//
// The points are the game's own livePoints rather than anything reconstructed here. Checked
// against the per-manager totals the week-scoped lineup route serves: all thirteen agree to the
// point.
//
// What is still to come is the honest weak spot, and it is a ceiling rather than a forecast: a
// rival's saved eleven is not readable without a request per manager, so the players counted are
// every one of his whose match has not kicked off, and only eleven of them can score. Injured and
// suspended men are left out, because they are not coming either.
func Matchday(universe Row, now time.Time) Row {
	week := mapOf(universe["week"])
	current := int(number(week["weekNumber"]))
	fixtures := weekFixtures(universe, current)
	if len(fixtures) == 0 {
		return nil
	}

	// Judged by kick-off, never by the match state: the calendar is cached for hours, so its
	// states go stale, and a kick-off time does not move.
	pendingClub := map[string]bool{}
	pending := []Row{}
	played := 0
	for _, fixture := range fixtures {
		when, err := time.Parse(time.RFC3339, text(fixture["kickoff"]))
		if err != nil || !when.After(now) {
			played++
			continue
		}
		pendingClub[text(fixture["local_id"])] = true
		pendingClub[text(fixture["visitor_id"])] = true
		pending = append(pending, fixture)
	}
	sort.SliceStable(pending, func(one, two int) bool {
		return text(pending[one]["kickoff"]) < text(pending[two]["kickoff"])
	})

	teams := mapOf(universe["league_teams"])
	if len(teams) == 0 {
		return nil
	}
	mine := text(universe["my_team_id"])

	waiting := map[string][]Row{}
	for _, player := range rowsOf(universe["players"]) {
		owner := text(player["owner_team_id"])
		if owner == "" || !pendingClub[text(player["team_id"])] {
			continue
		}
		// A man who is injured or suspended has a match ahead of him and is not going to play in
		// it, so counting him as pending would promise points nobody is going to score.
		if !truthy(player["available"]) {
			continue
		}
		waiting[owner] = append(waiting[owner], player)
	}

	managers := make([]Row, 0, len(teams))
	for key, value := range teams {
		team := mapOf(value)
		if team == nil {
			continue
		}
		teamID := text(team["team_id"])
		if teamID == "" {
			teamID = key
		}
		managers = append(managers, standing(team, teamID, teamID == mine, waiting[teamID]))
	}

	rank(managers, "points")
	rank(managers, "projection")
	sort.SliceStable(managers, func(one, two int) bool {
		first, second := number(managers[one]["points"]), number(managers[two]["points"])
		if first != second {
			return first > second
		}
		return text(managers[one]["team_id"]) < text(managers[two]["team_id"])
	})

	return Row{
		"week": current, "managers": managers,
		"matches": len(fixtures), "played": played,
		"pending_matches": pending,
		// Live while a single match of it is still to come: that is what makes the difference
		// between a standing and a result.
		"live":   len(pending) > 0,
		"closes": week["closingWeekDate"],
	}
}

// standing is one manager's afternoon: what he has scored, who he has left, and where the two
// together would leave him.
func standing(team Row, teamID string, isMine bool, waiting []Row) Row {
	sort.SliceStable(waiting, func(one, two int) bool {
		first, second := number(waiting[one]["xpts"]), number(waiting[two]["xpts"])
		if first != second {
			return first > second
		}
		return text(waiting[one]["id"]) < text(waiting[two]["id"])
	})
	// Only eleven score, so eleven is as far as the ceiling can reach however deep the squad.
	if len(waiting) > 11 {
		waiting = waiting[:11]
	}

	toCome := 0.0
	names := make([]string, 0, len(waiting))
	for _, player := range waiting {
		toCome += number(player["xpts"])
		names = append(names, text(player["name"]))
	}

	manager := text(team["manager"])
	if manager == "" {
		manager = text(team["name"])
	}
	if manager == "" {
		manager = teamID
	}

	// livePoints absent is not zero points: the standings simply do not carry a figure for a
	// manager who has nothing on the pitch, and printing a nought would read as a bad matchday
	// rather than as no matchday at all.
	points, reported := 0.0, false
	if live := team["live_points"]; live != nil {
		points, reported = number(live), true
	}

	return Row{
		"team_id": teamID, "manager": manager, "is_me": isMine,
		"position": team["position"], "season_points": team["points"],
		"points": points, "reported": reported,
		"waiting": len(waiting), "to_come": toCome, "waiting_names": names,
		"projection": points + toCome,
		// Nobody left to play: his matchday is over whatever the rest of the league still does.
		"done": len(waiting) == 0,
	}
}

// rank writes the position each manager holds by one measure, so the table can show that the
// order it is sorted in is not the order it will end in.
func rank(managers []Row, key string) {
	order := append([]Row(nil), managers...)
	sort.SliceStable(order, func(one, two int) bool {
		first, second := number(order[one][key]), number(order[two][key])
		if first != second {
			return first > second
		}
		return text(order[one]["team_id"]) < text(order[two]["team_id"])
	})
	for index, row := range order {
		row[key+"_rank"] = index + 1
	}
}

// weekFixtures is that matchday's matches. The live fixture list is this matchday's by
// construction and carries no number of its own, so the tagged schedule is only the fallback.
func weekFixtures(universe Row, week int) []Row {
	if rows := rowsOf(universe["fixtures"]); len(rows) > 0 {
		return rows
	}
	out := []Row{}
	for _, fixture := range rowsOf(universe["schedule"]) {
		if int(number(fixture["week"])) == week {
			out = append(out, fixture)
		}
	}
	return out
}
