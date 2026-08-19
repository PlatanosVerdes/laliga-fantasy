package model

import (
	"log/slog"
	"sort"
	"time"
)

// OwnershipAt is who owned whom at a given instant, reconstructed by walking today's squads
// backwards through the transfer log.
//
// Everything here is in **user id** space, not team id: that is what the log carries, and mixing
// the two silently produces a squad that belongs to nobody.
//
// The API has no history: it answers "who owns him now" and nothing else. But every change of
// hands is in the league log with its date, so the past is recoverable by undoing the moves that
// happened after the instant asked about. A purchase after that instant means the buyer did not
// own him yet; a sale means the seller still did.
//
// What cannot be recovered is anything older than the log itself. The season is days old so the
// log reaches the start, and where it does not, the answer is today's owner — stated rather than
// hidden, because a squad quietly wrong about the past is worse than one that admits its limit.
func OwnershipAt(events []Event, current map[string]string, when time.Time) map[string]string {
	owners := make(map[string]string, len(current))
	for playerID, userID := range current {
		owners[playerID] = userID
	}

	// Newest first: undoing has to walk backwards in time.
	ordered := append([]Event{}, events...)
	sort.SliceStable(ordered, func(one, two int) bool {
		return ordered[one].Date > ordered[two].Date
	})

	undone := 0
	for _, event := range ordered {
		if event.PlayerID == nil {
			continue
		}
		at, err := parseTime(event.Date)
		if err != nil || !at.After(when) {
			continue
		}
		switch event.TypeID {
		case 31: // compra: user1 bought him from the game, so before this he had no owner
			delete(owners, *event.PlayerID)
			undone++
		case 33: // venta: user1 sold him to the game, so before this he still owned him
			owners[*event.PlayerID] = event.User1
			undone++
		case 1: // traspaso: user1 paid user2, so before this user2 had him
			if event.User2 != nil {
				owners[*event.PlayerID] = *event.User2
				undone++
			}
		}
	}
	slog.Debug("ownership reconstructed", "at", when.Format(time.RFC3339), "undone", undone,
		"owned", len(owners))
	return owners
}
