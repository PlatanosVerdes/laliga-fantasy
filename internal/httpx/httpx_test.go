package httpx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func scratchDirs(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	config.ConfigDir, config.StateDir, config.CacheDir, config.CacheRoot = dir, dir, dir, dir
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
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

func profiles(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	one, two := filepath.Join(root, "one"), filepath.Join(root, "two")
	config.CacheRoot, config.CacheDir = root, one
	return one, two
}

func counting(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func TestAGlobalTagIsFetchedOnceForEveryProfile(t *testing.T) {
	one, two := profiles(t)
	server, hits := counting(t)
	request := Request{URL: server.URL, TTL: time.Hour, Tag: "players"}
	if _, err := Fetch(request); err != nil {
		t.Fatal(err)
	}
	config.CacheDir = two
	if _, err := Fetch(request); err != nil || *hits != 1 {
		t.Fatalf("hits=%d err=%v", *hits, err)
	}
	shared, _ := filepath.Glob(filepath.Join(config.GlobalCacheDir(), "players_*.cache"))
	if len(shared) != 1 {
		t.Errorf("not in the shared folder: %v", shared)
	}
	for _, dir := range []string{one, two} {
		if found, _ := filepath.Glob(filepath.Join(dir, "*.cache")); len(found) != 0 {
			t.Errorf("a copy in the profile folder: %v", found)
		}
	}
}

func TestALeagueTagIsNotSharedWithAnotherProfile(t *testing.T) {
	one, two := profiles(t)
	server, hits := counting(t)
	request := Request{URL: server.URL, TTL: time.Hour, Tag: "market"}
	if _, err := Fetch(request); err != nil {
		t.Fatal(err)
	}
	config.CacheDir = two
	if _, err := Fetch(request); err != nil || *hits != 2 {
		t.Fatalf("hits=%d err=%v", *hits, err)
	}
	for _, dir := range []string{one, two} {
		if found, _ := filepath.Glob(filepath.Join(dir, "market_*.cache")); len(found) != 1 {
			t.Errorf("%s holds %v", dir, found)
		}
	}
}

func TestInvalidateDropsGlobalAndLeagueTagsWhereEachLives(t *testing.T) {
	profiles(t)
	server, _ := counting(t)
	for _, tag := range []string{"players", "market"} {
		if _, err := Fetch(Request{URL: server.URL, TTL: time.Hour, Tag: tag}); err != nil {
			t.Fatal(err)
		}
	}
	if dropped := Invalidate("players", "market"); dropped != 2 {
		t.Fatalf("dropped %d", dropped)
	}
	for _, tag := range []string{"players", "market"} {
		if _, ok := Cached(server.URL, tag, time.Hour); ok {
			t.Errorf("%s still cached", tag)
		}
	}
}
