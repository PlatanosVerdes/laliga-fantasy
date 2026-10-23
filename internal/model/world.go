package model

import (
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

// Compose never writes to Global or League: one value serves every tenant.
type Global struct {
	Week           api.Week
	NextWeekOpens  string
	Fixtures       []Fixture
	Schedule       []Fixture
	CompletedWeeks int
	CurrentWeight  float64
	TeamStrength   map[string]float64
	Bridge         *Bridge

	raw []map[string]any
	// Scored with the bulk status and no ficha; only the owner's view differs.
	rows    []Player
	entries []map[string]any
}

type League struct {
	ownership map[string]slot
	Teams     map[string]*LeagueTeam
	Unread    int
	Activity  []Event
	Lineups   map[string][]LineupPlayer
}

func BuildGlobal(global *api.Global, bridge *Bridge) (*Global, error) {
	week, err := global.CurrentWeek(30 * time.Minute)
	if err != nil {
		return nil, fmt.Errorf("week: %w", err)
	}
	teams, err := global.TeamsMaster(24 * time.Hour)
	if err != nil {
		return nil, fmt.Errorf("teams: %w", err)
	}
	raw, err := allPlayers(global)
	if err != nil {
		return nil, fmt.Errorf("players: %w", err)
	}
	reached("Jugadores de LaLiga", 1, 1)

	teamName := map[string]string{}
	teamShort := map[string]string{}
	for _, team := range teams {
		teamName[team.ID] = team.Name
		teamShort[team.ID] = team.ShortName
	}

	completedWeeks := max(0, week.WeekNumber-1)
	// The season's own points only start to outweigh last season's after a couple of
	// months; before that they are a handful of matches and mostly noise.
	currentWeight := math.Min(1.0, float64(completedWeeks)/8.0)

	// The first kick-off of next week: the widget says when the market opens again, and
	// without it the page loses that line entirely.
	nextOpens := ""
	if week.NextWeek != 0 {
		if fixtures, err := global.Calendar(week.NextWeek, 6*time.Hour); err == nil {
			var dates []string
			for _, match := range fixtures {
				if when := fallback(match.Date, match.MatchDate); when != "" {
					dates = append(dates, when)
				}
			}
			sort.Strings(dates)
			if len(dates) > 0 {
				nextOpens = dates[0]
			}
		}
	}

	world := &Global{
		Week:           week,
		NextWeekOpens:  nextOpens,
		Fixtures:       LoadFixtures(global, week, teams),
		Schedule:       LoadSchedule(global, week, teams),
		CompletedWeeks: completedWeeks,
		CurrentWeight:  currentWeight,
		Bridge:         bridge,
		raw:            raw,
	}

	// Both need every player, so they are computed before the row loop.
	rawPlayers := make([]Player, 0, len(raw))
	for _, entry := range raw {
		positionID := int(number(entry["positionId"]))
		rawPlayers = append(rawPlayers, Player{
			PositionID: positionID, TeamID: text(entry["teamId"]),
			Value: number(entry["marketValue"]), LastSeason: number(entry["lastSeasonPoints"]),
		})
	}
	strength := TeamStrength(rawPlayers)
	curves := PricePrior(rawPlayers)
	world.TeamStrength = strength

	// futbolfantasy names its teams differently, so the rival is resolved by normalized
	// name. Key order and first-wins are matching.build_team_index's, not a detail: the
	// feed carries 42 teams including historic ones, so a collision resolved the other
	// way picks a different club and quietly changes that player's fixture factor.
	teamByName := map[string]string{}
	for _, team := range teams {
		for _, candidate := range []string{team.Name, team.Slug, team.ShortName} {
			key := normalizeTeam(candidate)
			if key == "" {
				continue
			}
			if _, taken := teamByName[key]; !taken {
				teamByName[key] = team.ID
			}
		}
	}

	for _, entry := range raw {
		id := text(entry["id"])
		if id == "" {
			continue
		}
		positionID := int(number(entry["positionId"]))
		if positionID == PositionCoach {
			continue
		}
		// The player master carries a flat teamId, not a nested team object.
		teamID := text(entry["teamId"])
		player := Player{
			ID:           id,
			Name:         label(entry),
			Position:     fallback(positions[positionID], "?"),
			PositionID:   positionID,
			TeamID:       teamID,
			Team:         teamName[teamID],
			TeamShort:    teamShort[teamID],
			Value:        number(entry["marketValue"]),
			SeasonPoints: number(entry["points"]),
			SeasonAvg:    number(entry["averagePoints"]),
			LastSeason:   number(entry["lastSeasonPoints"]),
			Image:        faceFor(entry, "", ""),
		}
		if full := text(entry["name"]); full != "" {
			player.FullName = &full
		}

		// --- the scoring half ---------------------------------------------------
		trend := bridge.trendOf(id)
		if bridge != nil {
			if absence, ok := bridge.Absences[id]; ok {
				player.Absence = absence
			}
			player.Role = bridge.Roles[id]
		}

		player.PriorBased = player.LastSeason <= 0
		perWeekLast := player.LastSeason / WeeksInSeason
		if player.PriorBased {
			perWeekLast = PriorFor(curves, positionID, player.Value)
		}
		perWeekNow := 0.0
		if completedWeeks > 0 {
			perWeekNow = player.SeasonPoints / float64(completedWeeks)
		}
		player.BaseWeek = currentWeight*perWeekNow + (1-currentWeight)*perWeekLast

		var rivalStrength *float64
		if trend != nil && trend.NextRival != nil {
			if rivalID, ok := teamByName[normalizeTeam(*trend.NextRival)]; ok {
				player.NextRivalID = rivalID
				if value, ok := strength[rivalID]; ok {
					rivalStrength = &value
				}
			}
		}
		var home *bool
		if trend != nil {
			home = trend.NextHome
			player.NextWeek, player.NextRival, player.NextHome = trend.NextWeek, trend.NextRival, trend.NextHome
			player.TrendLabel, player.StreakDays, player.StreakDir = trend.TrendLabel, trend.StreakDays, trend.StreakDir
			player.Acceleration = trend.Acceleration
			player.Pct1d, player.Pct7d, player.Pct30d = trend.Pct1d, trend.Pct7d, trend.Pct30d
			player.FFValue = trend.Value
			if trend.FFID != "" {
				id := trend.FFID
				player.FFID = &id
			}
			if trend.FFName != "" {
				name := trend.FFName
				player.FFName = &name
			}
		}
		player.FixtureFactor = FixtureFactor(rivalStrength, home)
		player.ProjectedPct = ProjectedPct(trend)
		player.ProjectedGain = player.Value * player.ProjectedPct / 100.0

		player.rate(trend, nil, bulkStatus(entry))
		world.rows = append(world.rows, player)
		world.entries = append(world.entries, entry)
	}
	return world, nil
}

func BuildLeague(global *api.Global, world *Global, league *api.League) (*League, error) {
	ownership, teams, unread, err := loadOwnership(league)
	if err != nil {
		return nil, fmt.Errorf("ownership: %w", err)
	}
	managers := map[string]string{}
	for _, team := range teams {
		if team.UserID == "" {
			continue
		}
		if team.Manager != nil && *team.Manager != "" {
			managers[team.UserID] = *team.Manager
		} else if team.Name != nil {
			managers[team.UserID] = *team.Name
		}
	}
	names := make(map[string]string, len(world.rows))
	for _, player := range world.rows {
		names[player.ID] = player.Name
	}
	activity := loadActivity(league, managers, names)
	EnrichActivityValues(global.At(httpx.Enrich), activity, 18)
	return &League{ownership: ownership, Teams: teams, Unread: unread, Activity: activity,
		Lineups: loadLineups(league, teams, world.Week.WeekNumber)}, nil
}

// league is nil when the member has no league yet.
func Compose(world *Global, league *League, member *api.Member, state State) (*Universe, error) {
	global, myTeamID := member.Global(), member.TeamID()
	universe := &Universe{
		Week:           world.Week,
		NextWeekOpens:  world.NextWeekOpens,
		Fixtures:       world.Fixtures,
		Schedule:       world.Schedule,
		CompletedWeeks: world.CompletedWeeks,
		CurrentWeight:  world.CurrentWeight,
		TeamStrength:   world.TeamStrength,
	}
	if world.Bridge != nil {
		universe.MatchedCount = world.Bridge.MatchedCount
	}
	if myTeamID != "" {
		universe.MyTeamID = &myTeamID
	}

	ownership := map[string]slot{}
	// Faces come from wherever they appear — squads and the market — because the public
	// player list carries none.
	faces := map[string]string{}
	var status map[string]string
	if league != nil {
		ownership = league.ownership
		universe.LeagueTeams = cloneTeams(league.Teams)
		universe.UnreadSquads = league.Unread
		universe.OwnershipLoaded = len(ownership) > 0
		var err error
		universe.Market, err = loadMarket(member, faces)
		if err != nil {
			return nil, fmt.Errorf("market: %w", err)
		}
		reached("Mercado", 1, 1)
		status = freshStatus(global, world.raw, ownership, myTeamID)
	}
	fichas := squadOdds(world.raw, ownership, myTeamID, world.Bridge, futbolfantasy.DetailTTL)

	universe.Players = slices.Clone(world.rows)
	for index := range universe.Players {
		player := &universe.Players[index]
		id, entry := player.ID, world.entries[index]
		player.Starred = state.Starred[id]
		player.RaidScheduled = state.Raids[id]
		player.Image = faceFor(entry, faces[id], ownership[id].Image)

		owned, ok := ownership[id]
		if !ok {
			continue
		}
		player.Owner = &owned.Owner
		player.OwnerTeamID = &owned.TeamID
		player.Clause = owned.Clause
		player.ClauseUntil = owned.LockedUntil
		player.ClauseLocked = owned.Locked
		player.Shielded = owned.Shielded
		player.ShieldUntil = owned.ShieldUntil
		player.PlayerTeamID = owned.PlayerTeamID
		player.IsMine = myTeamID != "" && owned.TeamID == myTeamID
		if player.IsMine {
			player.rate(world.Bridge.trendOf(id), fichas[id], ownStatus(entry, status[id]))
		}
	}

	// Listings and offers hang off the player, so they are attached after the rows exist.
	listingByPlayer := map[string]*Listing{}
	for index := range universe.Market {
		listingByPlayer[universe.Market[index].PlayerID] = &universe.Market[index]
	}
	offersByPlayer := loadOffers(member, ownership)
	for index := range universe.Players {
		id := universe.Players[index].ID
		universe.Players[index].MarketEntry = listingByPlayer[id]
		// Empty rather than absent: "no offers" is an answer, and the page iterates it.
		universe.Players[index].Offers = offersByPlayer[id]
		if universe.Players[index].Offers == nil {
			universe.Players[index].Offers = []map[string]any{}
		}
	}

	if league != nil {
		universe.Activity = slices.Clone(league.Activity)
		universe.Lineups = league.Lineups

		var myCash *float64
		if myTeamID != "" {
			if money, err := member.Money(time.Minute); err == nil {
				cash := money.TeamMoney
				myCash = &cash
			} else {
				slog.Warn("money unreachable", "reason", err.Error())
			}
		}
		universe.CashModel = ReconstructCash(universe.Activity, universe.LeagueTeams,
			myTeamID, myCash)
	}

	ApplyScores(universe.Players)

	// The league's hold rule: bought less than N days ago means not sellable, and the only
	// record of when a player changed hands is the league log.
	ApplyHoldRule(universe, myTeamID, state.House)

	// Clock-derived, so it comes after the rows exist and before anything reads it.
	now := time.Now().UTC()
	for index := range universe.Players {
		if until := universe.Players[index].ClauseUntil; until != nil {
			if when, err := parseTime(*until); err == nil {
				hours := when.Sub(now).Hours()
				universe.Players[index].ClauseHoursLeft = &hours
			}
		}
	}
	universe.Clauses = ClauseCalendar(universe.Players, 24*10)

	slog.Debug("universe built", "players", len(universe.Players),
		"owned", len(ownership), "listings", len(universe.Market),
		"fixtures", len(universe.Fixtures))
	return universe, nil
}

func (p *Player) rate(trend *Trend, ficha *Odds, status string) {
	p.Status = status
	// Only the severe statuses make a player unavailable; doubtful still plays.
	p.Available = !severeStatus[status]
	p.StartProbability, p.StartSource, p.StartWeek = nil, "", nil
	p.XPts, p.PointsValue = 0, 0
	p.applyOdds(trend, ficha)
	p.score(status)
	if p.Value > 0 {
		p.PointsValue = p.XPts / (p.Value / 1e6)
	}
}

func bulkStatus(entry map[string]any) string {
	return ownStatus(entry, "")
}

func ownStatus(entry map[string]any, fresh string) string {
	status := text(entry["playerStatus"])
	if fresh != "" {
		status = graverStatus(status, fresh)
	}
	if status = strings.ToLower(status); status == "" {
		return "ok"
	}
	return status
}

func (b *Bridge) trendOf(id string) *Trend {
	if b == nil {
		return nil
	}
	if found, ok := b.Trends[id]; ok {
		return &found
	}
	return nil
}

// ReconstructCash writes each member's own estimate into these.
func cloneTeams(teams map[string]*LeagueTeam) map[string]*LeagueTeam {
	out := make(map[string]*LeagueTeam, len(teams))
	for id, team := range teams {
		copied := *team
		out[id] = &copied
	}
	return out
}
