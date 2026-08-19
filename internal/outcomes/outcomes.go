// Package outcomes remembers how my bids and offers ended.
//
// The API answers what is pending and nothing else: an offer that is refused simply stops being
// there. So the ending has to be inferred from what changed around it, and written down, because
// nobody can go back and ask. "JMjugon rechazo tus 13,6M por De Galarreta" is not recoverable
// tomorrow if it is not saved today.
package outcomes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// Keep is how many endings are kept. Enough to answer "what happened this week" without the file
// growing without bound.
const Keep = 200

// Ending is one resolved bid or offer.
type Ending struct {
	At       string  `json:"at"`
	PlayerID string  `json:"player_id"`
	Player   string  `json:"player"`
	Amount   float64 `json:"amount"`
	// Kind is "puja" for the game's own market and "oferta" for a rival's sale.
	Kind string `json:"kind"`
	// Outcome is one of: aceptada, rechazada, perdida, caducada.
	Outcome string `json:"outcome"`
	// Who is the manager on the other side, when there is one.
	Who string `json:"who,omitempty"`
	// NewOwner is who ended up with him, when somebody did.
	NewOwner string `json:"new_owner,omitempty"`
}

func path() string { return filepath.Join(config.StateDir, "offer_endings.json") }

func Load() []Ending {
	body, err := os.ReadFile(path())
	if err != nil {
		return nil
	}
	var out []Ending
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	// Newest first: that is the order they are read in.
	sort.SliceStable(out, func(one, two int) bool { return out[one].At > out[two].At })
	return out
}

// Append writes new endings, newest first, keeping the file bounded.
func Append(fresh ...Ending) error {
	if len(fresh) == 0 {
		return nil
	}
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	all := append(fresh, Load()...)
	if len(all) > Keep {
		all = all[:Keep]
	}
	blob, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), blob, 0o600)
}

// Pending is one of my bids or offers as it stood on the previous pass: what has to be remembered
// to be able to tell how it ended.
type Pending struct {
	PlayerID string
	Player   string
	Amount   float64
	Kind     string
	Seller   string
}

// Now is what the world says about that player on this pass.
type Now struct {
	StillPending bool
	Listed       bool
	MineNow      bool
	Owner        string
}

// Classify is how a bid or offer that is no longer pending ended.
//
// Four endings, and telling them apart is the whole point: a refusal says something about the
// rival, losing an auction says something about the price, and a listing that simply closed says
// nothing about either.
func Classify(before Pending, after Now, when time.Time) *Ending {
	if after.StillPending {
		return nil
	}
	ending := Ending{
		At: when.Format(time.RFC3339), PlayerID: before.PlayerID, Player: before.Player,
		Amount: before.Amount, Kind: before.Kind, Who: before.Seller,
	}
	switch {
	case after.MineNow:
		ending.Outcome = "aceptada"
	case after.Owner != "" && after.Owner != before.Seller:
		// Somebody else has him now: in the free market that is being outbid, and on a rival's
		// sale it is being sold to somebody else, which is a refusal with a destination.
		ending.Outcome = "perdida"
		ending.NewOwner = after.Owner
	case after.Listed:
		// The listing is still open and the offer is gone: he said no.
		ending.Outcome = "rechazada"
	default:
		ending.Outcome = "caducada"
	}
	return &ending
}
