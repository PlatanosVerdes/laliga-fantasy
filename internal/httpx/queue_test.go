package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx/httpxtest"
)

type fakeTimer struct {
	at   time.Time
	run  func()
	done bool
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(wait time.Duration, run func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &fakeTimer{at: c.now.Add(wait), run: run}
	c.timers = append(c.timers, timer)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		pending := !timer.done
		timer.done = true
		return pending
	}
}

func (c *fakeClock) Advance(wait time.Duration) {
	c.mu.Lock()
	target := c.now.Add(wait)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var due *fakeTimer
		for _, timer := range c.timers {
			if !timer.done && !timer.at.After(target) && (due == nil || timer.at.Before(due.at)) {
				due = timer
			}
		}
		if due == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		due.done = true
		if due.at.After(c.now) {
			c.now = due.at
		}
		c.mu.Unlock()
		due.run()
	}
}

type backend struct {
	mu     sync.Mutex
	calls  []ticket
	status func(call int) (int, string)
}

func (b *backend) RoundTrip(request *http.Request) (*http.Response, error) {
	b.mu.Lock()
	b.calls = append(b.calls, ticketOf(request.Context()))
	call := len(b.calls)
	b.mu.Unlock()
	status, retryAfter := http.StatusOK, ""
	if b.status != nil {
		status, retryAfter = b.status(call)
	}
	header := http.Header{}
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return &http.Response{StatusCode: status, Header: header,
		Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func (b *backend) sent() []ticket {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ticket(nil), b.calls...)
}

const testHost = "laliga.test"

func testQueue(limits Limits) (*Queue, *backend, *fakeClock) {
	server, clock := &backend{}, newClock()
	queue := NewQueue(func() http.RoundTripper { return server }, limits, clock, testHost)
	queue.jitter = func(time.Duration) time.Duration { return 0 }
	return queue, server, clock
}

func limits(rate float64, burst, inFlight int) Limits {
	chosen := DefaultLimits()
	chosen.Rate, chosen.Burst, chosen.InFlight = rate, burst, inFlight
	return chosen
}

type outcome struct {
	status int
	err    error
}

func send(queue *Queue, priority Priority, scope string) outcome {
	request, _ := http.NewRequestWithContext(withTicket(context.Background(), priority, scope),
		http.MethodGet, "https://"+testHost+"/x", nil)
	response, err := queue.RoundTrip(request)
	if err != nil {
		return outcome{err: err}
	}
	_ = response.Body.Close()
	return outcome{status: response.StatusCode}
}

func sendLater(queue *Queue, priority Priority, scope string) <-chan outcome {
	answer := make(chan outcome, 1)
	go func() { answer <- send(queue, priority, scope) }()
	return answer
}

func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("never happened: %s", what)
}

func queued(queue *Queue, priority Priority) int {
	for _, host := range queue.Report().Hosts {
		return host.Queued[priority.String()]
	}
	return 0
}

func scopeOf(queue *Queue, scope string) ScopeCounters {
	for _, host := range queue.Report().Hosts {
		return host.Scopes[scopeName(scope)]
	}
	return ScopeCounters{}
}

func TestAWriteJumpsAQueueOfAHundredRebuildReads(t *testing.T) {
	queue, server, clock := testQueue(limits(1, 1, 100))
	var reads []<-chan outcome
	for range 100 {
		reads = append(reads, sendLater(queue, Rebuild, ""))
	}
	eventually(t, "99 reads queued", func() bool { return queued(queue, Rebuild) == 99 })
	write := sendLater(queue, Write, "")
	eventually(t, "the write queued", func() bool { return queued(queue, Write) == 1 })

	clock.Advance(time.Second)
	if got := <-write; got.err != nil {
		t.Fatal(got.err)
	}
	if calls := server.sent(); len(calls) != 2 || calls[1].priority != Write {
		t.Fatalf("the write was not next: %+v", calls)
	}
	clock.Advance(99 * time.Second)
	for _, read := range reads {
		if got := <-read; got.err != nil {
			t.Fatal(got.err)
		}
	}
}

func TestScopesAtTheSamePriorityTakeTurns(t *testing.T) {
	queue, server, clock := testQueue(limits(1, 1, 100))
	if got := send(queue, Rebuild, "a"); got.err != nil {
		t.Fatal(got.err)
	}
	var waiting []<-chan outcome
	for index, scope := range []string{"a", "a", "a", "b", "b", "b"} {
		waiting = append(waiting, sendLater(queue, Rebuild, scope))
		eventually(t, "queued in order", func() bool { return queued(queue, Rebuild) == index+1 })
	}
	for index := range waiting {
		clock.Advance(time.Second)
		eventually(t, "the next turn", func() bool { return len(server.sent()) == index+2 })
	}
	for _, answer := range waiting {
		<-answer
	}
	var order []string
	for _, call := range server.sent()[1:] {
		order = append(order, call.scope)
	}
	if strings.Join(order, "") != "ababab" {
		t.Errorf("order %v", order)
	}
}

func TestTheBucketLetsTheBurstThroughThenTheRate(t *testing.T) {
	queue, server, clock := testQueue(limits(2, 10, 100))
	var answers []<-chan outcome
	for range 30 {
		answers = append(answers, sendLater(queue, Rebuild, ""))
	}
	steady := func(want int) {
		t.Helper()
		eventually(t, "requests sent", func() bool { return len(server.sent()) >= want })
		time.Sleep(5 * time.Millisecond)
		if got := len(server.sent()); got != want {
			t.Fatalf("sent %d, want %d", got, want)
		}
	}
	steady(10)
	clock.Advance(time.Second)
	steady(12)
	clock.Advance(5 * time.Second)
	steady(22)
	clock.Advance(10 * time.Second)
	steady(30)
	for _, answer := range answers {
		<-answer
	}
	counted := scopeOf(queue, "")
	if counted.Sent != 30 || counted.Throttled != 0 {
		t.Errorf("counters %+v", counted)
	}
	// Two a second after the ten: the eleventh waited half a second, the thirtieth ten.
	if want := 105 * time.Second; counted.Waited != want {
		t.Errorf("waited %s, want %s", counted.Waited, want)
	}
}

func TestOnlyTwoReadsAreInFlightButAWriteIsNeverHeldByThem(t *testing.T) {
	release := make(chan struct{})
	held := &holding{release: release}
	queue := NewQueue(func() http.RoundTripper { return held }, limits(100, 100, 2), newClock(),
		testHost)
	var answers []<-chan outcome
	for range 4 {
		answers = append(answers, sendLater(queue, Rebuild, ""))
	}
	eventually(t, "two in flight", func() bool { return held.count() == 2 })
	eventually(t, "two queued", func() bool { return queued(queue, Rebuild) == 2 })
	write := sendLater(queue, Write, "")
	eventually(t, "the write sent", func() bool { return held.count() == 3 })
	close(release)
	<-write
	for _, answer := range answers {
		<-answer
	}
}

type holding struct {
	mu      sync.Mutex
	calls   int
	release chan struct{}
}

func (h *holding) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

func (h *holding) RoundTrip(*http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	<-h.release
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func TestA429PausesEverythingButWritesForEveryScopeAndSpacesTheWrites(t *testing.T) {
	queue, server, clock := testQueue(limits(1000, 10, 10))
	server.status = func(call int) (int, string) {
		if call == 1 {
			return http.StatusTooManyRequests, "120"
		}
		return http.StatusOK, ""
	}
	if got := send(queue, Rebuild, "a"); got.status != http.StatusTooManyRequests {
		t.Fatalf("%+v", got)
	}
	for _, priority := range []Priority{Interactive, Probe, Rebuild, Enrich} {
		for _, scope := range []string{"a", "b"} {
			got := send(queue, priority, scope)
			var throttled *Throttled
			if !errors.As(got.err, &throttled) || !throttled.Paused ||
				throttled.RetryIn != 120*time.Second {
				t.Fatalf("%s %s went through a pause: %+v", priority, scope, got)
			}
		}
	}
	if got := send(queue, Rebuild, "a").err.Error(); got != "LaLiga nos frena: reintenta en 2 min" {
		t.Errorf("message %q", got)
	}

	var writes []<-chan outcome
	writes = append(writes, sendLater(queue, Write, "a"))
	eventually(t, "one write out at once", func() bool { return len(server.sent()) == 2 })
	for index, scope := range []string{"b", "a"} {
		writes = append(writes, sendLater(queue, Write, scope))
		eventually(t, "writes spaced", func() bool { return queued(queue, Write) == index+1 })
	}
	clock.Advance(4 * time.Second)
	time.Sleep(5 * time.Millisecond)
	if got := len(server.sent()); got != 2 {
		t.Fatalf("a write went before its five seconds: %d", got)
	}
	clock.Advance(time.Second)
	eventually(t, "the second write", func() bool { return len(server.sent()) == 3 })
	clock.Advance(5 * time.Second)
	eventually(t, "the third write", func() bool { return len(server.sent()) == 4 })
	for _, write := range writes {
		if got := <-write; got.err != nil || got.status != http.StatusOK {
			t.Fatalf("%+v", got)
		}
	}

	report := queue.Report().Hosts[0]
	if report.PauseSeconds != 110 || report.BackoffSecs != 60 {
		t.Errorf("pause %d backoff %d", report.PauseSeconds, report.BackoffSecs)
	}
	clock.Advance(110 * time.Second)
	if got := send(queue, Rebuild, "b"); got.err != nil {
		t.Fatalf("still paused after the Retry-After: %v", got.err)
	}
	if report := queue.Report().Hosts[0]; report.PausedUntil != nil || report.BackoffSecs != 0 {
		t.Errorf("the pause outlived itself: %+v", report)
	}

	a, b := scopeOf(queue, "a"), scopeOf(queue, "b")
	if a.Sent != 3 || a.RateLimited != 1 || a.Throttled != 5 || a.Waited != 10*time.Second {
		t.Errorf("scope a %+v", a)
	}
	if b.Sent != 2 || b.RateLimited != 0 || b.Throttled != 4 || b.Waited != 5*time.Second {
		t.Errorf("scope b %+v", b)
	}
}

func TestTheBackoffDoublesUpToHalfAnHourAndReadsInFlightDoNotDoubleIt(t *testing.T) {
	queue, server, clock := testQueue(limits(1000, 10, 10))
	server.status = func(int) (int, string) { return http.StatusTooManyRequests, "" }
	var pauses []int
	for range 7 {
		send(queue, Rebuild, "")
		pauses = append(pauses, queue.Report().Hosts[0].PauseSeconds/60)
		clock.Advance(time.Duration(pauses[len(pauses)-1]) * time.Minute)
	}
	if got := pauses; len(got) != 7 || got[0] != 1 || got[1] != 2 || got[4] != 16 ||
		got[5] != 30 || got[6] != 30 {
		t.Errorf("pauses in minutes %v", got)
	}

	queue, server, _ = testQueue(limits(1000, 10, 10))
	server.status = func(int) (int, string) { return http.StatusTooManyRequests, "" }
	send(queue, Rebuild, "")
	queue.mu.Lock()
	queue.lanes[testHost].inFlight++
	queue.mu.Unlock()
	queue.release(testHost, ticket{priority: Probe}, &http.Response{
		StatusCode: http.StatusTooManyRequests, Header: http.Header{}}, true)
	if backoff := queue.Report().Hosts[0].BackoffSecs; backoff != 60 {
		t.Errorf("a late 429 doubled the backoff to %d s", backoff)
	}
}

func TestAnInteractiveRequestThatWaitsPastItsPatienceFailsWithAReadableError(t *testing.T) {
	queue, _, clock := testQueue(limits(0.01, 1, 10))
	send(queue, Rebuild, "")
	answer := sendLater(queue, Interactive, "")
	eventually(t, "queued", func() bool { return queued(queue, Interactive) == 1 })
	clock.Advance(14 * time.Second)
	select {
	case got := <-answer:
		t.Fatalf("gave up early: %+v", got)
	case <-time.After(5 * time.Millisecond):
	}
	clock.Advance(time.Second)
	got := <-answer
	var throttled *Throttled
	if !errors.As(got.err, &throttled) || throttled.Paused || throttled.Priority != Interactive {
		t.Fatalf("%+v", got)
	}
	if queued(queue, Interactive) != 0 || scopeOf(queue, "").Throttled != 1 {
		t.Errorf("left in line or not counted: %+v", queue.Report())
	}
}

func TestOtherHostsDoNotWaitInTheQueue(t *testing.T) {
	queue, server, _ := testQueue(limits(0.01, 1, 1))
	send(queue, Rebuild, "")
	request, _ := http.NewRequest(http.MethodGet, "https://www.futbolfantasy.com/x", nil)
	if _, err := queue.RoundTrip(request); err != nil || len(server.sent()) != 2 {
		t.Fatalf("err=%v calls=%d", err, len(server.sent()))
	}
	if hosts := queue.Report().Hosts; len(hosts) != 1 {
		t.Errorf("futbolfantasy counted: %+v", hosts)
	}
}

func TestACacheHitTakesNoTurnInTheQueue(t *testing.T) {
	scope := config.ProfileIn(t.TempDir(), "a", "L").Scope(config.LevelGlobal)
	fake := httpxtest.New(t)
	fake.Answer("GET /api/v3/week/current", http.StatusOK, `{"weekNumber":9}`)
	t.Cleanup(SetTransport(fake))
	limited := NewQueue(nil, DefaultLimits(), nil, LaLigaHosts()...)
	t.Cleanup(SetQueue(limited))

	request := Request{URL: "https://fantasy-api.llt-services.com/api/v3/week/current",
		TTL: time.Hour, Tag: "week", Priority: Probe, Cache: scope}
	for range 3 {
		if _, err := Fetch(request); err != nil {
			t.Fatal(err)
		}
	}
	if sent := scopeOf(limited, "").Sent; sent != 1 || len(fake.Calls()) != 1 {
		t.Errorf("sent %d, calls %d", sent, len(fake.Calls()))
	}
}

func TestAThrottledRequestIsNotRetried(t *testing.T) {
	fake := httpxtest.New(t)
	fake.Handle("GET /x", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "60")
		writer.WriteHeader(http.StatusTooManyRequests)
	})
	t.Cleanup(SetTransport(fake))
	t.Cleanup(SetQueue(NewQueue(nil, DefaultLimits(), nil, testHost)))

	_, err := Fetch(Request{URL: "https://" + testHost + "/x"})
	var limited *RateLimited
	if !errors.As(err, &limited) {
		t.Fatalf("err=%v", err)
	}
	_, err = Fetch(Request{URL: "https://" + testHost + "/x", Priority: Enrich})
	var throttled *Throttled
	if !errors.As(err, &throttled) || len(fake.Calls()) != 1 {
		t.Fatalf("err=%v calls=%d", err, len(fake.Calls()))
	}
}

type slowBackend struct {
	*backend
	clock *fakeClock
	takes time.Duration
}

func (s slowBackend) RoundTrip(request *http.Request) (*http.Response, error) {
	s.clock.Advance(s.takes)
	return s.backend.RoundTrip(request)
}

func TestTheQueueTimesEveryAnswerAndSortsItByClass(t *testing.T) {
	server, clock := &backend{status: func(call int) (int, string) {
		if call == 2 {
			return http.StatusServiceUnavailable, ""
		}
		return http.StatusOK, ""
	}}, newClock()
	slow := slowBackend{backend: server, clock: clock, takes: 3 * time.Second}
	queue := NewQueue(func() http.RoundTripper { return slow }, limits(100, 100, 4), clock, testHost)
	send(queue, Rebuild, "a")
	send(queue, Rebuild, "a")
	host := queue.Report().Hosts[0]
	if host.Latency.Count != 2 || host.Latency.Sum != 6 || host.Latency.Counts[5] != 2 {
		t.Errorf("two answers of 3s land in the 5s bucket: %+v", host.Latency)
	}
	if host.Answers["2xx"] != 1 || host.Answers["5xx"] != 1 {
		t.Errorf("answers by class: %v", host.Answers)
	}
}
