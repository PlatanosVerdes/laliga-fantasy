package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx/httpxtest"
)

const tokenPath = "/laligadspprob2c.onmicrosoft.com/oauth2/v2.0/token"

func fakeProvider(t *testing.T, revoked ...string) *httpxtest.Fake {
	t.Helper()
	fake := httpxtest.New(t)
	fake.Handle("POST "+tokenPath, func(writer http.ResponseWriter, request *http.Request) {
		_ = request.ParseForm()
		token := request.PostForm.Get("refresh_token")
		for _, gone := range revoked {
			if token == gone {
				http.Error(writer, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "new-" + token,
			"refresh_token": token + "-2", "expires_in": 3600})
	})
	t.Cleanup(httpx.SetTransport(fake))
	return fake
}

func refreshes(fake *httpxtest.Fake) map[string]int {
	out := map[string]int{}
	for _, call := range fake.Calls() {
		if call.Path != tokenPath {
			continue
		}
		form, _ := url.ParseQuery(string(call.Body))
		out[form.Get("refresh_token")]++
	}
	return out
}

func tempRoot(t *testing.T) {
	t.Helper()
	root := config.ConfigRoot
	t.Cleanup(func() { config.ConfigRoot = root })
	config.ConfigRoot = t.TempDir()
}

func expiring(t *testing.T, account string, tokens Tokens) *Source {
	t.Helper()
	tokens.ExpiresOn = time.Now().Add(time.Minute).Unix()
	source := For(account)
	if err := source.Save(&tokens); err != nil {
		t.Fatal(err)
	}
	return source
}

func bearers(t *testing.T, source *Source, times int, wait *sync.WaitGroup, want string) {
	for range times {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if bearer, err := source.Bearer(); err != nil || bearer != want {
				t.Errorf("bearer %q, err %v; want %q", bearer, err, want)
			}
		}()
	}
}

func TestConcurrentRefreshesWriteOnlyTheirOwnSession(t *testing.T) {
	tempRoot(t)
	fake := fakeProvider(t)
	a := expiring(t, "a", Tokens{AccessToken: "old-a", RefreshToken: "rA", Email: "a@x.com"})
	b := expiring(t, "b", Tokens{AccessToken: "old-b", RefreshToken: "rB", Email: "b@x.com"})

	var wait sync.WaitGroup
	bearers(t, a, 10, &wait, "new-rA")
	bearers(t, b, 10, &wait, "new-rB")
	wait.Wait()

	for _, c := range []struct {
		source              *Source
		access, refresh, at string
	}{{a, "new-rA", "rA-2", config.AccountTokenFile("a")},
		{b, "new-rB", "rB-2", config.AccountTokenFile("b")}} {
		if c.source.path != c.at {
			t.Errorf("session kept at %s, want %s", c.source.path, c.at)
		}
		stored, err := c.source.Load()
		if err != nil || stored.AccessToken != c.access || stored.RefreshToken != c.refresh {
			t.Errorf("%s holds %+v (err %v)", c.at, stored, err)
		}
	}
	if got := refreshes(fake); got["rA"] != 1 || got["rB"] != 1 || len(got) != 2 {
		t.Errorf("renewals asked: %v, want one each", got)
	}
}

func TestAStoppedAccountLeavesTheOtherWorking(t *testing.T) {
	tempRoot(t)
	fakeProvider(t, "rA")
	a := expiring(t, "a", Tokens{AccessToken: "old-a", RefreshToken: "rA",
		FailedRefreshes: MaxFailedRefreshes - 1})
	b := expiring(t, "b", Tokens{AccessToken: "old-b", RefreshToken: "rB"})

	if _, err := a.Bearer(); !errors.Is(err, ErrStopped) {
		t.Fatalf("A answered %v, want ErrStopped", err)
	}
	if bearer, err := b.Bearer(); err != nil || bearer != "new-rB" {
		t.Errorf("B answered %q, %v", bearer, err)
	}
	if stored, _ := b.Load(); stored.Stopped() || stored.FailedRefreshes != 0 {
		t.Errorf("B caught A's failures: %+v", stored)
	}
	if _, err := a.Bearer(); !errors.Is(err, ErrStopped) {
		t.Errorf("A came back on its own: %v", err)
	}
}

func TestOneAccountIsOneSourceAndRenewsOnce(t *testing.T) {
	tempRoot(t)
	fake := fakeProvider(t)
	expiring(t, "a", Tokens{AccessToken: "old-a", RefreshToken: "rA"})
	if For("a") != For("a") {
		t.Fatal("two sources for one account")
	}
	var wait sync.WaitGroup
	for range 4 {
		bearers(t, For("a"), 5, &wait, "new-rA")
	}
	wait.Wait()
	if got := refreshes(fake); got["rA"] != 1 || len(got) != 1 {
		t.Errorf("renewals asked: %v, want rA once", got)
	}
}
