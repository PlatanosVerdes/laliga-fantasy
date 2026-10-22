package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

type Priority int

// Write is P0 and Enrich P4; an untagged request goes as Rebuild.
const (
	Untagged Priority = iota
	Write
	Interactive
	Probe
	Rebuild
	Enrich
)

const levels = 5

func (p Priority) level() int {
	if p <= Untagged || p > Enrich {
		return int(Rebuild) - 1
	}
	return int(p) - 1
}

func (p Priority) String() string { return "P" + strconv.Itoa(p.level()) }

type Limits struct {
	Rate     float64
	Burst    int
	InFlight int
	Patience time.Duration
	// WriteGap spaces the writes that still go out while LaLiga has us paused.
	WriteGap   time.Duration
	FirstPause time.Duration
	MaxPause   time.Duration
}

func DefaultLimits() Limits {
	// Close to today's unthrottled pace: the counters are there to find LaLiga's real limit first.
	return Limits{Rate: 5, Burst: 30, InFlight: 4, Patience: 15 * time.Second,
		WriteGap: 5 * time.Second, FirstPause: time.Minute, MaxPause: 30 * time.Minute}
}

func LimitsFromEnv() Limits {
	limits := DefaultLimits()
	if value, ok := envValue("FANTASY_LALIGA_RATE", parseFloat); ok && value > 0 {
		limits.Rate = value
	}
	if value, ok := envValue("FANTASY_LALIGA_BURST", strconv.Atoi); ok && value > 0 {
		limits.Burst = value
	}
	if value, ok := envValue("FANTASY_LALIGA_IN_FLIGHT", strconv.Atoi); ok && value > 0 {
		limits.InFlight = value
	}
	if value, ok := envValue("FANTASY_LALIGA_PATIENCE", time.ParseDuration); ok && value > 0 {
		limits.Patience = value
	}
	return limits
}

func parseFloat(raw string) (float64, error) { return strconv.ParseFloat(raw, 64) }

func envValue[T any](name string, parse func(string) (T, error)) (T, bool) {
	var zero T
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return zero, false
	}
	value, err := parse(raw)
	if err != nil {
		slog.Warn("ignoring an unreadable setting", "name", name, "value", raw)
		return zero, false
	}
	return value, true
}

type Clock interface {
	Now() time.Time
	AfterFunc(wait time.Duration, run func()) (stop func() bool)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(wait time.Duration, run func()) func() bool {
	return time.AfterFunc(wait, run).Stop
}

// Throttled is a request never sent: LaLiga has us paused, or it waited past its patience.
type Throttled struct {
	Host     string
	Priority Priority
	RetryIn  time.Duration
	Paused   bool
}

func (e *Throttled) Error() string {
	if !e.Paused {
		return "demasiadas peticiones a LaLiga en cola: reintenta en unos segundos"
	}
	return "LaLiga nos frena: reintenta en " + roughly(e.RetryIn)
}

func roughly(wait time.Duration) string {
	if wait < time.Minute {
		return fmt.Sprintf("%d s", int(math.Ceil(wait.Seconds())))
	}
	return fmt.Sprintf("%d min", int(math.Ceil(wait.Minutes())))
}

type ticketKey struct{}

type ticket struct {
	priority Priority
	scope    string
}

func withTicket(ctx context.Context, priority Priority, scope string) context.Context {
	return context.WithValue(ctx, ticketKey{}, ticket{priority, scope})
}

func ticketOf(ctx context.Context) ticket {
	found, _ := ctx.Value(ticketKey{}).(ticket)
	return found
}

type ScopeCounters struct {
	Sent        int           `json:"sent"`
	Waited      time.Duration `json:"-"`
	WaitedMS    int64         `json:"waited_ms"`
	RateLimited int           `json:"rate_limited"`
	Throttled   int           `json:"throttled"`
}

type waiter struct {
	level  int
	scope  string
	since  time.Time
	answer chan error
	stop   func() bool
}

type turns struct {
	order  []string
	queues map[string][]*waiter
}

func (t *turns) push(w *waiter) {
	if t.queues == nil {
		t.queues = map[string][]*waiter{}
	}
	if len(t.queues[w.scope]) == 0 {
		t.order = append(t.order, w.scope)
	}
	t.queues[w.scope] = append(t.queues[w.scope], w)
}

func (t *turns) empty() bool { return len(t.order) == 0 }

func (t *turns) size() int {
	total := 0
	for _, queue := range t.queues {
		total += len(queue)
	}
	return total
}

func (t *turns) pop() *waiter {
	scope := t.order[0]
	t.order = t.order[1:]
	queue := t.queues[scope]
	first := queue[0]
	if len(queue) == 1 {
		delete(t.queues, scope)
	} else {
		t.queues[scope] = queue[1:]
		t.order = append(t.order, scope)
	}
	return first
}

func (t *turns) remove(w *waiter) bool {
	queue := t.queues[w.scope]
	for index, queued := range queue {
		if queued != w {
			continue
		}
		queue = append(queue[:index:index], queue[index+1:]...)
		if len(queue) > 0 {
			t.queues[w.scope] = queue
			return true
		}
		delete(t.queues, w.scope)
		for at, scope := range t.order {
			if scope == w.scope {
				t.order = append(t.order[:at:at], t.order[at+1:]...)
				break
			}
		}
		return true
	}
	return false
}

func (t *turns) drain() []*waiter {
	var all []*waiter
	for !t.empty() {
		all = append(all, t.pop())
	}
	return all
}

type lane struct {
	host        string
	tokens      float64
	refilled    time.Time
	inFlight    int
	waiting     [levels]turns
	pausedUntil time.Time
	backoff     time.Duration
	lastWrite   time.Time
	wakeAt      time.Time
	stopWake    func() bool
	total       map[string]*ScopeCounters
	today       map[string]*ScopeCounters
}

func (l *lane) count(scope string, change func(*ScopeCounters)) {
	for _, book := range []map[string]*ScopeCounters{l.total, l.today} {
		if book[scope] == nil {
			book[scope] = &ScopeCounters{}
		}
		change(book[scope])
	}
}

type Queue struct {
	next   func() http.RoundTripper
	limits Limits
	clock  Clock
	jitter func(time.Duration) time.Duration
	hosts  map[string]bool

	mu    sync.Mutex
	lanes map[string]*lane
	day   string
}

// NewQueue takes a nil next as the transport SetTransport swaps, and a nil clock as the real one.
func NewQueue(next func() http.RoundTripper, limits Limits, clock Clock, hosts ...string) *Queue {
	if next == nil {
		next = wire
	}
	if clock == nil {
		clock = realClock{}
	}
	queue := &Queue{next: next, limits: limits, clock: clock, hosts: map[string]bool{},
		lanes: map[string]*lane{}, jitter: tenthAtMost}
	for _, host := range hosts {
		queue.hosts[host] = true
	}
	return queue
}

func tenthAtMost(pause time.Duration) time.Duration {
	return time.Duration(rand.Int64N(int64(pause)/10 + 1))
}

func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

var (
	queueMu sync.RWMutex
	queue   = NewQueue(nil, LimitsFromEnv(), nil, LaLigaHosts()...)
)

// LaLigaHosts leaves futbolfantasy out: it keeps its own throttle.
func LaLigaHosts() []string {
	return []string{hostOf(config.APIBase), hostOf(config.B2CTokenURL)}
}

func SetQueue(next *Queue) func() {
	queueMu.Lock()
	previous := queue
	queue = next
	queueMu.Unlock()
	return func() {
		queueMu.Lock()
		queue = previous
		queueMu.Unlock()
	}
}

func current() *Queue {
	queueMu.RLock()
	defer queueMu.RUnlock()
	return queue
}

func (q *Queue) lane(host string) *lane {
	found := q.lanes[host]
	if found == nil {
		found = &lane{host: host, tokens: float64(q.limits.Burst), refilled: q.clock.Now(),
			total: map[string]*ScopeCounters{}, today: map[string]*ScopeCounters{}}
		q.lanes[host] = found
	}
	return found
}

func (q *Queue) RoundTrip(request *http.Request) (*http.Response, error) {
	host := request.URL.Hostname()
	if !q.hosts[host] {
		return q.next().RoundTrip(request)
	}
	ticket := ticketOf(request.Context())
	if err := q.acquire(request.Context(), host, ticket); err != nil {
		return nil, err
	}
	response, err := q.next().RoundTrip(request)
	q.release(host, ticket, response, true)
	return response, err
}

func (q *Queue) acquire(ctx context.Context, host string, ticket ticket) error {
	q.mu.Lock()
	lane := q.lane(host)
	now := q.clock.Now()
	level := ticket.priority.level()
	if level > 0 && now.Before(lane.pausedUntil) {
		lane.count(ticket.scope, func(c *ScopeCounters) { c.Throttled++ })
		q.mu.Unlock()
		return &Throttled{Host: host, Priority: ticket.priority,
			RetryIn: lane.pausedUntil.Sub(now), Paused: true}
	}
	w := &waiter{level: level, scope: ticket.scope, since: now, answer: make(chan error, 1)}
	lane.waiting[level].push(w)
	if level == Interactive.level() && q.limits.Patience > 0 {
		w.stop = q.clock.AfterFunc(q.limits.Patience, func() { q.giveUp(lane, w, ticket) })
	}
	q.pump(lane)
	q.mu.Unlock()

	select {
	case err := <-w.answer:
		return err
	case <-ctx.Done():
		q.mu.Lock()
		removed := lane.waiting[level].remove(w)
		q.mu.Unlock()
		if !removed {
			if err := <-w.answer; err == nil {
				q.release(host, ticket, nil, false)
			}
		} else if w.stop != nil {
			w.stop()
		}
		return ctx.Err()
	}
}

func (q *Queue) giveUp(lane *lane, w *waiter, ticket ticket) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !lane.waiting[w.level].remove(w) {
		return
	}
	lane.count(w.scope, func(c *ScopeCounters) { c.Throttled++ })
	w.answer <- &Throttled{Host: lane.host, Priority: ticket.priority}
}

func (q *Queue) release(host string, ticket ticket, response *http.Response, sent bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	lane := q.lanes[host]
	lane.inFlight--
	now := q.clock.Now()
	if sent {
		lane.count(ticket.scope, func(c *ScopeCounters) { c.Sent++ })
	}
	if response != nil {
		switch {
		case response.StatusCode == http.StatusTooManyRequests:
			q.trip(lane, ticket, response.Header.Get("Retry-After"), now)
		case response.StatusCode < 400 && !now.Before(lane.pausedUntil):
			lane.backoff = 0
		}
	}
	q.summarize(now)
	q.pump(lane)
}

// A 429 for a read sent before the pause began says nothing new: it does not double the backoff.
func (q *Queue) trip(lane *lane, ticket ticket, retryAfter string, now time.Time) {
	lane.count(ticket.scope, func(c *ScopeCounters) { c.RateLimited++ })
	level := ticket.priority.level()
	if now.Before(lane.pausedUntil) && level > 0 {
		return
	}
	if lane.backoff == 0 {
		lane.backoff = q.limits.FirstPause
	} else {
		lane.backoff = min(2*lane.backoff, q.limits.MaxPause)
	}
	pause := max(lane.backoff, waitOf(retryAfter, now))
	pause += q.jitter(pause)
	if until := now.Add(pause); until.After(lane.pausedUntil) {
		lane.pausedUntil = until
	}
	slog.Warn("LaLiga answered 429: only writes go out until the pause ends",
		"host", lane.host, "pause", lane.pausedUntil.Sub(now).Round(time.Second).String(),
		"retry_after", retryAfter, "priority", ticket.priority.String())
	for queued := 1; queued < levels; queued++ {
		for _, w := range lane.waiting[queued].drain() {
			if w.stop != nil {
				w.stop()
			}
			lane.count(w.scope, func(c *ScopeCounters) { c.Throttled++ })
			w.answer <- &Throttled{Host: lane.host, Priority: Priority(queued + 1),
				RetryIn: lane.pausedUntil.Sub(now), Paused: true}
		}
	}
}

func waitOf(retryAfter string, now time.Time) time.Duration {
	retryAfter = strings.TrimSpace(retryAfter)
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(retryAfter); err == nil {
		return max(when.Sub(now), 0)
	}
	return 0
}

func (q *Queue) refill(lane *lane, now time.Time) {
	elapsed := now.Sub(lane.refilled).Seconds()
	if elapsed > 0 {
		lane.tokens = min(float64(q.limits.Burst), lane.tokens+elapsed*q.limits.Rate)
	}
	lane.refilled = now
}

// Writes ignore the in-flight cap, so a raid never waits for two slow reads to come back.
func (q *Queue) turn(lane *lane, now time.Time) (int, time.Time) {
	paused := now.Before(lane.pausedUntil)
	if !lane.waiting[0].empty() {
		if paused && !lane.lastWrite.IsZero() && now.Before(lane.lastWrite.Add(q.limits.WriteGap)) {
			return -1, lane.lastWrite.Add(q.limits.WriteGap)
		}
		return 0, time.Time{}
	}
	if paused || lane.inFlight >= q.limits.InFlight {
		return -1, time.Time{}
	}
	for level := 1; level < levels; level++ {
		if !lane.waiting[level].empty() {
			return level, time.Time{}
		}
	}
	return -1, time.Time{}
}

func (q *Queue) pump(lane *lane) {
	for {
		now := q.clock.Now()
		q.refill(lane, now)
		level, later := q.turn(lane, now)
		if level < 0 {
			if !later.IsZero() {
				q.wake(lane, later, now)
			}
			return
		}
		if lane.tokens < 1 {
			missing := (1 - lane.tokens) / q.limits.Rate * float64(time.Second)
			q.wake(lane, now.Add(time.Duration(math.Ceil(missing))), now)
			return
		}
		w := lane.waiting[level].pop()
		lane.tokens--
		lane.inFlight++
		if level == 0 {
			lane.lastWrite = now
		}
		if w.stop != nil {
			w.stop()
		}
		lane.count(w.scope, func(c *ScopeCounters) { c.Waited += now.Sub(w.since) })
		w.answer <- nil
	}
}

func (q *Queue) wake(lane *lane, at, now time.Time) {
	if !lane.wakeAt.IsZero() && !at.Before(lane.wakeAt) {
		return
	}
	if lane.stopWake != nil {
		lane.stopWake()
	}
	lane.wakeAt = at
	lane.stopWake = q.clock.AfterFunc(at.Sub(now), func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		lane.wakeAt, lane.stopWake = time.Time{}, nil
		q.pump(lane)
	})
}

// One line a day, so a week of them says whether LaLiga counts per IP or per token.
func (q *Queue) summarize(now time.Time) {
	day := now.Format("2006-01-02")
	if q.day == day {
		return
	}
	if q.day != "" {
		var parts []string
		for _, host := range sortedKeys(q.lanes) {
			lane := q.lanes[host]
			for _, scope := range sortedKeys(lane.today) {
				counted := lane.today[scope]
				parts = append(parts, fmt.Sprintf("%s/%s sent=%d waited=%s 429=%d throttled=%d",
					host, scopeName(scope), counted.Sent, counted.Waited.Round(time.Second),
					counted.RateLimited, counted.Throttled))
			}
			lane.today = map[string]*ScopeCounters{}
		}
		slog.Info("laliga requests", "day", q.day, "summary", strings.Join(parts, "; "))
	}
	q.day = day
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func scopeName(scope string) string {
	if scope == "" {
		return "instance"
	}
	return scope
}

type HostReport struct {
	Host         string                   `json:"host"`
	PausedUntil  *time.Time               `json:"paused_until,omitempty"`
	PauseSeconds int                      `json:"pause_seconds"`
	BackoffSecs  int                      `json:"backoff_seconds"`
	InFlight     int                      `json:"in_flight"`
	Queued       map[string]int           `json:"queued"`
	Scopes       map[string]ScopeCounters `json:"scopes"`
}

type QueueReport struct {
	Rate     float64      `json:"rate"`
	Burst    int          `json:"burst"`
	InFlight int          `json:"in_flight"`
	Hosts    []HostReport `json:"hosts"`
}

func (q *Queue) Report() QueueReport {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.clock.Now()
	report := QueueReport{Rate: q.limits.Rate, Burst: q.limits.Burst, InFlight: q.limits.InFlight,
		Hosts: []HostReport{}}
	for _, host := range sortedKeys(q.lanes) {
		lane := q.lanes[host]
		line := HostReport{Host: host, BackoffSecs: int(lane.backoff.Seconds()),
			InFlight: lane.inFlight, Queued: map[string]int{}, Scopes: map[string]ScopeCounters{}}
		if now.Before(lane.pausedUntil) {
			until := lane.pausedUntil
			line.PausedUntil = &until
			line.PauseSeconds = int(math.Ceil(until.Sub(now).Seconds()))
		}
		for level := range levels {
			if size := lane.waiting[level].size(); size > 0 {
				line.Queued["P"+strconv.Itoa(level)] = size
			}
		}
		for scope, counted := range lane.total {
			copied := *counted
			copied.WaitedMS = counted.Waited.Milliseconds()
			line.Scopes[scopeName(scope)] = copied
		}
		report.Hosts = append(report.Hosts, line)
	}
	return report
}

func QueueStats() QueueReport { return current().Report() }
