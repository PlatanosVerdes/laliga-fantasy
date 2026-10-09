// Necroporra is the league's side game: each round you name the two teams you think finish
// last. The data lives in an external app (tebasfury), so this package only holds what the
// panel needs to predict the bottom two and cast your own vote: the ballot's gameweek, the
// name-to-id roster, and the session that authorises the vote.
//
// The cookie and action id expire, so they are read from a file (or the environment) and
// never compiled in. A missing or empty config disables voting; the prediction still renders.
package necroporra

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

type Config struct {
	// Cookie is the better-auth session value (without the name=), ActionID the server-action
	// hash. Both expire: when the vote 303s to login, they must be re-captured from the browser.
	Cookie   string `json:"cookie"`
	ActionID string `json:"action_id"`
	Gameweek int    `json:"gameweek"`
	// Roster maps a manager's name, as the standings show it, to his tebasfury team id.
	Roster map[string]string `json:"roster"`
}

// CanVote is whether the config carries enough to cast a vote.
func (c Config) CanVote() bool {
	return c.Cookie != "" && c.ActionID != "" && c.Gameweek > 0 && len(c.Roster) > 0
}

func file() string {
	if path := os.Getenv("FANTASY_NECROPORRA"); path != "" {
		return path
	}
	if config.ConfigDir != "" {
		if path := filepath.Join(config.ConfigDir, "necroporra.json"); exists(path) {
			return path
		}
	}
	return filepath.Join("data", "necroporra.json")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Load reads the config. A read error returns the zero Config, which disables voting without
// failing the page.
func Load() Config {
	var cfg Config
	body, err := os.ReadFile(file())
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(body, &cfg)
	return cfg
}

// State is the live half the config cannot know: which round is open right now and what you
// have already voted. Read from the external app, so a vote always targets the open gameweek
// rather than a number written by hand.
type State struct {
	Gameweek int
	Deadline string
	ClosesAt time.Time
	Chosen   []string
}

// Voted is whether the current round already carries a pick.
func (s State) Voted() bool { return len(s.Chosen) >= 2 }

var (
	reGameweek = regexp.MustCompile(`"gameweek":(\d+),"teams":\[\{"id":"\d+","managerName"`)
	reChosen   = regexp.MustCompile(`"chosen":\[([^\]]*)\]`)
	reDeadline = regexp.MustCompile(`"children":"(Round \d+ [^"]+)"`)
	reID       = regexp.MustCompile(`"(\d+)"`)
)

// FetchState reads the open round. The response is cached briefly so the panel's repaint loop
// does not hammer the external app, and a read failure falls back to the configured gameweek
// rather than breaking the page. The call also keeps the sliding session alive.
func FetchState(cfg Config) State {
	state := State{Gameweek: cfg.Gameweek}
	if cfg.Cookie == "" {
		return state
	}
	body, err := httpx.Fetch(httpx.Request{
		URL:    "https://tebasfury.vercel.app/necroporra",
		Method: http.MethodGet,
		Headers: map[string]string{
			"rsc":    "1",
			"accept": "text/x-component",
			"cookie": "__Secure-better-auth.session_token=" + cfg.Cookie,
		},
		Tag: "necro-state",
		TTL: 30 * time.Second,
	})
	if err != nil {
		return state
	}
	if match := reGameweek.FindStringSubmatch(body); match != nil {
		if gw, err := strconv.Atoi(match[1]); err == nil {
			state.Gameweek = gw
		}
	}
	if match := reDeadline.FindStringSubmatch(body); match != nil {
		state.Deadline = match[1]
		state.ClosesAt = parseCloses(match[1])
	}
	if match := reChosen.FindStringSubmatch(body); match != nil {
		for _, id := range reID.FindAllStringSubmatch(match[1], -1) {
			state.Chosen = append(state.Chosen, id[1])
		}
	}
	return state
}

// parseCloses turns "Round 8 — closes Fri 09 Oct, 21:00" into a time. The label carries no
// year or zone, so the league's zone and the current year are assumed; a label that rolls into
// next year (parsed month already past) is pushed forward. A parse failure returns the zero
// time, which only disables the countdown and the auto-vote, never the page.
func parseCloses(label string) time.Time {
	idx := strings.Index(label, "closes ")
	if idx < 0 {
		return time.Time{}
	}
	zone, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		zone = time.Local
	}
	when, err := time.ParseInLocation("Mon 02 Jan, 15:04", label[idx+len("closes "):], zone)
	if err != nil {
		return time.Time{}
	}
	now := time.Now().In(zone)
	when = time.Date(now.Year(), when.Month(), when.Day(), when.Hour(), when.Minute(), 0, 0, zone)
	if when.Before(now.Add(-180 * 24 * time.Hour)) {
		when = when.AddDate(1, 0, 0)
	}
	return when
}

// Ranked is a manager's team in the weakest-to-strongest order the side game reads: his best
// eleven's expected points, his season points, and the tebasfury id to vote him with.
type Ranked struct {
	TeamID, Name, NecroID string
	Predicted             float64
	SeasonPoints          float64
}

// Predict ranks the league from likeliest-last to likeliest-first by each manager's best
// eleven (the lineup that actually scores). Your own team is left out: you cannot vote for
// yourself. The input is the generic universe the advice layer and the page already share.
func Predict(universe map[string]any, roster map[string]string, myTeamID string) []Ranked {
	teams := asMap(universe["league_teams"])
	if teams == nil {
		return nil
	}
	xpts := map[string][]float64{}
	for _, player := range asList(universe["players"]) {
		row := asMap(player)
		if owner := str(row["owner_team_id"]); owner != "" {
			xpts[owner] = append(xpts[owner], num(row["xpts"]))
		}
	}
	out := make([]Ranked, 0, len(teams))
	for key, value := range teams {
		team := asMap(value)
		if team == nil {
			continue
		}
		teamID := str(team["team_id"])
		if teamID == "" {
			teamID = key
		}
		if teamID == myTeamID {
			continue
		}
		name := str(team["name"])
		if name == "" {
			name = str(team["manager"])
		}
		if name == "" {
			name = teamID
		}
		out = append(out, Ranked{
			TeamID:       teamID,
			Name:         name,
			NecroID:      roster[name],
			Predicted:    bestEleven(xpts[teamID]),
			SeasonPoints: num(team["points"]),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Predicted != out[j].Predicted {
			return out[i].Predicted < out[j].Predicted
		}
		return out[i].SeasonPoints < out[j].SeasonPoints
	})
	return out
}

// Picks is the two weakest votable teams: the suggestion, and what the auto-vote casts.
func Picks(ranked []Ranked) []Ranked {
	out := make([]Ranked, 0, 2)
	for _, team := range ranked {
		if team.NecroID == "" {
			continue
		}
		out = append(out, team)
		if len(out) == 2 {
			break
		}
	}
	return out
}

func bestEleven(xpts []float64) float64 {
	sorted := append([]float64(nil), xpts...)
	sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))
	total := 0.0
	for i := 0; i < len(sorted) && i < 11; i++ {
		total += sorted[i]
	}
	return total
}

const (
	voteURL   = "https://tebasfury.vercel.app/necroporra"
	stateTree = "%5B%22%22%2C%7B%22children%22%3A%5B%22(portal)%22%2C%7B%22children%22%3A%5B%22necroporra%22%2C%7B%22children%22%3A%5B%22__PAGE__%22%2C%7B%7D%2Cnull%2Cnull%2C4096%5D%7D%2Cnull%2Cnull%2C4096%5D%7D%2Cnull%2Cnull%2C4096%5D%7D%2Cnull%2Cnull%2C4112%5D"
)

// CastVote replays the "save picks" server action with your session. It reports loggedOut when
// the session has expired (the app bounces the request to its login page) so the caller can
// tell "rejected" from "network failed".
func CastVote(cfg Config, gameweek int, teamA, teamB string) (loggedOut bool, err error) {
	if cfg.Cookie == "" || cfg.ActionID == "" {
		return false, fmt.Errorf("necroporra sin configurar")
	}
	boundary := fmt.Sprintf("----tfNecro%d", time.Now().UnixNano())
	answer, err := httpx.Fetch(httpx.Request{
		URL:    voteURL,
		Method: http.MethodPost,
		Headers: map[string]string{
			"accept":                 "text/x-component",
			"content-type":           "multipart/form-data; boundary=" + boundary,
			"cookie":                 "__Secure-better-auth.session_token=" + cfg.Cookie,
			"next-action":            cfg.ActionID,
			"next-router-state-tree": stateTree,
			"origin":                 "https://tebasfury.vercel.app",
			"referer":                voteURL,
		},
		Body:    []byte(voteBody(boundary, gameweek, teamA, teamB)),
		Tag:     fmt.Sprintf("necro-vote-%d", time.Now().UnixNano()),
		Retries: 1,
	})
	if err != nil {
		return false, err
	}
	if strings.Contains(answer, "Sign in") || strings.Contains(answer, "/login") {
		return true, fmt.Errorf("sesión de la necroporra caducada")
	}
	return false, nil
}

func voteBody(boundary string, gameweek int, a, b string) string {
	part := func(name, value string) string {
		return "--" + boundary + "\r\nContent-Disposition: form-data; name=\"" + name +
			"\"\r\n\r\n" + value + "\r\n"
	}
	return part("_1_gameweek", fmt.Sprintf("%d", gameweek)) +
		part("_1_teamId", a) + part("_1_teamId", b) +
		part("0", `["$K1"]`) + "--" + boundary + "--\r\n"
}

func asMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

func asList(value any) []any {
	list, _ := value.([]any)
	return list
}

func str(value any) string {
	s, _ := value.(string)
	return s
}

func num(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}
