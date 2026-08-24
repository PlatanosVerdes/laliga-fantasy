// Package config holds the paths, endpoints and constants, mirroring the Python
// fantasy/config.py so both implementations read the same files and talk to the same
// hosts. Where the two must agree, the Python name is kept.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

const AppName = "laliga-fantasy"

// Season 26/27 host. The old api-fantasy host is frozen on 25/26.
const APIBase = "https://fantasy-api.llt-services.com/api"

// LaLiga Azure AD B2C.
const (
	B2CTokenURL       = "https://login.laliga.es/laligadspprob2c.onmicrosoft.com/oauth2/v2.0/token"
	B2CSignInPolicy   = "B2C_1A_5ULAIP_PARAMETRIZED_SIGNIN"
	B2CWebClientID    = "6457fa17-1224-416a-b21a-ee6ce76e9bc0"
	B2CNativeClientID = "af88bcff-1157-40a0-b579-030728aacf0b"
)

var (
	// ConfigDir holds credentials and preferences, StateDir regenerable output,
	// CacheDir scraped and fetched responses.
	ConfigDir string
	StateDir  string
	CacheDir  string

	// ExplicitDir records whether FANTASY_DATA_DIR was set. Migration is skipped when
	// it is: an explicit override means "use exactly this", and a container mounting
	// its own volume must not vacuum a live ./data from elsewhere into it.
	ExplicitDir bool

	CompetitionID = envOr("FANTASY_COMPETITION_ID", "1")
	// CMP is the prefix every route in use hangs off.
	CMP = "/v1/competition/" + CompetitionID
)

// Files, split by nature exactly as the Python side splits them.
var (
	TokenFile      string
	SettingsFile   string
	FavouritesFile string
	PolicyFile     string
	RulesFile      string
	ReportFile     string
	LogFile        string
)

// APIHeaders are what the official app sends. x-app: 2 is not optional.
func APIHeaders() map[string]string {
	return map[string]string{
		"x-app":      "2",
		"x-lang":     "es",
		"Referer":    "https://fantasy.laliga.com/",
		"User-Agent": userAgent,
	}
}

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

// futbolfantasy.com. No API, so these are pages and the parsers read HTML.
const (
	FFBase         = "https://www.futbolfantasy.com"
	FFMarketURL    = FFBase + "/analytics/laliga-fantasy/mercado"
	FFDetailURL    = FFBase + "/analytics/laliga-fantasy/mercado/detalle/{ff_id}?perfil=1"
	FFPlayerURL    = FFBase + "/jugadores/{slug}"
	FFInjuredURL   = FFBase + "/laliga/lesionados"
	FFSuspendedURL = FFBase + "/laliga/sancionados"
)

func FFHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      userAgent,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "es-ES,es;q=0.9",
	}
}

// Tags name cached responses. Other packages invalidate by tag, and a tag that matches
// nothing fails silently — it drops no files and reports success — so the set is
// declared once and checked against.
var Tags = map[string]bool{
	"activity": true, "calendar": true, "formations": true, "leagues": true,
	"lineup": true, "market": true, "me": true, "money": true, "mv": true,
	"offers": true, "player": true, "players": true, "reward": true, "squad": true,
	"standing": true, "teams": true, "week": true,
	// futbolfantasy's pages, cached the same way.
	"ff_market": true, "ff_detail": true, "ff_player": true, "ff_absences": true,
}

func init() {
	ConfigDir, StateDir, CacheDir = resolveDirs()
	TokenFile = filepath.Join(ConfigDir, "tokens.json")
	SettingsFile = filepath.Join(ConfigDir, "settings.json")
	FavouritesFile = filepath.Join(ConfigDir, "favourites.json")
	PolicyFile = filepath.Join(ConfigDir, "policies.json")
	// The league's house rules, per league id: they belong with the session and the
	// preferences, not with the cache, because nothing can regenerate them.
	RulesFile = filepath.Join(ConfigDir, "rules.json")
	ReportFile = filepath.Join(StateDir, "report.html")
	LogFile = filepath.Join(StateDir, "fantasy.log")
}

// resolveDirs honours one override and the XDG spec, in the same order as Python.
//
// FANTASY_DATA_DIR collapses all three into one directory, which is what a container
// wants. Without it, a legacy ./data holding a session still wins so an existing
// install keeps working, and otherwise the files land where the system keeps them.
func resolveDirs() (string, string, string) {
	if override := os.Getenv("FANTASY_DATA_DIR"); override != "" {
		one := expand(override)
		ExplicitDir = true
		return one, one, filepath.Join(one, "cache")
	}
	// Relative to the working directory: run from the repo and the Python session is
	// picked up, which is the whole point during the port.
	if legacy, err := filepath.Abs("data"); err == nil {
		if exists(filepath.Join(legacy, "tokens.json")) || exists(filepath.Join(legacy, "settings.json")) {
			return legacy, legacy, filepath.Join(legacy, "cache")
		}
	}
	return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), AppName),
		filepath.Join(xdg("XDG_STATE_HOME", ".local/state"), AppName),
		filepath.Join(xdg("XDG_CACHE_HOME", ".cache"), AppName)
}

func xdg(variable, fallback string) string {
	if base := os.Getenv(variable); base != "" {
		return expand(base)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fallback
	}
	return filepath.Join(home, filepath.FromSlash(fallback))
}

func expand(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// EnsureDirs creates the three directories. Tokens are written 0600 by the auth
// package; the directories themselves are 0700 because two of them hold credentials.
func EnsureDirs() error {
	for _, dir := range []string{ConfigDir, StateDir, CacheDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
