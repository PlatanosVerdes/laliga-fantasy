package httpx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func scratchDirs(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	config.ConfigDir, config.StateDir, config.CacheDir = dir, dir, dir
}

// A cached body that does not parse used to be the answer until its TTL ran out.
func TestACachedBodyThatDoesNotParseIsFetchedAgain(t *testing.T) {
	scratchDirs(t)
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"week":7}`))
	}))
	defer server.Close()

	if err := os.WriteFile(cachePath(server.URL, "week"), []byte("<html>busy</html>"),
		0o600); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Week int `json:"week"`
	}
	request := Request{URL: server.URL, TTL: time.Hour, Tag: "week"}
	if err := GetJSON(request, &got); err != nil {
		t.Fatal(err)
	}
	if got.Week != 7 || hits != 1 {
		t.Fatalf("week=%d hits=%d", got.Week, hits)
	}
	if err := GetJSON(request, &got); err != nil || hits != 1 {
		t.Fatalf("the fresh body was not cached: hits=%d err=%v", hits, err)
	}
}
