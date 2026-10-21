package advice

import (
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

// Filler lets the page be drawn without waiting for the per-player pages. Every ceiling stored
// is applied, the old ones marked with their age; the players never read are marked as waiting.
// Both are read again behind the page, one at a time and in the order the page shows them, and
// every batch that lands is reported so the page can be drawn again with it.
type Filler struct {
	// Stored is the last ceiling read for a player, fresh or not.
	Stored func(ffID string) (futbolfantasy.Detail, bool)
	// Fetch reads one player's page now, and stores it.
	Fetch func(ffID string) (futbolfantasy.Detail, error)
	// Landed is told about every batch, from the filler's own goroutine.
	Landed func(Batch)
	// Gap is the least time between two batches, and Retry how long a page that could not be
	// read is left alone.
	Gap   time.Duration
	Retry time.Duration

	mu       sync.Mutex
	queue    []string
	inFlight string
	running  bool
	done     int
	players  map[string][]string
	failed   map[string]time.Time
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

// Enrich applies every stored ceiling to the shortlists, marks the rows of a stale one with
// AgeKey and those of a player never read with PendingKey, and queues both, in the order of
// DetailBuckets. A page that could not be read lately is not queued again: a stale ceiling stays,
// with its age, and a missing one is drawn the way a failed fetch always left its row. It returns
// how many players are queued.
func (f *Filler) Enrich(buckets Row, limit int) int {
	order, byPlayer := detailRows(buckets, limit)
	now := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.players == nil {
		f.players, f.failed = map[string][]string{}, map[string]time.Time{}
	}
	var queued []string
	for _, ffID := range order {
		rows := byPlayer[ffID]
		stored, known := f.Stored(ffID)
		if known {
			fresh := stored.Fresh(now)
			for _, row := range rows {
				ApplyCeiling(row, stored.IdealBid)
				if !fresh {
					row[AgeKey] = stored.Age(now)
				}
			}
			if fresh {
				continue
			}
		}
		if when, failed := f.failed[ffID]; failed && now.Sub(when) < f.retry() {
			continue
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			if !known {
				row[PendingKey] = true
			}
			if id := text(row["id"]); id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		f.players[ffID] = ids
		if ffID != f.inFlight {
			queued = append(queued, ffID)
		}
	}
	f.queue = queued
	if len(queued) > 0 && !f.running {
		f.running, f.done = true, 0
		go f.run()
	}
	return len(queued)
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

		_, err := f.Fetch(ffID)
		now := time.Now()

		f.mu.Lock()
		f.inFlight = ""
		f.done++
		batch = append(batch, f.players[ffID]...)
		if err == nil {
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
