// Player detail and the pitch. Both merge the live API with the built world, so a shirt or a
// drawer always carries the same numbers as the tables.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
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
	if _, present := player["ideal_bid"]; !present {
		if ffID := text(player["ff_id"]); ffID != "" {
			if detail, err := futbolfantasy.PlayerDetail(ffID, 24*time.Hour); err == nil {
				player["ideal_bid"] = number(detail["ideal_bid"])
			}
		}
	}

	armed, _ := policies.Load()
	listing := mapOf(player["market"])
	offers := listOf(player["offers"])
	clause := number(player["clause"])
	budget := s.budget()
	actions := s.actions(player, rows, armed, listing, offers, clause, budget)

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

	s.json(writer, http.StatusOK, map[string]any{"player": player, "offers": offers,
		"listing": listing, "actions": actions, "history": history,
		"writes_enabled": s.opts.AllowWrites})
}

func (s *Server) actions(player map[string]any, rows []map[string]any,
	armed map[string]policies.Policy, listing map[string]any, offers []map[string]any,
	clause, budget float64) []map[string]any {
	id := text(player["id"])
	actions := []map[string]any{}
	policy := armed[id]
	_, standing := armed[id]

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
		} else {
			actions = append(actions, map[string]any{"op": "sell_to_market",
				"label": "Poner en venta", "kind": "amount",
				"suggested":      int64(number(player["value"])),
				"player_team_id": player["player_team_id"]})
		}
		for _, offer := range offers {
			amount := int64(number(offer["money"]))
			actions = append(actions,
				map[string]any{"op": "accept_offer", "label": "Aceptar " + thousands(amount),
					"kind": "confirm", "offer_id": text(offer["id"]),
					"market_id": listing["market_id"], "amount": amount},
				map[string]any{"op": "decline_offer", "label": "Rechazar", "kind": "confirm",
					"danger": true, "offer_id": text(offer["id"]),
					"market_id": listing["market_id"]})
		}
		actions = append(actions, map[string]any{"op": "raise_clause",
			"label": "Subir clausula", "kind": "amount",
			"player_team_id": player["player_team_id"],
			"suggested":      int64(number(player["value"]) * 0.5)})

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
		if marketID := text(listing["market_id"]); marketID != "" {
			suggested := number(listing["min_bid"])
			if suggested == 0 {
				suggested = number(player["value"])
			}
			actions = append(actions,
				map[string]any{"op": "direct_offer", "label": "Ofrecer a " + owner,
					"kind": "amount", "market_id": marketID, "suggested": int64(suggested)})
			actions = append(actions, bidActions(listing, number(listing["min_bid"]))...)
		} else {
			actions = append(actions, map[string]any{"op": "note", "kind": "note",
				"label": owner + " no lo tiene en venta: solo se le puede pagar la clausula"})
		}
		if clause > 0 && !truthy(player["clause_locked"]) {
			actions = append(actions, map[string]any{"op": "pay_clause",
				"label": fmt.Sprintf("Pagar clausula (%s)", thousands(int64(clause))),
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
func bidActions(listing map[string]any, suggested float64) []map[string]any {
	marketID := text(listing["market_id"])
	minBid := int64(number(listing["min_bid"]))
	if bidID := text(listing["my_bid_id"]); bidID != "" {
		mine := int64(number(listing["my_bid"]))
		if suggested <= float64(mine) {
			suggested = float64(mine)
		}
		return []map[string]any{
			{"op": "modify_bid", "label": fmt.Sprintf("Cambiar tu puja (%s)", thousands(mine)),
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

	// The payload's bench comes back empty, so the reserves are simply the rest of the
	// squad, rebuilt into the same shirt shape as the starters.
	bench := []map[string]any{}
	for _, row := range rows {
		if !truthy(row["is_mine"]) || starters[text(row["id"])] {
			continue
		}
		bench = append(bench, shirtOf(map[string]any{
			"playerTeamId": row["player_team_id"],
			"playerMaster": map[string]any{
				"id": row["id"], "nickname": row["name"], "positionId": row["position_id"],
				"teamId": row["team_id"], "marketValue": row["value"],
				"playerStatus": row["status"], "lastStats": []any{},
			}}, known))
	}

	s.json(writer, http.StatusOK, map[string]any{"lines": lines, "bench": bench,
		"formation": formation["tacticalFormation"],
		"formations": map[string]any{"free": free, "premium": premium},
		"updated_at": payload["updatedAt"], "writes_enabled": s.opts.AllowWrites})
}

func shirtOf(slot map[string]any, known map[string]map[string]any) map[string]any {
	master := mapOf(slot["playerMaster"])
	id := text(master["id"])
	extra := known[id]
	weeks := []map[string]any{}
	for _, week := range listOf(master["lastStats"]) {
		weeks = append(weeks,
			map[string]any{"week": week["weekNumber"], "points": week["totalPoints"]})
	}
	pick := func(first, second string) any {
		if value, ok := master[first]; ok && value != nil {
			return value
		}
		return extra[second]
	}
	return map[string]any{
		"player_team_id": text(slot["playerTeamId"]),
		"id":             id,
		"name":           fallback(text(master["nickname"]), text(master["name"])),
		"position_id":    master["positionId"],
		"team_id":        text(pick("teamId", "team_id")),
		"value":          pick("marketValue", "value"),
		"status":         pick("playerStatus", "status"),
		"week_points":    master["weekPoints"],
		"average":        master["averagePoints"],
		"last_season_points": master["lastSeasonPoints"],
		"weeks":              weeks,
		// from the analysis, so the pitch agrees with the tables
		"xpts":              extra["xpts"],
		"projected_pct":     extra["projected_pct"],
		"start_probability": extra["start_probability"],
		"next_rival":        extra["next_rival"],
		"next_home":         extra["next_home"],
		"starred":           extra["starred"],
		"absence":           extra["absence"],
	}
}

// fragments serves the page in pieces so a repaint replaces the sections that changed instead
// of the whole document, which is what lets the pitch keep unsaved changes.
func (s *Server) fragments(writer http.ResponseWriter, _ *http.Request) {
	if s.opts.Page == nil {
		s.json(writer, http.StatusServiceUnavailable, map[string]any{"error": "sin pagina"})
		return
	}
	s.json(writer, http.StatusOK, map[string]any{"version": s.state.Health().Version,
		"sections": Sections(s.opts.Page())})
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
