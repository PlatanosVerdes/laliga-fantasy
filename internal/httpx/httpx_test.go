package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// A cached body that does not parse used to be the answer until its TTL ran out.
func TestACachedBodyThatDoesNotParseIsFetchedAgain(t *testing.T) {
	scope := config.ProfileIn(t.TempDir(), "a", "L").Scope(config.LevelGlobal)
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"week":7}`))
	}))
	defer server.Close()

	writeCache(scope, server.URL, "week", "<html>busy</html>")
	var got struct {
		Week int `json:"week"`
	}
	request := Request{URL: server.URL, TTL: time.Hour, Tag: "week", Cache: scope}
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

func echo(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`"` + r.Header.Get("Authorization") + `"`))
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func members(t *testing.T) (config.Profile, config.Profile) {
	t.Helper()
	root := t.TempDir()
	return config.ProfileIn(root, "a", "L"), config.ProfileIn(root, "b", "L")
}

func as(profile config.Profile, url, tag, bearer string) Request {
	return Request{URL: url, TTL: time.Hour, Tag: tag, Cache: profile.ScopeOf(tag),
		Headers: map[string]string{"Authorization": bearer}}
}

func TestAGlobalTagIsFetchedOnceForEveryProfile(t *testing.T) {
	a, b := members(t)
	server, hits := echo(t)
	for _, profile := range []config.Profile{a, b} {
		if _, err := Fetch(as(profile, server.URL, "players", "")); err != nil {
			t.Fatal(err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d", hits.Load())
	}
	shared, _ := filepath.Glob(filepath.Join(a.Scope(config.LevelGlobal).Dir(), "players_*.cache"))
	if len(shared) != 1 {
		t.Errorf("not in the shared folder: %v", shared)
	}
}

func TestTheSameMarketURLIsKeptApartForTwoMembers(t *testing.T) {
	a, b := members(t)
	server, hits := echo(t)
	bodyA, errA := Fetch(as(a, server.URL, "market", "A"))
	bodyB, errB := Fetch(as(b, server.URL, "market", "B"))
	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	if hits.Load() != 2 || bodyA != `"A"` || bodyB != `"B"` {
		t.Fatalf("hits=%d A=%s B=%s", hits.Load(), bodyA, bodyB)
	}
	again, _ := Fetch(as(b, server.URL, "market", "B"))
	if again != `"B"` || hits.Load() != 2 {
		t.Fatalf("B read %s from the cache, hits=%d", again, hits.Load())
	}
	for _, profile := range []config.Profile{a, b} {
		dir := profile.ScopeOf("market").Dir()
		if found, _ := filepath.Glob(filepath.Join(dir, "market_*.cache")); len(found) != 1 {
			t.Errorf("%s holds %v", dir, found)
		}
	}
}

func TestInvalidatingOneMemberLeavesTheOther(t *testing.T) {
	a, b := members(t)
	server, _ := echo(t)
	for _, request := range []Request{as(a, server.URL, "market", "A"), as(b, server.URL, "market", "B")} {
		if _, err := Fetch(request); err != nil {
			t.Fatal(err)
		}
	}
	if dropped := Invalidate(a.ScopeOf("market"), "market"); dropped != 1 {
		t.Fatalf("dropped %d", dropped)
	}
	if _, ok := Cached(a.ScopeOf("market"), server.URL, "market", time.Hour); ok {
		t.Error("A's market is still cached")
	}
	if body, ok := Cached(b.ScopeOf("market"), server.URL, "market", time.Hour); !ok || body != `"B"` {
		t.Errorf("B's market went with A's: %q %v", body, ok)
	}
}

func TestATagOutsideItsLevelIsRefusedBeforeTheWire(t *testing.T) {
	a, _ := members(t)
	server, hits := echo(t)
	for _, scope := range []config.CacheScope{a.Scope(config.LevelGlobal), a.Scope(config.LevelLeague),
		a.Scope(config.LevelAccount), {}} {
		var misscoped *Misscoped
		_, err := Fetch(Request{URL: server.URL, TTL: time.Hour, Tag: "market", Cache: scope})
		if !errors.As(err, &misscoped) {
			t.Errorf("market scoped %s: %v", scope.Level(), err)
		}
	}
	if _, err := Fetch(Request{URL: server.URL, TTL: time.Hour, Tag: "squad",
		Cache: a.Scope(config.LevelMember)}); err == nil {
		t.Error("a league tag in a member folder went through")
	}
	if _, err := Fetch(Request{URL: server.URL, TTL: time.Hour, Tag: "made-up"}); err == nil {
		t.Error("an undeclared tag was cached")
	}
	if hits.Load() != 0 {
		t.Fatalf("%d refused requests reached the wire", hits.Load())
	}
	if dropped := Invalidate(a.Scope(config.LevelLeague), "market"); dropped != 0 {
		t.Errorf("invalidated a member tag in a league folder: %d", dropped)
	}
}

func TestALeagueTagIsReadOnceForTwoMembersOfThatLeague(t *testing.T) {
	a, b := members(t)
	server, hits := echo(t)
	bodyA, _ := Fetch(as(a, server.URL, "squad", "A"))
	bodyB, err := Fetch(as(b, server.URL, "squad", "B"))
	if err != nil || hits.Load() != 1 || bodyB != bodyA {
		t.Fatalf("hits=%d A=%s B=%s err=%v", hits.Load(), bodyA, bodyB, err)
	}
	other := a.In("M")
	if a.ScopeOf("squad").Dir() == other.ScopeOf("squad").Dir() {
		t.Fatal("two leagues share a folder")
	}
}

func TestConcurrentIdenticalRequestsShareOneFetch(t *testing.T) {
	a, b := members(t)
	release := make(chan struct{})
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if body, err := Fetch(as(a, server.URL, "standing", "A")); err != nil || body != `[]` {
				t.Errorf("body=%q err=%v", body, err)
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		_, _ = Fetch(as(b, server.URL, "market", "B"))
	}()
	for hits.Load() < 2 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wait.Wait()
	if hits.Load() != 2 {
		t.Fatalf("hits=%d, want one per scope", hits.Load())
	}
}

func TestAFrozenCacheReplaysWithoutTheNetwork(t *testing.T) {
	defer func(was bool) { Frozen = was }(Frozen)
	a, _ := members(t)
	server, hits := echo(t)
	if _, err := Fetch(as(a, server.URL, "market", "A")); err != nil {
		t.Fatal(err)
	}
	// Captured before scopes: league and account answers sat in the member's folder.
	for _, tag := range []string{"standing", "me"} {
		if err := os.MkdirAll(a.CacheDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(a.CacheDir, cacheName(server.URL, tag)),
			[]byte(`"old `+tag+`"`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	Frozen = true
	for tag, want := range map[string]string{"market": `"A"`, "standing": `"old standing"`,
		"me": `"old me"`} {
		request := as(a, server.URL, tag, "A")
		request.TTL = 0
		if body, err := Fetch(request); err != nil || body != want {
			t.Errorf("%s: %q %v", tag, body, err)
		}
	}
	var miss *FrozenMiss
	if _, err := Fetch(as(a, server.URL+"/other", "market", "A")); !errors.As(err, &miss) {
		t.Errorf("a frozen miss went elsewhere: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("the frozen replay reached the wire: %d", hits.Load())
	}
}
