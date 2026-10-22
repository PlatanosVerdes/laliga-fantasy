package necroporra

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func necroFile(t *testing.T, body string) (string, *Store) {
	t.Helper()
	root := config.ConfigRoot
	t.Cleanup(func() { config.ConfigRoot = root })
	config.ConfigRoot = t.TempDir()
	t.Setenv("FANTASY_SECRET_KEY", "")
	t.Setenv("FANTASY_SECRET_FILE", "")
	path := filepath.Join(t.TempDir(), "necroporra.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FANTASY_NECROPORRA", path)
	return path, Open(config.ProfileIn(t.TempDir(), "cuenta", "L"))
}

func TestTheCookieAndActionAreSealedAndTheRestStaysReadable(t *testing.T) {
	path, store := necroFile(t, `{"cookie":"the-cookie","action_id":"the-action","gameweek":9,"roster":{"Ana":"1"}}`)
	want := Config{Cookie: "the-cookie", ActionID: "the-action", Gameweek: 9, Roster: map[string]string{"Ana": "1"}}
	for range 2 {
		cfg := store.Load()
		if cfg.Cookie != want.Cookie || cfg.ActionID != want.ActionID || cfg.Gameweek != 9 || cfg.Roster["Ana"] != "1" {
			t.Fatalf("read %+v", cfg)
		}
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "the-cookie") || strings.Contains(string(body), "the-action") {
		t.Fatalf("secrets left in the clear: %s", body)
	}
	var stored struct {
		Gameweek int               `json:"gameweek"`
		Roster   map[string]string `json:"roster"`
	}
	if json.Unmarshal(body, &stored) != nil || stored.Gameweek != 9 || stored.Roster["Ana"] != "1" {
		t.Errorf("the rest is no longer readable: %s", body)
	}
}

func TestAFreshCookiePastedInIsSealedBesideASealedAction(t *testing.T) {
	path, store := necroFile(t, `{"cookie":"old","action_id":"the-action","gameweek":9}`)
	store.Load()
	var fields map[string]json.RawMessage
	body, _ := os.ReadFile(path)
	_ = json.Unmarshal(body, &fields)
	fields["cookie"] = json.RawMessage(`"new"`)
	body, _ = json.Marshal(fields)
	_ = os.WriteFile(path, body, 0o600)
	if cfg := store.Load(); cfg.Cookie != "new" || cfg.ActionID != "the-action" {
		t.Fatalf("read %+v", cfg)
	}
	if body, _ := os.ReadFile(path); strings.Contains(string(body), `"new"`) {
		t.Errorf("the pasted cookie was not sealed: %s", body)
	}
}

func TestAnotherKeyDisablesVotingAndKeepsTheFile(t *testing.T) {
	path, store := necroFile(t, `{"cookie":"the-cookie","action_id":"the-action","gameweek":9}`)
	store.Load()
	before, _ := os.ReadFile(path)
	t.Setenv("FANTASY_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if cfg := store.Load(); cfg.Cookie != "" || cfg.CanVote() {
		t.Errorf("read under another key: %+v", cfg)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("the file was rewritten")
	}
}

func TestEachProfileReadsItsOwnConfig(t *testing.T) {
	t.Setenv("FANTASY_NECROPORRA", "")
	root := t.TempDir()
	for league, body := range map[string]string{"A": `{"gameweek":9}`, "B": `{"gameweek":10}`} {
		profile := config.ProfileIn(root, "cuenta", league)
		if err := profile.EnsureDirs(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(profile.NecroporraFile, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := Open(config.ProfileIn(root, "cuenta", "A")).Load()
	b := Open(config.ProfileIn(root, "cuenta", "B")).Load()
	if a.Gameweek != 9 || b.Gameweek != 10 {
		t.Errorf("gameweeks: a %d, b %d", a.Gameweek, b.Gameweek)
	}
}
