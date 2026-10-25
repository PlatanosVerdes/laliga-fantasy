// The write half of the API: the two-step guard, the standing instructions and the stars.
//
// Nothing here decides an amount. Every operation is prepared, shown to a person, and only
// then confirmed with a single-use token, which is the whole reason money has never moved
// without somebody asking for it.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/dismissed"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/outcomes"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
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
	if s.throttled(writer, err) {
		slog.Warn("write refused", "status", http.StatusServiceUnavailable, "reason", err.Error())
		return
	}
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, writes.ErrDisabled):
		status = http.StatusForbidden
	case errors.Is(err, writes.ErrLoading):
		status = http.StatusServiceUnavailable
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
	if s.opts.Guard == nil {
		s.json(writer, http.StatusNotImplemented, map[string]any{"error": "sin escrituras"})
		return
	}
	starred := true
	if universe := s.state.Universe(); universe != nil {
		for _, player := range universe.Players {
			if player.ID == id {
				starred = !player.Starred
			}
		}
	}
	args := writes.Args{PlayerID: id, Favourite: starred}
	if _, err := s.opts.Guard.Do("favourite", args, writes.Player{Name: text(body["name"])},
		s.opts.AllowWrites); err != nil {
		s.writeError(writer, err)
		return
	}
	slog.Info("favourite toggled", "player_id", id, "player", text(body["name"]),
		"starred", starred)
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
		entry, err := policies.Open(s.profile()).Set(id, func(policy *policies.Policy) {
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
		entry, err := policies.Open(s.profile()).Set(id, func(policy *policies.Policy) {
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

	armed, err := policies.Open(s.profile()).Load()
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	// The flag and not the entry: with a shield scheduled on him there is an entry either way,
	// and removing it to turn a listing off would take the appointment with it.
	on := true
	if armed[id].AlwaysList {
		err, on = policies.Open(s.profile()).Clear(id, "listing"), false
	} else {
		_, err = policies.Open(s.profile()).Set(id, func(policy *policies.Policy) {
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
	armed, err := policies.Open(s.profile()).Load()
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
		err = policies.Open(s.profile()).Remove(id)
	} else {
		_, err = policies.Open(s.profile()).Set(id, func(policy *policies.Policy) { policy.Raid = false },
			"max_pay")
	}
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("raid cancelled", "player_id", id, "player", text(body["name"]))
	// Written down for the same reason the automatic ones are: the game never knew the order
	// existed, so cancelling it leaves nothing behind unless the panel keeps it.
	ending := outcomes.Order{PlayerID: id, Player: fallback(text(body["name"]), entry.Name),
		Kind: "clausulazo", Outcome: "cancelada", Why: "la cancelaste tu"}
	if entry.MaxPay != nil {
		ending.Amount = *entry.MaxPay
	}
	if err := outcomes.Open(s.profile()).AppendOrders(ending); err != nil {
		slog.Warn("instruction ending not saved", "reason", err.Error())
	}
	s.settle("raid")
	s.json(writer, http.StatusOK, map[string]any{"id": id, "raid": false})
}

// dismiss hides a decision card, or with restore brings them all back. Only the page changes,
// so the world is patched rather than rebuilt.
func (s *Server) dismiss(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	body := s.body(request)
	key := text(body["key"])
	var err error
	switch {
	case truthy(body["restore"]):
		err = dismissed.Open(s.profile()).Clear()
	case key != "":
		err = dismissed.Open(s.profile()).Add(key)
	default:
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "falta la tarjeta"})
		return
	}
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("decision dismissed", "key", key, "restore", truthy(body["restore"]))
	s.state.Patch("dismiss", func(*model.Universe) bool { return true })
	s.json(writer, http.StatusOK, map[string]any{"ok": true})
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
	owner := ""
	for _, row := range s.rows() {
		if text(row["id"]) == id {
			owner = text(row["owner"])
			break
		}
	}
	entry, err := policies.Open(s.profile()).Set(id, func(policy *policies.Policy) {
		policy.Name = text(body["name"])
		policy.Raid = true
		policy.MaxPay = &maxPay
		policy.Owner = owner
	})
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("raid scheduled", "player_id", id, "max_pay", int64(maxPay))
	s.settle("raid")
	s.json(writer, http.StatusOK, entry)
}

// shield schedules the 24h shield on one of your own players for an hour you pick. It spends no
// money — the shield costs an advert — so what is being authorised is the moment, and that is
// the whole reason it is worth scheduling: the cover is worthless during the hours nobody can
// pay a clause anyway.
func (s *Server) shield(writer http.ResponseWriter, request *http.Request) {
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
	when := time.Now()
	if at := strings.TrimSpace(text(body["at"])); at != "" {
		parsed, err := time.Parse(time.RFC3339, at)
		if err != nil {
			s.json(writer, http.StatusBadRequest,
				map[string]any{"error": "no entiendo esa fecha: " + at})
			return
		}
		when = parsed
	}
	at := when.Format(time.RFC3339)
	if budget := s.shieldBudget(when); budget.Known && budget.Left() <= 0 {
		s.json(writer, http.StatusConflict, map[string]any{"error": fmt.Sprintf(
			"la jornada %d ya tiene sus %d blindajes (%s): el juego rechazaria este",
			budget.Round.Week, schedule.ShieldsPerRound, budget.Describe())})
		return
	}
	entry, err := policies.Open(s.profile()).BookShield(id, text(body["name"]), at)
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("shield scheduled", "player_id", id, "at", at)
	s.settle("shield")
	s.json(writer, http.StatusOK, entry)
}

// cancelShield calls off a scheduled shield and leaves every other instruction on that player
// alone.
func (s *Server) cancelShield(writer http.ResponseWriter, request *http.Request) {
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
	drop := func() error { return policies.Open(s.profile()).Clear(id, "shield") }
	if at := text(body["at"]); at != "" {
		drop = func() error { return policies.Open(s.profile()).UnbookShield(id, at) }
	}
	if err := drop(); err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("shield cancelled", "player_id", id)
	s.settle("shield")
	s.json(writer, http.StatusOK, map[string]any{"id": id, "shield": false})
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
		Amount:       int64(amount),
		MarketID:     text(body["market_id"]),
		BidID:        text(body["bid_id"]),
		OfferID:      text(body["offer_id"]),
		PlayerID:     text(body["player_id"]),
		PlayerTeamID: text(body["player_team_id"]),
	}
	// The confirm buttons carry the player id and nothing else, so the slot is resolved here.
	if args.PlayerTeamID == "" {
		args.PlayerTeamID = s.slotFor(args.PlayerID)
	}
	who, _ := visitorOf(request)
	summary, err := s.opts.Guard.PrepareFor(who.key, operation, args,
		s.playerFor(text(body["player_id"]), writes.JudgedByCeiling[operation]), s.opts.AllowWrites)
	if err != nil {
		s.writeError(writer, err)
		return
	}
	s.json(writer, http.StatusOK, summary)
}

// room is what the amount dialog may promise while it is typed: the limit prepare refuses beyond.
func (s *Server) room(writer http.ResponseWriter, request *http.Request) {
	if s.opts.Guard == nil {
		s.json(writer, http.StatusNotImplemented, map[string]any{"error": "sin escrituras"})
		return
	}
	cash := s.opts.Guard.Balance()
	if cash == nil {
		s.json(writer, http.StatusServiceUnavailable, map[string]any{"error": "saldo ilegible"})
		return
	}
	query := request.URL.Query()
	room, spends := writes.RoomFor(query.Get("operation"), *cash,
		s.playerFor(query.Get("player_id"), false))
	if !spends {
		s.json(writer, http.StatusOK, map[string]any{"cash": *cash})
		return
	}
	s.json(writer, http.StatusOK, room)
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
	who, _ := visitorOf(request)
	result, err := s.opts.Guard.ConfirmFor(who.key, text(body["token"]), s.opts.AllowWrites,
		truthy(body["dry_run"]))
	if err != nil {
		if args, ok := result["args"].(writes.Args); ok {
			s.applied(text(result["operation"]), args)
			// Declining an offer that is already declined got what was asked for.
			if text(result["operation"]) == "decline_offer" {
				s.settle("decline_offer")
				s.json(writer, http.StatusOK, map[string]any{"ok": true, "operation": "decline_offer"})
				return
			}
		}
		s.writeError(writer, err)
		return
	}
	cause := text(result["operation"])
	if cause == "" {
		cause = "write"
	}
	// Say what just happened before going to ask: the rebuild takes about five seconds against
	// the API and the person is looking at the screen waiting for the one thing they did. What
	// it did is read off the token, which is the only thing the page sends at this point.
	args, _ := result["args"].(writes.Args)
	s.applied(cause, args)
	// A rebuild would read the league as it really is and undo the pretence at once.
	if !s.opts.Rehearse {
		s.settle(cause)
	}
	s.json(writer, http.StatusOK, result)
}

// applied patches the world with what an operation obviously did, so the page shows it now
// rather than in five seconds. Only the effects that are certain without asking the API: an
// offer that is gone is gone, a bid that changed is that amount. Everything else waits for the
// rebuild, which is the authority.
func (s *Server) applied(operation string, args writes.Args) {
	switch operation {
	case "cancel_bid":
		s.state.Withdrawn(args.MarketID, args.BidID, args.PlayerID)
	case "cancel_offer", "change_offer":
		s.state.Withdrawn(args.MarketID, args.OfferID, args.PlayerID)
	case "accept_offer", "decline_offer":
		s.state.Answered(args.OfferID)
	}
	s.state.Patch(operation, func(universe *model.Universe) bool {
		return patch(universe, operation, args)
	})
}

func patch(universe *model.Universe, operation string, args writes.Args) bool {
	changed := false
	for index := range universe.Players {
		player := &universe.Players[index]
		switch operation {
		case "accept_offer", "decline_offer":
			if args.OfferID == "" {
				continue
			}
			kept := player.Offers[:0]
			for _, offer := range player.Offers {
				if text(offer["id"]) == args.OfferID {
					changed = true
					continue
				}
				kept = append(kept, offer)
			}
			player.Offers = kept

		case "withdraw":
			if listing := player.MarketEntry; listing != nil && listing.MarketID == args.MarketID {
				player.MarketEntry = nil
				changed = true
			}

		case "modify_bid", "change_offer":
			if listing := player.MarketEntry; listing != nil && listing.MarketID == args.MarketID {
				value := float64(args.Amount)
				listing.MyBid = &value
				changed = true
			}

		case "cancel_bid", "cancel_offer":
			if listing := player.MarketEntry; listing != nil && listing.MarketID == args.MarketID {
				listing.MyBidID, listing.MyBid, listing.MyBidStatus = nil, nil, nil
				changed = true
			}
		}
	}
	return changed
}

// readCeiling reads a player's futbolfantasy page for an operation about to be checked, and
// storedCeiling is the last one read, for when it cannot be.
var (
	readCeiling   = futbolfantasy.Interactive.FetchDetail
	storedCeiling = futbolfantasy.StoredDetail
)

// playerFor is the context the confirmation dialog quotes: what he is worth, what the listing
// asks, how long it has left. Read from the last built world, not from the API, so a
// confirmation never waits on the network.
func (s *Server) playerFor(id string, withCeiling bool) writes.Player {
	universe := s.state.Universe()
	if universe == nil || id == "" {
		return writes.Player{}
	}
	for _, player := range universe.Players {
		if player.ID != id {
			continue
		}
		who := writes.Player{Name: player.Name, Value: player.Value,
			SaleLocked: player.SaleLocked, Available: player.Available,
			Shielded: player.Shielded}
		if player.ShieldUntil != nil {
			who.ShieldedUntil = *player.ShieldUntil
		}
		if player.HoldUntil != nil {
			who.HoldUntil = *player.HoldUntil
		}
		who.HoldExceptions = s.holdExceptions()
		who.DebtRoom, who.Committed = s.biddingRoom(universe, id)
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
		// than in the model, so it is read again here, whatever the page drawn behind it holds.
		// A page that cannot be read falls back on the stored ceiling with its age, and without
		// one the ceiling is said to be unknown: read as zero, every bid was warned as
		// unprofitable, which is worse than saying nothing.
		if withCeiling && player.FFID != nil && *player.FFID != "" {
			ceiling, err := readCeiling(*player.FFID)
			if err != nil {
				slog.Warn("futbolfantasy page unread for a write", "player", player.Name,
					"reason", err.Error())
				stored, ok := storedCeiling(*player.FFID)
				ceiling = stored
				if ok {
					who.CeilingAge = stored.Age(time.Now())
				} else {
					who.CeilingUnread = true
				}
			}
			who.IdealBid = int64(ceiling.IdealBid)
		}
		return who
	}
	return writes.Player{}
}

// slotFor is the squad slot a player occupies, which is not his own id: the API addresses a
// clause or a shield by the slot.
func (s *Server) slotFor(id string) string {
	universe := s.state.Universe()
	if universe == nil || id == "" {
		return ""
	}
	for _, player := range universe.Players {
		if player.ID == id && player.PlayerTeamID != nil {
			return *player.PlayerTeamID
		}
	}
	return ""
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

// biddingRoom is the debt the league lets a bid run into, a share of my squad's value, and what
// my live bids and offers on other listings already hold. Without the second, two bids that
// each fit the balance could both win and break the cap together.
func (s *Server) biddingRoom(universe *model.Universe, playerID string) (int64, int64) {
	squad, committed := 0.0, 0.0
	for _, player := range universe.Players {
		if player.IsMine {
			squad += player.Value
		}
		if listing := player.MarketEntry; listing != nil && player.ID != playerID &&
			listing.MyBidID != nil && listing.MyBid != nil {
			committed += *listing.MyBid
		}
	}
	return int64(squad * s.maxDebtPct() / 100), int64(committed)
}
