package httpx

import (
	"errors"
	"sync"
)

type flights struct {
	mu    sync.Mutex
	calls map[string]*flight
}

type flight struct {
	done chan struct{}
	body string
	err  error
}

var inFlight = &flights{calls: map[string]*flight{}}

func (f *flights) do(key string, fetch func() (string, error)) (string, error) {
	f.mu.Lock()
	if call, ok := f.calls[key]; ok {
		f.mu.Unlock()
		<-call.done
		return call.body, call.err
	}
	call := &flight{done: make(chan struct{}), err: errors.New("the shared fetch did not finish")}
	f.calls[key] = call
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		delete(f.calls, key)
		f.mu.Unlock()
		close(call.done)
	}()
	call.body, call.err = fetch()
	return call.body, call.err
}
