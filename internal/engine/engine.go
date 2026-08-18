// Package engine runs the refresh cycle. This is the part the port was for.
//
// In Python the same job is one loop with one sleep, so three independent timers — a
// scheduled clause opening, an auction closing, a match under way — have to be folded into
// a single next-wake and then untangled again on the way out. Here each source of work is
// a goroutine that sends on a channel, and the engine is a `select` over them. Cutting a
// wait short stops being a special case (a nudge is just another channel), and the
// serialisation that matters — never two rebuilds at once — is the one goroutine reading
// the channel rather than a lock somebody has to remember to take.
package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
)

// Work is one thing to do, and why. The reason travels with it because it is logged and
// shown to the user, and because a rebuild caused by a transfer is not the same event as
// one caused by a match.
type Work struct {
	Kind string // schedule.Probe | schedule.Rebuild | schedule.KindDeadline
	Why  string
}

// Deps is everything the engine needs from the outside, injected so the cycle can be
// exercised without a network or a real clock.
type Deps struct {
	// Payload returns the world as it currently stands, for the scheduler to read.
	Payload func() schedule.Payload
	// Probe answers whether anything moved, and which half did.
	Probe func() (moved bool, halves []string, err error)
	// Rebuild rebuilds the world and reports what changed.
	Rebuild func(cause string) error
	// Watchers is how many browsers are connected: with one, the cadence stays tight.
	Watchers func() int
	// LastFull is when the last full rebuild finished.
	LastFull func() time.Time
	// Now exists so a test can control the clock.
	Now func() time.Time
	// Tick is the base cadence.
	Tick time.Duration
	// Invalidate drops caches by tag before a rebuild that needs fresher data.
	Invalidate func(tags ...string)
}

type Engine struct {
	deps Deps

	nudge chan string
	mu    sync.Mutex
	// A deadline once woken for is remembered as spent: an expiry the API keeps
	// reporting as imminent must not become a loop of rebuilds.
	deadlineFloor time.Time
	next          time.Time
	nextWhy       string
	probes        int
	rebuilds      int
}

func New(deps Deps) *Engine {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Tick == 0 {
		deps.Tick = 2 * time.Minute
	}
	if deps.Watchers == nil {
		deps.Watchers = func() int { return 0 }
	}
	if deps.LastFull == nil {
		deps.LastFull = func() time.Time { return time.Time{} }
	}
	if deps.Invalidate == nil {
		deps.Invalidate = func(...string) {}
	}
	return &Engine{deps: deps, nudge: make(chan string, 8)}
}

// Nudge cuts the current wait short. Opening the page has to tighten the cadence now, not
// at the end of a wait that was planned for an empty room.
func (e *Engine) Nudge(why string) {
	select {
	case e.nudge <- why:
	default: // already pending; one is enough
	}
}

// Next reports the planned wake-up, for /healthz.
func (e *Engine) Next() (time.Time, string, int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.next, e.nextWhy, e.probes, e.rebuilds
}

// Run drives the cycle until the context is cancelled.
func (e *Engine) Run(ctx context.Context) {
	for {
		payload := e.deps.Payload()
		now := e.deps.Now()
		decision := schedule.NextWake(payload, now, e.deps.Tick, e.deps.LastFull(),
			e.deps.Watchers() > 0, e.deadlineFloor)

		e.mu.Lock()
		e.next, e.nextWhy = decision.At, decision.Why
		e.mu.Unlock()

		delay := decision.At.Sub(now)
		if delay < schedule.MinSleep {
			delay = schedule.MinSleep
		}
		slog.Debug("sleeping", "seconds", delay.Seconds(), "why", decision.Why,
			"kind", decision.Kind)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case why := <-e.nudge:
			// Replan instead of finishing a wait calculated for a different world.
			timer.Stop()
			slog.Debug("wait cut short", "why", why)
			continue
		case <-timer.C:
		}

		e.doWork(decision)
	}
}

func (e *Engine) doWork(decision schedule.Decision) {
	payload := e.deps.Payload()
	now := e.deps.Now()

	live := schedule.LiveMatches(payload, now, false)
	if len(live) > 0 || startsWithAny(decision.Why, "termina", "cierra la jornada") {
		// Points come from the player master, cached for six hours because it barely
		// changes — except while a match is on, when it is the only thing that changes.
		e.deps.Invalidate("players", "lineup", "week")
	}

	if decision.Kind == schedule.KindDeadline {
		e.deadlineFloor = decision.At.Add(schedule.Lead + time.Second)
		cause := "vencimiento"
		if len(live) > 0 {
			cause = "partido"
		}
		e.rebuild(cause, decision.Why)
		return
	}

	moved, halves, err := e.deps.Probe()
	e.mu.Lock()
	e.probes++
	e.mu.Unlock()
	if err != nil {
		// A failed probe must not silence the cycle: fall back to rebuilding, which has
		// its own error handling and keeps the last good page up.
		slog.Warn("probe failed", "reason", err.Error())
		e.rebuild("refresco", "el sondeo ha fallado")
		return
	}

	if !moved && len(live) == 0 && decision.Kind != schedule.Rebuild {
		slog.Debug("nothing moved")
		return
	}

	cause := "mercado"
	switch {
	case len(live) > 0:
		cause = "partido"
	case contains(halves, "events"):
		// A transfer in the log is somebody else acting on our squad: a sale we did not
		// make lands here, not in a write path.
		cause = "traspaso"
	}
	e.rebuild(cause, decision.Why)
}

func (e *Engine) rebuild(cause, why string) {
	slog.Info("rebuilding", "cause", cause, "why", why)
	if err := e.deps.Rebuild(cause); err != nil {
		slog.Error("rebuild failed", "cause", cause, "reason", err.Error())
		return
	}
	e.mu.Lock()
	e.rebuilds++
	e.mu.Unlock()
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func startsWithAny(text string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if len(text) >= len(prefix) && text[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
