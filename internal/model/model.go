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
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
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
}

type Fixture struct {
	ID           string `json:"id"`
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
	CurrentWeight   float64            `json:"current_weight"`
	TeamStrength    map[string]float64 `json:"team_strength"`
	MatchedCount    int                `json:"matched_count"`
}

// Positions as the API numbers them, with the names the report uses.
var positions = map[int]string{1: "POR", 2: "DEF", 3: "MED", 4: "DEL", 5: "ENT"}

// Build assembles the structural universe. TTLs match the Python ones so a frozen cache
// serves both sides identically.
func Build(client *api.Client, leagueID, myTeamID string, bridge *Bridge) (*Universe, error) {
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

	universe := &Universe{
		Week:           week,
		Fixtures:       LoadFixtures(client, week, teams),
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
	if leagueID != "" {
		ownership, err = loadOwnership(client, leagueID)
		if err != nil {
			return nil, fmt.Errorf("ownership: %w", err)
		}
		universe.OwnershipLoaded = len(ownership) > 0
		universe.Market, err = loadMarket(client, leagueID, myTeamID)
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

		if owned, ok := ownership[id]; ok {
			player.Owner = &owned.Owner
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

	ApplyScores(universe.Players)

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
	Clause       *float64
	LockedUntil  *string
	Locked       bool
	Shielded     bool
	PlayerTeamID *string
}

// loadOwnership walks every squad in the league. It is the only source for the clause,
// the shield and the squad-slot id — and that slot id, not the player's own id, is what
// the write endpoints want.
func loadOwnership(client *api.Client, leagueID string) (map[string]slot, error) {
	standings, err := client.Standings(leagueID, 30*time.Minute)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	ownership := map[string]slot{}

	for _, entry := range standings {
		teamID := text(nested(entry, "team", "id"))
		if teamID == "" {
			teamID = text(entry["id"])
		}
		if teamID == "" {
			continue
		}
		owner := text(nested(entry, "team", "manager", "managerName"))
		if owner == "" {
			owner = text(nested(entry, "team", "teamName"))
		}
		if owner == "" {
			owner = teamID
		}

		squad, err := client.TeamSquad(leagueID, teamID, 30*time.Minute)
		if err != nil {
			slog.Warn("squad unreachable", "team", teamID, "reason", err.Error())
			continue
		}
		for _, raw := range squadPlayers(squad) {
			id := text(nested(raw, "playerMaster", "id"))
			if id == "" {
				continue
			}
			held := slot{Owner: owner, TeamID: teamID}
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
		}
	}
	return ownership, nil
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

func loadMarket(client *api.Client, leagueID, myTeamID string) ([]Listing, error) {
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
		}
		if expires := text(entry["expirationDate"]); expires != "" {
			listing.Expires = &expires
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

func text(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
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
