// Package api is the LaLiga Fantasy client. Mirrors fantasy/laliga.py, including the
// inconsistency the official app also carries: standings and squads live under
// /leagues/{id}/..., market and buyout under /league/{id}/... Getting that wrong is a
// 404, and it is not a typo in this file.
package api

import (
	"encoding/json"
	"fmt"
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
	ID              string          `json:"id"`
	Discr           string          `json:"discr"`
	SalePrice       json.Number     `json:"salePrice"`
	ExpirationDate  string          `json:"expirationDate"`
	NumberOfBids    *int            `json:"numberOfBids"`
	NumberOfOffers  *int            `json:"numberOfOffers"`
	DirectOffer     bool            `json:"directOffer"`
	PlayerMaster    json.RawMessage `json:"playerMaster"`
	SellerTeam      json.RawMessage `json:"sellerTeam"`
}

type Activity struct {
	ID   json.Number     `json:"id"`
	Type int             `json:"type"`
	Date string          `json:"date"`
	Raw  json.RawMessage `json:"-"`
}

// Client holds nothing but the competition prefix; the session comes from the auth
// package on every call so a rotation mid-run is picked up.
type Client struct{}

func New() *Client { return &Client{} }

func (c *Client) get(path string, authenticated bool, ttl time.Duration, tag string,
	store bool, target any) error {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	url := config.APIBase + path + separator + "x-lang=es"

	headers := config.APIHeaders()
	if authenticated {
		bearer, err := auth.Bearer()
		if err != nil {
			return err
		}
		headers["Authorization"] = "Bearer " + bearer
	}
	return httpx.GetJSON(httpx.Request{
		URL: url, Headers: headers, TTL: ttl, Tag: tag, Store: store}, target)
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

func (c *Client) getList(path string, ttl time.Duration, tag string, store bool,
	target any) error {
	var raw json.RawMessage
	if err := c.get(path, true, ttl, tag, store, &raw); err != nil {
		return err
	}
	list, err := unwrap(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(list, target)
}

func (c *Client) CurrentWeek(ttl time.Duration) (Week, error) {
	var week Week
	err := c.get(config.CMP+"/week/current", false, ttl, "week", false, &week)
	return week, err
}

func (c *Client) Calendar(week int, ttl time.Duration) ([]Match, error) {
	var matches []Match
	err := c.getList(fmt.Sprintf("%s/calendar?weekNumber=%d", config.CMP, week), ttl,
		"calendar", false, &matches)
	return matches, err
}

func (c *Client) TeamsMaster(ttl time.Duration) ([]Team, error) {
	var teams []Team
	// Not under the competition prefix, and it is "teams-master": the plain /teams
	// route answers something else entirely.
	err := c.getList("/v3/teams-master", ttl, "teams", false, &teams)
	return teams, err
}

func (c *Client) Money(teamID string, ttl time.Duration) (Money, error) {
	var money Money
	err := c.get(fmt.Sprintf("%s/teams/%s/money", config.CMP, teamID), true, ttl,
		"money", false, &money)
	return money, err
}

// Market: note /league/ singular. See the package comment.
func (c *Client) Market(leagueID string, ttl time.Duration, store bool) ([]Listing, error) {
	var listings []Listing
	err := c.getList(fmt.Sprintf("%s/league/%s/market", config.CMP, leagueID), ttl,
		"market", store, &listings)
	return listings, err
}

// ActivityRaw keeps the events as they came, because the probe digests the ids and the
// model reads fields this package has no opinion about.
func (c *Client) ActivityRaw(leagueID string, index int, ttl time.Duration,
	store bool) ([]map[string]any, error) {
	var events []map[string]any
	err := c.getList(fmt.Sprintf("%s/leagues/%s/activity/%d", config.CMP, leagueID, index),
		ttl, "activity", store, &events)
	return events, err
}

// PlayerOffers are the offers received for one of our listed players.
//
// The only route that lists them is keyed by playerTeamId — the squad-slot id — because
// /market/{id}/offer is POST-only and answers 405 to a GET.
func (c *Client) PlayerOffers(leagueID, playerTeamID string, ttl time.Duration) ([]map[string]any, error) {
	var offers []map[string]any
	err := c.getList(fmt.Sprintf("%s/league/%s/playerTeam/%s/offer",
		config.CMP, leagueID, playerTeamID), ttl, "offers", false, &offers)
	return offers, err
}

func (c *Client) TeamSquad(leagueID, teamID string, ttl time.Duration) (map[string]any, error) {
	var squad map[string]any
	err := c.get(fmt.Sprintf("%s/leagues/%s/teams/%s", config.CMP, leagueID, teamID),
		true, ttl, "squad", false, &squad)
	return squad, err
}

func (c *Client) Standings(leagueID string, ttl time.Duration) ([]map[string]any, error) {
	var rows []map[string]any
	// Singular: /standing. The plural is a 404.
	err := c.getList(fmt.Sprintf("%s/leagues/%s/standing", config.CMP, leagueID), ttl,
		"standing", false, &rows)
	return rows, err
}

func (c *Client) Leagues(ttl time.Duration) ([]map[string]any, error) {
	var leagues []map[string]any
	err := c.getList(config.CMP+"/leagues", ttl, "leagues", false, &leagues)
	return leagues, err
}

// RawList is for the endpoints the model reads generically, where naming every field
// would be a liability: the player master alone has dozens and they change between
// seasons.
func (c *Client) RawList(path string, ttl time.Duration, tag string, target any) error {
	return c.getList(path, ttl, tag, false, target)
}

// MarketRaw keeps the listings as maps, because the model reads nested seller and
// playerMaster fields that this package deliberately does not model.
func (c *Client) MarketRaw(leagueID string, ttl time.Duration, store bool) ([]map[string]any, error) {
	var listings []map[string]any
	err := c.getList(fmt.Sprintf("%s/league/%s/market", config.CMP, leagueID), ttl,
		"market", store, &listings)
	return listings, err
}

func (c *Client) Me(ttl time.Duration) (map[string]any, error) {
	var me map[string]any
	err := c.get("/v4/user/me", true, ttl, "me", false, &me)
	return me, err
}
