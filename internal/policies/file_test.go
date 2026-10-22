package policies

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// A file that does not parse stops every edit; read as empty, the next save wiped every
// armed instruction.
func TestACorruptFileIsAnErrorNotAnEmptySet(t *testing.T) {
	profile := config.ProfileIn(t.TempDir(), "cuenta", "L")
	store := Open(profile)
	if err := os.MkdirAll(profile.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profile.PolicyFile, []byte(`{"1": {"id": "1", "raid": tr`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Error("a half-written file loaded as if it were fine")
	}
	if _, err := store.Set("2", func(p *Policy) { p.AlwaysList = true }); err == nil {
		t.Error("an edit went through over a corrupt file")
	}
	if body, _ := os.ReadFile(profile.PolicyFile); string(body) != `{"1": {"id": "1", "raid": tr` {
		t.Errorf("the corrupt file was overwritten: %s", body)
	}
}

// Edits from the engine, a rebuild and a click land side by side; none may lose another's.
func TestEditsSideBySideKeepEachOther(t *testing.T) {
	store := Open(config.ProfileIn(t.TempDir(), "cuenta", "L"))
	var wait sync.WaitGroup
	for i := range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := store.Set(fmt.Sprint(i), func(p *Policy) { p.AlwaysList = true }); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	armed, err := store.Load()
	if err != nil || len(armed) != 20 {
		t.Errorf("kept %d of 20 instructions (err %v)", len(armed), err)
	}
}

// Two leagues arm their own instructions: what one arms the other never reads.
func TestAProfileNeverSeesAnothersInstructions(t *testing.T) {
	root := t.TempDir()
	a, b := Open(config.ProfileIn(root, "cuenta", "A")), Open(config.ProfileIn(root, "cuenta", "B"))
	if _, err := a.Set("1", func(p *Policy) { p.Raid = true }); err != nil {
		t.Fatal(err)
	}
	if armed, err := b.Load(); err != nil || len(armed) != 0 {
		t.Errorf("B reads A's instructions: %v (err %v)", armed, err)
	}
	if err := b.Remove("1"); err != nil {
		t.Fatal(err)
	}
	if armed, _ := a.Load(); !armed["1"].Raid {
		t.Errorf("a removal on B disarmed A: %v", armed)
	}
}

// Edits on two profiles at once keep every one of each; a second Open of a profile is its store,
// lock included.
func TestEditsOnTwoProfilesSideBySide(t *testing.T) {
	root := t.TempDir()
	stores := []*Store{Open(config.ProfileIn(root, "uno", "L")), Open(config.ProfileIn(root, "dos", "L"))}
	if Open(config.ProfileIn(root, "uno", "L")) != stores[0] {
		t.Fatal("opening a profile twice gave two locks over one file")
	}
	var wait sync.WaitGroup
	for i := range 40 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := stores[i%2].Set(fmt.Sprint(i), func(p *Policy) { p.AlwaysList = true }); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	for index, store := range stores {
		armed, err := store.Load()
		if err != nil || len(armed) != 20 {
			t.Errorf("store %d kept %d of 20 (err %v)", index, len(armed), err)
		}
		for id := range armed {
			var n int
			fmt.Sscan(id, &n)
			if n%2 != index {
				t.Errorf("store %d holds %s", index, id)
			}
		}
	}
}
