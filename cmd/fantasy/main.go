// Command fantasy is the Go engine. During the port it exists to be compared against
// the Python implementation: every subcommand here has a counterpart in fantasy.py, and
// the point is that they agree.
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
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
	universe, err := model.Build(api.New(), leagueID, teamID, bridge)
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

	parts := ProbeParts(events, listings)
	fmt.Printf("liga %s · %d eventos · %d anuncios\n", leagueID, len(events), len(listings))
	fmt.Printf("  events %s\n  market %s\n", parts["events"], parts["market"])
	fmt.Printf("  peticiones: %+v\n", httpx.Stats())
	return nil
}

// ProbeParts is the digest, split in two because the halves mean different things: a new
// activity event means somebody changed hands and every squad is stale; a market-only
// change is a listing or a rival bid and nothing moved owner.
//
// It must produce the same hex as fantasy/schedule.py for the same state, so the JSON
// shapes it hashes are chosen to match Python's json.dumps of the same lists.
func ProbeParts(events []map[string]any, listings []api.Listing) map[string]string {
	ids := make([]string, 0, 8)
	for index, event := range events {
		if index >= 8 {
			break
		}
		ids = append(ids, fmt.Sprint(event["id"]))
	}

	rows := make([]string, 0, len(listings))
	for _, listing := range listings {
		rows = append(rows, fmt.Sprintf("%s:%s:%s:%s",
			listing.ID, amount(listing.SalePrice.String()),
			nilable(listing.NumberOfBids), nilable(listing.NumberOfOffers)))
	}
	sort.Strings(rows)

	return map[string]string{
		"events": sha1Hex(pythonJSON(ids)),
		"market": sha1Hex(pythonJSON(rows)),
	}
}

// amount renders a number the way Python's str(int(float(value))) does, so the digests
// agree whichever side parsed the response.
func amount(value string) string {
	if value == "" {
		return ""
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%f", &parsed); err != nil {
		return ""
	}
	return fmt.Sprintf("%d", int64(parsed))
}

func nilable(value *int) string {
	if value == nil {
		return "None"
	}
	return fmt.Sprint(*value)
}

// pythonJSON mimics json.dumps of a list of strings: separators are ", " and ": ".
func pythonJSON(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		blob, _ := json.Marshal(value)
		quoted = append(quoted, string(blob))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func sha1Hex(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

