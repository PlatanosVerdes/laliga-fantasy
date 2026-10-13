package policies

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// A file that does not parse stops every edit; read as empty, the next save wiped every
// armed instruction.
func TestACorruptFileIsAnErrorNotAnEmptySet(t *testing.T) {
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	if err := os.WriteFile(config.PolicyFile, []byte(`{"1": {"id": "1", "raid": tr`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Error("a half-written file loaded as if it were fine")
	}
	if _, err := Set("2", func(p *Policy) { p.AlwaysList = true }); err == nil {
		t.Error("an edit went through over a corrupt file")
	}
	if body, _ := os.ReadFile(config.PolicyFile); string(body) != `{"1": {"id": "1", "raid": tr` {
		t.Errorf("the corrupt file was overwritten: %s", body)
	}
}

// Edits from the engine, a rebuild and a click land side by side; none may lose another's.
func TestEditsSideBySideKeepEachOther(t *testing.T) {
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	var wait sync.WaitGroup
	for i := range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := Set(fmt.Sprint(i), func(p *Policy) { p.AlwaysList = true }); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	armed, err := Load()
	if err != nil || len(armed) != 20 {
		t.Errorf("kept %d of 20 instructions (err %v)", len(armed), err)
	}
}
