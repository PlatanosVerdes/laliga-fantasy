package model

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
)

// Preview is the league as far as a cold start has read it, for a page to show while the rest
// loads: my squad first, then the market, then the rivals. It is a type of its own and never a
// Universe, so nothing that bids, sells or arms an instruction can be handed half a league.
type Preview struct {
	Stage  int              `json:"stage"`
	Week   int              `json:"week"`
	Mine   PreviewTeam      `json:"mine"`
	Cash   *float64         `json:"cash"`
	Market []PreviewListing `json:"market"`
	Rivals []PreviewTeam    `json:"rivals"`
}

// The stages, in the order they are read. PreviewComplete is the whole world, published by
// the ordinary build once it is over.
const (
	PreviewMine = 1 + iota
	PreviewMarket
	PreviewRivals
	PreviewComplete
)

type PreviewTeam struct {
	TeamID   string          `json:"team_id"`
	Name     string          `json:"name"`
	Position *int            `json:"position"`
	Points   float64         `json:"points"`
	Value    float64         `json:"value"`
	Players  []PreviewPlayer `json:"players"`
}

type PreviewPlayer struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	PositionID int      `json:"position_id"`
	Team       string   `json:"team"`
	Value      float64  `json:"value"`
	Points     float64  `json:"points"`
	Status     string   `json:"status"`
	Clause     *float64 `json:"clause"`
	Starter    bool     `json:"starter"`
}

type PreviewListing struct {
	Player  PreviewPlayer `json:"player"`
	Listing Listing       `json:"listing"`
}

// PreviewSource is what the preview reads; *api.Client is one.
type PreviewSource interface {
	lister
	CurrentWeek(ttl time.Duration) (api.Week, error)
	TeamsMaster(ttl time.Duration) ([]api.Team, error)
	Standings(leagueID string, ttl time.Duration) ([]map[string]any, error)
	TeamSquad(leagueID, teamID string, ttl time.Duration) (map[string]any, error)
	TeamLineupWeek(teamID string, week int, ttl time.Duration) (map[string]any, error)
	Player(playerID string, ttl time.Duration) (map[string]any, error)
	Money(teamID string, ttl time.Duration) (api.Money, error)
	MarketRaw(leagueID string, ttl time.Duration, store bool) ([]map[string]any, error)
}

// BuildPreview reads my squad, the market and the rivals' squads, in that order, and hands
// over what it has after each. Every request is one Build makes too, with the same lifetime,
// so the full build that follows reads them back from the cache: the preview moves them to the
// front of the cold start without adding any, and without touching what Build produces.
func BuildPreview(source PreviewSource, leagueID, myTeamID string, publish func(Preview)) error {
	if leagueID == "" || myTeamID == "" {
		return fmt.Errorf("sin liga o sin equipo")
	}
	week, err := source.CurrentWeek(30 * time.Minute)
	if err != nil {
		return fmt.Errorf("week: %w", err)
	}
	teams, err := source.TeamsMaster(24 * time.Hour)
	if err != nil {
		return fmt.Errorf("teams: %w", err)
	}
	raw, err := allPlayers(source)
	if err != nil {
		return fmt.Errorf("players: %w", err)
	}
	clubs := make(map[string]string, len(teams))
	for _, team := range teams {
		clubs[team.ID] = team.Name
	}
	masters := make(map[string]map[string]any, len(raw))
	for _, entry := range raw {
		masters[text(entry["id"])] = entry
	}
	playerOf := func(id string) (PreviewPlayer, bool) {
		entry, ok := masters[id]
		if !ok || int(number(entry["positionId"])) == PositionCoach {
			return PreviewPlayer{}, false
		}
		return PreviewPlayer{ID: id, Name: label(entry), PositionID: int(number(entry["positionId"])),
			Team: clubs[text(entry["teamId"])], Value: number(entry["marketValue"]),
			Points: number(entry["points"]), Status: strings.ToLower(text(entry["playerStatus"]))}, true
	}

	standings, err := source.Standings(leagueID, 30*time.Minute)
	if err != nil {
		return fmt.Errorf("standings: %w", err)
	}
	squadOf := func(row standing) (PreviewTeam, error) {
		team := PreviewTeam{TeamID: row.TeamID, Name: row.TeamID, Position: row.Position,
			Points: row.Points}
		if row.Manager != nil && *row.Manager != "" {
			team.Name = *row.Manager
		} else if row.Name != nil && *row.Name != "" {
			team.Name = *row.Name
		}
		squad, err := source.TeamSquad(leagueID, row.TeamID, 30*time.Minute)
		if err != nil {
			return team, err
		}
		for _, held := range squadPlayers(squad) {
			player, ok := playerOf(text(nested(held, "playerMaster", "id")))
			if !ok {
				continue
			}
			if clause, present := held["buyoutClause"]; present && clause != nil {
				value := number(clause)
				player.Clause = &value
			}
			team.Value += player.Value
			team.Players = append(team.Players, player)
		}
		sort.SliceStable(team.Players, func(one, two int) bool {
			return team.Players[one].Value > team.Players[two].Value
		})
		return team, nil
	}

	preview := Preview{Stage: PreviewMine, Week: week.WeekNumber}
	var rivals []standing
	found := false
	for _, entry := range standings {
		row := normalizeStanding(entry)
		switch {
		case row.TeamID == "":
		case row.TeamID == myTeamID:
			found = true
			if preview.Mine, err = squadOf(row); err != nil {
				return fmt.Errorf("my squad: %w", err)
			}
		default:
			rivals = append(rivals, row)
		}
	}
	if !found {
		return fmt.Errorf("my team is not in the standings")
	}
	starters := map[string]bool{}
	if payload, err := source.TeamLineupWeek(myTeamID, week.WeekNumber, 5*time.Minute); err == nil {
		for _, shirt := range LineupOf(payload, week.WeekNumber) {
			starters[shirt.ID] = true
		}
	}
	for index := range preview.Mine.Players {
		player := &preview.Mine.Players[index]
		player.Starter = starters[player.ID]
		if detail, err := source.Player(player.ID, OwnStatusTTL); err == nil {
			player.Status = graverStatus(player.Status, text(detail["playerStatus"]))
		}
	}
	if money, err := source.Money(myTeamID, time.Minute); err == nil {
		cash := money.TeamMoney
		preview.Cash = &cash
	}
	reached("Tu equipo", 1, 1)
	publish(preview)

	entries, err := source.MarketRaw(leagueID, time.Minute, false)
	if err != nil {
		return fmt.Errorf("market: %w", err)
	}
	listed := make([]PreviewListing, 0, len(entries))
	for _, entry := range entries {
		listing := listingOf(entry, myTeamID)
		player, ok := playerOf(listing.PlayerID)
		if !ok {
			continue
		}
		listed = append(listed, PreviewListing{Player: player, Listing: listing})
	}
	preview.Stage, preview.Market = PreviewMarket, listed
	reached("Mercado abierto", 1, 1)
	publish(preview)

	read := make([]PreviewTeam, 0, len(rivals))
	for index, row := range rivals {
		reached("Plantillas rivales", index+1, len(rivals))
		team, err := squadOf(row)
		if err != nil {
			// The full build counts it as unread and says so; the preview just goes on without it.
			continue
		}
		read = append(read, team)
	}
	sort.SliceStable(read, func(one, two int) bool { return read[one].Points > read[two].Points })
	preview.Stage, preview.Rivals = PreviewRivals, read
	publish(preview)
	return nil
}
