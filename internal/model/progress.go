package model

import (
	"log/slog"
	"sync"
	"time"
)

// Progress is how far the build in course has got, for a page waiting on it: the step it is
// on, how many of that step's items are done, and the whole build as a percentage.
type Progress struct {
	Step    string `json:"step"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Percent int    `json:"percent"`
}

// Stage is one step of a tracked run: what the page calls it, what the log calls it, and its
// usual share of the run, so the bar moves at the pace the run does.
type Stage struct {
	Name   string
	Key    string
	Weight int
}

// Tracker follows one run at a time through its stages, for a page to show and for the log to
// say where the time went.
type Tracker struct {
	stages []Stage

	mu      sync.Mutex
	current Progress
	running bool
	started time.Time
	last    time.Time
	spent   map[string]time.Duration
}

func NewTracker(stages ...Stage) *Tracker {
	return &Tracker{stages: stages}
}

// Start begins a run on the first stage.
func (t *Tracker) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	t.current, t.running = Progress{Step: t.stages[0].Name}, true
	t.started, t.last, t.spent = now, now, map[string]time.Duration{}
}

// Current is the run in course, or false when none is running.
func (t *Tracker) Current() (Progress, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current, t.running
}

// Reached records the run at item done of total in a stage; a total of 0 is a step whose
// length is not known up front, counted as half done. The time since the previous call is
// charged to this stage, since callers report a step once its work is over.
func (t *Tracker) Reached(stage string, done, total int) {
	before, weight := 0, 0
	for _, entry := range t.stages {
		if entry.Name == stage {
			weight = entry.Weight
			break
		}
		before += entry.Weight
	}
	share := 0.5
	if total > 0 {
		share = float64(min(done, total)) / float64(total)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return
	}
	now := time.Now()
	t.spent[stage] += now.Sub(t.last)
	t.last = now
	percent := before + int(float64(weight)*share)
	// A bar never goes back: a cached step finishing late must not undo a later one.
	if percent < t.current.Percent {
		percent = t.current.Percent
	}
	t.current = Progress{Step: stage, Done: done, Total: total, Percent: percent}
}

// End closes the run and says where its time went: the milliseconds of every stage that ran,
// by its log key, and the whole run as total_ms, ready to hand to slog. What ran after the
// last report is charged to the stage it left the bar on.
func (t *Tracker) End() []any {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return nil
	}
	now := time.Now()
	t.spent[t.current.Step] += now.Sub(t.last)
	t.running = false
	attrs := make([]any, 0, 2*len(t.stages)+2)
	for _, stage := range t.stages {
		if spent, ok := t.spent[stage.Name]; ok {
			attrs = append(attrs, stage.Key+"_ms", spent.Milliseconds())
		}
	}
	return append(attrs, "total_ms", now.Sub(t.started).Milliseconds())
}

// The build's steps in the order it takes them. The first three are the preview a cold start
// reads before the build, which leaves the build's own squads to the cache. The page is drawn
// inside the first build, and on the Pi that is about a quarter of a cold start.
var build = NewTracker(
	Stage{"Tu equipo", "mine", 4}, Stage{"Mercado abierto", "listings", 2},
	Stage{"Plantillas rivales", "rivals", 8},
	Stage{"Datos de futbolfantasy", "futbolfantasy", 12}, Stage{"Jugadores de LaLiga", "players", 6},
	Stage{"Calendario", "calendar", 9}, Stage{"Plantillas de la liga", "squads", 6},
	Stage{"Mercado", "market", 4}, Stage{"Estado de tus jugadores", "status", 5},
	Stage{"Ofertas recibidas", "offers", 5}, Stage{"Movimientos de la liga", "activity", 7},
	Stage{"Detalle de candidatos", "deep", 7}, Stage{"Preparando la página", "page", 25},
)

// CurrentProgress is the build in course, or false when none is running.
func CurrentProgress() (Progress, bool) { return build.Current() }

// StartProgress and EndProgress bracket a whole build, everything the caller does after the
// model included, so the bar reaches its end when the build does. EndProgress logs one line
// with what each step took.
func StartProgress() { buildStarted() }
func EndProgress() {
	if spent := build.End(); spent != nil {
		slog.Info("build phases", spent...)
	}
}

// Reached is reached for the steps a caller runs after the model.
func Reached(stage string, done, total int) { reached(stage, done, total) }

func buildStarted()                         { build.Start() }
func buildEnded()                           { build.End() }
func reached(stage string, done, total int) { build.Reached(stage, done, total) }
