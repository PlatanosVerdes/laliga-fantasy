// Command fantasy is the Go engine. During the port it exists to be compared against
// the Python implementation: every subcommand here has a counterpart in fantasy.py, and
// the point is that they agree.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/advice"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/cli"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/engine"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/matching"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/render"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/rules"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/server"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
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
	// In a container the collector reads stdout, so emit JSON there instead of the terminal
	// format, and at info rather than warn: a log that only speaks when something breaks
	// cannot tell you what it was doing when it broke. Vector picks it up from the docker
	// source with no extra config.
	if asJSON := strings.ToLower(os.Getenv("FANTASY_LOG_JSON")); asJSON == "1" ||
		asJSON == "true" || asJSON == "yes" {
		if level > slog.LevelInfo {
			level = slog.LevelInfo
		}
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout,
			&slog.HandlerOptions{Level: level})))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr,
			&slog.HandlerOptions{Level: level})))
	}

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
	case "serve":
		err = cmdServe(rest[1:])
	case "model":
		err = cmdModel(rest[1:])
	case "wake":
		err = cmdWake(rest[1:])
	case "section":
		err = cmdSection(rest[1:])
	case "plan":
		err = cmdPlan(rest[1:])
	case "advise-json":
		err = cmdAdviseJSON(rest[1:])
	case "match":
		err = cmdMatch(rest[1:])
	case "scrape":
		err = cmdScrape(rest[1:])
	case "squad":
		err = cmdSquad(rest[1:])
	case "market":
		err = cmdMarket(rest[1:])
	case "advise":
		err = cmdAdvise(rest[1:])
	case "standings":
		err = cmdStandings(rest[1:])
	case "activity":
		err = cmdActivity(rest[1:])
	case "player":
		err = cmdPlayer(rest[1:])
	case "fav":
		err = cmdFav(rest[1:])
	case "always":
		err = cmdAlways(rest[1:])
	case "raid":
		err = cmdRaid(rest[1:])
	case "rules":
		err = cmdRules(rest[1:])
	case "leagues":
		err = cmdLeagues(rest[1:])
	case "report":
		err = cmdReport(rest[1:])
	case "page":
		err = cmdPage(rest[1:])
	case "shell":
		err = cmdShell(rest[1:])
	case "cells":
		err = cmdCells()
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

  squad         tu plantilla
  market        ranking del mercado
  advise        que hacer: pujar, vender, clausulas, ofertas
  standings     poder de compra de la liga
  activity      movimientos de la liga
  player <n>    la ficha de un jugador
  fav           favoritos
  always        instrucciones permanentes (siempre en mercado)
  raid          clausulazos programados
  leagues       tus ligas
  rules         las normas de tu liga (plazo de venta y acuerdos)
  report        la pagina, a un fichero
  serve         el motor: pagina, API, SSE y refresco
  auth          browser, code y status: la sesion
  cache         tamano de la cache
  probe         las dos peticiones del detector de cambios, y su huella
  serve         el motor: API JSON, SSE y refresco por vencimientos
  model --json  volcar el modelo, para compararlo con el de Python
  wake <p.json> <now> [tick] [last_full] [watched]
                que haria el planificador con ese payload, para comparar
  calls         la peticion que construiria cada operacion, sin enviarla
  checks        que aceptaria y que rechazaria la guardia, caso por caso
  cells         como se formatea cada celda, para compararlo con Python
  plan <players.json> [saldo]   que harian las instrucciones permanentes
  advise-json <universe.json> <saldo>   los cubos de consejo en JSON, para comparar
  match <players.json> <ffmarket.json> <teams.json>   emparejar las dos fuentes
  scrape <que> <fichero.html>   parsear una pagina de futbolfantasy y volcarla en JSON
  report [--output f] [--generado t]   la pagina, desde el modelo propio
  page <dump.json> <generado> [liga]   la pagina entera, desde un volcado
  shell <caso>  cabecera, widget, pie o pestanas, para compararlo
  section <n> <rows.json>   una seccion renderizada, para compararla
  paths         donde vive cada cosa
`))
}

// cmdServe runs the engine and the HTTP surface.
func cmdServe(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	host := flags.String("host", "0.0.0.0", "interfaz")
	port := flags.Int("port", 8000, "puerto")
	// Accepts "90s", "2m" and a bare number of seconds, because the deployed command line
	// has always been written the second way.
	interval := flags.String("interval", "2m", "cadencia base del sondeo")
	readOnly := flags.Bool("read-only", false, "no ejecutar ninguna escritura")
	noAuto := flags.Bool("no-auto", false,
		"mostrar las instrucciones permanentes sin ejecutarlas")
	deep := flags.Bool("deep", false,
		"leer la ficha de futbolfantasy de los candidatos sin historico")
	if err := flags.Parse(args); err != nil {
		return err
	}
	tick, err := parseInterval(*interval)
	if err != nil {
		return err
	}
	allowWrites := !*readOnly

	client := api.New()
	var (
		mu       sync.Mutex
		leagueID string
		teamID   string
	)
	// Resolved on first use rather than at startup: a fresh deploy has no settings.json and
	// no session either, and the whole point of the setup page is to be reachable anyway.
	ensureLeague := func() (string, string, error) {
		mu.Lock()
		defer mu.Unlock()
		if leagueID != "" && teamID != "" {
			return leagueID, teamID, nil
		}
		league, team, err := resolveLeague("")
		if err != nil {
			return "", "", err
		}
		leagueID, teamID = league, team
		return league, team, nil
	}

	world := state.New(func() (*model.Universe, error) {
		league, team, err := ensureLeague()
		if err != nil {
			return nil, err
		}
		universe, err := model.BuildLive(client, league, team, loadState(), 2*time.Hour)
		if err != nil {
			return nil, err
		}
		if *deep {
			shortlist := make([]*model.Player, 0, len(universe.Players))
			for index := range universe.Players {
				shortlist = append(shortlist, &universe.Players[index])
			}
			sort.SliceStable(shortlist, func(one, two int) bool {
				return shortlist[one].Value > shortlist[two].Value
			})
			if model.DeepEnrich(shortlist, 20, 2*time.Hour) > 0 {
				model.ApplyScores(universe.Players)
			}
		}
		return universe, nil
	})

	guard := writes.NewGuard(client)
	guard.Cash = func(team string) (int64, error) {
		money, err := client.Money(team, time.Minute)
		if err != nil {
			return 0, err
		}
		return int64(money.TeamMoney), nil
	}

	// Automatic execution of the standing instructions. Off in read-only and with --no-auto; on
	// otherwise, because an instruction exists precisely to fire while nobody is watching.
	automate := func(cause string) {
		if !allowWrites || *noAuto {
			return
		}
		universe := world.Universe()
		if universe == nil {
			return
		}
		armed, err := policies.Load()
		if err != nil || len(armed) == 0 {
			return
		}
		league, team, err := ensureLeague()
		if err != nil {
			return
		}
		cash := 0.0
		if money, err := client.Money(team, time.Minute); err == nil {
			cash = money.TeamMoney
		}
		rows := playerRows(universe)
		done := policies.Enforce(policies.Plan(rows, armed), policies.RaidPlan(rows, armed, cash),
			func(operation string, action policies.Row) error {
				args := writes.Args{LeagueID: league, TeamID: team,
					Amount:   int64(number(action["amount"])),
					MarketID: text(action["market_id"]), OfferID: text(action["offer_id"]),
					PlayerID: text(action["player_id"]),
					// A sale and a clause are addressed by squad slot, not by player.
					PlayerTeamID: text(action["player_team_id"]),
				}
				who := writes.Player{Name: text(action["name"]), Available: true,
					Value: number(action["value"]), Clause: int64(number(action["clause"]))}
				_, err := guard.Automatic(operation, args, who, allowWrites)
				return err
			})
		if len(done) == 0 {
			return
		}
		for _, action := range done {
			slog.Info("automatic action", "detail", policies.Describe(action), "cause", cause)
		}
		// The world moved because we moved it: rebuild so the page tells the truth.
		if err := world.RefreshWith("automatico", true); err != nil {
			slog.Error("rebuild after automatic actions failed", "reason", err.Error())
		}
	}

	var probeParts map[string]string
	engineRef := engine.New(engine.Deps{
		Payload:  world.SchedulePayload,
		LastFull: world.LastFull,
		Watchers: world.Watchers,
		Tick:     tick,
		Rebuild: func(cause string) error {
			if err := world.Refresh(cause); err != nil {
				return err
			}
			automate(cause)
			return nil
		},
		Invalidate: func(tags ...string) {
			httpx.Invalidate(tags...)
		},
		Probe: func() (bool, []string, error) {
			league, _, err := ensureLeague()
			if err != nil {
				return false, nil, err
			}
			events, err := client.ActivityRaw(league, 0, 0, true)
			if err != nil {
				return false, nil, err
			}
			listings, err := client.MarketRaw(league, 0, true)
			if err != nil {
				return false, nil, err
			}
			parts := schedule.ProbeParts(events, listings)
			var moved []string
			for half, digest := range parts {
				if probeParts[half] != digest {
					moved = append(moved, half)
				}
			}
			probeParts = parts
			if contains(moved, "events") {
				// Somebody changed hands: every squad is stale however recently it was
				// read, and so are the standings and the cash.
				httpx.Invalidate("squad", "money", "standing")
			}
			return len(moved) > 0, moved, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// boot is everything that needs a session: the first build, the probe seed and the loop.
	// Called at startup when there is one, and from the setup page when one arrives.
	var once sync.Once
	boot := func() {
		once.Do(func() {
			if err := world.Refresh("arranque"); err != nil {
				slog.Error("first build failed", "reason", err.Error())
			}
			// Seed the probe from what the first rebuild already read, so the loop does not
			// begin by asking the API to confirm data it is holding.
			if league, _, err := ensureLeague(); err == nil {
				if events, err := client.ActivityRaw(league, 0, time.Minute, false); err == nil {
					if listings, err := client.MarketRaw(league, time.Minute, false); err == nil {
						probeParts = schedule.ProbeParts(events, listings)
					}
				}
			}
			go engineRef.Run(ctx)
			world.OnFirstWatcher(engineRef.Nudge)
		})
	}

	if server.HasSession() {
		boot()
	} else {
		fmt.Println("Sin sesion: abre la pagina y pega el login.")
	}
	if *noAuto {
		fmt.Println("--no-auto: las instrucciones se muestran pero no se ejecutan.")
	}
	if !allowWrites {
		fmt.Println("--read-only: no se ejecutara ninguna escritura.")
	}

	// Three modes, and the page says which one it is running in: what it may do is not something
	// to guess from whether a button works.
	mode := "auto"
	if !allowWrites {
		mode = "solo lectura"
	} else if *noAuto {
		mode = "manual"
	}
	fmt.Printf("Modo: %s\n", mode)

	league, team, _ := currentLeague(&mu, &leagueID, &teamID)
	return server.New(world, server.Options{
		Host: *host, Port: *port, AllowWrites: allowWrites, Mode: mode,
		Nudge: engineRef.Nudge, Refresh: world.RefreshWith,
		Client: client, Guard: guard, LeagueID: league, MyTeamID: team,
		HoldExceptions: rules.For(league).HoldExceptions,
		Settle: func(cause string) {
			// Force it: a write whose effect the fingerprint cannot see still has to make
			// the page react, or the click looks like it did nothing.
			if err := world.RefreshWith(cause, true); err != nil {
				slog.Error("settle failed", "cause", cause, "reason", err.Error())
			}
		},
		Adopted: boot,
		// The page is rendered on demand from whatever the last rebuild left, so a slow
		// refresh never blocks a request and a failed one still serves the last good world.
		Page: func() string {
			universe := world.Universe()
			if universe == nil {
				return ""
			}
			_, team, err := ensureLeague()
			if err != nil {
				return ""
			}
			page, err := renderPage(universe, client, team, "", mode)
			if err != nil {
				slog.Error("render failed", "reason", err.Error())
				return "<title>Error</title><p>No he podido construir la pagina.</p>"
			}
			return page
		},
	}).ListenAndServe()
}

// currentLeague reads the ids the server was started with, if they are already known. The
// server re-reads nothing: on a fresh deploy they arrive with the session, and the process is
// restarted by the container as soon as anything else changes.
func currentLeague(mu *sync.Mutex, leagueID, teamID *string) (string, string, error) {
	mu.Lock()
	defer mu.Unlock()
	if *leagueID == "" {
		league, team, err := resolveLeague("")
		if err != nil {
			return "", "", err
		}
		*leagueID, *teamID = league, team
	}
	return *leagueID, *teamID, nil
}

// parseInterval accepts a Go duration or a bare number of seconds.
func parseInterval(value string) (time.Duration, error) {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	return time.ParseDuration(value)
}

// cmdModel dumps the universe so tools/diff_model.py can compare it against Python's.
// Deliberately only the ported half: claiming a field that is not built yet would make
// the comparison green for the wrong reason.
func cmdModel(args []string) error {
	leagueID, teamID, err := savedLeague()
	if err != nil {
		return err
	}
	universe, err := model.BuildLive(api.New(), leagueID, teamID, loadState(), 2*time.Hour)
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

// loadState reads the two files that hold what we chose rather than what the feed says. A
// missing file is not an error: no stars and no instructions is a perfectly good state.
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
	armed, err := policies.Load()
	if err == nil {
		for id, policy := range armed {
			if policy.Raid {
				state.Raids[id] = true
			}
		}
	}
	return state
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// resolveLeague returns (league_id, team_id), remembering the choice in settings.json. A
// fresh install has nothing saved, so the ids are discovered from the account: the first
// league it belongs to, and our team inside it.
func resolveLeague(wanted string) (string, string, error) {
	settings := loadSettings()
	leagueID := fallbackText(wanted, text(settings["league_id"]))
	teamID := text(settings["team_id"])
	if leagueID != "" && teamID != "" {
		return leagueID, teamID, nil
	}

	client := api.New()
	entries, err := client.Leagues(time.Hour)
	if err != nil {
		return "", "", err
	}
	if len(entries) == 0 {
		return "", "", fmt.Errorf("la cuenta no esta en ninguna liga")
	}
	chosen := entries[0]
	if leagueID != "" {
		for _, entry := range entries {
			if text(entry["id"]) == leagueID {
				chosen = entry
				break
			}
		}
	}
	leagueID = text(chosen["id"])
	teamID = text(mapFrom(chosen["team"])["id"])
	if teamID == "" {
		// Some payloads leave the team out, so it has to be found by matching our own user
		// id against the standings.
		user, err := client.Me(time.Hour)
		if err != nil {
			return "", "", err
		}
		mine := fallbackText(text(user["id"]), text(user["userId"]))
		rows, err := client.Standings(leagueID, time.Hour)
		if err != nil {
			return "", "", err
		}
		for _, row := range rows {
			if text(row["userId"]) == mine {
				teamID = text(row["teamId"])
				break
			}
		}
	}
	if teamID == "" {
		return "", "", fmt.Errorf("no encuentro tu equipo en la liga %s", leagueID)
	}
	if err := saveSettings(map[string]any{"league_id": leagueID, "team_id": teamID,
		"league_name": chosen["name"]}); err != nil {
		return "", "", err
	}
	slog.Info("league resolved", "league", leagueID, "team", teamID)
	return leagueID, teamID, nil
}

func loadSettings() map[string]any {
	settings := map[string]any{}
	if raw, err := os.ReadFile(config.SettingsFile); err == nil {
		if json.Unmarshal(raw, &settings) != nil {
			return map[string]any{}
		}
	}
	return settings
}

func saveSettings(values map[string]any) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	merged := loadSettings()
	for key, value := range values {
		merged[key] = value
	}
	blob, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(config.SettingsFile, blob, 0o600)
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

// cellCases are the inputs both implementations are checked against. The edges are here on
// purpose: 999_500 must read as millions rather than "1.000K", a negative must keep its
// sign in front of the separators, and an absent value must be the em dash rather than a
// zero.
var cellCases = []struct {
	Kind  string
	Value any
}{
	{"money", nil}, {"money", 0}, {"money", 1}, {"money", 999.0}, {"money", 1000.0},
	{"money", 999_499.0}, {"money", 999_500.0}, {"money", 1_000_000.0},
	{"money", 9_867_495.0}, {"money", 130_960_400.0}, {"money", -2_500_000.0},
	{"money", -999.0},
	{"num", nil}, {"num", 0}, {"num", 2.4055}, {"num", -1.5}, {"num", 121.0},
	{"int", nil}, {"int", 0}, {"int", 211.0}, {"int", -3.0},
	{"pct", nil}, {"pct", 0}, {"pct", 5.83}, {"pct", -11.4}, {"pct", 12.0}, {"pct", -30.0},
	{"mag", nil}, {"mag", 0}, {"mag", 0.244}, {"mag", 0.45}, {"mag", 1.7},
	{"text", nil}, {"text", "Barcelona (casa)"}, {"text", "M. Dituro"},
	{"text", "O'Neill & co"},
	{"spark", []any{}}, {"spark", []any{1.0, 2.0, 3.0}},
	{"spark", []any{9_000_000.0, 9_100_000.0, 8_900_000.0, 9_400_000.0, 9_867_495.0}},
	{"spark", []any{5.0, 5.0, 5.0, 5.0, 5.0, 5.0}},
	{"starts", nil}, {"starts", 0}, {"starts", 29.0}, {"starts", 30.0}, {"starts", 50.0},
	{"starts", 75.0}, {"starts", 100.0},
	{"star", map[string]any{"id": "1300", "name": "Camavinga", "starred": true}},
	{"star", map[string]any{"id": "184", "name": "David Soria"}},
	{"player", map[string]any{"id": "1300", "name": "Camavinga", "team": "Real Madrid",
		"team_short": "RMA", "team_id": "1", "position": "MED", "position_id": 3.0,
		"available": true}},
	{"player", map[string]any{"id": "7", "name": "Lesionado", "team": "Elche CF",
		"team_short": "ELC", "team_id": "7", "position": "POR", "position_id": 1.0,
		"available": false, "status": "injured"}},
	{"player", map[string]any{"id": "8", "name": "Dudoso", "team": "Getafe",
		"team_short": "GET", "team_id": "17", "position": "DEL", "position_id": 4.0,
		"available": true, "status": "doubtful", "prior_based": true, "is_mine": true}},
	// Big and fractional, in a column that renders it as text: the shape that turned into
	// scientific notation and sorted wrongly while looking fine.
	{"text", 17761424.4}, {"text", 130960400.0}, {"text", 0.00001},
	{"num", 17761424.4}, {"money", 17761424.4},
	{"list", []any{"score bajo", "valor cayendo"}}, {"list", []any{}},
	{"pct_plain", 5.83}, {"pct_plain", nil},
	{"ratio", nil}, {"ratio", 0.9}, {"ratio", 1.0}, {"ratio", 1.06}, {"ratio", 1.31},
	{"ratio_sell", 1.16}, {"ratio_sell", 1.03}, {"ratio_sell", 0.99}, {"ratio_sell", 0.95},
	{"ratio_sell", 0.5},
	{"ideal", nil}, {"ideal", 0}, {"ideal", 11_000_000.0},
}

// cmdSection renders one section from rows handed to it, so the HTML can be compared
// against Python's for the very same input. Rendering from a live model instead would
// compare two data reads as much as two renderers.
func cmdSection(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: section <plantilla|mercado> <rows.json>")
	}
	raw, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return err
	}

	// Two sections are not tables at all: the calendar is a shape and the feed is a list
	// of sentences, so they render from the same rows by their own route.
	switch args[0] {
	case "calendario":
		spending := 0.0
		if len(args) > 2 {
			parsed, err := strconv.ParseFloat(args[2], 64)
			if err != nil {
				return err
			}
			spending = parsed
		}
		fmt.Print(render.Calendar(rows, spending))
		return nil
	case "movimientos":
		fmt.Print(render.Feed(rows))
		return nil
	}

	html, err := render.SectionTable(args[0], rows)
	if err != nil {
		return err
	}
	fmt.Print(html)
	return nil
}

// cmdPlan is what the standing instructions would do, and what the scheduled raids would do,
// from a recorded squad. Nothing is executed: this prints the plan.
func cmdPlan(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: plan <players.json> [saldo]")
	}
	body, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var players []map[string]any
	if err := json.Unmarshal(body, &players); err != nil {
		return err
	}
	cash := 0.0
	if len(args) > 1 {
		if cash, err = strconv.ParseFloat(args[1], 64); err != nil {
			return err
		}
	}
	armed, err := policies.Load()
	if err != nil {
		return err
	}
	blob, err := json.Marshal(map[string]any{
		"plan":  policies.Plan(players, armed),
		"raids": policies.RaidPlan(players, armed, cash),
	})
	if err != nil {
		return err
	}
	fmt.Println(string(blob))
	return nil
}

// cmdAdvise runs the advice layer over a recorded universe, so the buckets can be compared
// bucket by bucket. Budget and debt are arguments: they come from the API in a live run and
// from the comparison in this one.
func cmdAdviseJSON(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: advise-json <universe.json> <saldo> [deuda] [limite]")
	}
	body, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var universe map[string]any
	if err := json.Unmarshal(body, &universe); err != nil {
		return err
	}
	budget, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return err
	}
	debt := 0.0
	if len(args) > 2 {
		if debt, err = strconv.ParseFloat(args[2], 64); err != nil {
			return err
		}
	}
	limit := 15
	if len(args) > 3 {
		if limit, err = strconv.Atoi(args[3]); err != nil {
			return err
		}
	}

	blob, err := json.Marshal(advice.Recommend(universe, budget, debt, limit))
	if err != nil {
		return err
	}
	fmt.Println(string(blob))
	return nil
}

// cmdMatch resolves players across the two sources from files, so the result can be
// compared: the matcher is a pure function of three lists.
func cmdMatch(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("uso: match <players.json> <ffmarket.json> <teams.json>")
	}
	load := func(path string) ([]map[string]any, error) {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var out []map[string]any
		return out, json.Unmarshal(body, &out)
	}
	players, err := load(args[0])
	if err != nil {
		return err
	}
	market, err := load(args[1])
	if err != nil {
		return err
	}
	teams, err := load(args[2])
	if err != nil {
		return err
	}

	matched, unmatched := matching.MatchMarket(players, market,
		matching.BuildTeamIndex(teams))

	// Only the ids: comparing the whole rows would compare the scraper again, which has its
	// own harness.
	pairs := make(map[string]string, len(matched))
	for id, row := range matched {
		pairs[id] = fmt.Sprint(row["ff_id"])
	}
	loose := make([]string, 0, len(unmatched))
	for _, row := range unmatched {
		loose = append(loose, fmt.Sprint(row["ff_id"]))
	}
	sort.Strings(loose)

	blob, err := json.Marshal(map[string]any{"matched": pairs, "unmatched": loose})
	if err != nil {
		return err
	}
	fmt.Println(string(blob))
	return nil
}

// cmdScrape parses one saved futbolfantasy page and prints the result as JSON. The page
// comes from a file rather than the network so both implementations can be handed the exact
// same bytes: these parsers read somebody else's HTML, which is the most fragile code here
// and the most worth comparing.
func cmdScrape(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: scrape <mercado|detalle|jugador|ausencias> <fichero.html> [tipo]")
	}
	body, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	page := string(body)

	var out any
	switch args[0] {
	case "mercado":
		out = futbolfantasy.ParseMarket(page)
	case "equipos":
		out = futbolfantasy.ParseTeamMap(page)
	case "detalle":
		out = futbolfantasy.ParseDetail(page)
	case "jugador":
		out = futbolfantasy.ParsePlayerPage(page)
	case "ausencias":
		kind := "lesionado"
		if len(args) > 2 {
			kind = args[2]
		}
		out = futbolfantasy.ParseAbsences(page, kind)
	default:
		return fmt.Errorf("pagina desconocida: %s", args[0])
	}

	blob, err := json.Marshal(out)
	if err != nil {
		return err
	}
	fmt.Println(string(blob))
	return nil
}

// cmdReport is the page from Go's own model: its API client, its scrapers, its matcher, its
// advice layer, its renderer. Nothing calls Python.
func cmdReport(args []string) error {
	flags := flag.NewFlagSet("report", flag.ContinueOnError)
	output := flags.String("output", "", "donde escribirla (por defecto, salida estandar)")
	generated := flags.String("generado", "", "marca de tiempo fija, para comparar")
	budget := flags.Float64("budget", 0, "saldo, si no se quiere leer de la API")
	if err := flags.Parse(args); err != nil {
		return err
	}

	leagueID, teamID, err := savedLeague()
	if err != nil {
		return err
	}
	client := api.New()
	universe, err := model.BuildLive(client, leagueID, teamID, loadState(), 2*time.Hour)
	if err != nil {
		return err
	}
	if *budget != 0 {
		cashOverride = budget
	}

	// A file written by `report` acts on nothing, whatever the server would do.
	page, err := renderPage(universe, client, teamID, *generated, "informe")
	if err != nil {
		return err
	}
	if *output == "" {
		fmt.Print(page)
		return nil
	}
	return os.WriteFile(*output, []byte(page), 0o644)
}

// cashOverride lets `report --budget` skip reading /money, which the frozen comparison needs.
var cashOverride *float64

// renderPage is the whole document from a built universe: the advice layer, the per-player
// enrichment, the standing instructions and the renderer. Shared by `report` and the server,
// so the page cannot differ between them.
// playerRows is the universe's players as the generic rows the policy engine reads.
func playerRows(universe *model.Universe) []map[string]any {
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

// mineByWeek is how many of my players each team had, per matchday already played. The API has no
// history, so it comes from undoing the transfer log back to each kick-off: counting a fortnight
// old fixture with today's squad says "3 de los tuyos juegan" about players who were not there.
func mineByWeek(universe *model.Universe) map[int]map[string]int {
	if universe == nil || universe.MyTeamID == nil {
		return nil
	}
	userOfTeam := map[string]string{}
	for teamID, team := range universe.LeagueTeams {
		if team != nil && team.UserID != "" {
			userOfTeam[teamID] = team.UserID
		}
	}
	me := userOfTeam[*universe.MyTeamID]
	if me == "" {
		return nil
	}

	today := map[string]string{}
	teamOf := map[string]string{}
	for _, player := range universe.Players {
		teamOf[player.ID] = player.TeamID
		if player.OwnerTeamID == nil {
			continue
		}
		if user := userOfTeam[*player.OwnerTeamID]; user != "" {
			today[player.ID] = user
		}
	}

	// The first kick-off of each matchday is the instant its squads counted.
	kickoffs := map[int]time.Time{}
	for _, fixture := range universe.Schedule {
		when, err := time.Parse(time.RFC3339, fixture.Kickoff)
		if err != nil {
			continue
		}
		if seen, ok := kickoffs[fixture.Week]; !ok || when.Before(seen) {
			kickoffs[fixture.Week] = when
		}
	}

	now := time.Now()
	out := map[int]map[string]int{}
	for week, kickoff := range kickoffs {
		if !kickoff.Before(now) {
			continue // still to come: today's squad is the only truthful answer
		}
		counts := map[string]int{}
		for playerID, user := range model.OwnershipAt(universe.Activity, today, kickoff) {
			if user == me {
				counts[teamOf[playerID]]++
			}
		}
		out[week] = counts
	}
	return out
}

func renderPage(universe *model.Universe, client *api.Client, teamID, generated,
	mode string) (string, error) {
	cash := 0.0
	if cashOverride != nil {
		cash = *cashOverride
	} else if money, err := client.Money(teamID, time.Minute); err == nil {
		cash = money.TeamMoney
	}

	// The advice layer and the page read the same generic rows, so the universe goes through
	// JSON once. One conversion, in one place, rather than two shapes of the world.
	blob, err := json.Marshal(universe)
	if err != nil {
		return "", err
	}
	var generic map[string]any
	if err := json.Unmarshal(blob, &generic); err != nil {
		return "", err
	}
	buckets := advice.Recommend(generic, cash, 0, 15)
	// The per-player pages, once each: this is what fills the profitable ceiling and the
	// value history the page draws.
	advice.EnrichBuckets(buckets, 15, futbolfantasy.DetailTTL)

	armed, err := policies.Load()
	if err != nil {
		return "", err
	}
	players := rowsFrom(generic["players"])
	policyRows := map[string]map[string]any{}
	for id, policy := range armed {
		row := map[string]any{}
		if policy.MinPrice != nil {
			row["min_price"] = *policy.MinPrice
		}
		if policy.AcceptAbove != nil {
			row["accept_above"] = *policy.AcceptAbove
		}
		policyRows[id] = row
	}

	// The rules are per league, so they are read with the league the page belongs to.
	leagueID, _, _ := savedLeague()
	house := rules.For(leagueID)

	stamp := generated
	if stamp == "" {
		stamp = time.Now().Format("02/01/2006 15:04")
	}
	league := ""
	if settings, err := os.ReadFile(config.SettingsFile); err == nil {
		var parsed struct {
			LeagueName string `json:"league_name"`
		}
		if json.Unmarshal(settings, &parsed) == nil {
			league = parsed.LeagueName
		}
	}

	assets := os.Getenv("FANTASY_ASSETS")
	if assets == "" {
		assets = "assets"
	}
	read := func(name string) string {
		body, err := os.ReadFile(filepath.Join(assets, name))
		if err != nil {
			return ""
		}
		return string(body)
	}
	render.Pitch, render.Filters = read("pitch.html"), read("filters.html")
	var crests map[string]string
	if body, err := os.ReadFile(filepath.Join(config.CacheDir, "crests.json")); err == nil {
		if json.Unmarshal(body, &crests) == nil {
			render.Crests = crests
		}
	}

	document := render.Document{
		Universe: generic, Advice: buckets, Generated: stamp, LeagueName: league,
		MineByWeek: mineByWeek(universe), Mode: mode,
		// The plan reads the same buckets the tables do, so what it proposes and what they
		// list can never disagree.
		Swaps:          advice.Swaps(generic, buckets, cash),
		HoldDays:       house.HoldDays,
		HoldExceptions: house.HoldExceptions,
		RuleNotes:      house.Notes,
		CSS: read("report.css"), JS: read("report.js"),
		Modal: read("modal.html"), Drawer: read("drawer.html"),
		Plan:     policies.Plan(players, armed),
		Raids:    policies.RaidPlan(players, armed, cash),
		Policies: policyRows,
	}
	return document.HTML(), nil
}

// cmdPage renders the whole document from a dump, so it can be compared with Python's
// byte for byte. The timestamp and the league name are arguments because a page that reads
// the clock cannot be compared with anything.
func cmdPage(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("uso: page <dump.json> <generado> [liga]")
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var blob struct {
		Universe map[string]any `json:"universe"`
		Advice   map[string]any `json:"advice"`
	}
	if err := json.Unmarshal(raw, &blob); err != nil {
		return err
	}
	league := ""
	if len(args) > 2 {
		league = args[2]
	}

	assets := os.Getenv("FANTASY_ASSETS")
	if assets == "" {
		assets = "assets"
	}
	read := func(name string) string {
		body, err := os.ReadFile(filepath.Join(assets, name))
		if err != nil {
			return ""
		}
		return string(body)
	}
	render.Pitch = read("pitch.html")
	render.Filters = read("filters.html")

	// The crests come from the same cache file Python fills, so the page carries the same
	// badges rather than a second download.
	var crests map[string]string
	if body, err := os.ReadFile(filepath.Join(config.CacheDir, "crests.json")); err == nil {
		_ = json.Unmarshal(body, &crests)
		render.Crests = crests
	}

	// The two standing-instruction tables come precomputed in the dump when it has them:
	// their rows are the policy engine's output, not the model's.
	document := render.Document{
		Universe: blob.Universe, Advice: blob.Advice,
		Swaps: advice.Swaps(blob.Universe, blob.Advice, number(blob.Advice["budget"])),
		Generated: args[1], LeagueName: league,
		CSS: read("report.css"), JS: read("report.js"),
		Modal: read("modal.html"), Drawer: read("drawer.html"),
		Plan: rowsFrom(blob.Advice["_plan"]), Raids: rowsFrom(blob.Advice["_raids"]),
		Policies: policiesFrom(blob.Advice["_policies"]),
	}
	fmt.Print(document.HTML())
	return nil
}

// rowsFrom takes rows in either shape: []any when they were parsed from JSON, and
// []map[string]any when they came straight from the advice layer. Handling only the first is
// how every bucket in the CLI came back empty while the model was perfectly fine.
func rowsFrom(value any) []map[string]any {
	if already, ok := value.([]map[string]any); ok {
		return already
	}
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func policiesFrom(value any) map[string]map[string]any {
	asMap, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]map[string]any, len(asMap))
	for key, item := range asMap {
		if row, ok := item.(map[string]any); ok {
			out[key] = row
		}
	}
	return out
}

// cmdShell renders the pieces of the page that are not sections, from arguments rather
// than from a model, so they can be compared one at a time.
func cmdShell(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: shell <widgets|cabecera|pie|pestanas|rangos>")
	}
	switch args[0] {
	case "pestanas":
		fmt.Print(render.Tabs)

	case "pie":
		for _, weight := range []float64{0, 0.125, 0.5, 1} {
			fmt.Println(render.Footer(weight))
		}

	case "widgets":
		half, top := 0.5, 1.0
		cases := []render.KPI{
			{Label: "Jornada 1", Value: "en juego", Hint: "J2 desde vie 22 ago 19:30",
				Rank: "cierra jue 20 ago 03:00", Status: "neutral"},
			{Label: "Mi puesto", Value: "7º", Hint: "121 puntos", Rank: "7º de 13",
				Meter: &half, Status: "neutral", Tab: "liga"},
			{Label: "Mi saldo", Value: "93.62M", Hint: "le llega a la mayoria del mercado",
				Rank: "3º de 13", Meter: &top, Status: "good", Tab: "liga"},
			{Label: "Sin rango", Value: "—"},
		}
		for _, kpi := range cases {
			fmt.Println(render.Widget(kpi))
		}

	case "rangos":
		others := []float64{130960400, 100600000, 93617740, 55266919, 48869526, 0}
		for _, value := range []float64{130960400, 93617740, 0, 999} {
			label, share, status := render.RankOf(value, others)
			fmt.Printf("%.0f|%s|%.6f|%s\n", value, label, share, status)
		}
		// And the degenerate cases, which is where an off-by-one hides.
		label, share, status := render.RankOf(5, nil)
		fmt.Printf("vacio|%s|%.6f|%s\n", label, share, status)
		label, share, status = render.RankOf(5, []float64{5})
		fmt.Printf("uno|%s|%.6f|%s\n", label, share, status)

	case "cabecera":
		fmt.Println(render.Header("18/08/2026 16:20", "Liga Fantasy Comité 2026-", 1,
			[]string{`<div class="kpi">uno</div>`, `<div class="kpi">dos</div>`}, true, "auto"))
		fmt.Println(render.Header("18/08/2026 16:20", "", 3, nil, false, ""))

	default:
		return fmt.Errorf("caso desconocido: %s", args[0])
	}
	return nil
}

func cmdCells() error {
	for _, row := range cellCases {
		inner, sort := render.Cell(row.Value, row.Kind)
		fmt.Printf("%s|%v|%s|%s\n", row.Kind, row.Value, sort, inner)
	}
	return nil
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
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "browser":
		pending, err := auth.StartLogin()
		if err != nil {
			return err
		}
		fmt.Println("Abre esto, entra, y cuando el navegador falle al ir a")
		fmt.Println("authredirect:// copia la direccion completa de la barra:")
		fmt.Println()
		fmt.Println(pending.URL)
		fmt.Println()
		fmt.Println(cli.Dim("  Luego: fantasy auth code '<la direccion>'"))
		fmt.Println(cli.Dim("  Caduca en 15 minutos."))
		return nil

	case "code":
		if len(args) < 2 {
			return fmt.Errorf("uso: auth code '<url de redireccion>'")
		}
		pending, err := auth.LoadPending()
		if err != nil {
			return fmt.Errorf("no hay login empezado: ejecuta `auth browser` primero")
		}
		code, err := auth.ExtractCode(strings.Join(args[1:], " "), pending.State)
		if err != nil {
			return err
		}
		tokens, err := auth.ExchangeCode(code, pending.Verifier)
		if err != nil {
			return err
		}
		fmt.Println(cli.Green(fmt.Sprintf("Sesion guardada para %s",
			fallbackText(tokens.Email, "tu cuenta"))))
		return nil

	case "status":
	default:
		return fmt.Errorf("no se que es `auth %s`: usa browser, code o status", action)
	}

	tokens, err := auth.Load()
	if err != nil {
		return err
	}
	if tokens == nil {
		fmt.Println("Sin sesion. Ejecuta: fantasy auth browser")
		fmt.Println(cli.Dim("  O abre la pagina: si no hay sesion, la pide ella misma."))
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


