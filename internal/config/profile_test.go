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
