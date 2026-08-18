// Command fantasy is the Go engine. During the port it exists to be compared against
// the Python implementation: every subcommand here has a counterpart in fantasy.py, and
// the point is that they agree.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

func main() {
	level := slog.LevelWarn
	args := os.Args[1:]
	var rest []string
	for _, arg := range args {
		switch arg {
		case "-v", "--verbose":
			level = slog.LevelDebug
		case "-q", "--quiet":
			level = slog.LevelError
		default:
			rest = append(rest, arg)
		}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr,
		&slog.HandlerOptions{Level: level})))

	if len(rest) == 0 {
		usage()
		os.Exit(2)
	}

	var err error
	switch rest[0] {
	case "auth":
		err = cmdAuth(rest[1:])
	case "cache":
		err = cmdCache()
	case "probe":
		err = cmdProbe(rest[1:])
	case "model":
		err = cmdModel(rest[1:])
	case "wake":
		err = cmdWake(rest[1:])
	case "checks":
		err = cmdChecks()
	case "calls":
		err = cmdCalls()
	case "paths":
		err = cmdPaths()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(`
uso: fantasy [-v|-q] <comando>

  auth status   estado de la sesion
  cache         tamano de la cache
  probe         las dos peticiones del detector de cambios, y su huella
  model --json  volcar el modelo, para compararlo con el de Python
  wake <p.json> <now> [tick] [last_full] [watched]
                que haria el planificador con ese payload, para comparar
  calls         la peticion que construiria cada operacion, sin enviarla
  checks        que aceptaria y que rechazaria la guardia, caso por caso
  paths         donde vive cada cosa
`))
}

// cmdModel dumps the universe so tools/diff_model.py can compare it against Python's.
// Deliberately only the ported half: claiming a field that is not built yet would make
// the comparison green for the wrong reason.
func cmdModel(args []string) error {
	leagueID, teamID, err := savedLeague()
	if err != nil {
		return err
	}
	bridge, err := loadBridge()
	if err != nil {
		return err
	}
	universe, err := model.Build(api.New(), leagueID, teamID, bridge, loadState())
	if err != nil {
		return err
	}
	blob, err := json.MarshalIndent(map[string]any{"universe": universe}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(blob))
	if !contains(args, "--json") {
		fmt.Fprintf(os.Stderr, "  %d jugadores · %d anuncios · %d partidos · peticiones %+v\n",
			len(universe.Players), len(universe.Market), len(universe.Fixtures),
			httpx.Stats())
	}
	return nil
}

// loadBridge runs the Python side and reads its JSON. That subprocess is the port's
// boundary: the futbolfantasy scrapers and the cross-source name matching stay in
// Python, because they are regex over HTML that changes without notice and the most
// fragile code in the project. What crosses is data, never parsing.
// loadState reads the two files that hold what we chose rather than what the feed says.
// A missing file is not an error: no stars and no instructions is a perfectly good state.
func loadState() model.State {
	state := model.State{Starred: map[string]bool{}, Raids: map[string]bool{}}

	var favourites map[string]map[string]any
	if raw, err := os.ReadFile(config.FavouritesFile); err == nil {
		if json.Unmarshal(raw, &favourites) == nil {
			for id := range favourites {
				state.Starred[id] = true
			}
		}
	}
	var policies map[string]map[string]any
	if raw, err := os.ReadFile(config.PolicyFile); err == nil {
		if json.Unmarshal(raw, &policies) == nil {
			for id, entry := range policies {
				if raid, ok := entry["raid"].(bool); ok && raid {
					state.Raids[id] = true
				}
			}
		}
	}
	return state
}

func loadBridge() (*model.Bridge, error) {
	command := exec.Command("python3", "fantasy.py", "bridge")
	command.Stderr = os.Stderr
	blob, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("el puente de futbolfantasy ha fallado: %w", err)
	}
	var bridge model.Bridge
	if err := json.Unmarshal(blob, &bridge); err != nil {
		return nil, fmt.Errorf("el puente no ha devuelto JSON valido: %w", err)
	}
	slog.Debug("bridge loaded", "trends", len(bridge.Trends),
		"absences", len(bridge.Absences))
	return &bridge, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func savedLeague() (string, string, error) {
	var settings struct {
		LeagueID string `json:"league_id"`
		TeamID   string `json:"team_id"`
	}
	raw, err := os.ReadFile(config.SettingsFile)
	if err != nil {
		return "", "", fmt.Errorf("no hay liga guardada: %w", err)
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", "", err
	}
	return settings.LeagueID, settings.TeamID, nil
}

// cmdWake prints what the scheduler would decide for a recorded payload at a given
// instant. Both sides take `now` as an argument precisely so the decision is reproducible:
// a scheduler that can only be observed live cannot be compared.
func cmdWake(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: wake <payload.json> <now RFC3339> [tick_s] [last_full] [watched]")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var wrapper struct {
		Universe schedule.Payload `json:"universe"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return err
	}
	payload := wrapper.Universe
	if len(payload.Players) == 0 {
		// Also accept a bare payload, which is what the server's /api/state returns.
		if err := json.Unmarshal(raw, &payload); err != nil {
			return err
		}
	}
	// Policies live in a file, not in the dump.
	state := loadState()
	payload.Policies = map[string]schedule.Policy{}
	for id := range state.Raids {
		payload.Policies[id] = schedule.Policy{Raid: true}
	}

	now, err := time.Parse(time.RFC3339, args[1])
	if err != nil {
		return err
	}
	tick := 120 * time.Second
	if len(args) > 2 {
		seconds, err := strconv.Atoi(args[2])
		if err != nil {
			return err
		}
		tick = time.Duration(seconds) * time.Second
	}
	var lastFull time.Time
	if len(args) > 3 && args[3] != "-" {
		if lastFull, err = time.Parse(time.RFC3339, args[3]); err != nil {
			return err
		}
	}
	watched := len(args) > 4 && args[4] == "true"

	deadlines := schedule.Deadlines(payload, now, time.Time{})
	fmt.Printf("vencimientos: %d\n", len(deadlines))
	for index, deadline := range deadlines {
		if index >= 10 {
			break
		}
		fmt.Printf("  %+7.0fs  %s\n", deadline.At.Sub(now).Seconds(), deadline.Why)
	}
	fmt.Printf("en juego: %d (mios %d)\n",
		len(schedule.LiveMatches(payload, now, false)),
		len(schedule.LiveMatches(payload, now, true)))
	decision := schedule.NextWake(payload, now, tick, lastFull, watched, time.Time{})
	fmt.Printf("decision: +%.0fs %s %s\n", decision.At.Sub(now).Seconds(),
		decision.Kind, decision.Why)
	return nil
}

// cmdCalls prints the request each operation would build, with fixed arguments and
// without sending anything. It exists to be compared against Python's: the ids here are
// unobvious and hard-won, and a port that swaps two of them fails in the most expensive
// way there is.
// validationTable is shared with tools/diff_writes.py by construction: the same rows, in
// the same order, so the two answers can be lined up.
var validationTable = []struct {
	Label string
	Case  writes.ValidationCase
}{
	{"puja normal", writes.ValidationCase{"bid",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: 1_000_000},
		writes.Player{Name: "X", MinBid: 900_000, IdealBid: 2_000_000}, 50_000_000}},
	{"puja por debajo del minimo", writes.ValidationCase{"bid",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: 800_000},
		writes.Player{Name: "X", MinBid: 900_000, IdealBid: 2_000_000}, 50_000_000}},
	{"puja de cero", writes.ValidationCase{"bid",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: 0},
		writes.Player{Name: "X"}, 50_000_000}},
	{"puja mayor que el saldo", writes.ValidationCase{"bid",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: 2_000_000},
		writes.Player{Name: "X", IdealBid: 9_000_000}, 1_000_000}},
	{"puja sobre el techo rentable", writes.ValidationCase{"bid",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: 3_000_000},
		writes.Player{Name: "X", IdealBid: 2_000_000}, 50_000_000}},
	{"puja sin rentabilidad conocida", writes.ValidationCase{"bid",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: 1_000_000},
		writes.Player{Name: "X"}, 50_000_000}},
	{"puja que se come medio saldo", writes.ValidationCase{"bid",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: 600_000},
		writes.Player{Name: "X", IdealBid: 900_000}, 1_000_000}},
	{"puja con rivales", writes.ValidationCase{"bid",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: 1_000_000},
		writes.Player{Name: "X", IdealBid: 2_000_000, Bids: 3}, 50_000_000}},
	{"clausula pagada de menos", writes.ValidationCase{"pay_clause",
		writes.Args{LeagueID: "L", TeamID: "T", PlayerTeamID: "PT", Amount: 9_000_000},
		writes.Player{Name: "X", Clause: 10_000_000}, 50_000_000}},
	{"clausula exacta", writes.ValidationCase{"pay_clause",
		writes.Args{LeagueID: "L", TeamID: "T", PlayerTeamID: "PT", Amount: 10_000_000},
		writes.Player{Name: "X", Clause: 10_000_000}, 50_000_000}},
	{"clausula sin saldo", writes.ValidationCase{"pay_clause",
		writes.Args{LeagueID: "L", TeamID: "T", PlayerTeamID: "PT", Amount: 10_000_000},
		writes.Player{Name: "X", Clause: 10_000_000}, 5_000_000}},
	{"oferta directa negativa", writes.ValidationCase{"direct_offer",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", Amount: -1},
		writes.Player{Name: "X"}, 50_000_000}},
	{"aceptar por debajo del valor", writes.ValidationCase{"accept_offer",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", OfferID: "O", Amount: 8_000_000},
		writes.Player{Name: "X", Value: 10_000_000}, 0}},
	{"aceptar por encima del techo", writes.ValidationCase{"accept_offer",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M", OfferID: "O", Amount: 12_000_000},
		writes.Player{Name: "X", Value: 10_000_000, IdealBid: 11_000_000}, 0}},
	{"retirar del mercado", writes.ValidationCase{"withdraw",
		writes.Args{LeagueID: "L", TeamID: "T", MarketID: "M"},
		writes.Player{Name: "X"}, 50_000_000}},
}

func cmdChecks() error {
	for _, row := range validationTable {
		refused, _, warnings := writes.Validate(row.Case)
		fmt.Printf("%-32s %-8s %d\n", row.Label, boolWord(refused), len(warnings))
	}
	return nil
}

func boolWord(value bool) string {
	if value {
		return "rechaza"
	}
	return "acepta"
}

func cmdCalls() error {
	fixed := writes.Args{
		LeagueID: "L", TeamID: "T", MarketID: "M", BidID: "B", OfferID: "O",
		PlayerID: "P", PlayerTeamID: "PT", Amount: 1000,
		Goalkeeper: "G", Defender: []string{"D"}, Midfield: []string{"M"},
		Striker: []string{"S"}, Formation: []int{3, 4, 3},
	}
	names := make([]string, 0, len(writes.Operations))
	for name := range writes.Operations {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		call, err := writes.Build(name, fixed)
		if err != nil {
			return err
		}
		body := ""
		if call.Body != nil {
			blob, err := json.Marshal(call.Body)
			if err != nil {
				return err
			}
			body = string(blob)
		}
		fmt.Printf("%-15s %-6s %s\n", name, call.Method, call.Path)
		if body != "" {
			fmt.Printf("%16s%s\n", "", body)
		}
	}
	return nil
}

func cmdPaths() error {
	fmt.Printf("  config %s\n  state  %s\n  cache  %s\n",
		config.ConfigDir, config.StateDir, config.CacheDir)
	return nil
}

func cmdAuth(args []string) error {
	if len(args) == 0 || args[0] != "status" {
		return fmt.Errorf("solo `auth status` esta portado todavia")
	}
	tokens, err := auth.Load()
	if err != nil {
		return err
	}
	if tokens == nil {
		fmt.Println("Sin sesion. Ejecuta: python3 fantasy.py auth browser")
		return nil
	}
	left := tokens.SecondsLeft()
	state := fmt.Sprintf("valida (%d min)", left/60)
	if left <= 0 {
		state = "caducada"
	}
	fmt.Printf("Cuenta      : %s\n", tokens.Email)
	fmt.Printf("Proveedor   : %s\n", tokens.IDP)
	fmt.Printf("Token       : %s\n", state)
	fmt.Printf("Refresh     : %s\n", yesNo(tokens.RefreshToken != ""))
	fmt.Printf("client_id   : %s\n", tokens.ClientID)
	return nil
}

func yesNo(value bool) string {
	if value {
		return "si"
	}
	return "no"
}

func cmdCache() error {
	entries, err := filepath.Glob(filepath.Join(config.CacheDir, "*.cache"))
	if err != nil {
		return err
	}
	var total int64
	for _, path := range entries {
		if info, err := os.Stat(path); err == nil {
			total += info.Size()
		}
	}
	// Megabytes as Python prints them: 10^6, not 2^20. The figure is for a human, and
	// the two sides have to agree on it or every comparison starts with a false alarm.
	fmt.Printf("%d ficheros · %.1f MB en %s\n", len(entries),
		float64(total)/1_000_000, config.CacheDir)
	return nil
}

// cmdProbe is step 3 of the port made visible: the two cheap requests that answer
// whether anything moved, and the digest they produce. The digest has to equal the
// Python one for the same league state, byte for byte.
func cmdProbe(args []string) error {
	leagueID := ""
	if len(args) > 0 {
		leagueID = args[0]
	}
	if leagueID == "" {
		var settings struct {
			LeagueID string `json:"league_id"`
		}
		raw, err := os.ReadFile(config.SettingsFile)
		if err != nil {
			return fmt.Errorf("no hay liga guardada y no me has dado una: %w", err)
		}
		if err := json.Unmarshal(raw, &settings); err != nil {
			return err
		}
		leagueID = settings.LeagueID
	}

	client := api.New()
	events, err := client.ActivityRaw(leagueID, 0, 0, true)
	if err != nil {
		return err
	}
	listings, err := client.Market(leagueID, 0, true)
	if err != nil {
		return err
	}

	rawListings := make([]map[string]any, 0, len(listings))
	for _, listing := range listings {
		blob, _ := json.Marshal(listing)
		var row map[string]any
		_ = json.Unmarshal(blob, &row)
		rawListings = append(rawListings, row)
	}
	parts := schedule.ProbeParts(events, rawListings)
	fmt.Printf("liga %s · %d eventos · %d anuncios\n", leagueID, len(events), len(listings))
	fmt.Printf("  events %s\n  market %s\n", parts["events"], parts["market"])
	fmt.Printf("  peticiones: %+v\n", httpx.Stats())
	return nil
}


