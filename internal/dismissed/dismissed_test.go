package dismissed

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func TestADismissalIsRememberedUntilCleared(t *testing.T) {
	store := Open(config.ProfileIn(t.TempDir(), "cuenta", "L"))
	if err := store.Add("swap:2384>2206@2026-10-13T19:00:00+02:00"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, ok := store.Load()["swap:2384>2206@2026-10-13T19:00:00+02:00"]; !ok {
		t.Fatal("the dismissal was not kept")
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(store.Load()) != 0 {
		t.Error("clearing left dismissals behind")
	}
}

func TestOldDismissalsAreForgotten(t *testing.T) {
	profile := config.ProfileIn(t.TempDir(), "cuenta", "L")
	store := Open(profile)
	if err := profile.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(map[string]time.Time{"old": time.Now().Add(-Keep - time.Hour),
		"new": time.Now()})
	if err := os.WriteFile(profile.DismissedFile, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	left := store.Load()
	if _, ok := left["old"]; ok {
		t.Error("a dismissal older than Keep survived")
	}
	if _, ok := left["new"]; !ok {
		t.Error("a fresh dismissal was dropped")
	}
}

func TestADismissalOnOneLeagueLeavesTheOther(t *testing.T) {
	root := t.TempDir()
	a, b := Open(config.ProfileIn(root, "cuenta", "A")), Open(config.ProfileIn(root, "cuenta", "B"))
	if err := a.Add("sell:1"); err != nil {
		t.Fatal(err)
	}
	if len(b.Load()) != 0 {
		t.Errorf("B sees A's dismissal: %v", b.Load())
	}
}
