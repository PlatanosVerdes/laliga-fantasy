package state

import (
	"testing"
	"time"
)

func TestHealthKeepsServingWhileTheDocumentIsRecent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		age       time.Duration
		lastError string
		want      string
	}{
		{"fresh and no error", time.Minute, "", "ok"},
		{"upstream blip over a recent document", time.Minute, "http 502", "stale"},
		{"upstream down for longer than the grace", staleGrace + time.Minute, "http 502", "degraded"},
		{"refresh loop wedged without ever erroring", staleGrace + time.Minute, "", "degraded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &State{generatedAt: time.Now().Add(-tc.age), lastError: tc.lastError}
			if got := s.Health().Status; got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHealthIsDegradedBeforeTheFirstBuild(t *testing.T) {
	if got := (&State{}).Health().Status; got != "degraded" {
		t.Fatalf("status = %q, want %q", got, "degraded")
	}
}
