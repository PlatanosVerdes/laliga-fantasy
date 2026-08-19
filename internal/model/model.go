// Package model builds the world from the API, mirroring the structural half of
// fantasy/analysis.py: who exists, who owns them, what is listed, what is played this
// week. The scoring half — xPts, the price prior, the score — comes next and needs the
// futbolfantasy data, which stays in Python.
//
// Field names are the Python ones on purpose. The differential harness compares the two
// dumps key by key, so a rename here would read as a mismatch there.
package model

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/matching"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/rules"
)

// Observed match states: 1 not started, 7 finished with a score. Nothing has ever been
// seen for a match under way, which is why liveness is judged by the clock.
const (
	MatchPending  = 1
	MatchFinished = 7
)

// Coaches are position 5 and are not players: they cannot be signed, scored or listed,
// so the model leaves them out.
const PositionCoach = 5

// Statuses that mean the player will not play at all. "doubtful" is not one of them —
// he is discounted, not written off.
var severeStatus = map[string]bool{
	"injured": true, "sanctioned": true, "suspended": true, "out_of_league": true,
}

type Player struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	FullName      *string  `json:"full_name"`
	Position      string   `json:"position"`
	PositionID    int      `json:"position_id"`
	Team          string   `json:"team"`
	TeamID        string   `json:"team_id"`
	TeamShort     string   `json:"team_short"`
	Value         float64  `json:"value"`
	SeasonPoints  float64  `json:"season_points"`
	SeasonAvg     float64  `json:"season_avg"`
	LastSeason    float64  `json:"last_season_points"`
	Status        string   `json:"status"`
	Available     bool     `json:"available"`
	Owner         *string  `json:"owner"`
	OwnerTeamID   *string  `json:"owner_team_id"`
	Clause        *float64 `json:"clause"`
	ClauseLocked  bool     `json:"clause_locked"`
	ClauseUntil   *string  `json:"clause_locked_until"`
	Shielded      bool     `json:"shielded"`
	PlayerTeamID  *string  `json:"player_team_id"`
	IsMine        bool     `json:"is_mine"`

	// --- scoring half: everything below depends on the futbolfantasy bridge -------
	BaseWeek         float64                `json:"base_week"`
	PriorBased       bool                   `json:"prior_based"`
	Confidence       float64                `json:"confidence"`
	StartProbability *float64               `json:"start_probability"`
	NextWeek         *int                   `json:"next_week"`
	NextRival        *string                `json:"next_rival"`
	NextHome         *bool                  `json:"next_home"`
	FixtureFactor    float64                `json:"fixture_factor"`
	XPts             float64                `json:"xpts"`
	PointsValue      float64                `json:"points_value"`
	ProjectedPct     float64                `json:"projected_pct"`
	ProjectedGain    float64                `json:"projected_gain"`
	TrendLabel       *string                `json:"trend_label"`
	StreakDays       *int                   `json:"streak_days"`
	StreakDir        *string                `json:"streak_dir"`
	Acceleration     *float64               `json:"acceleration"`
	Pct1d            *float64               `json:"pct_1d"`
	Pct7d            *float64               `json:"pct_7d"`
	Pct30d           *float64               `json:"pct_30d"`
	FFID             *string                `json:"ff_id"`
	FFName           *string                `json:"ff_name"`
	FFValue          *float64               `json:"ff_value"`
	Absence          map[string]any         `json:"absence"`
	ClauseHoursLeft  *float64               `json:"clause_hours_left"`
	Image            string                 `json:"image"`
	// When he was signed, read from the league's own log, and when the league's hold rule
	// lets him be sold. Absent when he was never bought (or the log does not reach that far).
	BoughtAt   *string `json:"bought_at"`
	HoldUntil  *string `json:"hold_until"`
	SaleLocked bool    `json:"sale_locked"`
	Starred          bool                   `json:"starred"`
	RaidScheduled    bool                   `json:"raid_scheduled"`
	MarketEntry      *Listing               `json:"market"`
	Offers           []map[string]any       `json:"offers"`
	Score            float64                `json:"score"`
	Rank             int                    `json:"rank"`
	PositionRank     int                    `json:"position_rank"`
}

type Listing struct {
	MarketID     string   `json:"market_id"`
	PlayerID     string   `json:"player_id"`
	Kind         string   `json:"kind"`
	MinBid       float64  `json:"min_bid"`
	MarketValue  float64  `json:"market_value"`
	Expires      *string  `json:"expires"`
	Bids         *int     `json:"bids"`
	Offers       *int     `json:"offers"`
	Seller       *string  `json:"seller"`
	SellerTeamID *string  `json:"seller_team_id"`
	IsMine       bool     `json:"is_mine"`
	// Absent rather than false when the API does not say: the listing simply has no
	// opinion, and flattening that to false would invent one.
	DirectOffer *bool `json:"direct_offer"`
	// There is no endpoint that lists your own bids (GET on .../bid is 405, POST-only),
	// so the id has to come from whatever the market entry itself exposes once a bid
	// exists. Looked up generically rather than guessed.
	MyBidID *string `json:"my_bid_id"`
	// What we bid, so the page can say "tienes 37M puestos" instead of just knowing that
	// something is there.
	MyBid       *float64 `json:"my_bid"`
	MyBidStatus *string  `json:"my_bid_status"`
}

type Fixture struct {
	ID           string `json:"id"`
	Week         int    `json:"week,omitempty"`
	Kickoff      string `json:"kickoff"`
	State        int    `json:"state"`
	LocalID      string `json:"local_id"`
	VisitorID    string `json:"visitor_id"`
	Local        string `json:"local"`
	Visitor      string `json:"visitor"`
	LocalScore   *int   `json:"local_score"`
	VisitorScore *int   `json:"visitor_score"`
}

type Universe struct {
	Week            api.Week           `json:"week"`
	Fixtures        []Fixture          `json:"fixtures"`
	Players         []Player           `json:"players"`
	Market          []Listing          `json:"market"`
	MyTeamID        *string            `json:"my_team_id"`
	OwnershipLoaded bool               `json:"ownership_loaded"`
	CompletedWeeks  int                `json:"completed_weeks"`
	NextWeekOpens   string             `json:"next_week_opens"`
	CurrentWeight   float64            `json:"current_weight"`
	TeamStrength    map[string]float64 `json:"team_strength"`
	MatchedCount    int                `json:"matched_count"`
	LeagueTeams     map[string]*LeagueTeam `json:"league_teams"`
	Activity        []Event            `json:"activity"`
	Schedule        []Fixture          `json:"schedule"`
	CashModel       CashModel          `json:"cash_model"`
	Clauses         map[string]any     `json:"clauses"`
}

// Positions as the API numbers them, with the names the report uses.
var positions = map[int]string{1: "POR", 2: "DEF", 3: "MED", 4: "DEL", 5: "ENT"}

// Build assembles the structural universe. TTLs match the Python ones so a frozen cache
// serves both sides identically.
// State is what lives in files rather than in the feed: the stars we set, the standing
// instructions we armed. Passed in rather than read here so the model stays a pure
// function of its inputs, which is what makes it comparable.
type State struct {
	Starred map[string]bool
	Raids   map[string]bool
}

// BuildLive is Build with the futbolfantasy half fetched and matched in Go, which is what
// the server uses now that nothing goes through Python.
func BuildLive(client *api.Client, leagueID, myTeamID string, state State,
	ffTTL time.Duration) (*Universe, error) {
	bridge, err := LoadBridge(client, ffTTL)
	if err != nil {
		return nil, err
	}
	return Build(client, leagueID, myTeamID, bridge, state)
}

// LoadBridge is what used to be a Python subprocess: the market page scraped, matched to
// LaLiga ids, and the absences resolved to the players they belong to.
func LoadBridge(client *api.Client, ffTTL time.Duration) (*Bridge, error) {
	players, err := allPlayers(client)
	if err != nil {
		return nil, err
	}
	teams, err := client.TeamsMaster(24 * time.Hour)
	if err != nil {
		return nil, err
	}
	teamRows := make([]map[string]any, 0, len(teams))
	for _, team := range teams {
		teamRows = append(teamRows, map[string]any{
			"id": team.ID, "name": team.Name, "slug": team.Slug,
			"shortName": team.ShortName})
	}

	market, err := futbolfantasy.Market(ffTTL)
	if err != nil {
		return nil, err
	}
	matched, unmatched := matching.MatchMarket(players, market,
		matching.BuildTeamIndex(teamRows))

	trends := make(map[string]Trend, len(matched))
	for id, row := range matched {
		blob, err := json.Marshal(row)
		if err != nil {
			continue
		}
		var trend Trend
		if err := json.Unmarshal(blob, &trend); err == nil {
			trends[id] = trend
		}
	}

	absences := MatchAbsences(players, futbolfantasy.Absences(3*time.Hour))
	loose := make([]any, 0, len(unmatched))
	for _, row := range unmatched {
		loose = append(loose, row)
	}
	return &Bridge{Trends: trends, Absences: absences, Unmatched: loose,
		MatchedCount: len(matched)}, nil
}

func Build(client *api.Client, leagueID, myTeamID string, bridge *Bridge,
	state State) (*Universe, error) {
	week, err := client.CurrentWeek(30 * time.Minute)
	if err != nil {
		return nil, fmt.Errorf("week: %w", err)
	}
	teams, err := client.TeamsMaster(24 * time.Hour)
	if err != nil {
		return nil, fmt.Errorf("teams: %w", err)
	}
	raw, err := allPlayers(client)
	if err != nil {
		return nil, fmt.Errorf("players: %w", err)
	}

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
		if fixtures, err := client.Calendar(week.NextWeek, 6*time.Hour); err == nil {
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

	universe := &Universe{
		Week:           week,
		NextWeekOpens:  nextOpens,
		Fixtures:       LoadFixtures(client, week, teams),
		Schedule:       LoadSchedule(client, week, teams),
		CompletedWeeks: completedWeeks,
		CurrentWeight:  currentWeight,
	}
	if bridge != nil {
		universe.MatchedCount = bridge.MatchedCount
	}
	if myTeamID != "" {
		universe.MyTeamID = &myTeamID
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
	universe.TeamStrength = strength

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

	ownership := map[string]slot{}
	// Faces come from wherever they appear — squads and the market — because the public
	// player list carries none.
	faces := map[string]string{}
	if leagueID != "" {
		ownership, universe.LeagueTeams, err = loadOwnership(client, leagueID)
		if err != nil {
			return nil, fmt.Errorf("ownership: %w", err)
		}
		universe.OwnershipLoaded = len(ownership) > 0
		universe.Market, err = loadMarket(client, leagueID, myTeamID, faces)
		if err != nil {
			return nil, fmt.Errorf("market: %w", err)
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
		status := strings.ToLower(text(entry["playerStatus"]))
		if status == "" {
			status = "ok"
		}
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
			Status:       status,
			// Only the severe statuses make a player unavailable; doubtful still plays.
			Available: !severeStatus[status],
		}
		if full := text(entry["name"]); full != "" {
			player.FullName = &full
		}

		// --- the scoring half ---------------------------------------------------
		var trend *Trend
		if bridge != nil {
			if found, ok := bridge.Trends[id]; ok {
				trend = &found
			}
			if absence, ok := bridge.Absences[id]; ok {
				player.Absence = absence
			}
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

		availability := 1.0
		if trend != nil && trend.StartProbability != nil {
			player.StartProbability = trend.StartProbability
			availability = (*trend.StartProbability / 100.0) / BaselineStartProbability
			availability = math.Max(AvailabilityFloor, math.Min(AvailabilityCeiling, availability))
		}

		var rivalStrength *float64
		if trend != nil && trend.NextRival != nil {
			if rivalID, ok := teamByName[normalizeTeam(*trend.NextRival)]; ok {
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

		// Unknown minutes and a price-derived baseline are both guesses; discount them
		// so a fringe player with no data cannot outrank a known starter.
		player.Confidence = 1.0
		if player.StartProbability == nil {
			player.Confidence *= NoTrendConfidence
		}
		if player.PriorBased {
			player.Confidence *= PriorConfidence
		}

		if player.Available {
			player.XPts = player.BaseWeek * availability * player.FixtureFactor * player.Confidence
			if status == "doubtful" {
				player.XPts *= DoubtfulXPts
			}
		}
		if player.Value > 0 {
			player.PointsValue = player.XPts / (player.Value / 1e6)
		}
		player.ProjectedPct = ProjectedPct(trend)
		player.ProjectedGain = player.Value * player.ProjectedPct / 100.0

		player.Starred = state.Starred[id]
		player.RaidScheduled = state.Raids[id]
		if face, ok := faces[id]; ok {
			player.Image = face
		}

		if owned, ok := ownership[id]; ok {
			player.Owner = &owned.Owner
			if owned.Image != "" {
				player.Image = owned.Image
			}
			player.OwnerTeamID = &owned.TeamID
			player.Clause = owned.Clause
			player.ClauseUntil = owned.LockedUntil
			player.ClauseLocked = owned.Locked
			player.Shielded = owned.Shielded
			player.PlayerTeamID = owned.PlayerTeamID
			player.IsMine = myTeamID != "" && owned.TeamID == myTeamID
		}
		universe.Players = append(universe.Players, player)
	}

	// Listings and offers hang off the player, so they are attached after the rows exist.
	listingByPlayer := map[string]*Listing{}
	for index := range universe.Market {
		listingByPlayer[universe.Market[index].PlayerID] = &universe.Market[index]
	}
	offersByPlayer := loadOffers(client, leagueID, universe.Market, ownership)
	for index := range universe.Players {
		id := universe.Players[index].ID
		universe.Players[index].MarketEntry = listingByPlayer[id]
		// Empty rather than absent: "no offers" is an answer, and the page iterates it.
		universe.Players[index].Offers = offersByPlayer[id]
		if universe.Players[index].Offers == nil {
			universe.Players[index].Offers = []map[string]any{}
		}
	}

	if leagueID != "" {
		managers := map[string]string{}
		for _, team := range universe.LeagueTeams {
			if team.UserID == "" {
				continue
			}
			if team.Manager != nil && *team.Manager != "" {
				managers[team.UserID] = *team.Manager
			} else if team.Name != nil {
				managers[team.UserID] = *team.Name
			}
		}
		names := make(map[string]string, len(universe.Players))
		for _, player := range universe.Players {
			names[player.ID] = player.Name
		}
		universe.Activity = loadActivity(client, leagueID, managers, names)

		var myCash *float64
		if myTeamID != "" {
			if money, err := client.Money(myTeamID, time.Minute); err == nil {
				cash := money.TeamMoney
				myCash = &cash
			} else {
				slog.Warn("money unreachable", "reason", err.Error())
			}
		}
		universe.CashModel = ReconstructCash(universe.Activity, universe.LeagueTeams,
			myTeamID, myCash)
		EnrichActivityValues(client, universe.Activity, 18)
	}

	ApplyScores(universe.Players)

	// The league's hold rule: bought less than N days ago means not sellable, and the only
	// record of when a player changed hands is the league log.
	ApplyHoldRule(universe, myTeamID, rules.For(leagueID))

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

func allPlayers(client *api.Client) ([]map[string]any, error) {
	var players []map[string]any
	err := client.RawList(config.CMP+"/players", 6*time.Hour, "players", &players)
	return players, err
}

type slot struct {
	Owner        string
	TeamID       string
	// The cutout the app uses. The public players list does not carry it, but every squad
	// slot does, which is the only reason a face can be shown at all.
	Image        string
	Clause       *float64
	LockedUntil  *string
	Locked       bool
	Shielded     bool
	PlayerTeamID *string
}

// loadOwnership walks every squad in the league. It is the only source for the clause,
// the shield and the squad-slot id — and that slot id, not the player's own id, is what
// the write endpoints want.
func loadOwnership(client *api.Client, leagueID string) (map[string]slot,
	map[string]*LeagueTeam, error) {
	standings, err := client.Standings(leagueID, 30*time.Minute)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	ownership := map[string]slot{}
	teams := map[string]*LeagueTeam{}

	for _, entry := range standings {
		row := normalizeStanding(entry)
		teamID := row.TeamID
		if teamID == "" {
			continue
		}
		// Python falls back manager -> teamName -> id, and the ownership rows carry
		// whichever it found; the page shows it as the owner.
		owner := teamID
		if row.Manager != nil && *row.Manager != "" {
			owner = *row.Manager
		} else if row.Name != nil && *row.Name != "" {
			owner = *row.Name
		}

		squad, err := client.TeamSquad(leagueID, teamID, 30*time.Minute)
		if err != nil {
			slog.Warn("squad unreachable", "team", teamID, "reason", err.Error())
			continue
		}
		squadValue, clauseTotal, count := 0.0, 0.0, 0
		for _, raw := range squadPlayers(squad) {
			id := text(nested(raw, "playerMaster", "id"))
			if id == "" {
				continue
			}
			held := slot{Owner: owner, TeamID: teamID,
				Image: text(nested(raw, "playerMaster", "images", "transparent", "256x256"))}
			if clause, ok := raw["buyoutClause"]; ok && clause != nil {
				value := number(clause)
				held.Clause = &value
			}
			if until := text(raw["buyoutClauseLockedEndTime"]); until != "" {
				held.LockedUntil = &until
				if when, err := parseTime(until); err == nil && when.After(now) {
					held.Locked = true
				}
			}
			held.Shielded = truthy(raw["isShielded"])
			if pt := text(raw["playerTeamId"]); pt != "" {
				held.PlayerTeamID = &pt
			} else if pt := text(raw["id"]); pt != "" {
				held.PlayerTeamID = &pt
			}
			ownership[id] = held
			squadValue += number(nested(raw, "playerMaster", "marketValue"))
			clauseTotal += number(raw["buyoutClause"])
			count++
		}

		teams[teamID] = &LeagueTeam{
			TeamID: teamID, UserID: row.UserID, Name: row.Name, Manager: row.Manager,
			Points: row.Points, LivePoints: row.LivePoints, Position: row.Position,
			ReportedValue: row.TeamValue, SquadValue: squadValue,
			ClauseTotal: clauseTotal, Players: count, CashIsEstimate: true,
		}
	}
	return ownership, teams, nil
}

// standing is the flattened standings row. The endpoint nests half of it under "team"
// and half of it at the top level, and which half depends on the field.
type standing struct {
	TeamID     string
	UserID     string
	Name       *string
	Manager    *string
	Points     float64
	LivePoints *float64
	Position   *int
	TeamValue  float64
}

func normalizeStanding(entry map[string]any) standing {
	row := standing{
		TeamID: fallback(text(entry["id"]), text(nested(entry, "team", "id"))),
		UserID: fallback(text(entry["userId"]), text(nested(entry, "team", "manager", "id"))),
	}
	row.Name = optional(fallback(text(entry["name"]), text(nested(entry, "team", "name"))))
	// `manager` is a string at the top level but an object under team.
	if manager, ok := entry["manager"].(string); ok && manager != "" {
		row.Manager = &manager
	} else {
		row.Manager = optional(text(nested(entry, "team", "manager", "managerName")))
	}
	for _, candidate := range []any{entry["points"], nested(entry, "team", "teamPoints"),
		nested(entry, "team", "points")} {
		if value := number(candidate); value != 0 {
			row.Points = value
			break
		}
	}
	for _, candidate := range []any{entry["teamValue"], nested(entry, "team", "teamValue")} {
		if value := number(candidate); value != 0 {
			row.TeamValue = value
			break
		}
	}
	if live, ok := entry["livePoints"]; ok && live != nil {
		value := number(live)
		row.LivePoints = &value
	}
	if position, ok := entry["position"]; ok && position != nil {
		value := int(number(position))
		row.Position = &value
	}
	return row
}

// findBidID digs the id of our own bid out of the listing, trying the shapes the API has
// been seen to use.
func findBidID(entry map[string]any) *string {
	for _, key := range []string{"bidId", "myBidId", "userBidId"} {
		if id := text(entry[key]); id != "" && id != "0" {
			return &id
		}
	}
	// `offer` covers what I sent on a rival's sale: same idea as a bid, different word, and the
	// payload uses whichever the listing type calls for.
	for _, key := range []string{"bids", "myBids", "userBids", "bid", "offer", "myOffer"} {
		switch value := entry[key].(type) {
		case map[string]any:
			if id := text(value["id"]); id != "" {
				return &id
			}
		case []any:
			for _, item := range value {
				if row, ok := item.(map[string]any); ok {
					if id := text(row["id"]); id != "" {
						return &id
					}
				}
			}
		}
	}
	return nil
}

// findBidAmount and findBidStatus read the same `bid` object findBidID found: the market list
// carries our own bid inline, which is the only place it is exposed at all.
func findBidAmount(entry map[string]any) *float64 {
	bid := myBid(entry)
	if bid == nil {
		return nil
	}
	if amount, present := bid["money"]; present && amount != nil {
		value := number(amount)
		return &value
	}
	return nil
}

func findBidStatus(entry map[string]any) *string {
	bid := myBid(entry)
	if bid == nil {
		return nil
	}
	if status := text(bid["status"]); status != "" {
		return &status
	}
	return nil
}

// myBid is the object the listing uses for what I put on it, under either name.
func myBid(entry map[string]any) map[string]any {
	for _, key := range []string{"bid", "offer", "myOffer"} {
		if row, ok := entry[key].(map[string]any); ok {
			return row
		}
	}
	return nil
}

// loadOffers asks only for our own listings: one request each, and there is no route that
// lists them all.
func loadOffers(client *api.Client, leagueID string, listings []Listing,
	ownership map[string]slot) map[string][]map[string]any {
	offers := map[string][]map[string]any{}
	if leagueID == "" {
		return offers
	}
	for _, listing := range listings {
		if !listing.IsMine {
			continue
		}
		held, ok := ownership[listing.PlayerID]
		if !ok || held.PlayerTeamID == nil {
			continue
		}
		received, err := client.PlayerOffers(leagueID, *held.PlayerTeamID, time.Minute)
		if err != nil {
			slog.Debug("offers unreachable", "player", listing.PlayerID, "reason", err.Error())
			continue
		}
		pending := make([]map[string]any, 0, len(received))
		for _, offer := range received {
			status := text(offer["status"])
			if status != "" && status != "pending" {
				continue
			}
			// Who and when, lifted out of the payload's corners. An offer with no buyerTeam is
			// the game's own daily bid, not a person: telling the two apart is the difference
			// between "somebody wants him" and "the market always offers this".
			offer["from_market"] = truthy(offer["isFromMarket"]) ||
				nested(offer, "buyerTeam") == nil
			if manager := text(nested(offer, "buyerTeam", "manager", "managerName")); manager != "" {
				offer["from"] = manager
			}
			if teamID := text(nested(offer, "buyerTeam", "id")); teamID != "" {
				offer["from_team_id"] = teamID
			}
			if worth := number(nested(offer, "buyerTeam", "teamValue")); worth != 0 {
				offer["from_team_value"] = worth
			}
			pending = append(pending, offer)
		}
		if len(pending) == 0 {
			continue
		}
		// Best first: the plan only ever looks at the top one.
		sort.SliceStable(pending, func(i, j int) bool {
			return number(pending[i]["money"]) > number(pending[j]["money"])
		})
		offers[listing.PlayerID] = pending
	}
	return offers
}

// loadActivity walks the paged log until a page comes back empty, exactly as Python does:
// the page size is not documented and the last page is how you learn there are no more.
func loadActivity(client *api.Client, leagueID string, managers, names map[string]string) []Event {
	var raw []map[string]any
	for index := 0; index < 20; index++ {
		page, err := client.ActivityRaw(leagueID, index, time.Minute, false)
		if err != nil {
			slog.Warn("activity unreachable", "index", index, "reason", err.Error())
			break
		}
		if len(page) == 0 {
			break
		}
		raw = append(raw, page...)
	}
	return NormalizeActivity(raw, managers, names)
}

func squadPlayers(squad map[string]any) []map[string]any {
	for _, key := range []string{"players", "playersTeams", "teamPlayers"} {
		if list, ok := squad[key].([]any); ok {
			out := make([]map[string]any, 0, len(list))
			for _, item := range list {
				if row, ok := item.(map[string]any); ok {
					out = append(out, row)
				}
			}
			return out
		}
	}
	return nil
}

func loadMarket(client *api.Client, leagueID, myTeamID string,
	faces map[string]string) ([]Listing, error) {
	entries, err := client.MarketRaw(leagueID, time.Minute, false)
	if err != nil {
		return nil, err
	}
	out := make([]Listing, 0, len(entries))
	for _, entry := range entries {
		sellerTeamID := text(nested(entry, "sellerTeam", "id"))
		kind := "venta"
		if text(entry["discr"]) == "marketPlayerLeague" {
			kind = "libre"
		}
		listing := Listing{
			MarketID:    text(entry["id"]),
			PlayerID:    text(nested(entry, "playerMaster", "id")),
			Kind:        kind,
			MinBid:      number(entry["salePrice"]),
			MarketValue: number(nested(entry, "playerMaster", "marketValue")),
			Bids:        intOrNil(entry["numberOfBids"]),
			Offers:      intOrNil(entry["numberOfOffers"]),
			IsMine:      myTeamID != "" && sellerTeamID == myTeamID,
			DirectOffer: boolOrNil(entry["directOffer"]),
			MyBidID:     findBidID(entry),
			MyBid:       findBidAmount(entry),
			MyBidStatus: findBidStatus(entry),
		}
		if expires := text(entry["expirationDate"]); expires != "" {
			listing.Expires = &expires
		}
		if face := text(nested(entry, "playerMaster", "images", "transparent", "256x256")); face != "" {
			faces[listing.PlayerID] = face
		}
		if seller := text(nested(entry, "sellerTeam", "manager", "managerName")); seller != "" {
			listing.Seller = &seller
		}
		if sellerTeamID != "" {
			listing.SellerTeamID = &sellerTeamID
		}
		out = append(out, listing)
	}
	return out, nil
}

// UpcomingWeeks is how far ahead the fixture calendar looks. Ten matchdays is about two and
// a half months, which is as far as a decision about a player reaches; each one is a request,
// cached for a day.
const UpcomingWeeks = 10

// LoadSchedule is the fixture list beyond this week: same shape as the live fixtures, one
// entry per match, tagged with its matchday so the page can group it.
func LoadSchedule(client *api.Client, week api.Week, teams []api.Team) []Fixture {
	names := map[string]string{}
	for _, team := range teams {
		names[team.ID] = fallback(team.ShortName, team.Name)
	}

	out := []Fixture{}
	for number := week.WeekNumber; number < week.WeekNumber+UpcomingWeeks; number++ {
		matches, err := client.Calendar(number, 24*time.Hour)
		if err != nil {
			slog.Debug("calendar unavailable", "week", number, "reason", err.Error())
			continue
		}
		for _, match := range matches {
			local := strconv.Itoa(match.LocalID)
			visitor := strconv.Itoa(match.VisitorID)
			out = append(out, Fixture{
				ID: match.ID, Week: number,
				Kickoff:   fallback(match.MatchDate, match.Date),
				State:     match.MatchState,
				LocalID:   local, VisitorID: visitor,
				Local:     fallback(names[local], local),
				Visitor:   fallback(names[visitor], visitor),
				LocalScore: match.LocalScore, VisitorScore: match.VisitorScore,
			})
		}
	}
	sort.SliceStable(out, func(one, two int) bool { return out[one].Kickoff < out[two].Kickoff })
	slog.Info("schedule loaded", "matches", len(out), "from", week.WeekNumber)
	return out
}

// ApplyHoldRule marks every player the league's own pact forbids selling yet — yours and
// everybody else's, because the rule binds the whole league. A rival who signed somebody
// three days ago cannot sell him to you either: that player is reachable only by paying his
// clause, and a plan that ignores this proposes trades nobody can accept.
//
// The API knows nothing about this: it is an agreement between people, and the only trace of
// a signing is the transfer log, so the purchase date is read from there. A player bought
// before the log's horizon has no date and is left alone rather than guessed at.
func ApplyHoldRule(universe *Universe, myTeamID string, league rules.League) {
	if league.HoldDays <= 0 {
		return
	}

	userOfTeam := map[string]string{}
	for teamID, team := range universe.LeagueTeams {
		if team != nil && team.UserID != "" {
			userOfTeam[teamID] = team.UserID
		}
	}

	// Latest purchase wins: a player sold and bought back starts his hold again.
	type purchase struct {
		buyer string
		when  time.Time
	}
	bought := map[string]purchase{}
	for _, event := range universe.Activity {
		if event.PlayerID == nil {
			continue
		}
		// 31 compra, 1 traspaso: in both, user1 is the one who paid.
		if event.TypeID != 31 && event.TypeID != 1 {
			continue
		}
		when, err := parseTime(event.Date)
		if err != nil {
			continue
		}
		if previous, seen := bought[*event.PlayerID]; !seen || when.After(previous.when) {
			bought[*event.PlayerID] = purchase{buyer: event.User1, when: when}
		}
	}

	now := time.Now()
	locked, mineLocked := 0, 0
	for index := range universe.Players {
		player := &universe.Players[index]
		if player.OwnerTeamID == nil && !player.IsMine {
			continue
		}
		last, ok := bought[player.ID]
		if !ok {
			continue
		}
		// The purchase only counts while the buyer still owns him: if he changed hands again
		// outside the log's reach, the date says nothing about the current owner.
		owner := myTeamID
		if player.OwnerTeamID != nil {
			owner = *player.OwnerTeamID
		}
		if userOfTeam[owner] != last.buyer {
			continue
		}

		stamp := last.when.Format(time.RFC3339)
		player.BoughtAt = &stamp
		until := league.HeldUntil(last.when)
		if until == nil {
			continue
		}
		free := until.Format(time.RFC3339)
		player.HoldUntil = &free
		if until.After(now) {
			player.SaleLocked = true
			locked++
			if player.IsMine {
				mineLocked++
			}
		}
	}
	slog.Info("hold rule applied", "days", league.HoldDays, "locked", locked,
		"mine", mineLocked)
}

// LoadFixtures is this week's matches. A match changes neither the transfer log nor the
// market, so the scheduler cannot see it any other way.
func LoadFixtures(client *api.Client, week api.Week, teams []api.Team) []Fixture {
	matches, err := client.Calendar(week.WeekNumber, 6*time.Hour)
	if err != nil {
		slog.Debug("calendar unavailable", "reason", err.Error())
		return nil
	}
	names := map[string]string{}
	for _, team := range teams {
		name := team.ShortName
		if name == "" {
			name = team.Name
		}
		names[team.ID] = name
	}

	out := make([]Fixture, 0, len(matches))
	for _, match := range matches {
		local := strconv.Itoa(match.LocalID)
		visitor := strconv.Itoa(match.VisitorID)
		kickoff := match.MatchDate
		if kickoff == "" {
			kickoff = match.Date
		}
		fixture := Fixture{
			ID: match.ID, Kickoff: kickoff, State: match.MatchState,
			LocalID: local, VisitorID: visitor,
			Local: fallback(names[local], local), Visitor: fallback(names[visitor], visitor),
			LocalScore: match.LocalScore, VisitorScore: match.VisitorScore,
		}
		out = append(out, fixture)
	}
	return out
}

// label is the display name: the nickname, or the full name, or the id. Same order as
// matching.player_label, because the report and the comparison both key on it.
func label(entry map[string]any) string {
	for _, key := range []string{"nickname", "name", "id"} {
		if value := text(entry[key]); value != "" {
			return value
		}
	}
	return ""
}

// --- small helpers, deliberately forgiving: the API mixes strings and numbers ------

func nested(source map[string]any, keys ...string) any {
	var current any = source
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = asMap[key]
	}
	return current
}

// Values from the scrapers are pointers, so absence survives into JSON as null rather than
// as a zero. Every reader has to dereference, and forgetting it is silent: a *string reads as
// "" and a match quietly fails. The comparison harness hid this by passing the rows through
// JSON first, which flattens them.
func text(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case *string:
		if typed == nil {
			return ""
		}
		return *typed
	case *float64:
		if typed == nil {
			return ""
		}
		return strconv.FormatFloat(*typed, 'f', -1, 64)
	case *int:
		if typed == nil {
			return ""
		}
		return strconv.Itoa(*typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	case nil:
		return ""
	default:
		// Never %v on a number: Go turns eight figures into scientific notation, and these
		// strings are ids and names that get compared against Python's.
		return fmt.Sprint(typed)
	}
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case *float64:
		if typed == nil {
			return 0
		}
		return *typed
	case *int:
		if typed == nil {
			return 0
		}
		return float64(*typed)
	case int:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return parsed
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	}
	return 0
}

func intOrNil(value any) *int {
	if value == nil {
		return nil
	}
	converted := int(number(value))
	return &converted
}

func boolOrNil(value any) *bool {
	if value == nil {
		return nil
	}
	converted := truthy(value)
	return &converted
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		return typed != "" && typed != "false" && typed != "0"
	}
	return false
}

func parseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if when, err := time.Parse(layout, value); err == nil {
			return when, nil
		}
	}
	return time.Time{}, fmt.Errorf("fecha no reconocida: %s", value)
}

func fallback(value, other string) string {
	if value != "" {
		return value
	}
	return other
}
