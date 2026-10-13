package dismissed

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func TestADismissalIsRememberedUntilCleared(t *testing.T) {
	config.StateDir = t.TempDir()
	if err := Add("swap:2384>2206@2026-10-13T19:00:00+02:00"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, ok := Load()["swap:2384>2206@2026-10-13T19:00:00+02:00"]; !ok {
		t.Fatal("the dismissal was not kept")
	}
	if err := Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if len(Load()) != 0 {
		t.Error("clearing left dismissals behind")
	}
}

func TestOldDismissalsAreForgotten(t *testing.T) {
	config.StateDir = t.TempDir()
	blob, _ := json.Marshal(map[string]time.Time{"old": time.Now().Add(-Keep - time.Hour),
		"new": time.Now()})
	if err := os.WriteFile(path(), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	left := Load()
	if _, ok := left["old"]; ok {
		t.Error("a dismissal older than Keep survived")
	}
	if _, ok := left["new"]; !ok {
		t.Error("a fresh dismissal was dropped")
	}
}
