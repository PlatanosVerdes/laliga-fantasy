// Package httpxtest is a fake LaLiga that records whose token asked for what.
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

type Call struct {
	Method        string
	Host          string
	Path          string
	Query         string
	Authorization string
	Header        http.Header
	Body          []byte
}

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

func (f *Fake) Handle(pattern string, handler http.HandlerFunc) {
	f.mux.HandleFunc(pattern, handler)
}

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
		Authorization: request.Header.Get("Authorization"),
		Header:        request.Header.Clone(), Body: body})
	f.mu.Unlock()

	forwarded := request.Clone(request.Context())
	forwarded.URL.Scheme, forwarded.URL.Host = f.target.Scheme, f.target.Host
	forwarded.Host = f.target.Host
	forwarded.Body = io.NopCloser(bytes.NewReader(body))
	forwarded.ContentLength = int64(len(body))
	return http.DefaultTransport.RoundTrip(forwarded)
}

func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}
