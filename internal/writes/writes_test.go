package writes

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Nothing here can reach the network: Cash is stubbed and Send is never called. Every case
// is a guarantee that stands between a click and somebody's money.

func guard(cash int64) *Guard {
	g := &Guard{tokens: map[string]pending{}}
	g.Cash = func(string) (int64, error) { return cash, nil }
	return g
}

func bidArgs(amount int64) Args {
	return Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: amount}
}

func TestTokenIsSingleUse(t *testing.T) {
	// The whole point of the two-step guard: a double click, a retry or a replayed request
	// must not bid twice.
	g := guard(50_000_000)
	summary, err := g.Prepare("bid", bidArgs(1_000_000), Player{Name: "X", IdealBid: 2_000_000}, true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := g.Confirm(summary.Token, true, true); err != nil {
		t.Fatalf("la primera confirmacion deberia valer: %v", err)
	}
	if _, err := g.Confirm(summary.Token, true, true); err == nil {
		t.Fatal("el mismo token ha valido dos veces")
	}
}

func TestTokenExpires(t *testing.T) {
	g := guard(50_000_000)
	summary, err := g.Prepare("bid", bidArgs(1_000_000), Player{Name: "X", IdealBid: 2_000_000}, true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// Reach in and age it rather than sleep for two minutes.
	g.mu.Lock()
	entry := g.tokens[summary.Token]
	entry.created = time.Now().Add(-PrepareTTL - time.Second)
	g.tokens[summary.Token] = entry
	g.mu.Unlock()

	if _, err := g.Confirm(summary.Token, true, true); err == nil {
		t.Fatal("un token caducado deberia rechazarse")
	}
}

func TestReadOnlyRefusesBothSteps(t *testing.T) {
	g := guard(50_000_000)
	if _, err := g.Prepare("bid", bidArgs(1_000_000), Player{}, false); !errors.Is(err, ErrDisabled) {
		t.Fatalf("prepare en solo lectura deberia negarse, dio %v", err)
	}
	// And confirm on its own, in case a token survived a restart into read-only.
	if _, err := g.Confirm("cualquiera", false, true); !errors.Is(err, ErrDisabled) {
		t.Fatalf("confirm en solo lectura deberia negarse, dio %v", err)
	}
}

func TestBidBelowTheFloorIsRefused(t *testing.T) {
	g := guard(50_000_000)
	_, err := g.Prepare("bid", bidArgs(900_000), Player{Name: "X", MinBid: 1_000_000}, true)
	if err == nil {
		t.Fatal("una puja por debajo del minimo deberia rechazarse")
	}
	if !strings.Contains(err.Error(), "1.000.000") {
		t.Fatalf("el motivo deberia decir el minimo con separadores: %v", err)
	}
}

func TestBidAboveCashIsRefused(t *testing.T) {
	g := guard(1_000_000)
	if _, err := g.Prepare("bid", bidArgs(2_000_000), Player{Name: "X"}, true); err == nil {
		t.Fatal("no te llega y aun asi lo ha aceptado")
	}
}

func TestBidAboveTheProfitableCeilingOnlyWarns(t *testing.T) {
	// A warning, not a refusal: paying over the odds is a decision, not a mistake.
	g := guard(50_000_000)
	summary, err := g.Prepare("bid", bidArgs(3_000_000),
		Player{Name: "X", IdealBid: 2_000_000}, true)
	if err != nil {
		t.Fatalf("deberia dejarte, avisando: %v", err)
	}
	if len(summary.Warnings) == 0 {
		t.Fatal("deberia haber avisado de que pasa el techo rentable")
	}
}

func TestNoProfitabilityAtAllStillWarns(t *testing.T) {
	g := guard(50_000_000)
	summary, err := g.Prepare("bid", bidArgs(1_000_000), Player{Name: "X"}, true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	found := false
	for _, warning := range summary.Warnings {
		if strings.Contains(warning, "rentabilidad") {
			found = true
		}
	}
	if !found {
		t.Fatalf("sin techo rentable deberia decirlo: %v", summary.Warnings)
	}
}

func TestPayingLessThanTheClauseIsRefused(t *testing.T) {
	g := guard(50_000_000)
	args := Args{LeagueID: "L", TeamID: "T", PlayerTeamID: "PT", Amount: 9_000_000}
	if _, err := g.Prepare("pay_clause", args, Player{Name: "X", Clause: 10_000_000}, true); err == nil {
		t.Fatal("pagar por debajo de la clausula deberia rechazarse")
	}
}

// The 400 the API answers to playerId "" is unreadable; refusing it here says which field is
// missing, and a listing priced at zero is not a listing.
func TestListingWithoutSlotOrPriceIsRefused(t *testing.T) {
	g := guard(50_000_000)
	if _, err := g.Prepare("sell_to_market",
		Args{LeagueID: "L", TeamID: "T", Amount: 9_000_000}, Player{Name: "X"}, true); err == nil {
		t.Error("poner en venta sin la ficha de la plantilla deberia rechazarse")
	}
	if _, err := g.Prepare("sell_to_market",
		Args{LeagueID: "L", TeamID: "T", PlayerTeamID: "PT"}, Player{Name: "X"}, true); err == nil {
		t.Error("poner en venta a cero deberia rechazarse")
	}
	if _, err := g.Prepare("pay_clause",
		Args{LeagueID: "L", TeamID: "T", Amount: 1_000_000}, Player{Name: "X"}, true); err == nil {
		t.Error("pagar una clausula sin la ficha deberia rechazarse")
	}
}

func TestAcceptingBelowMarketValueWarns(t *testing.T) {
	g := guard(0)
	args := Args{LeagueID: "L", TeamID: "T", MarketID: "M", OfferID: "O", Amount: 8_000_000}
	summary, err := g.Prepare("accept_offer", args,
		Player{Name: "X", Value: 10_000_000}, true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(summary.Warnings) == 0 {
		t.Fatal("aceptar un 20%% por debajo del valor deberia avisar")
	}
}

func TestCashMovesInTheRightDirection(t *testing.T) {
	// A bid takes money out, accepting an offer brings it in. Getting this backwards would
	// make the confirmation dialog lie about the consequence.
	g := guard(10_000_000)
	bid, err := g.Prepare("bid", bidArgs(1_000_000), Player{Name: "X", IdealBid: 5_000_000}, true)
	if err != nil {
		t.Fatalf("prepare bid: %v", err)
	}
	if bid.CashAfter == nil || *bid.CashAfter != 9_000_000 {
		t.Fatalf("una puja deberia dejar 9.000.000, dice %v", bid.CashAfter)
	}

	args := Args{LeagueID: "L", TeamID: "T", MarketID: "M", OfferID: "O", Amount: 2_000_000}
	sale, err := g.Prepare("accept_offer", args, Player{Name: "X", Value: 2_000_000}, true)
	if err != nil {
		t.Fatalf("prepare accept: %v", err)
	}
	if sale.CashAfter == nil || *sale.CashAfter != 12_000_000 {
		t.Fatalf("aceptar deberia dejar 12.000.000, dice %v", sale.CashAfter)
	}
}

func TestDryRunReturnsTheCallAndSendsNothing(t *testing.T) {
	g := guard(50_000_000)
	summary, err := g.Prepare("sell_to_market",
		Args{LeagueID: "L", TeamID: "T", PlayerTeamID: "PT", Amount: 5_000_000},
		Player{Name: "X"}, true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	answer, err := g.Confirm(summary.Token, true, true)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	call, ok := answer["call"].(Call)
	if !ok {
		t.Fatalf("el ensayo en seco deberia devolver la peticion: %v", answer)
	}
	// The slot id, not the player id: sending the player id answers 500.
	if call.Body["playerId"] != "PT" {
		t.Fatalf("deberia viajar el id de hueco de plantilla: %v", call.Body)
	}
	if answer["dry_run"] != true {
		t.Fatal("deberia marcarse como ensayo")
	}
}

func TestEveryOperationDeclaresItsEffects(t *testing.T) {
	// A write that invalidates nothing leaves the page describing a world that no longer
	// exists, which is how the squad stayed stale for half an hour after a sale.
	for name, spec := range Operations {
		if len(spec.Effects) == 0 {
			t.Errorf("%s no declara que caches invalida", name)
		}
		if spec.Label == "" {
			t.Errorf("%s no tiene etiqueta para la confirmacion", name)
		}
	}
}

// An unattended write with no readable balance is a write with no limit: it must not happen.
func TestAutomaticRefusesToSpendWithoutABalance(t *testing.T) {
	blind := &Guard{tokens: map[string]pending{},
		Cash: func(string) (int64, error) { return 0, errors.New("money 503") }}

	args := Args{LeagueID: "1", TeamID: "2", PlayerTeamID: "3", Amount: 5_000_000}
	_, err := blind.Automatic("pay_clause", args, Player{Name: "Youssef",
		Clause: 5_000_000}, true)
	if err == nil {
		t.Fatal("pagar una clausula sin poder leer el saldo deberia rechazarse")
	}
	if !strings.Contains(err.Error(), "saldo") {
		t.Errorf("el motivo deberia hablar del saldo, dijo %q", err.Error())
	}
}

// The claim was captured once, from the phone, and nothing else documents it: the path stops at
// the league and the team travels in the body. Getting either wrong is a 404 or a 400, and there
// is no second capture to compare against.
func TestClaimDailyRewardCallShape(t *testing.T) {
	call, err := Build("claim_daily_reward", Args{LeagueID: "018012894", TeamID: "38126981"})
	if err != nil {
		t.Fatal(err)
	}
	if call.Method != "POST" {
		t.Errorf("metodo %q, la captura dice POST", call.Method)
	}
	want := "/v1/competition/1/league/018012894/team/daily-reward"
	if call.Path != want {
		t.Errorf("ruta %q, esperaba %q", call.Path, want)
	}
	body := call.Body
	if body["teamId"] != "38126981" {
		t.Errorf("el equipo viaja en el cuerpo: %v", body["teamId"])
	}
	if body["rewardedAdType"] != "dailyreward" || body["rewardedAd"] != 1 {
		t.Errorf("el anuncio va como bandera: %v", body)
	}
}

// Raising a clause is the one operation whose amount is not what changes: you pay an amount and
// the clause goes up by twice it. Both numbers have to be in the confirmation, and the balance
// has to come down, which it did not.
func TestRaiseClauseSaysWhatTheClauseBecomes(t *testing.T) {
	g := guard(50_000_000)
	summary, err := g.Prepare("raise_clause",
		Args{LeagueID: "L", TeamID: "T", PlayerTeamID: "slot", Amount: 4_656_624},
		Player{Name: "Ferran Jutglà", Value: 9_313_248, Clause: 15_192_896}, true)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if summary.Clause != 15_192_896 {
		t.Errorf("clause = %d, want the current one", summary.Clause)
	}
	// 15.192.896 + 2 × 4.656.624
	if want := int64(24_506_144); summary.NewClause != want {
		t.Errorf("new clause = %d, want %d", summary.NewClause, want)
	}
	if summary.CashAfter == nil || *summary.CashAfter != 45_343_376 {
		t.Errorf("cash after = %v, want the balance minus what you pay", summary.CashAfter)
	}
}

// The factor lives in one place, and the call carries it.
func TestRaiseClauseCallCarriesTheFactor(t *testing.T) {
	call, err := Build("raise_clause", Args{LeagueID: "L", PlayerTeamID: "slot", Amount: 1_000})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if call.Body["factor"] != ClauseFactor {
		t.Errorf("factor = %v, want %d", call.Body["factor"], ClauseFactor)
	}
	if call.Body["valueToIncrease"] != int64(1_000) {
		t.Errorf("valueToIncrease = %v, want what you pay", call.Body["valueToIncrease"])
	}
}

// The shield is addressed by the slot, like a clause: sending the player's own id comes back
// 404 and looks like the route is wrong.
func TestShieldGoesByTheSlot(t *testing.T) {
	call, err := Build("shield_player", Args{LeagueID: "L", PlayerTeamID: "slot"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if call.Method != http.MethodPut {
		t.Errorf("method = %s, want PUT: the other verbs answer 405", call.Method)
	}
	if call.Body["playerId"] != "slot" {
		t.Errorf("playerId = %v, want the slot id", call.Body["playerId"])
	}
	if call.Body["rewardedAd"] != 1 || call.Body["rewardedAdType"] != "Blindaje" {
		t.Errorf("el anuncio va como bandera: %v", call.Body)
	}
}

// A live shield cannot be bought over: the advert would be spent for nothing.
func TestShieldRefusedWhileOneHolds(t *testing.T) {
	g := guard(50_000_000)
	_, err := g.Prepare("shield_player", Args{LeagueID: "L", TeamID: "T", PlayerTeamID: "slot"},
		Player{Name: "M. Dituro", Shielded: true,
			ShieldedUntil: "2026-09-05T17:46:36+02:00"}, true)
	if err == nil {
		t.Fatal("un blindaje vivo tiene que rechazarse")
	}
	if !strings.Contains(err.Error(), "05/09 17:46") {
		t.Errorf("la negativa tiene que decir hasta cuando: %v", err)
	}
}
