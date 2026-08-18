// Package state is the rendered world plus a version that only moves when something
// changed. Serving is decoupled from refreshing: a failed refresh keeps the last good
// answer up and surfaces in /healthz instead of taking the page down with it.
package state

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
)

// Builder produces the world. Injected so the state can be exercised without a network.
type Builder func() (*model.Universe, error)

// Snapshot is the handful of figures an operation moves, for a before/after comparison.
// Read from the payload already in memory, so taking one costs nothing.
type Snapshot struct {
	Cash       float64 `json:"cash"`
	Squad      int     `json:"squad"`
	SquadValue float64 `json:"squad_value"`
	Listed     int     `json:"listed"`
	Offers     int     `json:"offers"`
	Points     float64 `json:"points"`
	Absences   int     `json:"absences"`
}

// Change is one figure that moved, with both ends of it: a panel that only shows the new
// number is a panel that cannot be judged.
type Change struct {
	Before any     `json:"before"`
	After  any     `json:"after"`
	Delta  float64 `json:"delta"`
}

// Effect is what a rebuild moved, and what caused it.
type Effect struct {
	Operation string            `json:"operation"`
	At        string            `json:"at"`
	Changed   map[string]Change `json:"changed"`
}

// Discrete are the figures that only move when something actually happened. Squad value
// drifts on its own every time the market revalues a player, so it is worth reporting
// after an operation but would cry wolf on an ordinary refresh.
var Discrete = []string{"cash", "squad", "listed", "offers", "points", "absences"}

type State struct {
	builder Builder

	mu          sync.RWMutex
	universe    *model.Universe
	version     int
	generatedAt time.Time
	lastFull    time.Time
	lastError   string
	runs        int
	fingerprint string
	lastEffect  *Effect

	subs   map[chan []byte]bool
	nudged func(string)
}

func New(builder Builder) *State {
	return &State{builder: builder, subs: map[chan []byte]bool{}}
}

// OnFirstWatcher is called when a browser connects and nobody was watching. The engine
// uses it to tighten the cadence immediately rather than at the end of a wait planned for
// an empty room.
func (s *State) OnFirstWatcher(hook func(string)) { s.nudged = hook }

func (s *State) Universe() *model.Universe {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.universe
}

// Payload is the JSON the page and the API read. Keys match the Python server's so the
// same browser code works against either.
func (s *State) Payload() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.universe == nil {
		return map[string]any{}
	}
	return map[string]any{
		"generated_at": s.generatedAt.UTC().Format(time.RFC3339Nano),
		"week":         s.universe.Week,
		"fixtures":     s.universe.Fixtures,
		"market":       s.universe.Market,
		"activity":     s.universe.Activity,
		"cash_model":   s.universe.CashModel,
		"league_teams": s.universe.LeagueTeams,
		"players":      s.universe.Players,
		"my_team_id":   s.universe.MyTeamID,
		"version":      s.version,
	}
}

// SchedulePayload is the narrow view the scheduler reads.
func (s *State) SchedulePayload() schedule.Payload {
	s.mu.RLock()
	universe := s.universe
	s.mu.RUnlock()
	if universe == nil {
		return schedule.Payload{}
	}

	payload := schedule.Payload{
		Week: schedule.Week{WeekNumber: universe.Week.WeekNumber,
			ClosingWeekDate: universe.Week.ClosingWeekDate},
	}
	for _, listing := range universe.Market {
		payload.Market = append(payload.Market,
			schedule.Listing{PlayerID: listing.PlayerID, Expires: listing.Expires})
	}
	for _, fixture := range universe.Fixtures {
		payload.Fixtures = append(payload.Fixtures, schedule.Fixture{
			Kickoff: fixture.Kickoff, State: fixture.State, LocalID: fixture.LocalID,
			VisitorID: fixture.VisitorID, Local: fixture.Local, Visitor: fixture.Visitor})
	}
	for _, player := range universe.Players {
		row := schedule.Player{ID: player.ID, Name: player.Name, TeamID: player.TeamID,
			IsMine: player.IsMine, ClauseUntil: player.ClauseUntil}
		for _, offer := range player.Offers {
			expires, _ := offer["expirationDate"].(string)
			if expires != "" {
				value := expires
				row.Offers = append(row.Offers, schedule.Offer{ExpirationDate: &value})
			}
		}
		payload.Players = append(payload.Players, row)
	}
	return payload
}

func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	shot := Snapshot{}
	if s.universe == nil {
		return shot
	}
	for _, team := range s.universe.LeagueTeams {
		if s.universe.MyTeamID != nil && team.TeamID == *s.universe.MyTeamID {
			shot.Cash = team.EstimatedCash
		}
	}
	for _, player := range s.universe.Players {
		if !player.IsMine {
			continue
		}
		shot.Squad++
		shot.SquadValue += player.Value
		shot.Offers += len(player.Offers)
		shot.Points += player.SeasonPoints
		if player.Absence != nil {
			shot.Absences++
		}
	}
	for _, listing := range s.universe.Market {
		if listing.IsMine {
			shot.Listed++
		}
	}
	return shot
}

// Difference is only what actually moved, so an empty result means the rebuild changed
// nothing worth telling anybody about.
func Difference(before, after Snapshot, keys []string) map[string]Change {
	pick := func(shot Snapshot, key string) float64 {
		switch key {
		case "cash":
			return shot.Cash
		case "squad":
			return float64(shot.Squad)
		case "squad_value":
			return shot.SquadValue
		case "listed":
			return float64(shot.Listed)
		case "offers":
			return float64(shot.Offers)
		case "points":
			return shot.Points
		case "absences":
			return float64(shot.Absences)
		}
		return 0
	}
	changed := map[string]Change{}
	for _, key := range keys {
		left, right := pick(before, key), pick(after, key)
		if left != right {
			changed[key] = Change{Before: left, After: right, Delta: right - left}
		}
	}
	return changed
}

// fingerprint is what counts as a change worth telling the browser about.
func fingerprint(universe *model.Universe) string {
	if universe == nil {
		return ""
	}
	rows := make([]string, 0, len(universe.Market))
	for _, listing := range universe.Market {
		rows = append(rows, listing.MarketID)
	}
	blob, _ := json.Marshal(map[string]any{
		"market": rows, "players": len(universe.Players),
		"week": universe.Week.WeekNumber,
	})
	sum := sha1.Sum(blob)
	return hex.EncodeToString(sum[:])
}

// Refresh rebuilds and, when something changed, tells every connected browser.
func (s *State) Refresh(cause string) error {
	return s.RefreshWith(cause, false)
}

// RefreshWith can force the version to move even when the world came back identical.
// /refresh is a person pressing a button and expecting the page to react: staying silent
// because the fingerprint matched would look like the button is broken.
func (s *State) RefreshWith(cause string, force bool) error {
	before := s.Snapshot()
	universe, err := s.builder()
	if err != nil {
		s.mu.Lock()
		s.lastError = err.Error()
		s.mu.Unlock()
		slog.Error("refresh failed", "cause", cause, "reason", err.Error())
		return err
	}

	print := fingerprint(universe)
	s.mu.Lock()
	changed := force || print != s.fingerprint
	s.universe = universe
	s.generatedAt = time.Now()
	s.lastFull = s.generatedAt
	s.lastError = ""
	s.runs++
	if changed {
		s.fingerprint = print
		s.version++
	}
	version := s.version
	s.mu.Unlock()

	after := s.Snapshot()
	if diff := Difference(before, after, Discrete); len(diff) > 0 {
		effect := &Effect{Operation: cause, At: time.Now().UTC().Format(time.RFC3339),
			Changed: diff}
		s.mu.Lock()
		s.lastEffect = effect
		s.mu.Unlock()
		slog.Info("world moved", "cause", cause, "changed", keys(diff))
		s.publish(map[string]any{"type": "effect", "version": version,
			"operation": effect.Operation, "at": effect.At, "changed": effect.Changed})
	}

	if changed {
		s.publish(map[string]any{"type": "state", "version": version,
			"generated_at": s.generatedAt.UTC().Format(time.RFC3339)})
	}
	return nil
}

func keys(changed map[string]Change) []string {
	out := make([]string, 0, len(changed))
	for key := range changed {
		out = append(out, key)
	}
	return out
}

// --- push -------------------------------------------------------------------

func (s *State) Subscribe() chan []byte {
	channel := make(chan []byte, 8)
	s.mu.Lock()
	first := len(s.subs) == 0
	s.subs[channel] = true
	s.mu.Unlock()
	if first && s.nudged != nil {
		s.nudged("se ha abierto la pagina")
	}
	return channel
}

func (s *State) Unsubscribe(channel chan []byte) {
	s.mu.Lock()
	delete(s.subs, channel)
	s.mu.Unlock()
}

func (s *State) Watchers() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subs)
}

func (s *State) publish(message map[string]any) {
	blob, err := json.Marshal(message)
	if err != nil {
		return
	}
	s.mu.RLock()
	channels := make([]chan []byte, 0, len(s.subs))
	for channel := range s.subs {
		channels = append(channels, channel)
	}
	s.mu.RUnlock()

	for _, channel := range channels {
		select {
		case channel <- blob:
		default: // a browser that cannot keep up will catch up on its next poll
		}
	}
}

// Health is what /healthz answers.
type Health struct {
	Status      string  `json:"status"`
	Version     int     `json:"version"`
	GeneratedAt *string `json:"generated_at"`
	AgeSeconds  *int    `json:"age_seconds"`
	Runs        int     `json:"runs"`
	Subscribers int     `json:"subscribers"`
	LastError   *string `json:"last_error"`
	LastEffect  *Effect `json:"last_effect"`
	Frozen      bool    `json:"frozen"`
}

func (s *State) Health() Health {
	s.mu.RLock()
	defer s.mu.RUnlock()
	health := Health{Version: s.version, Runs: s.runs, Subscribers: len(s.subs),
		LastEffect: s.lastEffect, Status: "degraded", Frozen: httpx.Frozen}
	if !s.generatedAt.IsZero() && s.lastError == "" {
		health.Status = "ok"
	}
	if !s.generatedAt.IsZero() {
		stamp := s.generatedAt.UTC().Format(time.RFC3339)
		age := int(time.Since(s.generatedAt).Seconds())
		health.GeneratedAt, health.AgeSeconds = &stamp, &age
	}
	if s.lastError != "" {
		reason := s.lastError
		health.LastError = &reason
	}
	return health
}

func (s *State) LastFull() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastFull
}
