package main

import (
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/engine"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/server"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/services"
)

// Registry keys of the job clocks.
const (
	servicePoll    = "poll"
	serviceLive    = "live"
	serviceRebuild = "rebuild"
)

// defaultPoll is --interval's default.
const defaultPoll = 2 * time.Minute

// serviceJobs is every clock the page shows. poll is --interval: given explicitly, the flag
// replaces the default, and FANTASY_POLL_INTERVAL and the page still win over it, because the
// Dockerfile bakes the flag into CMD and the variable is the only knob a compose file has.
func serviceJobs(poll time.Duration, pollFromFlag bool) []services.Job {
	return []services.Job{
		{Key: servicePoll, Label: "Sondeo de cambios",
			Description: "Cada cuánto pregunta a LaLiga si se ha movido la liga o el mercado " +
				"(dos peticiones). Si nadie tiene la página abierta y no vence nada en 10 min, " +
				"espera el cuádruple.",
			Default: poll, FromFlag: pollFromFlag, Min: time.Minute, Max: 6 * time.Hour},
		{Key: serviceLive, Label: "Partido en juego",
			Description: "Mientras juega alguno de tus jugadores, cada cuánto se reconstruye " +
				"para traer los puntos. Solo acorta el sondeo, nunca lo alarga.",
			Default: schedule.LiveTick, Min: time.Minute, Max: 30 * time.Minute},
		// Capped under state's 30 min staleGrace: past it /healthz answers 503 on a quiet day.
		{Key: serviceRebuild, Label: "Reconstrucción completa",
			Description: "Cada cuánto se recalcula todo aunque nada lo anuncie: valores, " +
				"puntos y futbolfantasy cambian sin avisar.",
			Default: schedule.Ceiling, Min: 5 * time.Minute, Max: 25 * time.Minute},
		{Key: server.ServiceHeartbeat, Label: "Latido en vivo",
			Description: "Cada cuánto se manda un latido por la conexión en vivo, para que " +
				"ningún proxy la corte por inactiva.",
			Default: server.Heartbeat, Min: 5 * time.Second, Max: time.Minute, EnvOnly: true},
	}
}

func cadenceOf(registry *services.Registry) func() schedule.Cadence {
	return func() schedule.Cadence {
		return schedule.Cadence{Tick: registry.Get(servicePoll),
			Live: registry.Get(serviceLive), Ceiling: registry.Get(serviceRebuild)}
	}
}

// trackServices hangs the engine's timings on the registry and makes it replan on a change.
func trackServices(registry *services.Registry, cycle *engine.Engine, lastFull func() time.Time) {
	registry.Track(servicePoll, func() (time.Time, time.Time) {
		clocks := cycle.Clocks()
		if clocks.NextKind == schedule.Probe && !clocks.NextLive {
			return clocks.LastProbe, clocks.Next
		}
		return clocks.LastProbe, time.Time{}
	})
	registry.Track(serviceLive, func() (time.Time, time.Time) {
		clocks := cycle.Clocks()
		if clocks.NextKind == schedule.Probe && clocks.NextLive {
			return clocks.LastLive, clocks.Next
		}
		return clocks.LastLive, time.Time{}
	})
	registry.Track(serviceRebuild, func() (time.Time, time.Time) {
		last := lastFull()
		if last.IsZero() {
			return last, time.Time{}
		}
		return last, last.Add(registry.Get(serviceRebuild))
	})
	registry.OnChange(func(key string) {
		if key != server.ServiceHeartbeat {
			cycle.Nudge("ha cambiado un intervalo")
		}
	})
}
