// Player detail and the pitch. Both merge the live API with the built world, so a shirt or a
// drawer always carries the same numbers as the tables.
package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/advice"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/eleven"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/matching"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/render"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

// detail answers one player: everything the drawer shows, plus what can be done with him
// right now. The actions are computed here rather than in the browser because only the server
// knows the current market, offers and clause state — and because the page must never offer a
// button the API would refuse.
func (s *Server) detail(writer http.ResponseWriter, request *http.Request) {
	id := strings.TrimPrefix(request.URL.Path, "/api/player/")
	universe := s.state.Universe()
	if universe == nil {
		s.json(writer, http.StatusServiceUnavailable, map[string]any{"error": "generando"})
		return
	}
	rows := s.rows()
	var player map[string]any
	for _, row := range rows {
		if text(row["id"]) == id {
			player = row
			break
		}
	}
	if player == nil {
		// Coaches are in the game (positionId 5) and even appear in the market, but they are
		// excluded from the analysis, so say that rather than 404 blankly.
		s.json(writer, http.StatusNotFound, map[string]any{
			"error": "sin datos para este id: puede ser un entrenador, que el juego lista " +
				"pero el analisis no cubre"})
		return
	}

	// The profitable ceiling lives on futbolfantasy's page, not in the model, so the drawer
	// only knows it if the server puts it here: without it the dialog reads "sin margen" and
	// warns about every amount, however small.
	// The link is the page the ceiling was read from, so it comes with it rather than from a
	// slug of his name, which a short or shared name gets wrong.
	if ffID := text(player["ff_id"]); ffID != "" {
		if detail, err := futbolfantasy.PlayerDetail(ffID, futbolfantasy.DetailTTL); err == nil {
			if _, present := player["ideal_bid"]; !present {
				player["ideal_bid"] = number(detail["ideal_bid"])
			}
			if url, ok := detail["ff_url"].(*string); ok && url != nil {
				player["ff_url"] = *url
			}
		}
	}

	// Neither the hierarchy nor, for some players, the starting probability is in the market list.
	// Reading the page costs a request, so only the player being looked at gets one.
	var ffMatches []map[string]any
	if name := fallback(text(player["ff_name"]), text(player["name"])); name != "" {
		slug := matching.SlugifyFF(name)
		if url := text(player["ff_url"]); url != "" {
			slug = url[strings.LastIndex(url, "/")+1:]
		}
		if page, err := futbolfantasy.PlayerPageFor(slug, text(player["ff_id"]),
			futbolfantasy.DetailTTL); err == nil {
			if _, known := player["ff_url"]; !known {
				player["ff_url"] = strings.ReplaceAll(config.FFPlayerURL, "{slug}", slug)
			}
			if rank := page["hierarchy"]; rank != nil {
				player["hierarchy"] = text(rank)
				player["hierarchy_rank"] = number(page["hierarchy_rank"])
			}
			ffMatches = listOf(page["matches"])
			if chance := page["start_probability"]; chance != nil &&
				player["start_probability"] == nil {
				player["start_probability"] = number(chance)
				player["start_probability_source"] = "ficha"
				if week := page["start_week"]; week != nil {
					player["start_week"] = number(week)
				}
			}
		}
	}

	armed, _ := policies.Load()
	listing := mapOf(player["market"])
	offers := listOf(player["offers"])
	clause := number(player["clause"])
	budget := s.budget()
	actions := s.actions(player, rows, armed, listing, offers, clause, budget)
	if s.clauseRecommended(rows, player, budget) {
		for _, action := range actions {
			if op := text(action["op"]); op == "raid" || op == "pay_clause" {
				action["recommended"] = true
			}
		}
	}

	// Points matchday by matchday: an average of 9.8 hides whether it was five 9.8s or a 14 and
	// three sevens, and the shape is what a card is opened for. The opponents come off the
	// futbolfantasy page already read above, so they cost no request of their own.
	weeks := []map[string]any{}
	if s.opts.Client != nil {
		if master, err := s.opts.Client.Player(id, 6*time.Hour); err == nil {
			// The row lists both sides in the order they played, so at home the opponent is
			// the second name and away it is the first.
			rivals := map[float64]string{}
			for _, match := range ffMatches {
				teams, ok := match["teams"].([]string)
				if !ok || len(teams) != 2 {
					continue
				}
				if truthy(match["home"]) {
					rivals[number(match["week"])] = teams[1]
				} else {
					rivals[number(match["week"])] = teams[0]
				}
			}
			for _, week := range model.OneRowPerWeek(listOf(master["playerStats"])) {
				row := map[string]any{"week": week["weekNumber"], "points": week["totalPoints"],
					"ideal": truthy(week["isInIdealFormation"])}
				if rival := rivals[number(week["weekNumber"])]; rival != "" {
					row["rival"] = rival
				}
				weeks = append(weeks, row)
			}
		}
	}
	weeks = addForecasts(id, weeks)

	history := []map[string]any{}
	if s.opts.Client != nil {
		if series, err := s.opts.Client.PlayerMarketValue(id, 24*time.Hour); err == nil {
			for _, point := range series {
				date := text(point["date"])
				if len(date) > 10 {
					date = date[:10]
				}
				history = append(history,
					map[string]any{"date": date, "value": point["marketValue"]})
			}
			if len(history) > 90 {
				history = history[len(history)-90:]
			}
		}
	}

	recommended := recommendedBuy(rows, player)
	groupActions(actions, truthy(player["is_mine"]))
	markPrimary(actions, recommended)
	window := s.state.ClauseWindow(time.Now())
	s.json(writer, http.StatusOK, map[string]any{"player": player, "offers": offers,
		"listing": listing, "actions": actions, "history": history, "weeks": weeks,
		"writes_enabled": s.opts.AllowWrites, "recommended": recommended,
		"row": render.RowPlayer(player),
		// What the card needs to know about the league and the page used to stamp in itself.
		"facts": map[string]any{"window_open": window.Open, "opens": window.OpensAt,
			"closes": window.ClosesAt, "hold_except": s.opts.HoldExceptions}})
}

// actionGroups is the topic each button is shown under in the card.
var actionGroups = map[string]string{
	"accept_offer": "oferta", "decline_offer": "oferta",
	"always": "mercado", "sell_to_market": "mercado", "withdraw": "mercado",
	"raise_clause": "clausula", "shield": "clausula", "cancel_shield": "clausula",
	"bid": "fichar", "modify_bid": "fichar", "cancel_bid": "fichar", "buy_offer": "fichar",
	"cancel_offer": "fichar", "direct_offer": "fichar", "raid": "fichar", "pay_clause": "fichar",
}

// groupActions files every action under its topic. A note says only where it was written, so
// one without a topic of its own goes with the clause for my players and with signing for
// anybody else's.
func groupActions(actions []map[string]any, mine bool) {
	for _, action := range actions {
		if text(action["group"]) != "" {
			continue
		}
		group := actionGroups[text(action["op"])]
		if group == "" {
			group = "fichar"
			if mine {
				group = "clausula"
			}
		}
		action["group"] = group
	}
}

// markPrimary picks the one button the card fills: the panel's top recommendation for him, or
// none. An offer comes first, because it expires; then a signing the panel recommends.
func markPrimary(actions []map[string]any, recommendedBuy bool) {
	var pick map[string]any
	var best, bestTaken map[string]any
	for _, action := range actions {
		if text(action["op"]) != "accept_offer" || text(action["why"]) == "" {
			continue
		}
		if best == nil || number(action["amount"]) > number(best["amount"]) {
			best = action
		}
		if truthy(action["take"]) &&
			(bestTaken == nil || number(action["amount"]) > number(bestTaken["amount"])) {
			bestTaken = action
		}
	}
	switch {
	case bestTaken != nil:
		pick = bestTaken
	case best != nil:
		for _, action := range actions {
			if text(action["op"]) == "decline_offer" &&
				text(action["offer_id"]) == text(best["offer_id"]) {
				pick = action
				action["why"] = best["why"]
			}
		}
	}
	for _, action := range actions {
		if pick != nil || truthy(action["blocked"]) {
			continue
		}
		op := text(action["op"])
		if recommendedBuy && (op == "bid" || op == "buy_offer" || op == "direct_offer") {
			pick = action
		}
	}
	for _, action := range actions {
		if pick == nil && truthy(action["recommended"]) && !truthy(action["blocked"]) {
			pick = action
		}
	}
	if pick != nil {
		pick["primary"] = true
	}
}

func (s *Server) actions(player map[string]any, rows []map[string]any,
	armed map[string]policies.Policy, listing map[string]any, offers []map[string]any,
	clause, budget float64) []map[string]any {
	id := text(player["id"])
	actions := []map[string]any{}
	policy := armed[id]
	// Having an entry is not being always-listed: a scheduled shield is an entry too, and
	// reading the map instead of the flag turned the toggle on for a player nobody had listed.
	standing := policy.AlwaysList

	// The house rule decides what can even be offered: a button the league forbids is worse
	// than no button, because it looks like the tool disagrees with the pact.
	locked := truthy(player["sale_locked"])
	until := text(player["hold_until"])
	if len(until) > 10 {
		until = until[:10]
	}

	switch {
	case truthy(player["is_mine"]):
		floor, source := policies.GoodOfferFloor(player, policy)
		label := "Siempre en mercado"
		if standing {
			label = "Quitar de siempre-en-mercado"
		}
		actions = append(actions, map[string]any{"op": "always", "label": label,
			"kind": "toggle", "on": standing,
			"min_price": policy.MinPrice, "accept_above": policy.AcceptAbove,
			"auto_sell": policy.AutoSell,
			"asking":    int64(number(listing["min_bid"])),
			"value":     int64(number(player["value"])),
			// The bar the check would use, and which reference set it: a switch whose
			// number is invisible is a switch nobody can judge.
			"good_floor": floor, "good_source": source,
			"room": policies.SquadRoom(rows, int(number(player["position_id"])))})

		if marketID := text(listing["market_id"]); marketID != "" {
			actions = append(actions, map[string]any{"op": "withdraw",
				"label": "Quitar del mercado", "kind": "confirm", "market_id": marketID})
		} else if locked {
			actions = append(actions, map[string]any{"op": "note", "kind": "note",
				"group": "mercado",
				"label": "Lo fichaste hace poco: la norma de la liga no deja venderlo hasta " +
					"el " + until})
		} else {
			actions = append(actions, map[string]any{"op": "sell_to_market",
				"label": "Poner en venta", "kind": "amount",
				"suggested":      int64(number(player["value"])),
				"player_team_id": player["player_team_id"]})
		}
		for _, offer := range offers {
			amount := int64(number(offer["money"]))
			// Whose money, and since when: two offers for the same player differ in nothing
			// else, and the automatic one arrives every day whatever you do.
			who := text(offer["from"])
			if who == "" {
				who = "el mercado"
			}
			from := "de " + who
			if who == "el mercado" {
				from = "del mercado"
			}
			label := fmt.Sprintf("Aceptar %s %s", short(float64(amount)), from)
			note := ""
			if made := text(offer["createdAt"]); made != "" {
				note = "ofrecida " + made[:16]
			}
			if expires := text(offer["expirationDate"]); expires != "" {
				if note != "" {
					note += " · "
				}
				note += "caduca " + expires[:16]
			}
			take, why := offerAdvice(rows, id, float64(amount), number(player["value"]), budget,
				s.state.ClauseWindow(time.Now()), s.rivalBank())
			actions = append(actions,
				map[string]any{"op": "accept_offer", "label": label, "kind": "confirm",
					"offer_id": text(offer["id"]), "market_id": listing["market_id"],
					"amount": amount, "note": note, "take": take, "why": why,
					"created": text(offer["createdAt"]), "expires": text(offer["expirationDate"]),
					"from": who, "from_market": truthy(offer["from_market"])},
				map[string]any{"op": "decline_offer",
					"label": "Rechazar la " + from, "kind": "confirm", "danger": true,
					"offer_id": text(offer["id"]), "market_id": listing["market_id"]})
		}
		// What it would take to put the clause where the advice stops calling it a risk, and
		// nothing when it is already there. Half his market value was a number with no argument
		// behind it, and it contradicted the same page two sections up: over SafeMargin nobody in
		// the league gains by paying it, so there is nothing to buy.
		actions = append(actions, map[string]any{"op": "raise_clause",
			"label": "Subir cláusula", "kind": "amount",
			"player_team_id": player["player_team_id"],
			"safe_margin":    advice.SafeMargin,
			"suggested":      raiseToSafe(number(player["value"]), number(player["clause"]))})

		// The shield lasts 24h and lapses on its own, so what is left to decide is when the next
		// ones start: they are booked in a queue, two per matchday at most.
		shielded := truthy(player["shielded"])
		if shielded {
			actions = append(actions, map[string]any{"op": "note", "kind": "note",
				"label":    "Blindado: nadie puede pagar su cláusula",
				"deadline": player["shielded_until"]})
		}
		for _, stamp := range policy.ShieldTimes() {
			label := "Blindaje programado"
			if when, err := time.Parse(time.RFC3339, stamp); err == nil {
				label += " para el " + when.Local().Format("02/01 a las 15:04")
			}
			actions = append(actions,
				map[string]any{"op": "note", "kind": "note", "label": label, "deadline": stamp},
				map[string]any{"op": "cancel_shield", "kind": "prompt", "danger": true,
					"label": "Cancelar este", "player_id": id, "at": stamp})
		}
		// One button, because buying it and scheduling it are the same decision taken at
		// different hours. The hour suggested is when clauses can be paid again; with the
		// window open it is now, or when the current shield runs out.
		suggested, because := "", ""
		if window := s.state.ClauseWindow(time.Now()); !window.Open {
			suggested, because = window.OpensAt, "window"
		}
		if until := text(player["shielded_until"]); shielded && until > suggested {
			suggested, because = until, "shield"
		}
		actions = append(actions, shieldActions(id, player["player_team_id"], shielded,
			suggested, because, time.Now(), s.shieldBudget)...)

	case text(listing["kind"]) == "libre":
		suggested := number(player["ideal_bid"])
		if suggested == 0 {
			suggested = number(listing["min_bid"])
		}
		actions = append(actions, bidActions(listing, suggested)...)

	case text(player["owner"]) != "":
		owner := text(player["owner"])
		if truthy(player["shielded"]) {
			actions = append(actions, map[string]any{"op": "note", "kind": "note",
				"label": owner + " lo tiene blindado: no se puede clausular"})
		} else {
			suggested := 0.0
			if policy.MaxPay != nil {
				suggested = *policy.MaxPay
			} else if clause > 0 {
				suggested = clause * 1.2
			} else {
				suggested = number(player["value"]) * 1.5
			}
			label := "Programar clausulazo"
			if policy.Raid {
				label = "Cambiar clausulazo programado"
			}
			actions = append(actions, map[string]any{"op": "raid", "kind": "amount",
				"label": label, "player_id": id, "on": policy.Raid,
				"suggested": int64(suggested),
				"note": "Se paga en cuanto se libere la clausula, y solo si sigue por " +
					"debajo de este importe."})
		}
		// A direct offer goes against his listing: with no listing there is nothing to
		// make an offer on.
		if locked {
			actions = append(actions, map[string]any{"op": "note", "kind": "note",
				"label": owner + " lo ficho hace poco: la norma no le deja venderlo hasta el " +
					until + ", asi que solo se llega a el por clausula"})
		} else if marketID := text(listing["market_id"]); marketID != "" {
			suggested := number(listing["min_bid"])
			if suggested == 0 {
				suggested = number(player["value"])
			}
			// A direct offer needs the seller to accept them. The listing says so, and when it
			// says no the API answers a bare 403: offering the button anyway was offering an
			// operation that cannot work.
			if accepts, said := listing["direct_offer"].(bool); said && !accepts {
				actions = append(actions, map[string]any{"op": "note", "kind": "note",
					"label": owner + " no acepta ofertas directas: solo por lo que tenga " +
						"puesto en venta"})
			} else {
				actions = append(actions,
					map[string]any{"op": "direct_offer", "label": "Ofrecer a " + owner,
						"kind": "amount", "market_id": marketID, "suggested": int64(suggested)})
			}
			// Not the asking price: an offer under what the owner paid or under the clause he
			// is protected by gets refused, so the field opens at what a yes costs.
			realistic, floor := advice.RealCost(player)
			offers := bidActions(listing, realistic)
			if floor != "" && len(offers) > 0 && text(offers[0]["op"]) == "buy_offer" {
				offers[0]["note"] = "Le llega como oferta de compra y decide el. Te sugiero " +
					thousands(int64(realistic)) + ", que es " + floor + ": por debajo no suele " +
					"aceptar nadie."
			}
			actions = append(actions, offers...)
		} else {
			actions = append(actions, map[string]any{"op": "note", "kind": "note",
				"label": owner + " no lo tiene en venta: solo se le puede pagar la cláusula"})
		}
		if clause > 0 && !truthy(player["clause_locked"]) {
			actions = append(actions, map[string]any{"op": "pay_clause",
				"label": "Pagar cláusula " + short(clause),
				"kind":  "amount", "player_team_id": player["player_team_id"],
				"suggested": int64(clause), "min": int64(clause),
				"blocked": clause > budget})
		}
	}
	return actions
}

// bidActions is what you can do about a listing you do not own. The API refuses a second bid
// on the same listing with a bare 400, so once one exists the only honest options are to
// change it or to take it back — offering "Pujar" again would be offering a button that
// cannot work.
// shieldActions is the shield button for a player of yours. With the round's two shields
// spent, "Blindar 24h" would only be refused, so it says so and offers the next round instead.
func shieldActions(id string, slot any, shielded bool, suggested, because string, now time.Time,
	budgetAt func(time.Time) shieldBudget) []map[string]any {
	at := now
	if when, err := time.Parse(time.RFC3339, suggested); err == nil {
		at = when
	}
	budget := budgetAt(at)
	label := "Blindar 24h"
	if shielded {
		label = "Programar otro blindaje"
	}
	nowAllowed := !shielded
	actions := []map[string]any{}
	if budget.Known && budget.Left() <= 0 {
		spent := len(budget.Used) + len(budget.Booked)
		note := fmt.Sprintf("Blindajes de la J%d: %d de %d usados", budget.Round.Week, spent,
			budget.Limit)
		if len(budget.Booked) > 0 {
			note += fmt.Sprintf(" (%d programado", len(budget.Booked))
			if len(budget.Booked) > 1 {
				note += "s"
			}
			note += ")"
		}
		actions = append(actions, map[string]any{"op": "note", "kind": "note", "label": note})
		found := false
		for range 3 {
			at = budget.Round.End.Add(time.Minute)
			budget = budgetAt(at)
			if !budget.Known || budget.Left() > 0 {
				found = true
				break
			}
		}
		if !found {
			return actions
		}
		label, nowAllowed = "Programar otro blindaje", false
		suggested, because = at.Format(time.RFC3339), "round"
	}
	return append(actions, map[string]any{"op": "shield", "kind": "prompt",
		"label": label, "player_id": id, "player_team_id": slot,
		"suggested": suggested, "because": because, "now_allowed": nowAllowed,
		"budget": budget})
}

func bidActions(listing map[string]any, suggested float64) []map[string]any {
	marketID := text(listing["market_id"])
	minBid := int64(number(listing["min_bid"]))
	// The game's own market is bid on; a rival's sale is offered for. Sending a bid for the
	// second answers 404 and reads like a bug in the tool.
	if text(listing["kind"]) == "venta" {
		// One offer at a time: the API refuses a second and there is no route to change one, so
		// the only honest button is to take it back.
		if existing := text(listing["my_bid_id"]); existing != "" {
			amount := int64(number(listing["my_bid"]))
			return []map[string]any{{"op": "cancel_offer",
				"label": "Retirar tu oferta de " + short(float64(amount)),
				"kind": "confirm", "danger": true, "market_id": marketID, "offer_id": existing,
				"note": "No se puede cambiar una oferta: se retira y se hace otra."}}
		}
		return []map[string]any{{"op": "buy_offer", "label": "Ofertar por su venta",
			"kind": "amount", "market_id": marketID, "suggested": int64(suggested),
			"min": minBid, "note": "Le llega como oferta de compra y decide el."}}
	}
	if bidID := text(listing["my_bid_id"]); bidID != "" {
		mine := int64(number(listing["my_bid"]))
		if suggested <= float64(mine) {
			suggested = float64(mine)
		}
		return []map[string]any{
			{"op": "modify_bid", "label": "Cambiar tu puja de " + short(float64(mine)),
				"kind": "amount", "market_id": marketID, "bid_id": bidID,
				"suggested": int64(suggested), "min": minBid,
				"bids": listing["bids"], "expires": listing["expires"],
				"note": "Ya tienes una puja puesta: se sustituye por el nuevo importe."},
			{"op": "cancel_bid", "label": "Cancelar tu puja", "kind": "confirm",
				"danger": true, "market_id": marketID, "bid_id": bidID},
		}
	}
	return []map[string]any{{"op": "bid", "label": "Pujar", "kind": "amount",
		"market_id": marketID, "suggested": int64(suggested), "min": minBid,
		"bids": listing["bids"], "expires": listing["expires"]}}
}

// lineup is the pitch: who starts where, who sits, and each one's recent form. Merged with
// the analysis so a shirt carries the same numbers as its row in every table.
func (s *Server) lineup(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodPost {
		s.saveLineup(writer, request)
		return
	}
	if s.opts.Client == nil || s.opts.MyTeamID == "" {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "sin equipo resuelto"})
		return
	}
	payload, err := s.opts.Client.Lineup(s.opts.MyTeamID, time.Minute)
	if err != nil {
		s.json(writer, http.StatusBadGateway,
			map[string]any{"error": "no he podido leer la alineacion: " + err.Error()})
		return
	}
	free, _ := s.opts.Client.Formations(false, 24*time.Hour)
	premium, _ := s.opts.Client.Formations(true, 24*time.Hour)

	known := map[string]map[string]any{}
	rows := s.rows()
	for _, row := range rows {
		known[text(row["id"])] = row
	}
	formation := mapOf(payload["formation"])

	lines := map[string][]map[string]any{}
	starters := map[string]bool{}
	for _, line := range api.LineupLines {
		group := []map[string]any{}
		for _, slot := range listOf(formation[line]) {
			shirt := shirtOf(slot, known)
			starters[text(shirt["id"])] = true
			group = append(group, shirt)
		}
		lines[line] = group
	}
	padLines(lines, shapeOf(formation["tacticalFormation"]))

	// The lineup payload carries the points series only for the eleven on the pitch; the squad
	// payload carries it for the whole squad, and it is what gives a reserve his form back.
	masters := map[string]map[string]any{}
	if squad, err := s.opts.Client.TeamSquad(s.opts.LeagueID, s.opts.MyTeamID,
		30*time.Minute); err == nil {
		for _, key := range []string{"players", "playersTeams", "teamPlayers"} {
			for _, held := range listOf(squad[key]) {
				master := mapOf(held["playerMaster"])
				if id := text(master["id"]); id != "" {
					masters[id] = master
				}
			}
		}
	}

	// The payload's bench comes back empty, so the reserves are simply the rest of the
	// squad, rebuilt into the same shirt shape as the starters.
	bench := []map[string]any{}
	for _, row := range rows {
		if !truthy(row["is_mine"]) || starters[text(row["id"])] {
			continue
		}
		master := map[string]any{
			"id": row["id"], "nickname": row["name"], "positionId": row["position_id"],
			"teamId": row["team_id"], "marketValue": row["value"],
			"playerStatus": row["status"], "lastStats": []any{},
		}
		if held := masters[text(row["id"])]; held != nil {
			master["lastStats"] = held["lastStats"]
			master["averagePoints"] = held["averagePoints"]
			master["lastSeasonPoints"] = held["lastSeasonPoints"]
		}
		bench = append(bench, shirtOf(map[string]any{
			"playerTeamId": row["player_team_id"],
			"playerMaster": master}, known))
	}

	s.json(writer, http.StatusOK, map[string]any{"lines": lines, "bench": bench,
		"best":      bestLineup(rows),
		"formation": formation["tacticalFormation"],
		"formations": map[string]any{"free": free, "premium": premium},
		"updated_at": payload["updatedAt"], "writes_enabled": s.opts.AllowWrites})
}

// recommendedBuy is whether bidding for him is what the panel recommends: he improves the best
// eleven and his price is within futbolfantasy's ceiling. Same rule as the Comprar boxes.
func recommendedBuy(rows []map[string]any, player map[string]any) bool {
	listing := mapOf(player["market"])
	if truthy(player["is_mine"]) || text(listing["market_id"]) == "" {
		return false
	}
	cost := number(listing["min_bid"])
	if ceiling := number(player["ideal_bid"]); ceiling <= 0 || ceiling < cost {
		return false
	}
	var mine []map[string]any
	for _, row := range rows {
		if truthy(row["is_mine"]) {
			mine = append(mine, row)
		}
	}
	return elevenTotal(append(mine, player))-elevenTotal(mine) > render.MinShownGain
}

// clauseRecommended is whether paying his clause is what the panel recommends: the advice rates
// it "chollo" or "renta" and he improves the best eleven. Same rule as the Comprar list.
func (s *Server) clauseRecommended(rows []map[string]any, player map[string]any,
	budget float64) bool {
	if truthy(player["is_mine"]) || text(player["owner"]) == "" || truthy(player["shielded"]) {
		return false
	}
	blob, err := json.Marshal(s.state.Universe())
	if err != nil {
		return false
	}
	var universe map[string]any
	if json.Unmarshal(blob, &universe) != nil {
		return false
	}
	buckets := advice.Recommend(universe, budget, 0, len(rows))
	rated := false
	for _, key := range []string{"raids", "upcoming_raids"} {
		for _, raid := range listOf(buckets[key]) {
			if text(raid["id"]) == text(player["id"]) {
				verdict := text(raid["verdict"])
				rated = rated || verdict == "chollo" || verdict == "renta"
			}
		}
	}
	if !rated {
		return false
	}
	var mine []map[string]any
	for _, row := range rows {
		if truthy(row["is_mine"]) {
			mine = append(mine, row)
		}
	}
	return elevenTotal(append(mine, player))-elevenTotal(mine) > render.MinShownGain
}

func elevenTotal(squad []map[string]any) float64 {
	players := []eleven.Player{}
	points := map[string]float64{}
	for _, row := range squad {
		id := text(row["id"])
		players = append(players, eleven.Player{ID: id, Name: text(row["name"]),
			Position: int(number(row["position_id"])), XPts: number(row["xpts"]),
			Available: true})
		points[id] = number(row["xpts"])
	}
	choice, ok := eleven.Best(players)
	if !ok {
		return 0
	}
	total := 0.0
	for _, id := range choice.IDs() {
		total += points[id]
	}
	return total
}

// bestLineup is the best legal eleven of my squad, in the lineup's own lines, so the editor can
// offer it next to the saved one.
func bestLineup(rows []map[string]any) map[string]any {
	players := []eleven.Player{}
	points := map[string]float64{}
	for _, row := range rows {
		if !truthy(row["is_mine"]) {
			continue
		}
		id := text(row["id"])
		players = append(players, eleven.Player{ID: id, Name: text(row["name"]),
			Position: int(number(row["position_id"])), XPts: number(row["xpts"]),
			Available: truthy(row["available"])})
		points[id] = number(row["xpts"])
	}
	choice, ok := eleven.Best(players)
	if !ok {
		return nil
	}
	total := 0.0
	for _, id := range choice.IDs() {
		total += points[id]
	}
	return map[string]any{
		"formation": []int{choice.Shape.Need[2], choice.Shape.Need[3], choice.Shape.Need[4]},
		"lines": map[string][]string{"goalkeeper": {choice.Keeper}, "defender": choice.Defence,
			"midfield": choice.Middle, "striker": choice.Attack},
		"xpts": total,
	}
}

// nested walks a chain of keys, because the images live three levels down and any of them can
// be missing.
func nested(source map[string]any, keys ...string) any {
	var current any = source
	for _, key := range keys {
		row, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = row[key]
	}
	return current
}

func shirtOf(slot map[string]any, known map[string]map[string]any) map[string]any {
	master := mapOf(slot["playerMaster"])
	id := text(master["id"])
	extra := known[id]
	played := model.OneRowPerWeek(listOf(master["lastStats"]))
	weeks := []map[string]any{}
	total := 0.0
	for _, week := range played {
		weeks = append(weeks,
			map[string]any{"week": week["weekNumber"], "points": week["totalPoints"]})
		total += number(week["totalPoints"])
	}
	// The feed's own average divides by the rows it published, so a matchday sent four times
	// drags it down: Unai López came back as 3.9 instead of 5.8. Its number is the fallback for
	// a shirt whose series never arrived.
	average := master["averagePoints"]
	if len(played) > 0 {
		average = total / float64(len(played))
	}
	pick := func(first, second string) any {
		if value, ok := master[first]; ok && value != nil {
			return value
		}
		return extra[second]
	}
	// The API publishes a transparent cutout per player; it is the fastest way to recognise a
	// shirt on the pitch, and the crest stays in the corner for the fixture.
	face := text(nested(master, "images", "transparent", "256x256"))
	if face == "" {
		// The bench is rebuilt from our own rows, which carry the face the squad payload gave
		// us: without this the reserves are the only shirts without a photo.
		face = text(extra["image"])
	}
	return map[string]any{
		"player_team_id": text(slot["playerTeamId"]),
		"id":             id,
		"image":          face,
		"name":           fallback(text(master["nickname"]), text(master["name"])),
		"position_id":    master["positionId"],
		"team_id":        text(pick("teamId", "team_id")),
		"value":          pick("marketValue", "value"),
		"status":         pick("playerStatus", "status"),
		"week_points":    master["weekPoints"],
		"average":        average,
		"last_season_points": master["lastSeasonPoints"],
		"weeks":              weeks,
		// from the analysis, so the pitch agrees with the tables
		"xpts":              extra["xpts"],
		// Whether he can play at all, decided in the model from the official status, so the
		// pitch and the engine never disagree about who is on the pitch for nothing.
		"available":         extra["available"],
		"projected_pct":     extra["projected_pct"],
		"start_probability": extra["start_probability"],
		"next_rival":        extra["next_rival"],
		"next_home":         extra["next_home"],
		"starred":           extra["starred"],
		"absence":           extra["absence"],
		"shielded":          extra["shielded"],
		"shielded_until":    extra["shielded_until"],
		"sale_locked":       extra["sale_locked"],
		"hold_until":        extra["hold_until"],
		"listed_for":        mapOf(extra["market"])["min_bid"],
		"best_offer":        bestOffer(extra),
	}
}

// offerAdvice is the card's verdict on an offer: at 1.02x his value or more, and only when the
// best eleven loses at most one xPts without him.
func offerAdvice(rows []map[string]any, id string, amount, value, cash float64,
	window schedule.Window, rivals advice.RivalBank) (bool, string) {
	if value <= 0 {
		return false, ""
	}
	ratio := amount / value
	words := "×" + strings.Replace(fmt.Sprintf("%.2f", ratio), ".", ",", 1) + " su valor"
	without := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if text(row["id"]) != id {
			without = append(without, row)
		}
	}
	drop := number(bestLineup(rows)["xpts"]) - number(bestLineup(without)["xpts"])
	switch {
	case ratio >= policies.GoodOverValue && drop <= 1:
		return true, words
	case ratio >= policies.GoodOverValue:
		// Selling him is fine when the money takes somebody else's player who covers him.
		if move, ok := advice.SwapForSale(rows, id, amount, cash, window, time.Now(), nil,
			rivals); ok {
			return true, fmt.Sprintf("%s si clausulas a %s (%s xPts)", words,
				text(move.In["name"]), signedOne(move.Gain))
		}
		return false, fmt.Sprintf("%s pero tu once pierde %s xPts", words,
			strings.Replace(fmt.Sprintf("%.1f", drop), ".", ",", 1))
	}
	return false, words + ": no compensa"
}

func signedOne(value float64) string {
	sign := "+"
	if value < 0 {
		sign = "−"
	}
	return sign + strings.Replace(fmt.Sprintf("%.1f", math.Abs(value)), ".", ",", 1)
}

func bestOffer(row map[string]any) any {
	best := 0.0
	for _, offer := range listOf(row["offers"]) {
		if amount := number(offer["money"]); amount > best {
			best = amount
		}
	}
	if best == 0 {
		return nil
	}
	return best
}

// fragments serves the page in pieces so a repaint replaces the sections that changed instead
// of the whole document, which is what lets the pitch keep unsaved changes.
func (s *Server) fragments(writer http.ResponseWriter, _ *http.Request) {
	if s.opts.Page == nil {
		s.json(writer, http.StatusServiceUnavailable, map[string]any{"error": "sin pagina"})
		return
	}
	// The balance travels with the fragments: it is the number every button on the page is
	// judged against, and the live refresh only replaces sections, so it used to sit there
	// stale until somebody reloaded by hand.
	s.json(writer, http.StatusOK, map[string]any{"version": s.state.Health().Version,
		"cash": s.budget(), "sections": Sections(s.render().HTML)})
}

// budget is the cash the actions are judged against. Read from the API rather than the built
// world so a blocked button is blocked on the real balance.
func (s *Server) budget() float64 {
	if s.opts.Client == nil || s.opts.MyTeamID == "" {
		return 0
	}
	money, err := s.opts.Client.Money(s.opts.MyTeamID, time.Minute)
	if err != nil {
		return 0
	}
	return money.TeamMoney
}

// rows is the world as generic maps, which is what the policy helpers and the page speak.
func (s *Server) rows() []map[string]any {
	universe := s.state.Universe()
	if universe == nil {
		return nil
	}
	blob, err := json.Marshal(universe.Players)
	if err != nil {
		return nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(blob, &rows); err != nil {
		return nil
	}
	return rows
}

// saveLineup writes the eleven. No money moves, so it goes in one step.
func (s *Server) saveLineup(writer http.ResponseWriter, request *http.Request) {
	if s.opts.Guard == nil {
		s.json(writer, http.StatusNotImplemented, map[string]any{"error": "sin escrituras"})
		return
	}
	body := s.body(request)
	args := writes.Args{
		LeagueID:   s.opts.LeagueID,
		TeamID:     s.opts.MyTeamID,
		Goalkeeper: text(body["goalkeeper"]),
		Defender:   idsOf(body["defender"]),
		Midfield:   idsOf(body["midfield"]),
		Striker:    idsOf(body["striker"]),
		Formation:  shapeOf(body["formation"]),
	}
	result, err := s.opts.Guard.Do("save_lineup", args,
		writes.Player{Name: "tu alineacion"}, s.opts.AllowWrites)
	if err != nil {
		s.writeError(writer, err)
		return
	}
	slog.Info("lineup saved", "formation", body["formation"],
		"starters", 1+len(args.Defender)+len(args.Midfield)+len(args.Striker))
	s.settle("save_lineup")
	s.json(writer, http.StatusOK, map[string]any{"ok": true, "saved": result != nil,
		"formation": body["formation"]})
}

func idsOf(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if id := text(item); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func shapeOf(value any) []int {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]int, 0, len(items))
	for _, item := range items {
		out = append(out, int(number(item)))
	}
	return out
}

// raiseToSafe is what to pay so the clause lands on the line the advice draws, remembering that
// the clause goes up by twice what you pay. Zero when it is already above it, which the page
// then says out loud instead of proposing a number.
func raiseToSafe(value, clause float64) int64 {
	missing := advice.SafeMargin*value - clause
	if value <= 0 || missing <= 0 {
		return 0
	}
	return int64(missing / writes.ClauseFactor)
}

// rivalBank is each rival's estimated cash, for the moves that would pay one of them.
func (s *Server) rivalBank() advice.RivalBank {
	out := advice.RivalBank{}
	universe := s.state.Universe()
	if universe == nil {
		return out
	}
	for id, team := range universe.LeagueTeams {
		if team != nil && (universe.MyTeamID == nil || id != *universe.MyTeamID) {
			out[id] = team.EstimatedCash
		}
	}
	return out
}
