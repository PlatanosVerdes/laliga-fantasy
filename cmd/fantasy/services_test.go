package main

import (
	"testing"
	"time"
)

// The defaults are exactly what the server ran on before the clocks could be tuned.
func TestServiceDefaultsAreTodaysValues(t *testing.T) {
	want := map[string]time.Duration{"poll": 2 * time.Minute, "live": 2 * time.Minute,
		"rebuild": 15 * time.Minute, "heartbeat": 20 * time.Second}
	jobs := serviceJobs(defaultPoll, false)
	if len(jobs) != len(want) {
		t.Fatalf("%d relojes, esperaba %d", len(jobs), len(want))
	}
	for _, job := range jobs {
		if job.Default != want[job.Key] {
			t.Errorf("%s: %v, esperaba %v", job.Key, job.Default, want[job.Key])
		}
		if job.Default < job.Min || job.Default > job.Max {
			t.Errorf("%s: el defecto se sale de sus limites", job.Key)
		}
		if job.Label == "" || job.Description == "" {
			t.Errorf("%s sin texto para el panel", job.Key)
		}
	}
	for _, job := range jobs {
		if job.EnvOnly != (job.Key == "heartbeat") {
			t.Errorf("%s: solo el latido va por variable de entorno", job.Key)
		}
		if !job.EnvOnly && (job.Min%time.Minute != 0 || job.Max%time.Minute != 0) {
			t.Errorf("%s: el panel edita minutos enteros, sus limites tambien", job.Key)
		}
	}
	// The deployed command line passes --interval 3600: it has to fit.
	if poll := serviceJobs(time.Hour, true)[0]; time.Hour > poll.Max {
		t.Errorf("--interval 3600 no cabe en %v", poll.Max)
	}
}
