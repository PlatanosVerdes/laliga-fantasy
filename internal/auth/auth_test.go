package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// A failing renewal waits out the cooldown, and after the third one the panel stops asking.
func TestFailedRefreshesStopAfterThree(t *testing.T) {
	config.TokenFile = filepath.Join(t.TempDir(), "tokens.json")
	calls := 0
	renew = func(*Tokens) (*Tokens, error) { calls++; return nil, fmt.Errorf("%w: invalid_grant", ErrRejected) }
	defer func() { renew = Refresh }()
	if err := Save(&Tokens{AccessToken: "a", RefreshToken: "r",
		ExpiresOn: time.Now().Add(time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= MaxFailedRefreshes; attempt++ {
		_, _ = Bearer()
		_, _ = Bearer() // within the cooldown: no second try
		if calls != attempt {
			t.Fatalf("after attempt %d the provider was asked %d times", attempt, calls)
		}
		tokens, _ := Load()
		tokens.LastAttempt -= RefreshCooldown.Seconds() + 1 // let the cooldown run out
		if err := Save(tokens); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Bearer(); !errors.Is(err, ErrStopped) {
		t.Errorf("after %d failures Bearer answered %v, want ErrStopped", MaxFailedRefreshes, err)
	}
	if calls != MaxFailedRefreshes {
		t.Errorf("a stopped session still asked the provider: %d calls", calls)
	}
}

// No internet is not a refused session: it waits out the cooldown and never stops the panel.
func TestAnUnreachableProviderNeverStopsTheSession(t *testing.T) {
	config.TokenFile = filepath.Join(t.TempDir(), "tokens.json")
	renew = func(*Tokens) (*Tokens, error) { return nil, errors.New("dial tcp: no route to host") }
	defer func() { renew = Refresh }()
	if err := Save(&Tokens{AccessToken: "a", RefreshToken: "r",
		ExpiresOn: time.Now().Add(time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	for range MaxFailedRefreshes + 2 {
		_, _ = Bearer()
		tokens, _ := Load()
		tokens.LastAttempt -= RefreshCooldown.Seconds() + 1
		if err := Save(tokens); err != nil {
			t.Fatal(err)
		}
	}
	if tokens, _ := Load(); tokens.Stopped() || tokens.FailedRefreshes != 0 {
		t.Errorf("network failures counted as refusals: %+v", tokens)
	}
}

func fakeJWT(claims string) string {
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".sig"
}

func TestTheUserComesFromTheToken(t *testing.T) {
	for _, c := range []struct{ claims, want string }{
		{`{"oid":"o-1","sub":"s-1","email":"A@x.com"}`, "o-1"},
		{`{"sub":"Not supported currently. Use oid claim.","email":"A@x.com"}`, "a@x.com"},
		{`{"sub":"s-1"}`, "s-1"},
	} {
		tokens := Normalize(map[string]any{"access_token": fakeJWT(c.claims)})
		if tokens.UserID != c.want {
			t.Errorf("%s: user %q, want %q", c.claims, tokens.UserID, c.want)
		}
		tokens.UserID = ""
		if got := tokens.User(); got != c.want {
			t.Errorf("%s without a stored id: %q, want %q", c.claims, got, c.want)
		}
	}
}
