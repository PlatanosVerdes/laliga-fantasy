package model

import "sync"

// Progress is how far the build in course has got, for a page waiting on it: the step it is
// on, how many of that step's items are done, and the whole build as a percentage.
type Progress struct {
	Step    string `json:"step"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Percent int    `json:"percent"`
}

// stages are the build's steps in the order it takes them, each weighted by its usual share of
// a cold build, so the bar moves at the pace the build does.
var stages = []struct {
	name   string
	weight int
}{
	{"Datos de futbolfantasy", 15}, {"Jugadores de LaLiga", 8}, {"Calendario", 12},
	{"Plantillas de la liga", 28}, {"Mercado", 5}, {"Estado de tus jugadores", 7},
	{"Ofertas recibidas", 6}, {"Movimientos de la liga", 9}, {"Detalle de candidatos", 6},
	{"Preparando la página", 4},
}

var progress struct {
	sync.Mutex
	current  Progress
	building bool
}

// CurrentProgress is the build in course, or false when none is running.
func CurrentProgress() (Progress, bool) {
	progress.Lock()
	defer progress.Unlock()
	return progress.current, progress.building
}

// StartProgress and EndProgress bracket a whole build, everything the caller does after the
// model included, so the bar reaches its end when the build does.
func StartProgress() { buildStarted() }
func EndProgress()   { buildEnded() }

// Reached is reached for the steps a caller runs after the model.
func Reached(stage string, done, total int) { reached(stage, done, total) }

func buildStarted() {
	progress.Lock()
	progress.current, progress.building = Progress{Step: stages[0].name}, true
	progress.Unlock()
}

func buildEnded() {
	progress.Lock()
	progress.building = false
	progress.Unlock()
}

// reached records the build at item done of total in a stage; a total of 0 is a step whose
// length is not known up front, counted as half done.
func reached(stage string, done, total int) {
	before, weight := 0, 0
	for _, entry := range stages {
		if entry.name == stage {
			weight = entry.weight
			break
		}
		before += entry.weight
	}
	share := 0.5
	if total > 0 {
		share = float64(min(done, total)) / float64(total)
	}
	progress.Lock()
	defer progress.Unlock()
	if !progress.building {
		return
	}
	percent := before + int(float64(weight)*share)
	// A bar never goes back: a cached step finishing late must not undo a later one.
	if percent < progress.current.Percent {
		percent = progress.current.Percent
	}
	progress.current = Progress{Step: stage, Done: done, Total: total, Percent: percent}
}
