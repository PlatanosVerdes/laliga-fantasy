package model

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// ClauseLock is how long a signing's clause cannot be paid. Measured in docs/clauses.md.
const ClauseLock = 336 * time.Hour

// seenClause is a player's clause as the last build read it, and whose it was.
type seenClause struct {
	Owner  string  `json:"owner"`
	Clause float64 `json:"clause"`
	At     string  `json:"at"`
}

// clauseLog is what is kept between builds: the clauses last seen, and the transfers already
// recognised as clausulazos, so a label never depends on the build that noticed it.
type clauseLog struct {
	Seen        map[string]seenClause `json:"seen"`
	Clausulazos map[string]bool       `json:"clausulazos"`
}

// MarkClausulazos tells clause payments from agreed transfers, which the log does not: it calls
// both "traspaso". A transfer is a clausulazo when it was paid exactly the clause the seller's
// player had at the previous build, or, with nothing remembered, exactly what the seller paid
// for him once his lock was over (a clause nobody raised is the price paid).
func MarkClausulazos(universe *Universe, now time.Time) error {
	log := clauseLog{Seen: map[string]seenClause{}, Clausulazos: map[string]bool{}}
	body, err := os.ReadFile(config.ClauseFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(body, &log); err != nil {
			return err
		}
		if log.Seen == nil {
			log.Seen = map[string]seenClause{}
		}
		if log.Clausulazos == nil {
			log.Clausulazos = map[string]bool{}
		}
	}

	for index := range universe.Activity {
		event := &universe.Activity[index]
		if event.TypeID != 1 || event.Amount == nil || event.PlayerID == nil {
			continue
		}
		id := text(event.Raw["id"])
		if !log.Clausulazos[id] && paidTheClause(universe.Activity, index, log.Seen) {
			log.Clausulazos[id] = true
		}
		event.Clausulazo = log.Clausulazos[id]
	}

	for _, player := range universe.Players {
		if player.OwnerTeamID == nil || player.Clause == nil {
			continue
		}
		owner := ""
		if team := universe.LeagueTeams[*player.OwnerTeamID]; team != nil && team.Manager != nil {
			owner = *team.Manager
		}
		log.Seen[player.ID] = seenClause{Owner: owner, Clause: *player.Clause,
			At: now.Format(time.RFC3339)}
	}
	blob, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	return os.WriteFile(config.ClauseFile, blob, 0o600)
}

func paidTheClause(events []Event, index int, seen map[string]seenClause) bool {
	event := events[index]
	when, err := time.Parse(time.RFC3339, text(event.Raw["createdAt"]))
	if err != nil || event.Seller == nil {
		return false
	}
	if last, ok := seen[*event.PlayerID]; ok && last.Owner == *event.Seller {
		if at, err := time.Parse(time.RFC3339, last.At); err == nil && at.Before(when) {
			return math.Abs(last.Clause-*event.Amount) <= 1
		}
	}
	// The log is newest first, so what came before this transfer is further down.
	for _, earlier := range events[index+1:] {
		if earlier.PlayerID == nil || *earlier.PlayerID != *event.PlayerID ||
			earlier.Buyer == nil || *earlier.Buyer != *event.Seller || earlier.Amount == nil {
			continue
		}
		bought, err := time.Parse(time.RFC3339, text(earlier.Raw["createdAt"]))
		return err == nil && when.Sub(bought) >= ClauseLock &&
			math.Abs(*earlier.Amount-*event.Amount) <= 1
	}
	return false
}
