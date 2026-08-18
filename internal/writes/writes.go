// Package writes is every operation that changes something in the game. A port of
// fantasy/writes.py, and the one place where "roughly the same" is not good enough: the
// ids are unobvious and hard-won, and getting one wrong is a 500 at best and the wrong
// player sold at worst.
//
// Every write goes through Prepare then Confirm: Prepare validates the amount against the
// cash and futbolfantasy's ceiling and hands back a single-use token; Confirm spends that
// token and calls the API. A double click, a retry or a replayed request therefore cannot
// bid twice. The real guard is not a flag but the token.
package writes

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

const PrepareTTL = 120 * time.Second

var (
	ErrDisabled = errors.New("este servidor corre en modo solo lectura")
	ErrUnknown  = errors.New("operacion desconocida")
)

// Call is a request that has been built but not sent. Splitting building from sending is
// what makes a dry run worth anything: it shows the exact method, path and body that would
// travel, which is also what the harness compares against Python.
type Call struct {
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body"`
}

// Args carries every id an operation might need. Which ones matter is per operation, and
// the mapping below is deliberately explicit rather than clever.
type Args struct {
	LeagueID     string
	TeamID       string
	MarketID     string
	BidID        string
	OfferID      string
	PlayerID     string
	PlayerTeamID string
	Amount       int64
	// Lineup only.
	Goalkeeper string
	Defender   []string
	Midfield   []string
	Striker    []string
	Formation  []int
}

type operation struct {
	Label string
	Build func(Args) Call
	// Which cached answers this write makes false.
	Effects []string
}

// Operations, with the id semantics spelled out. Each comment is a bug that was paid for.
var Operations = map[string]operation{
	"bid": {"puja", func(a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/%s/bid", config.CMP, a.LeagueID, a.MarketID),
			map[string]any{"money": a.Amount}}
	}, []string{"market", "money"}},

	"modify_bid": {"modificar puja", func(a Args) Call {
		return Call{http.MethodPut,
			fmt.Sprintf("%s/league/%s/market/%s/bid/%s", config.CMP, a.LeagueID, a.MarketID, a.BidID),
			map[string]any{"money": a.Amount}}
	}, []string{"market", "money"}},

	"cancel_bid": {"cancelar puja", func(a Args) Call {
		return Call{http.MethodDelete,
			fmt.Sprintf("%s/league/%s/market/%s/bid/%s/cancel", config.CMP, a.LeagueID, a.MarketID, a.BidID),
			nil}
	}, []string{"market", "money"}},

	// The slot id, not the player's, and the factor the app uses is 2.
	"raise_clause": {"subir clausula", func(a Args) Call {
		return Call{http.MethodPut,
			fmt.Sprintf("%s/league/%s/buyout/player", config.CMP, a.LeagueID),
			map[string]any{"playerId": a.PlayerTeamID, "factor": 2, "valueToIncrease": a.Amount}}
	}, []string{"squad"}},

	// The API demands the amount in the body as a confirmation of what is being accepted;
	// without it, it answers 400.
	"accept_offer": {"aceptar oferta", func(a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/%s/offer/%s/accept", config.CMP, a.LeagueID, a.MarketID, a.OfferID),
			map[string]any{"offerMoney": a.Amount}}
	}, []string{"market", "squad", "money", "activity", "standing", "offers"}},

	"decline_offer": {"rechazar oferta", func(a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/%s/offer/%s/reject", config.CMP, a.LeagueID, a.MarketID, a.OfferID),
			nil}
	}, []string{"market", "offers"}},

	// `playerId` here is the squad-slot id (playerTeamId), not the player's own id.
	// Sending the player id answers 500. Listing him is not selling him, so the cash and
	// the log are untouched.
	"sell_to_market": {"poner en venta", func(a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/sell", config.CMP, a.LeagueID),
			map[string]any{"playerId": a.PlayerTeamID, "salePrice": a.Amount}}
	}, []string{"market", "squad"}},

	// A direct offer goes against the listing, so what travels as `playerId` is the market
	// id: you can only offer for somebody who is on the market.
	"direct_offer": {"oferta directa", func(a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/direct-offer", config.CMP, a.LeagueID),
			map[string]any{"playerId": a.MarketID, "money": a.Amount}}
	}, []string{"market", "money"}},

	"pay_clause": {"pagar clausula", func(a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/buyout/%s/pay", config.CMP, a.LeagueID, a.PlayerTeamID),
			map[string]any{"buyoutClauseToPay": a.Amount}}
	}, []string{"squad", "money", "activity", "standing", "market"}},

	// Shape matters: goalkeeper is a single id, not a list, and the key is snake_case.
	// Anything else answers 500.
	"save_lineup": {"guardar alineacion", func(a Args) Call {
		return Call{http.MethodPut,
			fmt.Sprintf("%s/teams/%s/lineup", config.CMP, a.TeamID),
			map[string]any{"goalkeeper": a.Goalkeeper, "defender": a.Defender,
				"midfield": a.Midfield, "striker": a.Striker,
				"tactical_formation": a.Formation}}
	}, []string{"lineup"}},

	"withdraw": {"retirar del mercado", func(a Args) Call {
		return Call{http.MethodDelete,
			fmt.Sprintf("%s/league/%s/market/%s/delete", config.CMP, a.LeagueID, a.MarketID),
			nil}
	}, []string{"market", "squad"}},
}

// ValidationCase is one row of the table both implementations are checked against: a
// refusal in one and a pass in the other is the regression that matters most here.
type ValidationCase struct {
	Name   string
	Args   Args
	Player Player
	Cash   int64
}

// Validate answers the question the harness asks: would this be refused, and how many
// things would a person be warned about?
func Validate(testCase ValidationCase) (refused bool, reason string, warnings []string) {
	cash := testCase.Cash
	warnings, err := check(testCase.Name, testCase.Args, testCase.Player, &cash)
	if err != nil {
		return true, err.Error(), nil
	}
	return false, "", warnings
}

// Build is the call an operation would make, without sending it.
func Build(name string, args Args) (Call, error) {
	spec, ok := Operations[name]
	if !ok {
		return Call{}, fmt.Errorf("%w: %s", ErrUnknown, name)
	}
	return spec.Build(args), nil
}

// Send puts the call on the wire. One attempt only: a write is not idempotent, and a retry
// is how you bid twice.
func Send(call Call) (any, error) {
	headers := config.APIHeaders()
	bearer, err := auth.Bearer()
	if err != nil {
		return nil, err
	}
	headers["Authorization"] = "Bearer " + bearer

	var body []byte
	if call.Body != nil {
		headers["Content-Type"] = "application/json"
		if body, err = json.Marshal(call.Body); err != nil {
			return nil, err
		}
	}
	separator := "?"
	if strings.Contains(call.Path, "?") {
		separator = "&"
	}
	raw, err := httpx.Fetch(httpx.Request{
		URL: config.APIBase + call.Path + separator + "x-lang=es", Method: call.Method,
		Headers: headers, Body: body, Retries: 1, Tag: "raw",
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var answer any
	if err := json.Unmarshal([]byte(raw), &answer); err != nil {
		return nil, err
	}
	return answer, nil
}

// --- the two-step guard -----------------------------------------------------

// Player is the little the guard needs to know about who is being traded, to judge the
// amount and to say something useful about it.
type Player struct {
	Name     string
	Value    float64
	MinBid   int64
	IdealBid int64
	Clause   int64
	Bids     int
	Expires  string
}

// Summary is what a person is asked to confirm.
type Summary struct {
	Token       string   `json:"token"`
	Operation   string   `json:"operation"`
	Label       string   `json:"label"`
	PlayerName  string   `json:"player_name"`
	Amount      int64    `json:"amount"`
	MinBid      int64    `json:"min_bid"`
	Bids        int      `json:"bids"`
	Expires     string   `json:"expires"`
	IdealBid    int64    `json:"ideal_bid"`
	MarketValue float64  `json:"market_value"`
	CashBefore  *int64   `json:"cash_before"`
	CashAfter   *int64   `json:"cash_after"`
	Warnings    []string `json:"warnings"`
	ExpiresIn   int      `json:"expires_in"`
}

type pending struct {
	created time.Time
	name    string
	args    Args
	summary Summary
}

type Guard struct {
	mu      sync.Mutex
	tokens  map[string]pending
	client  *api.Client
	// Cash reads the current balance. Injected so the guard can be exercised without a
	// session, and so a test cannot accidentally reach the API.
	Cash func(teamID string) (int64, error)
}

func NewGuard(client *api.Client) *Guard {
	guard := &Guard{tokens: map[string]pending{}, client: client}
	guard.Cash = func(teamID string) (int64, error) {
		money, err := client.Money(teamID, 0)
		if err != nil {
			return 0, err
		}
		return int64(money.TeamMoney), nil
	}
	return guard
}

func (g *Guard) purge() {
	for token, entry := range g.tokens {
		if time.Since(entry.created) > PrepareTTL {
			delete(g.tokens, token)
		}
	}
}

// Prepare validates an operation and returns a summary plus a single-use token.
func (g *Guard) Prepare(name string, args Args, who Player, allowWrites bool) (Summary, error) {
	if !allowWrites {
		return Summary{}, ErrDisabled
	}
	spec, ok := Operations[name]
	if !ok {
		return Summary{}, fmt.Errorf("%w: %s", ErrUnknown, name)
	}

	var cash *int64
	if g.Cash != nil && args.TeamID != "" {
		if balance, err := g.Cash(args.TeamID); err == nil {
			cash = &balance
		} else {
			slog.Warn("cash unreadable before write", "reason", err.Error())
		}
	}

	warnings, err := check(name, args, who, cash)
	if err != nil {
		return Summary{}, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	g.purge()

	summary := Summary{
		Token: newToken(), Operation: name, Label: spec.Label, PlayerName: who.Name,
		Amount: args.Amount, MinBid: who.MinBid, Bids: who.Bids, Expires: who.Expires,
		IdealBid: who.IdealBid, MarketValue: who.Value, CashBefore: cash,
		CashAfter: after(cash, args.Amount, name), Warnings: warnings,
		ExpiresIn: int(PrepareTTL.Seconds()),
	}
	g.tokens[summary.Token] = pending{created: time.Now(), name: name, args: args,
		summary: summary}
	slog.Info("write prepared", "operation", name, "amount", args.Amount, "player", who.Name)
	return summary, nil
}

// Confirm spends the token and performs the call. The token is consumed either way, so a
// failed attempt cannot be replayed.
func (g *Guard) Confirm(token string, allowWrites, dryRun bool) (map[string]any, error) {
	if !allowWrites {
		return nil, ErrDisabled
	}
	g.mu.Lock()
	g.purge()
	entry, ok := g.tokens[token]
	delete(g.tokens, token)
	g.mu.Unlock()
	if !ok {
		return nil, errors.New("confirmacion caducada o ya usada: vuelve a empezar")
	}

	call, err := Build(entry.name, entry.args)
	if err != nil {
		return nil, err
	}
	if dryRun {
		slog.Info("write dry-run", "operation", entry.name, "method", call.Method,
			"path", call.Path)
		return map[string]any{"ok": true, "dry_run": true, "operation": entry.name,
			"summary": entry.summary, "call": call}, nil
	}

	answer, err := Send(call)
	if err != nil {
		return nil, fmt.Errorf("la API ha rechazado la operacion: %w", err)
	}
	dropped := httpx.Invalidate(Operations[entry.name].Effects...)
	slog.Info("write done", "operation", entry.name, "amount", entry.args.Amount,
		"cache_dropped", dropped)
	return map[string]any{"ok": true, "operation": entry.name, "summary": entry.summary,
		"response": answer}, nil
}

// check is the validation, kept as one function so the rules are read together. Refusals
// are errors; everything a person should know but may still choose is a warning.
func check(name string, args Args, who Player, cash *int64) ([]string, error) {
	var warnings []string

	switch name {
	case "accept_offer":
		if who.Value > 0 && args.Amount > 0 && float64(args.Amount) < who.Value*0.9 {
			warnings = append(warnings,
				fmt.Sprintf("por debajo de su valor de mercado (%s)", money(int64(who.Value))))
		}
		if who.IdealBid > 0 && args.Amount > who.IdealBid {
			warnings = append(warnings,
				"te pagan mas de lo que futbolfantasy considera rentable: buena venta")
		}

	case "direct_offer", "pay_clause":
		if args.Amount <= 0 {
			return nil, errors.New("el importe tiene que ser positivo")
		}
		if cash != nil && args.Amount > *cash {
			return nil, fmt.Errorf("no te llega: tienes %s", money(*cash))
		}
		if name == "pay_clause" && who.Clause > 0 && args.Amount < who.Clause {
			return nil, fmt.Errorf("la clausula es %s", money(who.Clause))
		}

	case "bid", "modify_bid":
		if args.Amount <= 0 {
			return nil, errors.New("la puja tiene que ser un importe positivo")
		}
		if who.MinBid > 0 && args.Amount < who.MinBid {
			return nil, fmt.Errorf("la puja minima es %s", money(who.MinBid))
		}
		if cash != nil && args.Amount > *cash {
			return nil, fmt.Errorf("no te llega: tienes %s", money(*cash))
		}
		if who.IdealBid > 0 {
			if args.Amount > who.IdealBid {
				warnings = append(warnings,
					fmt.Sprintf("por encima de la puja maxima rentable de futbolfantasy (%s)",
						money(who.IdealBid)))
			}
		} else {
			warnings = append(warnings, "futbolfantasy no le ve rentabilidad a este precio")
		}
		if cash != nil && float64(args.Amount) > 0.5*float64(*cash) {
			warnings = append(warnings, "te deja con menos de la mitad del saldo")
		}
		// Only the count is published, never the amounts, so this is all there is to say.
		if who.Bids > 0 {
			warnings = append(warnings, fmt.Sprintf("ya hay %d puja(s) por el", who.Bids))
		}
	}
	return warnings, nil
}

// after is the balance the operation would leave: out for a purchase, in for a sale.
func after(cash *int64, amount int64, name string) *int64 {
	if cash == nil || amount == 0 {
		return cash
	}
	switch name {
	case "bid", "modify_bid", "direct_offer", "pay_clause":
		left := *cash - amount
		return &left
	case "accept_offer":
		left := *cash + amount
		return &left
	}
	return cash
}

func money(amount int64) string {
	text := fmt.Sprintf("%d", amount)
	var groups []string
	for len(text) > 3 {
		groups = append([]string{text[len(text)-3:]}, groups...)
		text = text[:len(text)-3]
	}
	groups = append([]string{text}, groups...)
	return strings.Join(groups, ".")
}

func newToken() string {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		// A predictable token would defeat the guard, so failing loudly is the only
		// acceptable outcome.
		panic("no hay entropia para un token de confirmacion: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Do runs an operation in one step, for the ones that move no money: a lineup change is
// undone by another lineup change, so making a person confirm it twice buys nothing. Anything
// with an amount still goes through Prepare and Confirm.
func (g *Guard) Do(name string, args Args, who Player, allowWrites bool) (map[string]any, error) {
	if args.Amount != 0 {
		return nil, fmt.Errorf("%s mueve dinero: usa prepare y confirm", name)
	}
	summary, err := g.Prepare(name, args, who, allowWrites)
	if err != nil {
		return nil, err
	}
	return g.Confirm(summary.Token, allowWrites, false)
}
