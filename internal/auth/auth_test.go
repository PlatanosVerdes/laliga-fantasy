package auth

import (
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
