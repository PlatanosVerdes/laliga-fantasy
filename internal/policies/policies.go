// Package policies is the standing instructions: keep a player listed, sell him when an
// offer is good enough, pay a clause the moment it opens. A port of fantasy/policies.py.
//
// Nothing here decides on its own what a good price is, and nothing sells a player unless
// you named the number or ticked the box. Two ways to authorise a sale and no third; without
// one of them a good offer produces a notice and you decide.
package policies

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/eleven"
)

type Row = map[string]any

// Policy is one player's standing instructions.
type Policy struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	AlwaysList  bool     `json:"always_listed,omitempty"`
	MinPrice    *float64 `json:"min_price,omitempty"`
	AcceptAbove *float64 `json:"accept_above,omitempty"`
	AutoSell    bool     `json:"auto_sell,omitempty"`
	Raid        bool     `json:"raid,omitempty"`
	MaxPay      *float64 `json:"max_pay,omitempty"`
}

// What counts as a good offer, when you would rather not pick a number: the highest of three
// references, because each catches a different way of being underpaid — your asking price,
// 1.02x market value (the app's own "buen precio" band, above where the daily automatic
// offers top out), and futbolfantasy's maximum profitable bid, since somebody paying more
// than the player can return is the winning side of the trade.
const GoodOverValue = 1.02

// Squad rules: eleven legal starters need a keeper, three defenders and three midfielders.
// Kept for what reads it from outside; what a sale is judged against is eleven.Room.
var MinPerPosition = map[int]int{1: 1, 2: 3, 3: 3, 4: 1}

func Load() (map[string]Policy, error) {
	body, err := os.ReadFile(config.PolicyFile)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Policy{}, nil
		}
		return nil, err
	}
	var out map[string]Policy
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]Policy{}, nil
	}
	return out, nil
}

func Save(policies map[string]Policy) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(policies, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(config.PolicyFile, blob, 0o600)
}

// GoodOfferFloor is (bar, which reference set it). The winning reference is quoted in the
// reason, so the number is never a mystery.
func GoodOfferFloor(player Row, policy Policy) (int64, string) {
	value := number(player["value"])
	listing := mapOf(player["market"])

	type candidate struct {
		amount int64
		source string
	}
	candidates := []candidate{
		{int64(number(listing["min_bid"])), "lo que pides"},
		{0, "un 2% sobre su valor"},
		{int64(number(player["ideal_bid"])), "la puja maxima rentable de futbolfantasy"},
		{0, "tu precio minimo"},
	}
	if value != 0 {
		candidates[1].amount = int64(value * GoodOverValue)
	}
	if policy.MinPrice != nil {
		candidates[3].amount = int64(*policy.MinPrice)
	}

	best := candidates[0]
	for _, item := range candidates[1:] {
		if item.amount > best.amount {
			best = item
		}
	}
	return best.amount, best.source
}

// SquadRoom is how many of that position could be sold with a legal eleven still standing.
// A price can be excellent and the sale still be a mistake.
//
// It used to answer "how many above the floor for his position", which is eight players in
// total and says nothing about fielding eleven: with a squad of exactly eleven it cleared the
// sale of any player in surplus at his position and left ten, which no formation fields.
func SquadRoom(players []Row, positionID int) int {
	counts := map[int]int{}
	for _, player := range players {
		if truthy(player["is_mine"]) {
			counts[int(number(player["position_id"]))]++
		}
	}
	return eleven.Room(counts, positionID)
}

// Plan is what the standing instructions would do right now, without doing it. Returned
// whether or not writes are enabled, because the plan is the useful part even when nothing
// executes.
func Plan(players []Row, policies map[string]Policy) []Row {
	actions := []Row{}
	// The squad the next sale is judged against, not the one this pass started with. A cycle
	// may perform three operations, and with every sale measured against the same untouched
	// squad two of them could each be "one to spare" and leave ten between them. The raids
	// already spend the balance down this way; this is the same idea for the eleven.
	counts := map[int]int{}
	for _, player := range players {
		if truthy(player["is_mine"]) {
			counts[int(number(player["position_id"]))]++
		}
	}
	for _, player := range players {
		policy, armed := policies[text(player["id"])]
		if !armed || !policy.AlwaysList {
			continue
		}
		if !truthy(player["is_mine"]) {
			actions = append(actions, Row{
				"player_id": text(player["id"]), "name": player["name"],
				"action": "ninguna", "why": "ya no es tuyo"})
			continue
		}

		value := number(player["value"])
		floor := value
		if policy.MinPrice != nil && *policy.MinPrice != 0 {
			floor = *policy.MinPrice
		}
		listing := mapOf(player["market"])
		offers := rowsOf(player["offers"])
		var best Row
		if len(offers) > 0 {
			best = offers[0]
		}
		asking := number(listing["min_bid"])
		if asking == 0 {
			asking = floor
		}
		if asking == 0 {
			asking = value
		}

		// Two ways to authorise a sale, and no third.
		var threshold *int64
		source := "tu limite"
		if policy.AcceptAbove != nil && *policy.AcceptAbove != 0 {
			amount := int64(*policy.AcceptAbove)
			threshold = &amount
		} else if policy.AutoSell {
			amount, why := GoodOfferFloor(player, policy)
			threshold, source = &amount, why
		}

		positionID := int(number(player["position_id"]))
		room := eleven.Room(counts, positionID)
		bestAmount := number(best["money"])

		if threshold != nil && best != nil && bestAmount >= float64(*threshold) && room <= 0 {
			// Good price, bad idea: after this sale there is no eleven to field. Either he is
			// the last one at his position or the squad is down to eleven and every sale is.
			actions = append(actions, Row{
				"player_id": text(player["id"]), "name": player["name"],
				"action": "avisar", "amount": int64(bestAmount),
				"offer_id": text(best["id"]), "market_id": listing["market_id"],
				"why": fmt.Sprintf("ofrecen %s (por encima de %s), pero venderlo te deja sin "+
					"once que alinear: ficha antes y decides tu",
					money(int64(bestAmount)), money(*threshold))})
			continue
		}

		if threshold != nil && best != nil && bestAmount >= float64(*threshold) {
			// He is gone as far as the rest of this plan is concerned.
			counts[positionID]--
			actions = append(actions, Row{
				"player_id": text(player["id"]), "name": player["name"],
				"action": "aceptar_oferta", "amount": int64(bestAmount),
				"offer_id": text(best["id"]), "market_id": listing["market_id"],
				"why": fmt.Sprintf("ofrecen %s, %s es %s", money(int64(bestAmount)), source,
					money(*threshold))})
			continue
		}

		if listing == nil {
			price := int64(math.Max(floor, value))
			actions = append(actions, Row{
				"player_id": text(player["id"]), "name": player["name"],
				// The API lists by squad slot, not by player: without it the call travels with
				// playerId "" and comes back 400.
				"player_team_id": player["player_team_id"],
				"action": "poner_en_venta", "amount": price,
				"why": fmt.Sprintf("no esta en el mercado; lo listo a %s", money(price))})
			continue
		}

		listedAt := int64(number(listing["min_bid"]))
		why := fmt.Sprintf("ya listado a %s", money(listedAt))
		if best != nil {
			why += fmt.Sprintf(", mejor oferta %s", money(int64(bestAmount)))
		} else {
			why += ", sin ofertas"
		}
		// An offer that already covers the asking price, on a player nobody authorised
		// selling: worth saying out loud rather than leaving in a table nobody reads. A
		// notice, not an action — Enforce skips it.
		if best != nil && threshold == nil && listedAt != 0 && bestAmount >= float64(listedAt) {
			actions = append(actions, Row{
				"player_id": text(player["id"]), "name": player["name"],
				"action": "avisar", "amount": int64(bestAmount),
				"offer_id": text(best["id"]), "market_id": listing["market_id"],
				"why": fmt.Sprintf("ofrecen %s, lo que pides (%s); no vendo solo, decides tu",
					money(int64(bestAmount)), money(listedAt))})
			continue
		}
		if threshold == nil {
			why += "; no vendo solo"
		}
		actions = append(actions, Row{"player_id": text(player["id"]), "name": player["name"],
			"action": "ninguna", "why": why})
	}
	return actions
}

// RaidPlan is the scheduled clause raids: pay the moment the lock lifts, if it is still worth
// it. A clause is not a fixed price — the owner can raise it or shield the player — so the
// instruction carries max_pay and stands down rather than overpaying for yesterday's price.
//
// Several raids can open at the same instant, and each one spends the same balance: checking
// every clause against the starting cash says yes to all of them and leaves the balance
// negative. So the payable ones go into a queue — best points per million paying the clause
// first — and the balance is spent down along it; whoever no longer fits stands down with the
// reason, and gets his turn on a later cycle if he is still there.
func RaidPlan(players []Row, policies map[string]Policy, cash float64) []Row {
	actions := []Row{}
	queue := []Row{}
	for _, player := range players {
		policy, armed := policies[text(player["id"])]
		if !armed || !policy.Raid {
			continue
		}
		clause := number(player["clause"])
		ceiling := int64(0)
		if policy.MaxPay != nil {
			ceiling = int64(*policy.MaxPay)
		}
		row := Row{"player_id": text(player["id"]), "name": player["name"],
			"clause": clause, "max_pay": ceiling, "owner": player["owner"],
			"owner_team_id": player["owner_team_id"]}

		switch {
		// Already yours: the instruction is spent, whether the raid paid it or you signed
		// him another way. Leaving the row turned the section into a list of players you
		// were not going to clause, which is the opposite of what it is for.
		case truthy(player["is_mine"]):
			continue
		// No cap, no automatic payment. The page always demands an amount when arming a raid, but
		// a policies.json edited by hand does not, and "pay whatever it costs" is not something
		// anybody authorised: without this the ceiling of zero skipped its own check and the only
		// remaining limit was the balance.
		case ceiling == 0:
			actions = append(actions, merge(row, Row{"action": "sin_limite",
				"why": "no tiene pago maximo: no pago solo sin un limite escrito"}))
		case text(player["owner"]) == "":
			actions = append(actions, merge(row, Row{"action": "ninguna",
				"why": "ya no lo tiene nadie"}))
		case truthy(player["shielded"]):
			actions = append(actions, merge(row, Row{"action": "bloqueada",
				"why": fmt.Sprintf("%s lo ha blindado", text(player["owner"]))}))
		case truthy(player["clause_locked"]):
			why := "clausula bloqueada"
			if hours := number(player["clause_hours_left"]); hours != 0 {
				why += fmt.Sprintf(", se abre en %.0fh", hours)
			}
			actions = append(actions, merge(row, Row{"action": "esperando", "why": why}))
		case ceiling != 0 && clause > float64(ceiling):
			actions = append(actions, merge(row, Row{"action": "cancelada",
				"why": fmt.Sprintf("la clausula subio a %s, tu limite es %s",
					money(int64(clause)), money(ceiling))}))
		default:
			queue = append(queue, merge(row, Row{"amount": int64(clause),
				"player_team_id": player["player_team_id"],
				"ppm_at_clause":  pointsPerMillion(player, clause)}))
		}
	}
	return append(actions, spendDown(queue, cash)...)
}

// pointsPerMillion is what the clause buys: expected points per million paid. Zero when there
// is nothing to divide, which sends the player to the back of the queue rather than the front.
func pointsPerMillion(player Row, clause float64) float64 {
	if clause <= 0 {
		return 0
	}
	return number(player["xpts"]) / (clause / 1e6)
}

// spendDown turns the payable raids into an ordered queue and pays along it while the balance
// lasts. Order is points per million first and the cheaper clause second: with two raids of the
// same worth, paying the cheap one leaves room for the next.
func spendDown(queue []Row, cash float64) []Row {
	sort.SliceStable(queue, func(one, two int) bool {
		left, right := number(queue[one]["ppm_at_clause"]), number(queue[two]["ppm_at_clause"])
		if left != right {
			return left > right
		}
		return number(queue[one]["clause"]) < number(queue[two]["clause"])
	})

	remaining := cash
	var ahead []string
	out := make([]Row, 0, len(queue))
	for _, row := range queue {
		clause := number(row["clause"])
		ceiling := int64(number(row["max_pay"]))
		if clause > remaining {
			why := fmt.Sprintf("cuesta %s y te quedan %s", money(int64(clause)),
				money(int64(remaining)))
			if len(ahead) > 0 {
				why += ": antes va " + strings.Join(ahead, ", ")
			}
			out = append(out, merge(row, Row{"action": "sin_saldo", "why": why}))
			continue
		}
		remaining -= clause
		out = append(out, merge(row, Row{"action": "pagar_clausula",
			"queue_position": len(ahead) + 1,
			"why": fmt.Sprintf("abierta a %s, por debajo de tu limite de %s; rinde %.2f pts "+
				"por millon y quedarian %s", money(int64(clause)), money(ceiling),
				number(row["ppm_at_clause"]), money(int64(remaining)))}))
		ahead = append(ahead, text(row["name"]))
	}
	return out
}

// money is the page's thousands-with-dots format, which these reasons are read in.
func money(amount int64) string {
	text := strconv.FormatInt(amount, 10)
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	var groups []string
	for len(text) > 3 {
		groups = append([]string{text[len(text)-3:]}, groups...)
		text = text[:len(text)-3]
	}
	groups = append([]string{text}, groups...)
	out := strings.Join(groups, ".")
	if negative {
		return "-" + out
	}
	return out
}

// SortedIDs keeps output stable for comparison.
func SortedIDs(policies map[string]Policy) []string {
	ids := make([]string, 0, len(policies))
	for id := range policies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func merge(base, extra Row) Row {
	out := make(Row, len(base)+len(extra))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func rowsOf(value any) []Row {
	switch typed := value.(type) {
	case []Row:
		return typed
	case []any:
		out := make([]Row, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(Row); ok {
				out = append(out, row)
			}
		}
		return out
	}
	return nil
}

func mapOf(value any) Row {
	if row, ok := value.(Row); ok {
		return row
	}
	return nil
}

func text(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case *string:
		if typed == nil {
			return ""
		}
		return *typed
	case float64:
		if typed == math.Trunc(typed) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ""
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case *float64:
		if typed == nil {
			return 0
		}
		return *typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return parsed
	}
	return 0
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case *bool:
		return typed != nil && *typed
	case *float64:
		return typed != nil && *typed != 0
	case float64:
		return typed != 0
	case string:
		return typed != ""
	}
	return false
}

// Set writes one instruction, merging with whatever is already stored so a page that only
// knows about one switch cannot wipe the amounts. Keys listed in unset are cleared.
func Set(id string, apply func(*Policy), unset ...string) (Policy, error) {
	armed, err := Load()
	if err != nil {
		return Policy{}, err
	}
	entry := armed[id]
	entry.ID = id
	apply(&entry)
	for _, key := range unset {
		switch key {
		case "min_price":
			entry.MinPrice = nil
		case "accept_above":
			entry.AcceptAbove = nil
		case "max_pay":
			entry.MaxPay = nil
		}
	}
	armed[id] = entry
	return entry, Save(armed)
}

func Remove(id string) error {
	armed, err := Load()
	if err != nil {
		return err
	}
	delete(armed, id)
	return Save(armed)
}

// Sold is the ids whose sell-side instructions can never run again: the player is in the
// universe and is not yours any more. An id the universe says nothing about is left alone:
// a missing player is a world that came back short, not a sale.
//
// A raid is not sell-side: it aims at somebody else's player, so not being yours is its
// normal state.
func Sold(mine map[string]bool, policies map[string]Policy) []string {
	var stale []string
	for _, id := range SortedIDs(policies) {
		policy := policies[id]
		if !policy.AlwaysList && !policy.AutoSell &&
			policy.MinPrice == nil && policy.AcceptAbove == nil {
			continue
		}
		if owned, known := mine[id]; !known || owned {
			continue
		}
		stale = append(stale, id)
	}
	return stale
}

// Signed is the ids whose scheduled raid can never run again: the player is in the universe
// and is already yours. Same guard as Sold — an id the universe says nothing about is left
// alone, because a missing player is a short read, not a signing.
func Signed(mine map[string]bool, policies map[string]Policy) []string {
	var done []string
	for _, id := range SortedIDs(policies) {
		if !policies[id].Raid {
			continue
		}
		if owned, known := mine[id]; !known || !owned {
			continue
		}
		done = append(done, id)
	}
	return done
}

// Disarm drops the raid side of these instructions and reports the names it dropped. The sell
// side survives, because a player you now own is exactly who those instructions are about.
//
// Not only tidying: an armed raid left on a player of yours would come back to life the day you
// sell him and pay a clause nobody armed again.
func Disarm(ids ...string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	armed, err := Load()
	if err != nil {
		return nil, err
	}
	var gone []string
	for _, id := range ids {
		entry, found := armed[id]
		if !found || !entry.Raid {
			continue
		}
		name := entry.Name
		if name == "" {
			name = id
		}
		gone = append(gone, name)
		entry.Raid, entry.MaxPay = false, nil
		if !entry.AlwaysList && !entry.AutoSell && entry.MinPrice == nil &&
			entry.AcceptAbove == nil {
			delete(armed, id)
			continue
		}
		armed[id] = entry
	}
	if len(gone) == 0 {
		return nil, nil
	}
	return gone, Save(armed)
}

// Forget drops the sell side of these instructions and reports the names it dropped. A
// scheduled raid on the same player survives, because that one is still about to happen.
func Forget(ids ...string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	armed, err := Load()
	if err != nil {
		return nil, err
	}
	var gone []string
	for _, id := range ids {
		entry, found := armed[id]
		if !found {
			continue
		}
		name := entry.Name
		if name == "" {
			name = id
		}
		gone = append(gone, name)
		if !entry.Raid {
			delete(armed, id)
			continue
		}
		entry.AlwaysList, entry.AutoSell = false, false
		entry.MinPrice, entry.AcceptAbove = nil, nil
		armed[id] = entry
	}
	if len(gone) == 0 {
		return nil, nil
	}
	return gone, Save(armed)
}
