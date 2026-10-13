package auth

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// A failing renewal waits out the cooldown, and after the third one the panel stops asking.
func TestFailedRefreshesStopAfterThree(t *testing.T) {
	config.TokenFile = filepath.Join(t.TempDir(), "tokens.json")
	calls := 0
	renew = func(*Tokens) (*Tokens, error) { calls++; return nil, errors.New("invalid_grant") }
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
