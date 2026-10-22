package necroporra

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// The local stand-in reads its round from a file and takes a vote into it, never the real app.
func TestTheFakeNecroporraTakesAVoteLocally(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.json")
	if err := os.WriteFile(path, []byte(`{"config":{"gameweek":9,"roster":{"a":"1"}},
		"state":{"Gameweek":9,"Open":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FANTASY_NECRO_FAKE", path)
	store := Open(config.ProfileIn(t.TempDir(), "cuenta", "L"))
	if !Faked() || store.Load().Gameweek != 9 || !FetchState(store.Load(), config.CacheScope{}).Open {
		t.Fatal("the fake was not read")
	}
	if _, err := CastVote(store.Load(), 9, "1", "2"); err != nil {
		t.Fatal(err)
	}
	if chosen := FetchState(store.Load(), config.CacheScope{}).Chosen; len(chosen) != 2 || chosen[0] != "1" {
		t.Errorf("the vote did not land in the fake: %v", chosen)
	}
}
