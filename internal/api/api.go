// Package api is the LaLiga Fantasy client. Mirrors fantasy/laliga.py, including the
// inconsistency the official app also carries: standings and squads live under
// /leagues/{id}/..., market and buyout under /league/{id}/... Getting that wrong is a
// 404, and it is not a typo in this file.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

const (
	Minute = time.Minute
	Hour   = time.Hour
)

// Week is the current matchday. isLive and the closing date drive the scheduler.
type Week struct {
	IsLive          bool   `json:"isLive"`
	WeekNumber      int    `json:"weekNumber"`
	NextWeek        int    `json:"nextWeek"`
	OpeningWeekDate string `json:"openingWeekDate"`
	ClosingWeekDate string `json:"closingWeekDate"`
}

// Match as the calendar returns it. matchState 1 is pending and 7 is finished; no value
// for "under way" has ever been observed, which is why liveness is judged by the clock.
type Match struct {
	ID           string `json:"id"`
	MatchDate    string `json:"matchDate"`
	Date         string `json:"date"`
	LocalID      int    `json:"localId"`
	VisitorID    int    `json:"visitorId"`
	MatchState   int    `json:"matchState"`
	LocalScore   *int   `json:"localScore"`
	VisitorScore *int   `json:"visitorScore"`
}

type Team struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ShortName  string `json:"shortName"`
	Slug       string `json:"slug"`
	BadgeColor string `json:"badgeColor"`
}

type Money struct {
	TeamMoney      float64 `json:"teamMoney"`
	TeamInvestment float64 `json:"teamInvestment"`
}

// Listing is a market entry. numberOfBids and numberOfOffers are in the probe digest,
// because a rival bidding changes what the page should say even though nothing has
// been transferred.
type Listing struct {
	ID             string          `json:"id"`
	Discr          string          `json:"discr"`
	SalePrice      json.Number     `json:"salePrice"`
	ExpirationDate string          `json:"expirationDate"`
	NumberOfBids   *int            `json:"numberOfBids"`
	NumberOfOffers *int            `json:"numberOfOffers"`
	DirectOffer    bool            `json:"directOffer"`
	PlayerMaster   json.RawMessage `json:"playerMaster"`
	SellerTeam     json.RawMessage `json:"sellerTeam"`
}

type Activity struct {
	ID   json.Number     `json:"id"`
	Type int             `json:"type"`
	Date string          `json:"date"`
	Raw  json.RawMessage `json:"-"`
}

// The token is read from the source on every call, so one rotated mid-run is picked up.
type reader struct {
	source   *auth.Source
	scope    config.CacheScope
	priority httpx.Priority
}

func (r reader) get(path string, ttl time.Duration, tag string, store bool, target any) error {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	url := config.APIBase + path + separator + "x-lang=es"

	headers := config.APIHeaders()
	if r.source != nil {
		bearer, err := r.source.Bearer()
		if err != nil {
			return err
		}
		headers["Authorization"] = "Bearer " + bearer
	}
	return httpx.GetJSON(httpx.Request{URL: url, Headers: headers, TTL: ttl, Tag: tag,
		Store: store, Priority: r.priority, Cache: r.scope}, target)
}

func (r reader) getList(path string, ttl time.Duration, tag string, store bool,
	target any) error {
	return r.get(path, ttl, tag, store, &listOf{target})
}

type Global struct{ reader }

func NewGlobal() *Global { return &Global{reader{scope: config.GlobalScope()}} }

func (g *Global) At(priority httpx.Priority) *Global {
	copied := *g
	copied.priority = priority
	return &copied
}

type Account struct {
	reader
	profile config.Profile
}

func AccountOf(profile config.Profile) *Account {
	return &Account{reader{source: auth.For(profile.Account),
		scope: profile.Scope(config.LevelAccount)}, profile}
}

func (a *Account) At(priority httpx.Priority) *Account {
	copied := *a
	copied.priority = priority
	return &copied
}

func (a *Account) Global() *Global {
	return &Global{reader{scope: a.profile.Scope(config.LevelGlobal), priority: a.priority}}
}

func (a *Account) in(league string) config.Profile {
	if a.profile.League == league {
		return a.profile
	}
	return a.profile.In(league)
}

func (a *Account) league(id string) *League {
	return &League{reader{source: a.source, scope: a.in(id).Scope(config.LevelLeague),
		priority: a.priority}, id}
}

// Team is the account's team in a league: from its list of leagues or, when that leaves the team
// out, by its user in the standings.
func (a *Account) Team(league string) (string, error) {
	entries, err := a.Leagues(time.Hour)
	if err != nil {
		return "", err
	}
	found := false
	for _, entry := range entries {
		if text(entry["id"]) != league {
			continue
		}
		found = true
		if team, _ := entry["team"].(map[string]any); text(team["id"]) != "" {
			return text(team["id"]), nil
		}
	}
	if !found {
		return "", fmt.Errorf("la cuenta no esta en la liga %s", league)
	}
	user, err := a.Me(time.Hour)
	if err != nil {
		return "", err
	}
	mine := text(user["id"])
	if mine == "" {
		mine = text(user["userId"])
	}
	rows, err := a.league(league).Standings(time.Hour)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if mine != "" && text(row["userId"]) == mine {
			return text(row["teamId"]), nil
		}
	}
	return "", fmt.Errorf("no encuentro tu equipo en la liga %s", league)
}

func (a *Account) Member(league, team string) (*Member, error) {
	mine, err := a.Team(league)
	if err != nil {
		return nil, err
	}
	if team == "" || mine != team {
		return nil, fmt.Errorf("el equipo %s no es de esta cuenta en la liga %s", team, league)
	}
	profile := a.in(league)
	return &Member{reader: reader{source: a.source, scope: profile.Scope(config.LevelMember),
		priority: a.priority}, account: a, league: a.league(league), team: team,
		profile: profile}, nil
}

type League struct {
	reader
	id string
}

func (l *League) ID() string { return l.id }

func (l *League) At(priority httpx.Priority) *League {
	copied := *l
	copied.priority = priority
	return &copied
}

// Member is one account's team in one league. No method takes a league or a team: a member
// reads only its own.
type Member struct {
	reader
	account *Account
	league  *League
	team    string
	profile config.Profile
}

func (m *Member) LeagueID() string        { return m.league.id }
func (m *Member) TeamID() string          { return m.team }
func (m *Member) Profile() config.Profile { return m.profile }
func (m *Member) Account() *Account       { return m.account }
func (m *Member) League() *League         { return m.league }
func (m *Member) Global() *Global         { return m.account.Global() }

func (m *Member) At(priority httpx.Priority) *Member {
	copied := *m
	copied.priority = priority
	copied.account = m.account.At(priority)
	copied.league = m.league.At(priority)
	return &copied
}

// Invalidate drops each tag in the folder this member reads it from, whatever its level.
func (m *Member) Invalidate(tags ...string) int {
	return httpx.InvalidateIn(m.profile, tags...)
}

func text(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	}
	return fmt.Sprint(value)
}

// unwrap handles the two shapes the API uses for a list: a bare array, or an object
// with the array under "data" or "elements".
func unwrap(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		return raw, nil
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	for _, key := range []string{"data", "elements", "items"} {
		if inner, ok := wrapper[key]; ok {
			return inner, nil
		}
	}
	return json.RawMessage("[]"), nil
}

// listOf decodes a list in either shape inside the one unmarshal, so a cached body of the
// wrong shape fails where httpx can drop it and fetch again.
type listOf struct{ target any }

func (l *listOf) UnmarshalJSON(raw []byte) error {
	list, err := unwrap(raw)
	if err != nil {
		return err
	}
	httpx.Reset(l.target)
	return json.Unmarshal(list, l.target)
}

func (g *Global) CurrentWeek(ttl time.Duration) (Week, error) {
	var week Week
	err := g.get(config.CMP+"/week/current", ttl, "week", false, &week)
	return week, err
}

func (g *Global) Calendar(week int, ttl time.Duration) ([]Match, error) {
	var matches []Match
	err := g.getList(fmt.Sprintf("%s/calendar?weekNumber=%d", config.CMP, week), ttl,
		"calendar", false, &matches)
	return matches, err
}

func (g *Global) TeamsMaster(ttl time.Duration) ([]Team, error) {
	var teams []Team
	// Not under the competition prefix, and it is "teams-master": the plain /teams
	// route answers something else entirely.
	err := g.getList("/v3/teams-master", ttl, "teams", false, &teams)
	return teams, err
}

func (m *Member) Money(ttl time.Duration) (Money, error) {
	var money Money
	err := m.get(fmt.Sprintf("%s/teams/%s/money", config.CMP, m.team), ttl, "money", false,
		&money)
	return money, err
}

// DailyReward is one line of the reward catalogue: what a claim is worth in that kind of
// league and how many a day are allowed.
type DailyReward struct {
	LeagueType string  `json:"leagueType"`
	Money      float64 `json:"money"`
	DailyLimit int     `json:"dailyLimit"`
}

// DailyRewardStatus is how many of today's rewards this team has already taken. It is the only
// authority on that: the league log does not record the daily reward at all.
type DailyRewardStatus struct {
	TeamID   json.Number `json:"teamId"`
	Redeemed int         `json:"dailyRewardsRedeemed"`
}

func (g *Global) DailyRewards(ttl time.Duration) ([]DailyReward, error) {
	var rewards []DailyReward
	err := g.getList(config.CMP+"/daily-reward", ttl, "rewards", false, &rewards)
	return rewards, err
}

// DailyLimit is how many rewards a private league may claim a day; one when the catalogue
// does not say.
func DailyLimit(catalogue []DailyReward) int {
	for _, reward := range catalogue {
		if reward.LeagueType == "private" && reward.DailyLimit > 0 {
			return reward.DailyLimit
		}
	}
	return 1
}

// ErrRewardTaken is today's reward already claimed. The check does not answer a counter of one
// in that case, it answers 400 with this code, so the absence of a body is the answer.
var ErrRewardTaken = errors.New("la recompensa de hoy ya esta cobrada")

const rewardTakenCode = "050.01.04"

// DailyRewardStatus asks whether today's is still there. Never cached: the answer changes the
// moment we claim, and a stale zero would claim twice.
func (m *Member) DailyRewardStatus() (DailyRewardStatus, error) {
	var status DailyRewardStatus
	err := m.get(fmt.Sprintf("%s/league/%s/team/%s/check-daily-reward", config.CMP,
		m.league.id, m.team), 0, "reward", false, &status)
	if err != nil && strings.Contains(err.Error(), rewardTakenCode) {
		return status, ErrRewardTaken
	}
	return status, err
}

// Market: note /league/ singular. See the package comment.
func (m *Member) Market(ttl time.Duration, store bool) ([]Listing, error) {
	var listings []Listing
	err := m.getList(fmt.Sprintf("%s/league/%s/market", config.CMP, m.league.id), ttl,
		"market", store, &listings)
	return listings, err
}

// ActivityRaw keeps the events as they came, because the probe digests the ids and the
// model reads fields this package has no opinion about.
func (l *League) ActivityRaw(index int, ttl time.Duration, store bool) ([]map[string]any,
	error) {
	var events []map[string]any
	err := l.getList(fmt.Sprintf("%s/leagues/%s/activity/%d", config.CMP, l.id, index),
		ttl, "activity", store, &events)
	return events, err
}

// Offers are the offers received for one of our listed players.
//
// The only route that lists them is keyed by playerTeamId — the squad-slot id — because
// /market/{id}/offer is POST-only and answers 405 to a GET.
func (m *Member) Offers(playerTeamID string, ttl time.Duration) ([]map[string]any, error) {
	var offers []map[string]any
	err := m.getList(fmt.Sprintf("%s/league/%s/playerTeam/%s/offer",
		config.CMP, m.league.id, playerTeamID), ttl, "offers", false, &offers)
	return offers, err
}

// PlayerMarketValue is the daily value series for one player.
func (g *Global) PlayerMarketValue(playerID string, ttl time.Duration) ([]map[string]any, error) {
	var series []map[string]any
	err := g.getList(fmt.Sprintf("%s/player/%s/market-value", config.CMP, playerID), ttl,
		"mv", false, &series)
	return series, err
}

// Player is the master record. Its playerStats is the points he scored matchday by matchday --
// the squad payload calls the same series lastStats -- and this is the only public route to it.
func (g *Global) Player(playerID string, ttl time.Duration) (map[string]any, error) {
	var master map[string]any
	err := g.get(fmt.Sprintf("%s/player/%s", config.CMP, playerID), ttl, "player", false,
		&master)
	return master, err
}

func (l *League) TeamSquad(teamID string, ttl time.Duration) (map[string]any, error) {
	var squad map[string]any
	err := l.get(fmt.Sprintf("%s/leagues/%s/teams/%s", config.CMP, l.id, teamID), ttl,
		"squad", false, &squad)
	return squad, err
}

// Favourites is the app's starred list: a bare array of player ids.
func (m *Member) Favourites(ttl time.Duration) ([]string, error) {
	var ids []json.Number
	err := m.getList(fmt.Sprintf("%s/teams/%s/favourite-players", config.CMP, m.team), ttl,
		"favourites", false, &ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out, err
}

func (l *League) Standings(ttl time.Duration) ([]map[string]any, error) {
	var rows []map[string]any
	// Singular: /standing. The plural is a 404.
	err := l.getList(fmt.Sprintf("%s/leagues/%s/standing", config.CMP, l.id), ttl,
		"standing", false, &rows)
	return rows, err
}

func (a *Account) Leagues(ttl time.Duration) ([]map[string]any, error) {
	var leagues []map[string]any
	err := a.getList(config.CMP+"/leagues", ttl, "leagues", false, &leagues)
	return leagues, err
}

// RawList is for the endpoints the model reads generically, where naming every field
// would be a liability: the player master alone has dozens and they change between
// seasons.
func (g *Global) RawList(path string, ttl time.Duration, tag string, target any) error {
	return g.getList(path, ttl, tag, false, target)
}

// MarketRaw keeps the listings as maps, because the model reads nested seller and
// playerMaster fields that this package deliberately does not model.
func (m *Member) MarketRaw(ttl time.Duration, store bool) ([]map[string]any, error) {
	var listings []map[string]any
	err := m.getList(fmt.Sprintf("%s/league/%s/market", config.CMP, m.league.id), ttl,
		"market", store, &listings)
	return listings, err
}

func (a *Account) Me(ttl time.Duration) (map[string]any, error) {
	var me map[string]any
	err := a.get("/v4/user/me", ttl, "me", false, &me)
	return me, err
}

// LineupLines are the four lines of the pitch, in the order the payload uses them.
var LineupLines = []string{"goalkeeper", "defender", "midfield", "striker"}

// OwnLineup is the current eleven. formation.tacticalFormation is the shape itself, e.g.
// [3,4,3], and each slot carries playerMaster.lastStats: points per matchday.
func (m *Member) OwnLineup(ttl time.Duration) (map[string]any, error) {
	var payload map[string]any
	err := m.get(fmt.Sprintf("%s/teams/%s/lineup", config.CMP, m.team), ttl, "own_lineup",
		false, &payload)
	return payload, err
}

// TeamLineupWeek is the eleven a team fielded on a matchday — anybody's team, not just ours.
//
// The plain /lineup route answers 403 for a rival, but this one answers for everybody, which is
// what makes a past matchday readable: ownership says who they had, this says who they played.
func (l *League) TeamLineupWeek(teamID string, week int,
	ttl time.Duration) (map[string]any, error) {
	var payload map[string]any
	err := l.get(fmt.Sprintf("%s/teams/%s/lineup/week/%d", config.CMP, teamID, week), ttl,
		"lineup", false, &payload)
	return payload, err
}

// Formations lists the shapes the account may use; the premium ones need the paid tier.
func (g *Global) Formations(premium bool, ttl time.Duration) ([]string, error) {
	option := "free"
	if premium {
		option = "premium"
	}
	var shapes []string
	err := g.getList("/v4/teams/lineup/formations?option="+option, ttl, "formations",
		false, &shapes)
	return shapes, err
}
