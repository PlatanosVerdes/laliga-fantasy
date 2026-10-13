package api

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

// Valid JSON of the wrong shape must fail inside the decode httpx retries, not after it.
func TestACachedListOfTheWrongShapeIsFetchedAgain(t *testing.T) {
	dir := t.TempDir()
	config.ConfigDir, config.StateDir, config.CacheDir = dir, dir, dir
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"data":[{"id":"m1","matchState":7}]}`))
	}))
	defer server.Close()

	sum := sha1.Sum([]byte("calendar|" + server.URL))
	path := filepath.Join(dir, "calendar_"+hex.EncodeToString(sum[:])[:20]+".cache")
	if err := os.WriteFile(path, []byte(`{"data":{"message":"busy"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var matches []Match
	err := httpx.GetJSON(httpx.Request{URL: server.URL, TTL: time.Hour, Tag: "calendar"},
		&listOf{&matches})
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 || len(matches) != 1 || matches[0].MatchState != 7 {
		t.Fatalf("hits=%d matches=%+v", hits, matches)
	}
}
