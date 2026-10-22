package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteAtomicReplacesAndLeavesNoTemporary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policies.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(path); string(body) != "new" {
		t.Errorf("content = %q", body)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("a temporary file was left behind: %v", entries)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
}

// Writers that each read, add their line and write must all find their line at the end.
func TestUpdateKeepsEveryConcurrentWritersChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.txt")
	var wait sync.WaitGroup
	for writer := range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := Update(path, 0o600, func(current []byte) ([]byte, error) {
				return append(current, fmt.Sprintf("%d\n", writer)...), nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	body, _ := os.ReadFile(path)
	if lines := strings.Count(string(body), "\n"); lines != 20 {
		t.Errorf("%d of 20 changes kept:\n%s", lines, body)
	}
}
