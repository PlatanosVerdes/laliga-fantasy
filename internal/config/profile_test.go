package config

import (
	"os"
	"path/filepath"
	"testing"
)

func roots(t *testing.T) {
	t.Helper()
	ConfigRoot, StateRoot, CacheRoot = t.TempDir(), t.TempDir(), t.TempDir()
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A single-account install becomes its first profile: session to the account, the rest to the
// league, and the shared log left where it was.
func TestASingleAccountInstallMovesIntoItsProfile(t *testing.T) {
	roots(t)
	write(t, filepath.Join(ConfigRoot, "tokens.json"), `{"email":"Jorge.G@Example.com"}`)
	write(t, filepath.Join(ConfigRoot, "settings.json"), `{"league_id":"018012894"}`)
	write(t, filepath.Join(ConfigRoot, "policies.json"), `{"1":{"id":"1"}}`)
	write(t, filepath.Join(StateRoot, "offer_endings.json"), `[]`)
	write(t, filepath.Join(StateRoot, "usage.log"), ``)

	if err := migrate(); err != nil {
		t.Fatal(err)
	}
	account, league := Active()
	if account != "jorge-g-example-com" || league != "018012894" {
		t.Fatalf("active = %q %q", account, league)
	}
	Use(account, league)
	for _, path := range []string{TokenFile, SettingsFile, PolicyFile,
		filepath.Join(StateDir, "offer_endings.json"), UsageFile} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing after the move: %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(ConfigRoot, "tokens.json")); err == nil {
		t.Error("the session was left at the root")
	}
	// Run again on the profile layout it changes nothing.
	if err := migrate(); err != nil {
		t.Fatal(err)
	}
	if again, _ := Active(); again != account {
		t.Errorf("a second run changed the profile: %q", again)
	}
}

// Two leagues of one account keep their instructions apart, and share the session.
func TestLeaguesOfOneAccountKeepTheirOwnFiles(t *testing.T) {
	roots(t)
	Use("cuenta", "A")
	tokensA, policiesA := TokenFile, PolicyFile
	Use("cuenta", "B")
	if TokenFile != tokensA {
		t.Error("two leagues of one account should share the session")
	}
	if PolicyFile == policiesA || CacheDir == filepath.Join(CacheRoot) {
		t.Error("two leagues share their instructions or their cache")
	}
}

func TestAccountIDIsSafeForAPath(t *testing.T) {
	for email, want := range map[string]string{
		"jorge_roller.98@hotmail.com": "jorge-roller-98-hotmail-com",
		"../../etc":                   "etc",
		"":                            "cuenta",
	} {
		if got := AccountID(email); got != want {
			t.Errorf("AccountID(%q) = %q, want %q", email, got, want)
		}
	}
}

// In a container every root is the same directory; both groups of files must still move.
func TestMigrationMovesEverythingWhenTheRootsAreOne(t *testing.T) {
	one := t.TempDir()
	ConfigRoot, StateRoot, CacheRoot = one, one, filepath.Join(one, "cache")
	write(t, filepath.Join(one, "tokens.json"), `{"email":"a@b.c"}`)
	write(t, filepath.Join(one, "settings.json"), `{"league_id":"L"}`)
	write(t, filepath.Join(one, "policies.json"), `{}`)
	write(t, filepath.Join(one, "rules.json"), `{}`)
	write(t, filepath.Join(one, "offer_endings.json"), `[]`)
	if err := migrate(); err != nil {
		t.Fatal(err)
	}
	Use(Active())
	for _, path := range []string{PolicyFile, RulesFile, SettingsFile,
		filepath.Join(StateDir, "offer_endings.json")} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("not moved: %s", path)
		}
	}
	for _, name := range []string{"policies.json", "rules.json", "settings.json"} {
		if _, err := os.Stat(filepath.Join(one, name)); err == nil {
			t.Errorf("%s was left at the root", name)
		}
	}
}

// Every path a profile names is the one the globals named before it existed.
func TestAProfileLaysOutTodaysPaths(t *testing.T) {
	ConfigRoot, StateRoot, CacheRoot = "/c", "/s", "/k"
	league := "/c/accounts/a/leagues/L/"
	history := "/s/accounts/a/leagues/L/"
	want := Profile{Account: "a", League: "L",
		ConfigDir: "/c/accounts/a/leagues/L", StateDir: "/s/accounts/a/leagues/L",
		CacheDir: "/k/accounts/a/leagues/L", TokenFile: "/c/accounts/a/tokens.json",
		SettingsFile: league + "settings.json", PolicyFile: league + "policies.json",
		RulesFile: league + "rules.json", ServicesFile: league + "services.json",
		NecroporraFile: league + "necroporra.json",
		ReportFile:     history + "report.html", ForecastFile: history + "forecasts.json",
		ClauseFile: history + "clauses.json", OfferEndingsFile: history + "offer_endings.json",
		OrderEndingsFile: history + "order_endings.json", DismissedFile: history + "dismissed.json",
		RewardFile: history + "daily_reward.json"}
	if got := ProfileFor("a", "L"); got != want {
		t.Errorf("ProfileFor:\n got %+v\nwant %+v", got, want)
	}
	Use("a", "L")
	if got := Default(); got != want {
		t.Errorf("Default after Use:\n got %+v\nwant %+v", got, want)
	}
	if TokenFile != want.TokenFile || PolicyFile != want.PolicyFile || ClauseFile != want.ClauseFile ||
		ServicesFile != want.ServicesFile || LogFile != "/s/fantasy.log" || UsageFile != "/s/usage.log" {
		t.Errorf("Use filled the globals differently: %s %s %s %s %s %s", TokenFile, PolicyFile,
			ClauseFile, ServicesFile, LogFile, UsageFile)
	}
}

// An account with no league keeps the league's files in its own folder, and no account at all
// is the flat layout of a single-account install: everything at the roots.
func TestAccountOnlyAndFlatLayoutsResolveAsBefore(t *testing.T) {
	ConfigRoot, StateRoot, CacheRoot = "/c", "/s", "/k"
	account := ProfileFor("a", "")
	if account.ConfigDir != "/c/accounts/a" || account.StateDir != "/s" || account.CacheDir != "/k" ||
		account.PolicyFile != "/c/accounts/a/policies.json" || account.DismissedFile != "/s/dismissed.json" ||
		account.TokenFile != "/c/accounts/a/tokens.json" {
		t.Errorf("account only: %+v", account)
	}
	flat := ProfileFor("", "")
	if flat.ConfigDir != "/c" || flat.PolicyFile != "/c/policies.json" ||
		flat.ForecastFile != "/s/forecasts.json" || flat.TokenFile != "/c/accounts/.none/tokens.json" {
		t.Errorf("flat: %+v", flat)
	}
	one := "/data"
	ConfigRoot, StateRoot, CacheRoot = one, one, filepath.Join(one, "cache")
	if container := ProfileFor("a", "L"); container.PolicyFile != "/data/accounts/a/leagues/L/policies.json" ||
		container.ClauseFile != "/data/accounts/a/leagues/L/clauses.json" ||
		container.CacheDir != "/data/cache/accounts/a/leagues/L" {
		t.Errorf("one root: %+v", container)
	}
	Use("", "")
	if Default() != ProfileFor("", "") || Default().PolicyFile != "/data/policies.json" {
		t.Errorf("Default on the flat layout: %+v", Default())
	}
}
