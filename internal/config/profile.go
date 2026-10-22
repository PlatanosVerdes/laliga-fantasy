package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// A profile is one LaLiga account and one of its leagues. The session belongs to the account;
// everything else (settings, standing instructions, house rules, history, cache) to the league,
// because a raid on a player in one league means nothing in another.
//
//	<config root>/active.json                       which account and league are in use
//	<config root>/accounts/<account>/tokens.json    the account's session
//	<config root>/accounts/<account>/leagues/<id>/  the league's settings and instructions
//	<state root>/accounts/<account>/leagues/<id>/   its history
//	<cache root>/accounts/<account>/leagues/<id>/   its cache
//
// One profile is active per process; switching restarts it, so no path changes under a reader.
var (
	ConfigRoot string
	StateRoot  string
	CacheRoot  string

	// Account and League are the active profile; League is empty until one is chosen.
	Account string
	League  string
)

func activeFile() string { return filepath.Join(ConfigRoot, "active.json") }

// AccountDir is where an account's session lives.
func AccountDir(account string) string { return filepath.Join(ConfigRoot, "accounts", account) }

// AccountTokenFile is an account's session file, active or not.
func AccountTokenFile(account string) string {
	return filepath.Join(AccountDir(account), "tokens.json")
}

// Profile is where one account in one league keeps its files. A value: two profiles can be
// open at once, and nothing in it changes under a reader.
type Profile struct {
	Account string
	League  string

	ConfigDir string
	StateDir  string
	CacheDir  string

	TokenFile      string
	SettingsFile   string
	PolicyFile     string
	RulesFile      string
	ServicesFile   string
	NecroporraFile string

	ReportFile       string
	ForecastFile     string
	ClauseFile       string
	OfferEndingsFile string
	OrderEndingsFile string
	DismissedFile    string
	RewardFile       string
}

// ProfileFor lays a profile out under the roots. Without an account every file sits at the
// roots, and without a league the account's own folder holds the league's files.
func ProfileFor(account, league string) Profile {
	return layout(ConfigRoot, StateRoot, CacheRoot, account, league)
}

// ProfileIn lays a profile out under one root, the way FANTASY_DATA_DIR does.
func ProfileIn(root, account, league string) Profile {
	return layout(root, root, filepath.Join(root, "cache"), account, league)
}

func layout(configRoot, stateRoot, cacheRoot, account, league string) Profile {
	p := Profile{Account: account, League: league,
		ConfigDir: configRoot, StateDir: stateRoot, CacheDir: cacheRoot,
		TokenFile: filepath.Join(configRoot, "accounts", ".none", "tokens.json")}
	if account != "" {
		p.ConfigDir = filepath.Join(configRoot, "accounts", account)
		p.TokenFile = filepath.Join(p.ConfigDir, "tokens.json")
		if league != "" {
			dir := filepath.Join("accounts", account, "leagues", league)
			p.ConfigDir = filepath.Join(configRoot, dir)
			p.StateDir = filepath.Join(stateRoot, dir)
			p.CacheDir = filepath.Join(cacheRoot, dir)
		}
	}
	p.fill()
	return p
}

func (p *Profile) fill() {
	p.SettingsFile = filepath.Join(p.ConfigDir, "settings.json")
	p.PolicyFile = filepath.Join(p.ConfigDir, "policies.json")
	// The league's house rules: they belong with the instructions, because nothing can
	// regenerate them.
	p.RulesFile = filepath.Join(p.ConfigDir, "rules.json")
	// The job intervals changed from the page: a preference, so it sits with the settings.
	p.ServicesFile = filepath.Join(p.ConfigDir, "services.json")
	p.NecroporraFile = filepath.Join(p.ConfigDir, "necroporra.json")
	p.ReportFile = filepath.Join(p.StateDir, "report.html")
	// What was forecast for each saved eleven before kick-off: once the ball rolls it cannot be
	// asked again, so losing the file loses that history.
	p.ForecastFile = filepath.Join(p.StateDir, "forecasts.json")
	// The clauses seen at the last build: the log does not tell a clause paid from an offer
	// accepted, and the clause before the transfer is the only way to.
	p.ClauseFile = filepath.Join(p.StateDir, "clauses.json")
	p.OfferEndingsFile = filepath.Join(p.StateDir, "offer_endings.json")
	p.OrderEndingsFile = filepath.Join(p.StateDir, "order_endings.json")
	p.DismissedFile = filepath.Join(p.StateDir, "dismissed.json")
	p.RewardFile = filepath.Join(p.StateDir, "daily_reward.json")
}

// ActiveProfile is the profile active.json names.
func ActiveProfile() Profile { return ProfileFor(Active()) }

// Default is the profile the process runs on, read from the globals Use fills until they go.
func Default() Profile {
	p := Profile{Account: Account, League: League,
		ConfigDir: ConfigDir, StateDir: StateDir, CacheDir: CacheDir, TokenFile: TokenFile}
	p.fill()
	p.SettingsFile, p.PolicyFile, p.RulesFile, p.ServicesFile = SettingsFile, PolicyFile, RulesFile,
		ServicesFile
	p.ReportFile, p.ForecastFile, p.ClauseFile = ReportFile, ForecastFile, ClauseFile
	return p
}

// EnsureDirs creates the profile's folders, 0700 like the roots.
func (p Profile) EnsureDirs() error {
	for _, dir := range []string{p.ConfigDir, p.StateDir, p.CacheDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// Use points every path at a profile. Called once at start; a switch writes active.json and
// restarts instead.
func Use(account, league string) {
	p := ProfileFor(account, league)
	Account, League = p.Account, p.League
	ConfigDir, StateDir, CacheDir = p.ConfigDir, p.StateDir, p.CacheDir
	TokenFile, SettingsFile, PolicyFile, RulesFile, ServicesFile = p.TokenFile, p.SettingsFile,
		p.PolicyFile, p.RulesFile, p.ServicesFile
	ReportFile, ForecastFile, ClauseFile = p.ReportFile, p.ForecastFile, p.ClauseFile
	// The log and the usage are the panel's, whoever is in it. The .log suffix is load-bearing:
	// Vector tails *.log, so both reach VictoriaLogs with no configuration.
	LogFile = filepath.Join(StateRoot, "fantasy.log")
	UsageFile = filepath.Join(StateRoot, "usage.log")
}

// Active is the profile active.json names, or empty when there is none yet.
func Active() (account, league string) {
	var active struct {
		Account string `json:"account"`
		League  string `json:"league"`
	}
	body, err := os.ReadFile(activeFile())
	if err != nil || json.Unmarshal(body, &active) != nil {
		return "", ""
	}
	return active.Account, active.League
}

// SetActive records which account and league the next start uses.
func SetActive(account, league string) error {
	if err := os.MkdirAll(ConfigRoot, 0o700); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(map[string]string{"account": account, "league": league}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(activeFile(), blob)
}

var notID = regexp.MustCompile(`[^a-z0-9]+`)

// AccountID is the folder name for an account: its email, made safe for a path.
func AccountID(email string) string {
	id := strings.Trim(notID.ReplaceAllString(strings.ToLower(email), "-"), "-")
	if id == "" {
		return "cuenta"
	}
	return id
}

// Accounts are the saved accounts, by folder name.
func Accounts() []string {
	entries, err := os.ReadDir(filepath.Join(ConfigRoot, "accounts"))
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") &&
			exists(AccountTokenFile(entry.Name())) {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

// AccountLeagues are the leagues an account has been used with, by id, with the name saved for
// each.
func AccountLeagues(account string) map[string]string {
	out := map[string]string{}
	dir := filepath.Join(AccountDir(account), "leagues")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var settings struct {
			Name string `json:"league_name"`
		}
		if body, err := os.ReadFile(filepath.Join(dir, entry.Name(), "settings.json")); err == nil {
			_ = json.Unmarshal(body, &settings)
		}
		out[entry.Name()] = settings.Name
	}
	return out
}

// legacyConfig and legacyState are the files a single-account install kept at the roots.
var (
	legacyConfig = []string{"settings.json", "policies.json", "rules.json", "services.json",
		"necroporra.json"}
	legacyState = []string{"report.html", "forecasts.json", "clauses.json", "offer_endings.json",
		"order_endings.json", "dismissed.json", "daily_reward.json"}
)

// migrate moves a single-account install into its first profile: the session to the account,
// the rest to its league. Only when there is no active.json yet and a session at the root, so
// it runs once and never on a profile layout.
func migrate() error {
	if exists(activeFile()) || !exists(filepath.Join(ConfigRoot, "tokens.json")) {
		return nil
	}
	var tokens struct {
		Email string `json:"email"`
	}
	if body, err := os.ReadFile(filepath.Join(ConfigRoot, "tokens.json")); err == nil {
		_ = json.Unmarshal(body, &tokens)
	}
	var settings struct {
		League string `json:"league_id"`
	}
	if body, err := os.ReadFile(filepath.Join(ConfigRoot, "settings.json")); err == nil {
		_ = json.Unmarshal(body, &settings)
	}
	account := AccountID(tokens.Email)
	if err := os.MkdirAll(AccountDir(account), 0o700); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(ConfigRoot, "tokens.json"), AccountTokenFile(account)); err != nil {
		return fmt.Errorf("moving the session: %w", err)
	}
	if settings.League != "" {
		profile := filepath.Join("accounts", account, "leagues", settings.League)
		// A list, not a map keyed by target: in a container both roots are one directory, and
		// keyed by target the second group overwrote the first and its files stayed behind.
		for _, group := range []struct {
			root  string
			names []string
		}{{ConfigRoot, legacyConfig}, {StateRoot, legacyState}} {
			target := filepath.Join(group.root, profile)
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			for _, name := range group.names {
				from := filepath.Join(group.root, name)
				if exists(from) {
					if err := os.Rename(from, filepath.Join(target, name)); err != nil {
						return fmt.Errorf("moving %s: %w", name, err)
					}
				}
			}
		}
	}
	return SetActive(account, settings.League)
}

func writeAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
