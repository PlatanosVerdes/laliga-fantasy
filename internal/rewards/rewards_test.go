package rewards

import (
	"testing"
	"time"
)

// The zone database has to travel inside the binary. It shipped once without it, on an alpine
// image with no /usr/share/zoneinfo, and the day silently became UTC's: the reward was claimed
// at 02:08 Madrid instead of just after midnight, which is when the game's counter resets.
func TestMadridIsLoadable(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Madrid"); err != nil {
		t.Fatalf("Europe/Madrid unavailable, tzdata is not embedded: %v", err)
	}
}

func TestTodayIsMadridsDay(t *testing.T) {
	t.Setenv("TZ", "UTC")
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, want := Today(), time.Now().In(madrid).Format("2006-01-02"); got != want {
		t.Fatalf("Today() = %q, want %q", got, want)
	}
}
