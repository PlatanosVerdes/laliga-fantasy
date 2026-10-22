// Package httpxtest is a fake LaLiga for tests: every request, whatever its host, lands on one
// local server that answers by route and records who asked for what.
package httpxtest

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// Call is one request as it left the process: whose token, which method, which path and body.
type Call struct {
	Method        string
	Host          string
	Path          string
	Query         string
	Authorization string
	Body          []byte
}

// Fake is an http.RoundTripper; hand it to httpx.SetTransport.
type Fake struct {
	server *httptest.Server
	mux    *http.ServeMux
	target *url.URL

	mu    sync.Mutex
	calls []Call
}

func New(t testing.TB) *Fake {
	t.Helper()
	fake := &Fake{mux: http.NewServeMux()}
	fake.mux.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, `{"message":"no route in the fake"}`, http.StatusNotFound)
	})
	fake.server = httptest.NewServer(fake.mux)
	t.Cleanup(fake.server.Close)
	fake.target, _ = url.Parse(fake.server.URL)
	return fake
}

// Handle answers a ServeMux pattern, such as "GET /api/v4/user/me".
func (f *Fake) Handle(pattern string, handler http.HandlerFunc) {
	f.mux.HandleFunc(pattern, handler)
}

// Answer is Handle with a fixed status and JSON body.
func (f *Fake) Answer(pattern string, status int, body string) {
	f.Handle(pattern, func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, body)
	})
}

func (f *Fake) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	if request.Body != nil {
		read, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		_ = request.Body.Close()
		body = read
	}
	f.mu.Lock()
	f.calls = append(f.calls, Call{Method: request.Method, Host: request.URL.Host,
		Path: request.URL.Path, Query: request.URL.RawQuery,
		Authorization: request.Header.Get("Authorization"), Body: body})
	f.mu.Unlock()

	forwarded := request.Clone(request.Context())
	forwarded.URL.Scheme, forwarded.URL.Host = f.target.Scheme, f.target.Host
	forwarded.Host = f.target.Host
	forwarded.Body = io.NopCloser(bytes.NewReader(body))
	forwarded.ContentLength = int64(len(body))
	return http.DefaultTransport.RoundTrip(forwarded)
}

// Calls is every request so far, oldest first.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}
