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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

const PrepareTTL = 120 * time.Second

var (
	ErrDisabled = errors.New("este servidor corre en modo solo lectura")
	ErrUnknown  = errors.New("operacion desconocida")
	// ErrLoading is a write asked for before the first build is over.
	ErrLoading = errors.New("Cargando la liga, espera a que termine")
	// ErrOutdated is the API saying the page is behind: what is being acted on is already gone,
	// so the caller rebuilds instead of only reporting it.
	ErrOutdated = errors.New("esa jugada ya no esta viva")
	ErrNoMember = errors.New("no se cual es tu equipo en esta liga: recarga la pagina")
	ErrNotMine  = errors.New("no es tuyo")
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
	// Favourite only: where the star lands.
	Favourite bool
}

// where is the league and team a call is built for, stamped from the member and never from Args.
type where struct{ LeagueID, TeamID string }

type operation struct {
	Label string
	Build func(where, Args) Call
	// Which cached answers this write makes false.
	Effects []string
}

// Operations, with the id semantics spelled out. Each comment is a bug that was paid for.
var Operations = map[string]operation{
	"bid": {"puja", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/%s/bid", config.CMP, w.LeagueID, a.MarketID),
			map[string]any{"money": a.Amount}}
	}, []string{"market", "money"}},

	// A rival's sale is not bid on, it is offered for: POST .../market/{id}/bid answers 404 for
	// a marketPlayerTeam entry, because the bid handler only knows the game's own listings.
	// Same route family as accepting one, which is .../offer/{id}/accept.
	"buy_offer": {"oferta de compra", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/%s/offer", config.CMP, w.LeagueID, a.MarketID),
			map[string]any{"money": a.Amount}}
	}, []string{"market", "money"}},

	// An offer cannot be changed, only withdrawn: this one sends the new offer, and confirm
	// withdraws the old one (OfferID) just before.
	"change_offer": {"cambiar la oferta", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/%s/offer", config.CMP, w.LeagueID, a.MarketID),
			map[string]any{"money": a.Amount}}
	}, []string{"market", "money"}},

	"modify_bid": {"modificar puja", func(w where, a Args) Call {
		return Call{http.MethodPut,
			fmt.Sprintf("%s/league/%s/market/%s/bid/%s", config.CMP, w.LeagueID, a.MarketID, a.BidID),
			map[string]any{"money": a.Amount}}
	}, []string{"market", "money"}},

	// Withdrawing an offer sent to a rival. Same shape as cancelling a bid, which is the only
	// evidence available: the route answers 405 to a GET, so it exists, and the bid version is
	// DELETE .../cancel. If the verb is wrong the API says so and the message now travels.
	"cancel_offer": {"retirar la oferta", func(w where, a Args) Call {
		return Call{http.MethodDelete,
			fmt.Sprintf("%s/league/%s/market/%s/offer/%s/cancel", config.CMP, w.LeagueID,
				a.MarketID, a.OfferID),
			nil}
	}, []string{"market", "money"}},

	"cancel_bid": {"cancelar puja", func(w where, a Args) Call {
		return Call{http.MethodDelete,
			fmt.Sprintf("%s/league/%s/market/%s/bid/%s/cancel", config.CMP, w.LeagueID, a.MarketID, a.BidID),
			nil}
	}, []string{"market", "money"}},

	// The slot id, not the player's. What you pay is valueToIncrease and the clause goes up by
	// ClauseFactor times that, which is the app's own arithmetic: pay 8.555 and the clause
	// gains 17.110.
	"raise_clause": {"subir clausula", func(w where, a Args) Call {
		return Call{http.MethodPut,
			fmt.Sprintf("%s/league/%s/buyout/player", config.CMP, w.LeagueID),
			map[string]any{"playerId": a.PlayerTeamID, "factor": ClauseFactor,
				"valueToIncrease": a.Amount}}
	}, []string{"squad"}},

	// The API demands the amount in the body as a confirmation of what is being accepted;
	// without it, it answers 400.
	"accept_offer": {"aceptar oferta", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/%s/offer/%s/accept", config.CMP, w.LeagueID, a.MarketID, a.OfferID),
			map[string]any{"offerMoney": a.Amount}}
	}, []string{"market", "squad", "money", "activity", "standing", "offers"}},

	"decline_offer": {"rechazar oferta", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/%s/offer/%s/reject", config.CMP, w.LeagueID, a.MarketID, a.OfferID),
			nil}
	}, []string{"market", "offers"}},

	// `playerId` here is the squad-slot id (playerTeamId), not the player's own id.
	// Sending the player id answers 500. Listing him is not selling him, so the cash and
	// the log are untouched.
	"sell_to_market": {"poner en venta", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/sell", config.CMP, w.LeagueID),
			map[string]any{"playerId": a.PlayerTeamID, "salePrice": a.Amount}}
	}, []string{"market", "squad"}},

	// A direct offer goes against the listing, so what travels as `playerId` is the market
	// id: you can only offer for somebody who is on the market.
	"direct_offer": {"oferta directa", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/market/direct-offer", config.CMP, w.LeagueID),
			map[string]any{"playerId": a.MarketID, "money": a.Amount}}
	}, []string{"market", "money"}},

	// The daily reward. The path stops at the league and the team travels in the body, which
	// is why every team-scoped path we probed answered 404. The advert is a flag: the API takes
	// our word for it and attaches no proof.
	"claim_daily_reward": {"cobrar la recompensa diaria", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/team/daily-reward", config.CMP, w.LeagueID),
			map[string]any{"rewardedAdType": "dailyreward", "rewardedAd": 1,
				"teamId": w.TeamID}}
	}, []string{"money", "reward"}},

	// The shield: 24h in which nobody can pay his clause. Found by verb rather than by path,
	// because POST, PATCH and DELETE answer 405 here and only PUT reaches the handler. The id
	// is the slot's, like a clause rise, and the advert is a flag the API takes on trust.
	"shield_player": {"blindar jugador", func(w where, a Args) Call {
		return Call{http.MethodPut,
			fmt.Sprintf("%s/league/%s/shield/player", config.CMP, w.LeagueID),
			map[string]any{"playerId": a.PlayerTeamID, "rewardedAdType": "Blindaje",
				"rewardedAd": 1}}
	}, []string{"squad"}},

	"pay_clause": {"pagar clausula", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/league/%s/buyout/%s/pay", config.CMP, w.LeagueID, a.PlayerTeamID),
			map[string]any{"buyoutClauseToPay": a.Amount}}
	}, []string{"squad", "money", "activity", "standing", "market"}},

	// Shape matters: goalkeeper is a single id, not a list, and the key is snake_case.
	// Anything else answers 500.
	"save_lineup": {"guardar alineacion", func(w where, a Args) Call {
		return Call{http.MethodPut,
			fmt.Sprintf("%s/teams/%s/lineup", config.CMP, w.TeamID),
			map[string]any{"goalkeeper": a.Goalkeeper, "defender": a.Defender,
				"midfield": a.Midfield, "striker": a.Striker,
				"tactical_formation": a.Formation}}
	}, []string{"own_lineup", "lineup"}},

	// The app's own favourites, keyed by the team in the league and the player's master id.
	"favourite": {"favorito", func(w where, a Args) Call {
		return Call{http.MethodPost,
			fmt.Sprintf("%s/teams/%s/favourite-players", config.CMP, w.TeamID),
			map[string]any{"player": numericID(a.PlayerID), "favourite": a.Favourite}}
	}, []string{"favourites"}},

	"withdraw": {"retirar del mercado", func(w where, a Args) Call {
		return Call{http.MethodDelete,
			fmt.Sprintf("%s/league/%s/market/%s/delete", config.CMP, w.LeagueID, a.MarketID),
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
	warnings, err := check(testCase.Name, testCase.Args, testCase.Player, &cash, nil)
	if err != nil {
		return true, err.Error(), nil
	}
	return false, "", warnings
}

func Build(name string, member *api.Member, args Args) (Call, error) {
	if member == nil {
		return Call{}, ErrNoMember
	}
	return build(name, where{member.LeagueID(), member.TeamID()}, args)
}

// Shape builds with L and T for league and team, to print what an operation sends.
func Shape(name string, args Args) (Call, error) {
	return build(name, where{"L", "T"}, args)
}

func build(name string, at where, args Args) (Call, error) {
	spec, ok := Operations[name]
	if !ok {
		return Call{}, fmt.Errorf("%w: %s", ErrUnknown, name)
	}
	return spec.Build(at, args), nil
}

// explain turns an API refusal into something a person can act on. The URL and the JSON body
// stay in the log: quoting them in a dialog says nothing about what to do next.
func explain(err error) error {
	code, message := refusal(err.Error())
	if code == "" && message == "" {
		return err
	}
	slog.Warn("api refusal", "code", code, "message", message, "raw", err.Error())
	switch code {
	// The offer was rejected or expired while the page still showed it: nothing to retire, and
	// nothing the person got wrong. The view was behind.
	case "030.01.48":
		return fmt.Errorf("%w: la rechazaron o caduco, asi que no hay nada que retirar",
			ErrOutdated)
	// The clause window, which the API states in English and without saying when it reopens.
	// Reached only when nothing told the guard the calendar; otherwise this never fires.
	case "030.01.17":
		return errors.New("no se pueden pagar clausulas en las 24h antes de que arranque la " +
			"jornada: reabre con el primer partido")
	}
	// A received offer answered twice, from the app and here or by a double click: it is
	// already off the table, which is what the person wanted.
	if strings.Contains(message, "already rejected") || strings.Contains(message, "already accepted") {
		return fmt.Errorf("%w: esa oferta ya estaba contestada", ErrOutdated)
	}
	return errors.New(message)
}

// refusal reads the code and the sentence out of the API's error body, which travels inside the
// transport error as JSON.
func refusal(text string) (string, string) {
	start := strings.Index(text, "{")
	if start < 0 {
		return "", ""
	}
	var body struct {
		Message   string `json:"message"`
		ErrorCode string `json:"errorCode"`
	}
	if json.Unmarshal([]byte(text[start:]), &body) != nil {
		return "", ""
	}
	return body.ErrorCode, body.Message
}

// --- the two-step guard -----------------------------------------------------

// ClauseFactor is what the game multiplies a clause rise by: you pay an amount and the clause
// goes up by twice it. The page has to say so, because paying 4.6M and watching the clause gain
// 9.3M is not something anybody should discover afterwards.
const ClauseFactor = 2

// Spends are the operations that take money out of the balance. Only they need a balance to be
// checked against, and an unattended one must not run without it.
var Spends = map[string]bool{
	"bid": true, "modify_bid": true, "buy_offer": true, "change_offer": true, "direct_offer": true,
	"pay_clause": true, "raise_clause": true,
}

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
	// Our own bid on this listing, when there is one. The API answers a second bid with a
	// bare 400, so knowing about it is what turns that into a sentence.
	MyBidID string
	MyBid   int64
	// The league's hold rule: bought too recently to be sold. No API enforces this, which is
	// exactly why the tool has to.
	SaleLocked bool
	HoldUntil  string
	// Exceptions the league agreed, in its own words, so the refusal says where to go next.
	HoldExceptions string
	Available      bool
	// DebtRoom is how far below zero a market bid may take the balance under the league's
	// rules, and Committed what my other live bids and offers already hold: the same limit the
	// page offers bids up to.
	DebtRoom  int64
	Committed int64
	// Whether the seller takes direct offers. Absent when the listing does not say, and only
	// an explicit no is treated as one.
	DirectOffer *bool
	Owner       string
	// Shielded, and until when: a second shield over a live one spends the advert for nothing.
	Shielded      bool
	ShieldedUntil string
	// CeilingUnread is a player whose futbolfantasy page could not be read: his IdealBid is
	// unknown, not zero, and nothing is checked against it as if it were.
	CeilingUnread bool
	// CeilingAge is how old IdealBid is when the page could not be read again and the stored
	// ceiling stands in for it: "hace 9 h".
	CeilingAge string
}

// JudgedByCeiling are the operations whose check reads futbolfantasy's profitable ceiling.
var JudgedByCeiling = map[string]bool{
	"bid": true, "modify_bid": true, "buy_offer": true, "change_offer": true, "accept_offer": true,
}

// Summary is what a person is asked to confirm.
type Summary struct {
	Token       string  `json:"token"`
	Operation   string  `json:"operation"`
	Label       string  `json:"label"`
	PlayerName  string  `json:"player_name"`
	Amount      int64   `json:"amount"`
	MinBid      int64   `json:"min_bid"`
	Bids        int     `json:"bids"`
	Expires     string  `json:"expires"`
	IdealBid    int64   `json:"ideal_bid"`
	MarketValue float64 `json:"market_value"`
	// What the clause is and what it would become, for the one operation that changes it.
	Clause     int64    `json:"clause"`
	NewClause  int64    `json:"new_clause"`
	CashBefore *int64   `json:"cash_before"`
	CashAfter  *int64   `json:"cash_after"`
	Warnings   []string `json:"warnings"`
	ExpiresIn  int      `json:"expires_in"`
}

type pending struct {
	created time.Time
	owner   string
	member  *api.Member
	name    string
	args    Args
	who     Player
	summary Summary
}

// Window is when the game accepts a clause payment: whether it is open and, when it is not,
// the hour it opens again. A nil window is nobody having told the guard, and then it has no
// opinion — refusing on an unknown would stop every payment there is.
type Window struct {
	Open    bool
	OpensAt string
}

type Guard struct {
	mu     sync.Mutex
	tokens map[string]pending
	member func() (*api.Member, error)
	// Rehearsal makes every write a pretence: it is checked, confirmed and patched into the world
	// as usual, and never reaches LaLiga. For trying the panel's flows against the real league.
	Rehearsal bool
	// Cash reads the current balance. Injected so the guard can be exercised without a
	// session, and so a test cannot accidentally reach the API.
	Cash func(teamID string) (int64, error)
	// Clauses is the clause window, read at the moment of the write rather than kept, because
	// it changes with the clock. Optional: without it the API's refusal is the only warning.
	Clauses func() *Window
	// Loading says the world is still being read for the first time. While it is, every
	// operation is refused, the unattended ones too: half a league is no ground to act on.
	Loading func() bool
	// Squad is what the member owns; without one, nothing acting on its own players goes through.
	Squad func() *Squad
}

type Squad struct {
	Slots    map[string]bool
	Listings map[string]bool
}

func (g *Guard) loading() bool { return g.Loading != nil && g.Loading() }

func NewGuard(member *api.Member) *Guard {
	return GuardOf(func() (*api.Member, error) { return member, nil })
}

// GuardOf asks for its member at each write, for a server that learns its league after start.
func GuardOf(member func() (*api.Member, error)) *Guard {
	guard := &Guard{tokens: map[string]pending{}, member: member}
	guard.Cash = func(teamID string) (int64, error) {
		member, err := guard.resolve()
		if err != nil {
			return 0, err
		}
		if teamID != member.TeamID() {
			return 0, fmt.Errorf("el saldo del equipo %s no es de este jugador", teamID)
		}
		money, err := member.Money(0)
		if err != nil {
			return 0, err
		}
		return int64(money.TeamMoney), nil
	}
	return guard
}

func (g *Guard) resolve() (*api.Member, error) {
	if g.member == nil {
		return nil, ErrNoMember
	}
	member, err := g.member()
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, ErrNoMember
	}
	return member, nil
}

func same(one, other *api.Member) bool {
	return one.Profile().Account == other.Profile().Account &&
		one.LeagueID() == other.LeagueID() && one.TeamID() == other.TeamID()
}

// An empty id is left to check, which refuses it with a sentence of its own.
func (g *Guard) mine(name string, args Args) error {
	var id string
	slot := false
	switch name {
	case "sell_to_market", "raise_clause", "shield_player":
		id, slot = args.PlayerTeamID, true
	case "accept_offer", "decline_offer", "withdraw":
		id = args.MarketID
	default:
		return nil
	}
	if id == "" {
		return nil
	}
	var squad *Squad
	if g.Squad != nil {
		squad = g.Squad()
	}
	if squad == nil {
		return errors.New("no se cual es tu plantilla: recarga la pagina")
	}
	owned := squad.Listings
	if slot {
		owned = squad.Slots
	}
	if !owned[id] {
		return fmt.Errorf("%w: %s no esta en tu plantilla ni en tus anuncios", ErrNotMine, id)
	}
	return nil
}

func (g *Guard) send(member *api.Member, call Call) (any, error) {
	if g.Rehearsal {
		slog.Info("write rehearsed, not sent", "method", call.Method, "path", call.Path)
		return nil, nil
	}
	answer, err := member.Send(api.Call(call))
	if err != nil {
		return nil, explain(err)
	}
	return answer, nil
}

// window is the clause window if anybody wired one, and nothing otherwise.
func (g *Guard) window() *Window {
	if g.Clauses == nil {
		return nil
	}
	return g.Clauses()
}

// whenText is a stamp somebody can read out loud: day and hour, no year and no zone. Empty for
// anything unparseable, so a refusal never carries half a date.
func whenText(stamp string) string {
	when, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return ""
	}
	days := []string{"domingo", "lunes", "martes", "miercoles", "jueves", "viernes", "sabado"}
	return fmt.Sprintf("%s a las %02d:%02d", days[int(when.Weekday())], when.Hour(), when.Minute())
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
	return g.PrepareFor("", name, args, who, allowWrites)
}

func (g *Guard) PrepareFor(owner, name string, args Args, who Player, allowWrites bool) (Summary, error) {
	if !allowWrites {
		return Summary{}, ErrDisabled
	}
	if g.loading() {
		return Summary{}, ErrLoading
	}
	spec, ok := Operations[name]
	if !ok {
		return Summary{}, fmt.Errorf("%w: %s", ErrUnknown, name)
	}
	member, err := g.resolve()
	if err != nil {
		return Summary{}, err
	}
	if err := g.mine(name, args); err != nil {
		return Summary{}, err
	}

	cash := g.balance(member)
	warnings, err := check(name, args, who, cash, g.window())
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
	if name == "raise_clause" {
		summary.Clause = who.Clause
		summary.NewClause = who.Clause + ClauseFactor*args.Amount
	}
	g.tokens[summary.Token] = pending{created: time.Now(), owner: owner, member: member,
		name: name, args: args, who: who, summary: summary}
	slog.Info("write prepared", "operation", name, "amount", args.Amount, "player", who.Name)
	return summary, nil
}

// balance is the current cash, or nil when it cannot be read.
func (g *Guard) balance(member *api.Member) *int64 {
	if g.Cash == nil {
		return nil
	}
	balance, err := g.Cash(member.TeamID())
	if err != nil {
		slog.Warn("cash unreadable before write", "reason", err.Error())
		return nil
	}
	return &balance
}

// Confirm spends the token and performs the call. The token is consumed either way, so a
// failed attempt cannot be replayed. The checks run again, since the cash and the clause
// window may have moved while the person read the summary.
func (g *Guard) Confirm(token string, allowWrites, dryRun bool) (map[string]any, error) {
	return g.confirm("", token, allowWrites, dryRun, true)
}

func (g *Guard) ConfirmFor(owner, token string, allowWrites, dryRun bool) (map[string]any, error) {
	return g.confirm(owner, token, allowWrites, dryRun, true)
}

func (g *Guard) confirm(owner, token string, allowWrites, dryRun, recheck bool) (map[string]any, error) {
	if !allowWrites {
		return nil, ErrDisabled
	}
	if g.loading() {
		return nil, ErrLoading
	}
	member, err := g.resolve()
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.purge()
	entry, ok := g.tokens[token]
	if ok && entry.owner != owner {
		g.mu.Unlock()
		return nil, errors.New("esa confirmacion es de otra sesion: vuelve a empezar")
	}
	if ok && !same(entry.member, member) {
		g.mu.Unlock()
		return nil, errors.New("esa confirmacion es de otro equipo: vuelve a empezar")
	}
	delete(g.tokens, token)
	g.mu.Unlock()
	if !ok {
		return nil, errors.New("confirmacion caducada o ya usada: vuelve a empezar")
	}
	if recheck {
		if err := g.mine(entry.name, entry.args); err != nil {
			return nil, err
		}
		if _, err := check(entry.name, entry.args, entry.who, g.balance(member),
			g.window()); err != nil {
			return nil, err
		}
	}

	call, err := Build(entry.name, member, entry.args)
	if err != nil {
		return nil, err
	}
	if dryRun {
		slog.Info("write dry-run", "operation", entry.name, "method", call.Method,
			"path", call.Path)
		return map[string]any{"ok": true, "dry_run": true, "operation": entry.name,
			"summary": entry.summary, "call": call}, nil
	}

	if entry.name == "change_offer" {
		withdraw, err := Build("cancel_offer", member, entry.args)
		if err != nil {
			return nil, err
		}
		if _, err := g.send(member, withdraw); err != nil {
			return nil, fmt.Errorf("no he podido retirar tu oferta anterior, sigue en pie: %w", err)
		}
		slog.Info("write done", "operation", "cancel_offer", "player", entry.summary.PlayerName,
			"method", withdraw.Method, "path", withdraw.Path, "for", "change_offer")
	}
	answer, err := g.send(member, call)
	if err != nil {
		if entry.name == "change_offer" {
			member.Invalidate(Operations[entry.name].Effects...)
			return nil, fmt.Errorf("he retirado tu oferta anterior, pero LaLiga ha rechazado la "+
				"nueva: ahora no tienes ninguna oferta por %s: %w", entry.summary.PlayerName, err)
		}
		// The args travel back with an outdated refusal too, so the page can drop what it was
		// showing instead of waiting for a market that still lists it.
		if errors.Is(err, ErrOutdated) {
			return map[string]any{"operation": entry.name, "args": entry.args}, err
		}
		return nil, fmt.Errorf("la API ha rechazado la operacion: %w", err)
	}
	dropped := member.Invalidate(Operations[entry.name].Effects...)
	slog.Info("write done", "operation", entry.name, "player", entry.summary.PlayerName,
		"amount", entry.args.Amount, "method", call.Method, "path", call.Path,
		"cash_before", entry.summary.CashBefore, "cache_dropped", dropped)
	// The args travel back because the page sends nothing but the token: whoever has to patch
	// the world with what this did cannot know which offer it was otherwise.
	return map[string]any{"ok": true, "operation": entry.name, "summary": entry.summary,
		"args": entry.args, "response": answer}, nil
}

// check is the validation, kept as one function so the rules are read together. Refusals
// are errors; everything a person should know but may still choose is a warning.
func check(name string, args Args, who Player, cash *int64, window *Window) ([]string, error) {
	var warnings []string

	// Unknown is not zero: read as zero, a missing ceiling says "futbolfantasy sees no margin"
	// about a player it was never asked about. Nor is it a reason to refuse: the call is yours.
	if who.CeilingUnread && JudgedByCeiling[name] {
		player := fallbackName(who.Name)
		warnings = append(warnings, fmt.Sprintf("no he podido leer la ficha de futbolfantasy "+
			"de %s: sin techo rentable, decide tú", player))
		who.IdealBid = 0
	} else if who.CeilingAge != "" && JudgedByCeiling[name] {
		warnings = append(warnings, fmt.Sprintf("no he podido leer la ficha de futbolfantasy "+
			"de %s: su techo rentable es el guardado %s", fallbackName(who.Name), who.CeilingAge))
	}

	// The answer goes through .../market/{id}/offer/{id}: an offer for somebody I never listed
	// has no market id, and no route for it has been seen yet.
	// Without the bid's id the path ends in .../bid//cancel and the bid stays live.
	if (name == "cancel_bid" || name == "modify_bid") && args.BidID == "" {
		return nil, errors.New("falta el id de tu puja: abre la puja desde el mercado")
	}
	if (name == "accept_offer" || name == "decline_offer") && args.MarketID == "" {
		return nil, errors.New("esta oferta es por un jugador que no tienes en venta: " +
			"respondela en la app de LaLiga")
	}
	if name == "withdraw" && args.MarketID == "" {
		return nil, errors.New("no se cual es su anuncio: recarga la pagina")
	}

	// The clause window is the calendar's rule and not the market's: the API refuses a payment
	// while a fixture starts within the day (030.01.17). Reading it here turns that refusal into
	// a sentence with an hour in it, before anything is sent.
	if name == "pay_clause" && window != nil && !window.Open {
		reason := "no se pueden pagar clausulas con la jornada a menos de un dia"
		if when := whenText(window.OpensAt); when != "" {
			reason += ": reabre el " + when
		}
		return nil, errors.New(reason)
	}

	// The house rule first: it is not the API that refuses, it is the league, and finding out
	// afterwards means having sold somebody you had agreed not to sell.
	// Selling is taking an offer. Listing him is not, and offers take a while to come, so he may
	// be listed during the hold; only accepting waits for its end.
	if who.SaleLocked {
		until := who.HoldUntil
		if len(until) > 10 {
			until = until[:10]
		}
		switch name {
		case "sell_to_market":
			if who.Available {
				warnings = append(warnings, fmt.Sprintf(
					"la norma no deja aceptar ofertas por el hasta el %s", until))
			}
		case "accept_offer", "direct_offer":
			if who.Available {
				reason := fmt.Sprintf("la norma de la liga no deja venderlo hasta el %s", until)
				if who.HoldExceptions != "" {
					reason += " (excepciones acordadas: " + who.HoldExceptions + ")"
				}
				return nil, errors.New(reason)
			}
			// Unavailable is the case the rule itself contemplates, so it warns instead of
			// refusing: the decision is the league's, not the tool's.
			warnings = append(warnings, fmt.Sprintf(
				"la norma no deja venderlo hasta el %s, pero esta lesionado o sancionado: "+
					"acuerdalo antes", until))
		}
	}

	// A listing and a clause are addressed by the squad slot, not by the player: an empty one
	// reaches the API as playerId "" and comes back as 400 "Player not found".
	switch name {
	case "sell_to_market", "pay_clause", "raise_clause", "shield_player":
		if args.PlayerTeamID == "" {
			return nil, errors.New("no se cual es su ficha en la plantilla: recarga la pagina")
		}
	}

	switch name {
	case "shield_player":
		if who.Shielded {
			until := who.ShieldedUntil
			if len(until) >= 16 {
				until = until[8:10] + "/" + until[5:7] + " " + until[11:16]
			}
			return nil, fmt.Errorf("ya esta blindado hasta el %s: el anuncio se gastaria "+
				"sin darte nada", until)
		}
	case "sell_to_market":
		if args.Amount <= 0 {
			return nil, errors.New("el precio de venta tiene que ser positivo")
		}
	case "accept_offer":
		if who.Value > 0 && args.Amount > 0 && float64(args.Amount) < who.Value*0.9 {
			warnings = append(warnings,
				fmt.Sprintf("por debajo de su valor de mercado (%s)", money(int64(who.Value))))
		}
		if who.IdealBid > 0 && args.Amount > who.IdealBid {
			warnings = append(warnings,
				"te pagan mas de lo que futbolfantasy considera rentable: buena venta")
		}

	case "change_offer":
		if args.OfferID == "" {
			return nil, errors.New("falta el id de tu oferta: abrela desde el mercado")
		}
		if args.Amount <= 0 {
			return nil, errors.New("la oferta tiene que ser un importe positivo")
		}
		if who.MyBid > 0 && args.Amount == who.MyBid {
			return nil, fmt.Errorf("ya le ofreces %s", money(who.MyBid))
		}
		// Committed counts the other listings only, so the offer being replaced frees its money.
		if cash != nil && args.Amount > *cash-who.Committed {
			return nil, fmt.Errorf("no te llega: tienes %s y %s en otras pujas", money(*cash),
				money(who.Committed))
		}
		if who.MinBid > 0 && args.Amount < who.MinBid {
			warnings = append(warnings, fmt.Sprintf("piden %s: por debajo puede que ni la miren",
				money(who.MinBid)))
		}
		if who.IdealBid > 0 && args.Amount > who.IdealBid {
			warnings = append(warnings,
				fmt.Sprintf("por encima de la puja maxima rentable de futbolfantasy (%s)",
					money(who.IdealBid)))
		}
		warnings = append(warnings, "se retira tu oferta actual y se manda esta: la nueva le "+
			"llega como otra oferta")

	case "buy_offer":
		if args.Amount <= 0 {
			return nil, errors.New("la oferta tiene que ser un importe positivo")
		}
		// The API refuses a second one with "Team has pending offer in this player", and it is
		// right: there is no route to change an offer, only to withdraw it.
		if who.MyBidID != "" {
			return nil, fmt.Errorf("ya tienes una oferta de %s por el: retirala primero, que "+
				"cambiarla no se puede", money(who.MyBid))
		}
		if cash != nil && args.Amount > *cash-who.Committed {
			return nil, fmt.Errorf("no te llega: tienes %s y %s en otras pujas", money(*cash),
				money(who.Committed))
		}
		if who.MinBid > 0 && args.Amount < who.MinBid {
			warnings = append(warnings, fmt.Sprintf("piden %s: por debajo puede que ni la miren",
				money(who.MinBid)))
		}
		if who.IdealBid > 0 && args.Amount > who.IdealBid {
			warnings = append(warnings,
				fmt.Sprintf("por encima de la puja maxima rentable de futbolfantasy (%s)",
					money(who.IdealBid)))
		}

	case "raise_clause":
		if args.Amount <= 0 {
			return nil, errors.New("el importe tiene que ser positivo")
		}
		if cash != nil && args.Amount > *cash {
			return nil, fmt.Errorf("no te llega: tienes %s", money(*cash))
		}

	case "direct_offer", "pay_clause":
		if args.Amount <= 0 {
			return nil, errors.New("el importe tiene que ser positivo")
		}
		if name == "direct_offer" && who.DirectOffer != nil && !*who.DirectOffer {
			owner := who.Owner
			if owner == "" {
				owner = "su dueno"
			}
			return nil, fmt.Errorf("%s no acepta ofertas directas por el: solo se puede pujar "+
				"por su venta", owner)
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
		if name == "bid" && who.MyBidID != "" {
			return nil, fmt.Errorf("ya tienes una puja de %s en este jugador: cambiala en vez "+
				"de poner otra", money(who.MyBid))
		}
		if name == "modify_bid" {
			if args.BidID == "" {
				return nil, errors.New("no se cual es tu puja: recarga la pagina")
			}
			if who.MyBid > 0 && args.Amount == who.MyBid {
				return nil, fmt.Errorf("tu puja ya es de %s", money(who.MyBid))
			}
		}
		if who.MinBid > 0 && args.Amount < who.MinBid {
			return nil, fmt.Errorf("la puja minima es %s", money(who.MinBid))
		}
		if cash != nil && args.Amount > *cash+who.DebtRoom-who.Committed {
			return nil, fmt.Errorf("no te llega: tienes %s, %s de margen de deuda y %s en otras "+
				"pujas", money(*cash), money(who.DebtRoom), money(who.Committed))
		}
		if who.IdealBid > 0 {
			if args.Amount > who.IdealBid {
				warnings = append(warnings,
					fmt.Sprintf("por encima de la puja maxima rentable de futbolfantasy (%s)",
						money(who.IdealBid)))
			}
		} else if !who.CeilingUnread {
			warnings = append(warnings, "futbolfantasy no le ve rentabilidad a este precio")
		}
		if cash != nil && float64(args.Amount) > 0.5*float64(*cash) {
			warnings = append(warnings, "te deja con menos de la mitad del saldo")
		}
		// Only the count is published, never the amounts, so this is all there is to say —
		// except which of them is ours, which we do know.
		if who.Bids > 0 {
			note := fmt.Sprintf("ya hay %d puja(s) por el", who.Bids)
			if who.MyBidID != "" {
				note += fmt.Sprintf(", una tuya de %s", money(who.MyBid))
			}
			warnings = append(warnings, note)
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
	case "bid", "modify_bid", "buy_offer", "change_offer", "direct_offer", "pay_clause", "raise_clause":
		left := *cash - amount
		return &left
	// Money coming in. A listing does not pay today — it pays if somebody buys — and the
	// page labels it with that condition, which is why the figure is worth giving.
	case "accept_offer", "sell_to_market":
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

// Automatic performs an operation the standing instructions authorised in advance.
//
// It skips the token, not the validation. The two-step guard exists so that a person confirms
// what a click is about to spend; here the confirmation happened earlier and in writing, when the
// instruction was armed with its limit — the whole reason to arm one is that clauses open and
// offers expire while nobody is looking. Everything else still applies: the same check, the same
// cash reading, the same cache invalidation, and a louder log because nobody watched.
func (g *Guard) Automatic(name string, args Args, who Player, allowWrites bool) (any, error) {
	if !allowWrites {
		return nil, ErrDisabled
	}
	if g.loading() {
		return nil, ErrLoading
	}
	spec, ok := Operations[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknown, name)
	}
	member, err := g.resolve()
	if err != nil {
		return nil, err
	}
	if err := g.mine(name, args); err != nil {
		return nil, err
	}

	var cash *int64
	if g.Cash != nil {
		balance, err := g.Cash(member.TeamID())
		if err != nil {
			slog.Warn("cash unreadable before automatic write", "operation", name,
				"reason", err.Error())
		} else {
			cash = &balance
		}
	}
	// Nobody is watching this one, so an unknown balance is a no: paying a clause blind is how
	// the balance ends up negative.
	if cash == nil && Spends[name] {
		return nil, fmt.Errorf("no puedo leer el saldo: no gasto a ciegas sin que nadie mire")
	}
	warnings, err := check(name, args, who, cash, g.window())
	if err != nil {
		return nil, err
	}

	call, err := Build(name, member, args)
	if err != nil {
		return nil, err
	}
	slog.Info("automatic write", "operation", name, "amount", args.Amount, "player", who.Name,
		"warnings", warnings, "method", call.Method, "path", call.Path)
	answer, err := g.send(member, call)
	if err != nil {
		return nil, fmt.Errorf("la API ha rechazado la operacion: %w", err)
	}
	dropped := member.Invalidate(spec.Effects...)
	slog.Info("automatic write done", "operation", name, "amount", args.Amount,
		"player", who.Name, "cache_dropped", dropped)
	return answer, nil
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
	return g.confirm("", summary.Token, allowWrites, false, false)
}

// numericID sends a player id the way the app does, as a number.
func numericID(id string) any {
	if number, err := strconv.Atoi(id); err == nil {
		return number
	}
	return id
}

func fallbackName(name string) string {
	if name == "" {
		return "este jugador"
	}
	return name
}
