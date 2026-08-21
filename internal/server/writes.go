// The write half of the API: the two-step guard, the standing instructions and the stars.
//
// Nothing here decides an amount. Every operation is prepared, shown to a person, and only
// then confirmed with a single-use token, which is the whole reason money has never moved
// without somebody asking for it.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"


	"github.com/PlatanosVerdes/laliga-fantasy/internal/favourites"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

func (s *Server) body(request *http.Request) map[string]any {
	body := map[string]any{}
	raw, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil || len(raw) == 0 {
		return body
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return map[string]any{}
	}
	return body
}

// writeError maps the guard's refusals onto status codes: 403 when writes are off, 400 when
// the operation itself is wrong.
func (s *Server) writeError(writer http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, writes.ErrDisabled) {
		status = http.StatusForbidden
	}
	// A refusal is the most informative thing the write path produces: it is the tool saying
	// no to a person, and if it is wrong nobody finds out unless it is written down.
	slog.Warn("write refused", "status", status, "reason", err.Error())
	// The API says the page is behind: rebuild, or the row that cannot be acted on stays there
	// inviting the next click.
	if errors.Is(err, writes.ErrOutdated) {
		s.settle("desfasado")
	}
	s.json(writer, status, map[string]any{"error": err.Error()})
}

// settle rebuilds after a write so the page shows what actually happened rather than what was
// asked for. In the background: the API takes a moment to agree with itself.
func (s *Server) settle(cause string) {
	if s.opts.Settle != nil {
		go s.opts.Settle(cause)
	}
}

func (s *Server) favourite(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	body := s.body(request)
	id := text(body["id"])
	if id == "" {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "falta el id"})
		return
	}
	starred, err := favourites.Toggle(id, body["name"])
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("favourite toggled", "player_id", id, "player", text(body["name"]),
		"starred", starred)
	// The star is ours, not the API's: it can be true on the page before any rebuild.
	s.state.Patch("favourite", func(universe *model.Universe) bool {
		for index := range universe.Players {
			if universe.Players[index].ID == id {
				universe.Players[index].Starred = starred
				return true
			}
		}
		return false
	})
	s.settle("favourite")
	s.json(writer, http.StatusOK, map[string]any{"id": id, "starred": starred})
}

// always is the standing instruction: keep him listed, and optionally what to accept.
//
// The auto-sell switch has its own key so it can be flipped without touching the amounts,
// and so turning it off never looks like clearing the instruction entirely.
func (s *Server) always(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	body := s.body(request)
	id := text(body["id"])
	if id == "" {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "falta el id"})
		return
	}
	name := text(body["name"])

	if _, ok := body["auto_sell"]; ok {
		entry, err := policies.Set(id, func(policy *policies.Policy) {
			policy.Name = name
			policy.AlwaysList = true
			policy.AutoSell = truthy(body["auto_sell"])
		})
		if err != nil {
			s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		slog.Info("instruction set", "player_id", id, "player", name,
			"always_listed", true, "auto_sell", entry.AutoSell)
		s.settle("always")
		s.json(writer, http.StatusOK, map[string]any{"id": id, "always_listed": true,
			"auto_sell": entry.AutoSell, "min_price": entry.MinPrice,
			"accept_above": entry.AcceptAbove})
		return
	}

	_, hasMin := body["min_price"]
	_, hasAccept := body["accept_above"]
	if hasMin || hasAccept {
		amounts := map[string]float64{}
		var unset []string
		for _, key := range []string{"min_price", "accept_above"} {
			raw, ok := body[key]
			if !ok {
				continue
			}
			amount, valid := amountOf(raw)
			if !valid {
				s.json(writer, http.StatusBadRequest,
					map[string]any{"error": key + " no es un importe"})
				return
			}
			// Zero means clear it, not "accept anything above nothing".
			if amount > 0 {
				amounts[key] = amount
			} else {
				unset = append(unset, key)
			}
		}
		entry, err := policies.Set(id, func(policy *policies.Policy) {
			policy.Name = name
			policy.AlwaysList = true
			if amount, ok := amounts["min_price"]; ok {
				policy.MinPrice = &amount
			}
			if amount, ok := amounts["accept_above"]; ok {
				policy.AcceptAbove = &amount
			}
		}, unset...)
		if err != nil {
			s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		slog.Info("instruction amounts set", "player_id", id, "player", name,
			"min_price", entry.MinPrice, "accept_above", entry.AcceptAbove,
			"cleared", unset)
		s.settle("always")
		s.json(writer, http.StatusOK, map[string]any{"id": id, "always_listed": true,
			"min_price": entry.MinPrice, "accept_above": entry.AcceptAbove})
		return
	}

	armed, err := policies.Load()
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	on := true
	if _, exists := armed[id]; exists {
		err, on = policies.Remove(id), false
	} else {
		_, err = policies.Set(id, func(policy *policies.Policy) {
			policy.Name = name
			policy.AlwaysList = true
		})
	}
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("instruction toggled", "player_id", id, "player", name, "always_listed", on)
	s.settle("always")
	s.json(writer, http.StatusOK, map[string]any{"id": id, "always_listed": on})
}

// raid arms a clause payment for the moment the shield drops, capped at what you said you
// would pay. Storing the cap is the point: the price can move between arming and firing.
// cancelRaid calls off a scheduled clause payment. It keeps whatever else the instruction says:
// somebody can have a player both always-listed and raid-armed, and calling off the raid is not
// calling off the rest.
func (s *Server) cancelRaid(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	body := s.body(request)
	id := text(body["id"])
	if id == "" {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "falta el id"})
		return
	}
	armed, err := policies.Load()
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	entry, exists := armed[id]
	if !exists || !entry.Raid {
		s.json(writer, http.StatusOK, map[string]any{"id": id, "raid": false})
		return
	}

	// Nothing else armed on him means the whole entry goes, so the file does not fill up with
	// instructions that say nothing.
	if !entry.AlwaysList && entry.MinPrice == nil && entry.AcceptAbove == nil && !entry.AutoSell {
		err = policies.Remove(id)
	} else {
		_, err = policies.Set(id, func(policy *policies.Policy) { policy.Raid = false }, "max_pay")
	}
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("raid cancelled", "player_id", id, "player", text(body["name"]))
	s.settle("raid")
	s.json(writer, http.StatusOK, map[string]any{"id": id, "raid": false})
}

func (s *Server) raid(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	body := s.body(request)
	id := text(body["id"])
	maxPay, ok := amountOf(body["max_pay"])
	if id == "" || !ok || maxPay <= 0 {
		s.json(writer, http.StatusBadRequest,
			map[string]any{"error": "falta el id o el pago maximo"})
		return
	}
	entry, err := policies.Set(id, func(policy *policies.Policy) {
		policy.Name = text(body["name"])
		policy.Raid = true
		policy.MaxPay = &maxPay
	})
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("raid scheduled", "player_id", id, "max_pay", int64(maxPay))
	s.settle("raid")
	s.json(writer, http.StatusOK, entry)
}

// prepare is step one of every money operation: it builds the call, checks it against the
// current cash and squad, and hands back a token plus everything a person needs to judge it.
func (s *Server) prepare(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	if s.opts.Guard == nil {
		s.json(writer, http.StatusNotImplemented, map[string]any{"error": "sin escrituras"})
		return
	}
	body := s.body(request)
	amount, _ := amountOf(body["amount"])
	operation := text(body["operation"])
	if operation == "" {
		operation = "bid"
	}
	args := writes.Args{
		LeagueID:     s.opts.LeagueID,
		TeamID:       s.opts.MyTeamID,
		Amount:       int64(amount),
		MarketID:     text(body["market_id"]),
		BidID:        text(body["bid_id"]),
		OfferID:      text(body["offer_id"]),
		PlayerID:     text(body["player_id"]),
		PlayerTeamID: text(body["player_team_id"]),
	}
	summary, err := s.opts.Guard.Prepare(operation, args, s.playerFor(text(body["player_id"])),
		s.opts.AllowWrites)
	if err != nil {
		s.writeError(writer, err)
		return
	}
	s.json(writer, http.StatusOK, summary)
}

// confirm is step two: it spends the token, sends the call, and then makes the world catch up.
func (s *Server) confirm(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	if s.opts.Guard == nil {
		s.json(writer, http.StatusNotImplemented, map[string]any{"error": "sin escrituras"})
		return
	}
	body := s.body(request)
	result, err := s.opts.Guard.Confirm(text(body["token"]), s.opts.AllowWrites,
		truthy(body["dry_run"]))
	if err != nil {
		s.writeError(writer, err)
		return
	}
	cause := text(result["operation"])
	if cause == "" {
		cause = "write"
	}
	// Say what just happened before going to ask: the rebuild takes about five seconds against
	// the API and the person is looking at the screen waiting for the one thing they did.
	s.applied(cause, body)
	s.settle(cause)
	s.json(writer, http.StatusOK, result)
}

// applied patches the world with what an operation obviously did, so the page shows it now
// rather than in five seconds. Only the effects that are certain without asking the API: an
// offer that is gone is gone, a bid that changed is that amount. Everything else waits for the
// rebuild, which is the authority.
func (s *Server) applied(operation string, body map[string]any) {
	offerID := text(body["offer_id"])
	marketID := text(body["market_id"])
	amount, _ := amountOf(body["amount"])

	s.state.Patch(operation, func(universe *model.Universe) bool {
		changed := false
		for index := range universe.Players {
			player := &universe.Players[index]
			switch operation {
			case "accept_offer", "decline_offer":
				if offerID == "" {
					continue
				}
				kept := player.Offers[:0]
				for _, offer := range player.Offers {
					if text(offer["id"]) == offerID {
						changed = true
						continue
					}
					kept = append(kept, offer)
				}
				player.Offers = kept

			case "withdraw":
				if listing := player.MarketEntry; listing != nil && listing.MarketID == marketID {
					player.MarketEntry = nil
					changed = true
				}

			case "modify_bid":
				if listing := player.MarketEntry; listing != nil && listing.MarketID == marketID {
					value := amount
					listing.MyBid = &value
					changed = true
				}

			case "cancel_bid":
				if listing := player.MarketEntry; listing != nil && listing.MarketID == marketID {
					listing.MyBidID, listing.MyBid, listing.MyBidStatus = nil, nil, nil
					changed = true
				}
			}
		}
		return changed
	})
}

// playerFor is the context the confirmation dialog quotes: what he is worth, what the listing
// asks, how long it has left. Read from the last built world, not from the API, so a
// confirmation never waits on the network.
func (s *Server) playerFor(id string) writes.Player {
	universe := s.state.Universe()
	if universe == nil || id == "" {
		return writes.Player{}
	}
	for _, player := range universe.Players {
		if player.ID != id {
			continue
		}
		who := writes.Player{Name: player.Name, Value: player.Value,
			SaleLocked: player.SaleLocked, Available: player.Available}
		if player.HoldUntil != nil {
			who.HoldUntil = *player.HoldUntil
		}
		who.HoldExceptions = s.opts.HoldExceptions
		if player.Clause != nil {
			who.Clause = int64(*player.Clause)
		}
		if listing := player.MarketEntry; listing != nil {
			who.MinBid = int64(listing.MinBid)
			if listing.Bids != nil {
				who.Bids = *listing.Bids
			}
			if listing.Expires != nil {
				who.Expires = *listing.Expires
			}
			if listing.MyBidID != nil {
				who.MyBidID = *listing.MyBidID
			}
			if listing.MyBid != nil {
				who.MyBid = int64(*listing.MyBid)
			}
			who.DirectOffer = listing.DirectOffer
		}
		if player.Owner != nil {
			who.Owner = *player.Owner
		}
		// The profitable ceiling is futbolfantasy's, and it lives on their player page rather
		// than in the model, so it is read here. Cached for a day: without it every bid was
		// warned as unprofitable, which is worse than saying nothing.
		if player.FFID != nil && *player.FFID != "" {
			if detail, err := futbolfantasy.PlayerDetail(*player.FFID, futbolfantasy.DetailTTL); err == nil {
				who.IdealBid = int64(number(detail["ideal_bid"]))
			}
		}
		return who
	}
	return writes.Player{}
}

func amountOf(value any) (float64, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case bool:
		return 0, false
	case string:
		clean := strings.TrimSpace(typed)
		if clean == "" {
			return 0, true
		}
		amount, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			return 0, false
		}
		return amount, true
	}
	return 0, false
}
