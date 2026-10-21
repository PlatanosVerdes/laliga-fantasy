package advice

import (
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

// Filler lets the page be drawn without waiting for the per-player pages: what is in hand is
// applied, the rest is marked as waiting and fetched behind the page, one at a time and in the
// order the page shows them, and every batch that lands is reported so the page can be drawn
// again with it. An expired batch is about a hundred pages behind a 400 ms throttle, forty
// seconds the panel used to spend blank.
type Filler struct {
	// Fetch reads one player's page, from the network when it has to.
	Fetch func(ffID string) (Row, error)
	// Known is a page already in hand, without asking anybody. Optional.
	Known func(ffID string) (Row, bool)
	// Landed is told about every batch, from the filler's own goroutine.
	Landed func(Batch)
	// TTL is how long a page fetched here is trusted, Gap the least time between two batches,
	// and Retry how long a page that could not be read is left alone.
	TTL   time.Duration
	Gap   time.Duration
	Retry time.Duration

	mu       sync.Mutex
	queue    []string
	inFlight string
	running  bool
	done     int
	players  map[string][]string
	got      map[string]landed
	failed   map[string]time.Time
}

type landed struct {
	detail Row
	at     time.Time
}

// Batch is what landed since the last one: the players whose page arrived or could not be
// read, and how far the pass has got.
type Batch struct {
	Players []string
	Done    int
	Total   int
	// Over is the last batch of a pass: nothing is waiting any more.
	Over bool
}

const (
	DefaultGap   = 2 * time.Second
	DefaultRetry = 5 * time.Minute
)

// Enrich applies every page in hand to the shortlists, marks the rows still waiting for one with
// PendingKey and queues those players, in the order of DetailBuckets. A page that could not be
// read lately is neither applied nor waited for, the way a failed fetch always left its row. It
// returns how many players are waiting.
func (f *Filler) Enrich(buckets Row, limit int) int {
	order, byPlayer := detailRows(buckets, limit)
	now := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.players == nil {
		f.players, f.got, f.failed = map[string][]string{}, map[string]landed{}, map[string]time.Time{}
	}
	var waiting []string
	for _, ffID := range order {
		rows := byPlayer[ffID]
		if detail, ok := f.known(ffID, now); ok {
			for _, row := range rows {
				ApplyDetail(row, detail)
			}
			continue
		}
		if when, failed := f.failed[ffID]; failed && now.Sub(when) < f.retry() {
			continue
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			row[PendingKey] = true
			if id := text(row["id"]); id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		f.players[ffID] = ids
		if ffID != f.inFlight {
			waiting = append(waiting, ffID)
		}
	}
	f.queue = waiting
	if len(waiting) > 0 && !f.running {
		f.running, f.done = true, 0
		go f.run()
	}
	return len(waiting)
}

func (f *Filler) known(ffID string, now time.Time) (Row, bool) {
	if kept, ok := f.got[ffID]; ok && now.Sub(kept.at) <= f.TTL {
		return kept.detail, true
	}
	if f.Known != nil {
		return f.Known(ffID)
	}
	return nil, false
}

func (f *Filler) retry() time.Duration {
	if f.Retry > 0 {
		return f.Retry
	}
	return DefaultRetry
}

func (f *Filler) gap() time.Duration {
	if f.Gap > 0 {
		return f.Gap
	}
	return DefaultGap
}

// run reads the queue until it is empty. The queue can be replaced while it runs: the page
// drawn last knows best what is still missing and in which order.
func (f *Filler) run() {
	started := time.Now()
	var sent time.Time
	var first, last time.Duration
	read, failed, limited := 0, 0, false
	var batch []string
	for {
		f.mu.Lock()
		if len(f.queue) == 0 {
			rest, landed := Batch{Players: batch, Done: f.done, Total: f.done, Over: true}, f.Landed
			f.running = false
			f.mu.Unlock()
			// The page replaced the queue with less than was left: its last players still count.
			if landed != nil && len(batch) > 0 {
				landed(rest)
			}
			break
		}
		ffID := f.queue[0]
		f.queue = f.queue[1:]
		f.inFlight = ffID
		f.mu.Unlock()

		detail, err := f.Fetch(ffID)
		now := time.Now()

		f.mu.Lock()
		f.inFlight = ""
		f.done++
		batch = append(batch, f.players[ffID]...)
		if err == nil {
			f.got[ffID] = landed{detail: detail, at: now}
			read++
			if first == 0 {
				first = now.Sub(started)
			}
			last = now.Sub(started)
		} else {
			f.failed[ffID] = now
			failed++
			var tooMany *httpx.RateLimited
			if errors.As(err, &tooMany) {
				// Being rate limited is an answer, not a glitch: what is left waits for the
				// next pass rather than making it worse.
				slog.Warn("futbolfantasy rate limited: detail fill stopped",
					"read", read, "pending", len(f.queue))
				limited = true
				for _, rest := range f.queue {
					f.failed[rest] = now
					batch = append(batch, f.players[rest]...)
				}
				failed += len(f.queue)
				f.done += len(f.queue)
				f.queue = nil
			}
		}
		over := len(f.queue) == 0
		progress := Batch{Players: batch, Done: f.done, Total: f.done + len(f.queue), Over: over}
		f.mu.Unlock()

		if f.Landed != nil && (over || sent.IsZero() || now.Sub(sent) >= f.gap()) {
			f.Landed(progress)
			batch, sent = nil, now
		}
	}
	slog.Info("details filled", "players", read+failed, "read", read, "failed", failed,
		"limited", limited, "ms", time.Since(started).Milliseconds(),
		"first_ms", first.Milliseconds(), "last_ms", last.Milliseconds())
}
