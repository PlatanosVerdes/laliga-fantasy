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

// Use points every path at a profile. Called once at start; a switch writes active.json and
// restarts instead.
func Use(account, league string) {
	Account, League = account, league
	ConfigDir, StateDir, CacheDir = ConfigRoot, StateRoot, CacheRoot
	TokenFile = filepath.Join(ConfigRoot, "accounts", ".none", "tokens.json")
	if account != "" {
		TokenFile = AccountTokenFile(account)
		ConfigDir = AccountDir(account)
		if league != "" {
			profile := filepath.Join("accounts", account, "leagues", league)
			ConfigDir = filepath.Join(ConfigRoot, profile)
			StateDir = filepath.Join(StateRoot, profile)
			CacheDir = filepath.Join(CacheRoot, profile)
		}
	}
	SettingsFile = filepath.Join(ConfigDir, "settings.json")
	PolicyFile = filepath.Join(ConfigDir, "policies.json")
	// The league's house rules: they belong with the instructions, because nothing can
	// regenerate them.
	RulesFile = filepath.Join(ConfigDir, "rules.json")
	// The job intervals changed from the page: a preference, so it sits with the settings.
	ServicesFile = filepath.Join(ConfigDir, "services.json")
	ReportFile = filepath.Join(StateDir, "report.html")
	// What was forecast for each saved eleven before kick-off: once the ball rolls it cannot be
	// asked again, so losing the file loses that history.
	ForecastFile = filepath.Join(StateDir, "forecasts.json")
	// The clauses seen at the last build: the log does not tell a clause paid from an offer
	// accepted, and the clause before the transfer is the only way to.
	ClauseFile = filepath.Join(StateDir, "clauses.json")
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
