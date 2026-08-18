package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
)

// The engine is driven with stub dependencies and a millisecond tick, so each case is a
// second at most and none of it touches the network. What is asserted is the decision the
// cycle takes, which is the part that can be wrong in a way nobody notices.

type recorder struct {
	mu       sync.Mutex
	causes   []string
	probes   int
	moved    bool
	halves   []string
	probeErr error
}

func (r *recorder) rebuild(cause string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.causes = append(r.causes, cause)
	return nil
}

func (r *recorder) probe() (bool, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probes++
	return r.moved, r.halves, r.probeErr
}

func (r *recorder) seen() ([]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.causes...), r.probes
}

func run(t *testing.T, payload schedule.Payload, rec *recorder, watchers int,
	wait time.Duration) {
	t.Helper()
	engine := New(Deps{
		Payload:  func() schedule.Payload { return payload },
		Probe:    rec.probe,
		Rebuild:  rec.rebuild,
		Watchers: func() int { return watchers },
		Now:      time.Now,
		// Below MinSleep on purpose: the floor is what should govern, and a test that
		// waited two real minutes would never be run.
		Tick: time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)
	time.Sleep(wait)
}

func TestProbeFindsNothingSoNoRebuild(t *testing.T) {
	rec := &recorder{moved: false}
	run(t, schedule.Payload{}, rec, 0, 7*time.Second)

	causes, probes := rec.seen()
	if probes == 0 {
		t.Fatalf("no ha sondeado nunca")
	}
	if len(causes) != 0 {
		t.Fatalf("ha reconstruido sin que nada se moviera: %v", causes)
	}
}

func TestProbeThatMovedRebuilds(t *testing.T) {
	rec := &recorder{moved: true, halves: []string{"market"}}
	run(t, schedule.Payload{}, rec, 0, 7*time.Second)

	causes, _ := rec.seen()
	if len(causes) == 0 {
		t.Fatalf("algo se movio y no ha reconstruido")
	}
	if causes[0] != "mercado" {
		t.Fatalf("un cambio solo en el mercado deberia decir 'mercado', dijo %q", causes[0])
	}
}

func TestTransferInTheLogIsNamedDifferently(t *testing.T) {
	// The distinction is not cosmetic: a transfer means somebody else acted on our squad,
	// and the panel says so.
	rec := &recorder{moved: true, halves: []string{"events", "market"}}
	run(t, schedule.Payload{}, rec, 0, 7*time.Second)

	causes, _ := rec.seen()
	if len(causes) == 0 || causes[0] != "traspaso" {
		t.Fatalf("esperaba 'traspaso', obtuve %v", causes)
	}
}

func TestDeadlineRebuildsWithoutProbing(t *testing.T) {
	// An auction closing is the instant we may have to act, so it rebuilds unconditionally
	// and does not waste two requests asking whether anything moved.
	soon := time.Now().Add(4 * time.Second).Format(time.RFC3339)
	payload := schedule.Payload{
		Market:  []schedule.Listing{{PlayerID: "7", Expires: &soon}},
		Players: []schedule.Player{{ID: "7", Name: "Sintetico"}},
	}
	rec := &recorder{}
	run(t, payload, rec, 0, 9*time.Second)

	causes, _ := rec.seen()
	if len(causes) == 0 {
		t.Fatalf("el vencimiento no ha disparado nada")
	}
	if causes[0] != "vencimiento" {
		t.Fatalf("esperaba 'vencimiento', obtuve %q", causes[0])
	}
}

func TestTheSameDeadlineDoesNotFireTwice(t *testing.T) {
	// The payload is frozen, so the expiry stays a second in the future forever: exactly
	// the shape that would become a rebuild loop without the spent-deadline floor.
	soon := time.Now().Add(4 * time.Second).Format(time.RFC3339)
	payload := schedule.Payload{
		Market:  []schedule.Listing{{PlayerID: "7", Expires: &soon}},
		Players: []schedule.Player{{ID: "7", Name: "Sintetico"}},
	}
	rec := &recorder{}
	run(t, payload, rec, 0, 12*time.Second)

	causes, _ := rec.seen()
	deadlines := 0
	for _, cause := range causes {
		if cause == "vencimiento" {
			deadlines++
		}
	}
	if deadlines > 1 {
		t.Fatalf("el mismo vencimiento ha disparado %d veces: %v", deadlines, causes)
	}
}

func TestNudgeCutsTheWaitShort(t *testing.T) {
	rec := &recorder{moved: true}
	engine := New(Deps{
		Payload:  func() schedule.Payload { return schedule.Payload{} },
		Probe:    rec.probe,
		Rebuild:  rec.rebuild,
		Watchers: func() int { return 0 },
		// A long tick with nobody watching: without a nudge this would sleep for minutes.
		Tick: 10 * time.Minute,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)

	time.Sleep(200 * time.Millisecond)
	planned, _, _, _ := engine.Next()
	if until := time.Until(planned); until < time.Minute {
		t.Fatalf("esperaba una espera larga, planeó %v", until)
	}
	engine.Nudge("se ha abierto la pagina")
	time.Sleep(300 * time.Millisecond)

	// Replanning means the wake-up was recomputed from the new now, so the instant moves.
	// Equal would mean the nudge was swallowed and the old timer is still the live one.
	replanned, _, _, _ := engine.Next()
	if replanned.Equal(planned) {
		t.Fatalf("el aviso no ha replanificado: sigue esperando a %v", planned)
	}
}

func TestAFailedProbeStillRebuilds(t *testing.T) {
	// Silence is the failure mode to avoid: a probe that errors must not leave the page
	// frozen with nobody noticing.
	rec := &recorder{probeErr: context.DeadlineExceeded}
	run(t, schedule.Payload{}, rec, 0, 7*time.Second)

	causes, _ := rec.seen()
	if len(causes) == 0 || causes[0] != "refresco" {
		t.Fatalf("esperaba un refresco de rescate, obtuve %v", causes)
	}
}
