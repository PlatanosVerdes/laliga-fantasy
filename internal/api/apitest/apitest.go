// Package apitest makes real members against the fake LaLiga, each account with its own token.
package apitest

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx/httpxtest"
)

type LaLiga struct {
	*httpxtest.Fake
	t testing.TB

	mu      sync.Mutex
	leagues map[string][]string
}

// New also points the config roots at a temporary folder. A route with no answer answers {}.
func New(t testing.TB) *LaLiga {
	t.Helper()
	roots := []string{config.ConfigRoot, config.StateRoot, config.CacheRoot}
	t.Cleanup(func() {
		config.ConfigRoot, config.StateRoot, config.CacheRoot = roots[0], roots[1], roots[2]
	})
	dir := t.TempDir()
	config.ConfigRoot, config.StateRoot, config.CacheRoot = dir, dir, filepath.Join(dir, "cache")

	laliga := &LaLiga{Fake: httpxtest.New(t), t: t, leagues: map[string][]string{}}
	laliga.Answer("/api/", http.StatusOK, `{}`)
	laliga.Handle("GET /api"+config.CMP+"/leagues", func(writer http.ResponseWriter,
		request *http.Request) {
		laliga.mu.Lock()
		entries := laliga.leagues[request.Header.Get("Authorization")]
		laliga.mu.Unlock()
		_, _ = io.WriteString(writer, "["+strings.Join(entries, ",")+"]")
	})
	t.Cleanup(httpx.SetTransport(laliga))
	t.Cleanup(httpx.SetQueue(httpx.NewQueue(nil, httpx.DefaultLimits(), nil)))
	return laliga
}

func Bearer(account string) string { return "Bearer token-" + account }

// Member is called once per account: the leagues list it is checked against is cached an hour.
func (l *LaLiga) Member(account, league, team string) *api.Member {
	l.t.Helper()
	if err := auth.For(account).Save(&auth.Tokens{AccessToken: "token-" + account,
		ExpiresOn: time.Now().Add(time.Hour).Unix()}); err != nil {
		l.t.Fatal(err)
	}
	l.mu.Lock()
	l.leagues[Bearer(account)] = append(l.leagues[Bearer(account)],
		fmt.Sprintf(`{"id":%q,"team":{"id":%q}}`, league, team))
	l.mu.Unlock()
	member, err := api.AccountOf(config.ProfileFor(account, league)).Member(league, team)
	if err != nil {
		l.t.Fatal(err)
	}
	return member
}

func (l *LaLiga) Writes() []httpxtest.Call {
	var out []httpxtest.Call
	for _, call := range l.Calls() {
		if call.Method != http.MethodGet {
			out = append(out, call)
		}
	}
	return out
}
