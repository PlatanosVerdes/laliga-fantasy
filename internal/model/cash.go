package model

import (
	"log/slog"
	"math"
	"sort"
)

// Activity types the log uses. `cash` is from user1's point of view: -1 paid, +1
// received; `counterparty` is the same for user2 when there is one.
type activitySpec struct {
	Kind         string
	Cash         float64
	Counterparty float64
}

var activityTypes = map[int]activitySpec{
	1:  {"traspaso", -1, +1},
	6:  {"recompensa", 0, 0},
	9:  {"se une a la liga", 0, 0},
	31: {"compra", -1, 0},
	33: {"venta", +1, 0},
}

// RewardType is the matchday prize: "en la jornada 1, TheMessias ha ganado 4.800.000". It is
// cash in, and a big one -- 1 to 4.8 M per manager on matchday 1 -- but it is not a transfer,
// so it is counted on its own rather than through activityTypes. That keeps "neto en fichajes"
// meaning what it says, and stops the league's cash from being wrong by the difference between
// what each manager scored.
const RewardType = 6

// Every manager starts on the same cash; the daily reward is the only drip that cannot
// be reconstructed from the log. Both are fallbacks only: when the session can read its
// own /money we anchor on that instead.
const (
	InitialCash = 100_000_000.0
	DailyReward = 100_000.0
)

// Event is one line of the league log, flattened. The raw event is kept so an unknown
// type can still be inspected instead of silently vanishing.
type Event struct {
	Date     string         `json:"date"`
	TypeID   int            `json:"type_id"`
	Kind     string         `json:"kind"`
	Known    bool           `json:"known"`
	User1    string         `json:"user1"`
	User2    *string        `json:"user2"`
	Buyer    *string        `json:"buyer"`
	Seller   *string        `json:"seller"`
	Actor    string         `json:"actor"`
	Player   *string        `json:"player"`
	PlayerID *string        `json:"player_id"`
	Amount   *float64       `json:"amount"`
	// Filled by EnrichActivityValues for the biggest trades only.
	ValueThen  *float64 `json:"value_then,omitempty"`
	Premium    *float64 `json:"premium,omitempty"`
	PremiumAbs *float64 `json:"premium_abs,omitempty"`
	Raw      map[string]any `json:"raw"`
}

// LeagueTeam is a rival's summary. The standings figure is squad value, not total worth
// — it matches the sum of the squad's market values exactly and carries no cash — so
// cash comes from ReconstructCash instead.
type LeagueTeam struct {
	TeamID        string   `json:"team_id"`
	UserID        string   `json:"user_id"`
	Name          *string  `json:"name"`
	Manager       *string  `json:"manager"`
	Points        float64  `json:"points"`
	LivePoints    *float64 `json:"live_points"`
	Position      *int     `json:"position"`
	ReportedValue float64  `json:"reported_value"`
	SquadValue    float64  `json:"squad_value"`
	ClauseTotal   float64  `json:"clause_total"`
	Players       int      `json:"players"`
	NetFlow       float64  `json:"net_flow"`
	Rewards       float64  `json:"rewards"`
	EstimatedCash float64  `json:"estimated_cash"`
	CashIsEstimate bool    `json:"cash_is_estimate"`
}

// CashModel is what the reconstruction assumed, reported so the page can say how much of
// it is measured and how much is inferred.
type CashModel struct {
	Base            float64  `json:"base"`
	Anchored        bool     `json:"anchored"`
	ImpliedRewards  *float64 `json:"implied_rewards"`
	Uncertainty     float64  `json:"uncertainty"`
	EventsWithCash  int      `json:"events_with_cash"`
	PrizesCounted   int      `json:"prizes_counted"`
}

// NormalizeActivity flattens the league log into one shape, resolving ids to names.
//
// The payload is all ids: activityTypeId, user1Id, optional user2Id, playerMasterId,
// amount.
func NormalizeActivity(events []map[string]any, managers map[string]string,
	playerNames map[string]string) []Event {
	out := make([]Event, 0, len(events))
	for _, event := range events {
		typeID := int(number(event["activityTypeId"]))
		spec, known := activityTypes[typeID]
		user1 := text(event["user1Id"])
		user2 := text(event["user2Id"])
		// cash == -1 means user1 paid, so user1 is the buyer and user2 the seller.
		pays := spec.Cash == -1

		row := Event{
			Date:   trim19(text(event["createdAt"])),
			TypeID: typeID,
			Kind:   fallback(spec.Kind, "tipo "+text(event["activityTypeId"])),
			Known:  known,
			User1:  user1,
			Actor:  fallback(managers[user1], user1),
			Raw:    event,
		}
		if user2 != "" {
			row.User2 = &user2
		}
		if amount, ok := event["amount"]; ok && amount != nil {
			if value := number(amount); value != 0 || isNumeric(amount) {
				row.Amount = &value
			}
		}
		if playerID := text(event["playerMasterId"]); playerID != "" {
			row.PlayerID = &playerID
			if name, ok := playerNames[playerID]; ok {
				row.Player = &name
			}
		}
		if typeID == RewardType {
			// Nobody sold anything: el premio lo cobra user1 y no hay otra parte.
			row.Buyer = optional(managers[user1])
		} else if pays {
			row.Buyer = optional(managers[user1])
			if user2 != "" {
				row.Seller = optional(managers[user2])
			}
		} else {
			if user2 != "" {
				row.Buyer = optional(managers[user2])
			}
			row.Seller = optional(managers[user1])
		}
		out = append(out, row)
	}
	return out
}

// ReconstructCash derives every manager's cash from the transfer log.
//
// The API exposes /money for your own team only, and the standings figure turns out to be
// squad value alone, so cash has to be reconstructed:
//
//	cash = starting cash + rewards claimed + sales - purchases
//
// Purchases and sales all live in the log. Rewards are the one term it does not record,
// so instead of guessing, the whole league is anchored on the one cash figure that can be
// read — our own: base = my_cash - my_net folds our starting cash and our claimed rewards
// into a single measured constant, and every rival gets that same base plus their own net.
// The residual error is therefore only the difference in rewards claimed, bounded by 100k
// a day.
func ReconstructCash(events []Event, teams map[string]*LeagueTeam, myTeamID string,
	myRealCash *float64) CashModel {
	net := map[string]float64{}
	rewards := map[string]float64{}
	prizes := 0
	for _, event := range events {
		if event.Amount == nil || *event.Amount == 0 {
			continue
		}
		if event.TypeID == RewardType {
			rewards[event.User1] += *event.Amount
			prizes++
			continue
		}
		spec, known := activityTypes[event.TypeID]
		if !known {
			continue
		}
		if spec.Cash != 0 {
			net[event.User1] += spec.Cash * *event.Amount
		}
		if spec.Counterparty != 0 && event.User2 != nil {
			net[*event.User2] += spec.Counterparty * *event.Amount
		}
	}

	for _, team := range teams {
		if team.UserID != "" {
			team.NetFlow = net[team.UserID]
			team.Rewards = rewards[team.UserID]
		}
	}

	base := InitialCash
	anchored := false
	mine := teams[myTeamID]
	if mine != nil && myRealCash != nil {
		// The anchor has to take the prizes out too, or my own prize would be baked into the
		// base and handed to everybody as if they had scored the same as me.
		base = *myRealCash - mine.NetFlow - mine.Rewards
		anchored = true
	}

	for _, team := range teams {
		team.EstimatedCash = math.Max(0, base+team.NetFlow+team.Rewards)
		team.CashIsEstimate = true
	}
	if mine != nil && myRealCash != nil {
		mine.EstimatedCash = *myRealCash
		mine.CashIsEstimate = false
	}

	model := CashModel{Base: base, Anchored: anchored, Uncertainty: DailyReward * 10,
		PrizesCounted: prizes}
	if anchored {
		implied := base - InitialCash
		model.ImpliedRewards = &implied
	}
	for _, event := range events {
		if event.Amount == nil {
			continue
		}
		if spec, known := activityTypes[event.TypeID]; known && spec.Cash != 0 {
			model.EventsWithCash++
		}
	}
	slog.Info("cash reconstructed", "base", math.Round(base), "anchored", anchored,
		"teams", len(teams), "prizes", prizes)
	return model
}

// SortedTeams is for output: a map's iteration order would make the dump unstable and the
// comparison would flag differences that are not.
func SortedTeams(teams map[string]*LeagueTeam) []*LeagueTeam {
	keys := make([]string, 0, len(teams))
	for key := range teams {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*LeagueTeam, 0, len(keys))
	for _, key := range keys {
		out = append(out, teams[key])
	}
	return out
}

func trim19(value string) string {
	if len(value) > 19 {
		return value[:19]
	}
	return value
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func isNumeric(value any) bool {
	switch value.(type) {
	case float64, int, int64:
		return true
	}
	return false
}
