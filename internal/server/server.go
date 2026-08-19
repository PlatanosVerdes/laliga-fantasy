// Package server is the HTTP surface: the JSON API, the push channel and the assets.
//
// The contract is the Python server's, key for key, because the same browser code has to
// work against either implementation while the port is half done.
package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

// Heartbeat keeps the SSE connection alive through proxies that close idle sockets.
const Heartbeat = 20 * time.Second

type Options struct {
	Host        string
	Port        int
	AllowWrites bool
	Assets      string
	// Nudge lets a request tighten the refresh cadence (a browser connecting, a write
	// landing) without the server knowing what the engine is.
	Nudge func(string)
	// Page renders the current world. Injected so the server does not need to know what a
	// document is, and so a run without a session can still answer /healthz.
	Page func() string
	// Refresh forces a rebuild, for /refresh.
	Refresh func(cause string, force bool) error
	// Client reads the live half of a request: the lineup, the value history, the balance.
	Client *api.Client
	// Guard is the two-step write path. Nil means the binary was built to only read.
	Guard *writes.Guard
	// Which league and team every write belongs to.
	LeagueID string
	MyTeamID string
	// HoldExceptions is what the league agreed as an escape hatch to its own hold rule,
	// quoted verbatim when an operation is refused because of it.
	HoldExceptions string
	// Settle makes the world catch up after a write, and is expected to block.
	Settle func(cause string)
	// Adopted runs once a session has just been stored: the world has to be built from
	// scratch, and the league resolved, before the page can show anything.
	Adopted func()
}

type Server struct {
	state *state.State
	opts  Options
	// The rendered page, keyed by what the world holds. Rendering it costs seconds — the
	// advice layer, the futbolfantasy details, the whole document — and every repaint was
	// paying that again for a page that had not changed. One entry: the only page worth
	// keeping is the current one.
	pageMu  sync.Mutex
	pageKey string
	page    string
}

// render returns the current page, rendering it only when the world has moved since the last
// time. Guarded rather than sharded: two requests arriving together should wait for one render,
// not run two.
func (s *Server) render() string {
	if s.opts.Page == nil {
		return ""
	}
	key := s.state.RenderKey()
	s.pageMu.Lock()
	defer s.pageMu.Unlock()
	if key == s.pageKey && s.page != "" {
		return s.page
	}
	started := time.Now()
	page := s.opts.Page()
	if page == "" {
		return ""
	}
	s.pageKey, s.page = key, page
	slog.Info("page rendered", "key", key, "bytes", len(page),
		"ms", time.Since(started).Milliseconds())
	return page
}

func New(world *state.State, opts Options) *Server {
	if opts.Assets == "" {
		opts.Assets = "assets"
	}
	return &Server{state: world, opts: opts}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/state", s.payload)
	mux.HandleFunc("/api/events", s.events)
	mux.HandleFunc("/api/player/", s.detail)
	mux.HandleFunc("/api/fragments", s.fragments)
	mux.HandleFunc("/api/lineup", s.lineup)
	mux.HandleFunc("/api/session", s.session)
	mux.HandleFunc("/api/favourite", s.favourite)
	mux.HandleFunc("/favourite", s.favourite)
	mux.HandleFunc("/api/always", s.always)
	mux.HandleFunc("/api/raid", s.raid)
	mux.HandleFunc("/api/bid/prepare", s.prepare)
	mux.HandleFunc("/api/bid/confirm", s.confirm)
	mux.HandleFunc("/refresh", s.refresh)
	mux.HandleFunc("/assets/", s.asset)
	mux.HandleFunc("/", s.index)
	return logging(mux)
}

func (s *Server) ListenAndServe() error {
	address := fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Port)
	server := &http.Server{Addr: address, Handler: s.Handler()}
	slog.Info("serving", "address", address, "writes", s.opts.AllowWrites)
	fmt.Printf("Sirviendo en http://%s\n", address)
	return server.ListenAndServe()
}

// counted remembers the status a handler wrote, which is the only part of an answer worth
// logging: a 200 and a 403 on the same path are different events.
type counted struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (c *counted) WriteHeader(status int) {
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *counted) Write(body []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	written, err := c.ResponseWriter.Write(body)
	c.bytes += written
	return written, err
}

// Flush keeps SSE working: without it the wrapper hides the flusher the stream needs.
func (c *counted) Flush() {
	if flusher, ok := c.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		wrapped := &counted{ResponseWriter: writer}
		next.ServeHTTP(wrapped, request)
		if wrapped.status == 0 {
			wrapped.status = http.StatusOK
		}
		elapsed := time.Since(started).Milliseconds()

		// Anything that changes something, anything that failed, and the page itself get an
		// info line. Assets and polling do not: a log that records every GET buries the one
		// line that mattered.
		mutating := request.Method != http.MethodGet && request.Method != http.MethodHead
		interesting := mutating || wrapped.status >= 400 || request.URL.Path == "/"
		level := slog.LevelDebug
		if interesting {
			level = slog.LevelInfo
		}
		if wrapped.status >= 500 {
			level = slog.LevelError
		}
		slog.Log(request.Context(), level, "http", "method", request.Method,
			"path", request.URL.Path, "status", wrapped.status, "ms", elapsed,
			"bytes", wrapped.bytes)
	})
}

func (s *Server) json(writer http.ResponseWriter, status int, body any) {
	blob, err := json.Marshal(body)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_, _ = writer.Write(blob)
}

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	health := s.state.Health()
	status := http.StatusOK
	if health.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	s.json(writer, status, health)
}

func (s *Server) payload(writer http.ResponseWriter, _ *http.Request) {
	payload := s.state.Payload()
	// The page reads this to decide whether to offer an operation at all, so it travels
	// with the world rather than being asked for separately.
	payload["writes_enabled"] = s.opts.AllowWrites
	s.json(writer, http.StatusOK, payload)
}

func (s *Server) refresh(writer http.ResponseWriter, _ *http.Request) {
	if s.opts.Refresh == nil {
		s.json(writer, http.StatusNotImplemented, map[string]any{"error": "sin refresco"})
		return
	}
	started := time.Now()
	err := s.opts.Refresh("manual", true)
	health := s.state.Health()
	if err != nil {
		slog.Error("manual refresh failed", "reason", err.Error(),
			"ms", time.Since(started).Milliseconds())
	} else {
		slog.Info("manual refresh", "version", health.Version,
			"ms", time.Since(started).Milliseconds())
	}
	s.json(writer, http.StatusOK, map[string]any{"refreshed": err == nil,
		"version": health.Version})
}

// events is the push channel. A browser gets told when the version moves and when an
// operation moved something, and a heartbeat in between so proxies leave it alone.
func (s *Server) events(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "sin streaming", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)

	channel := s.state.Subscribe()
	defer s.state.Unsubscribe(channel)

	// Say hello with the current version, so a browser that reconnected knows at once
	// whether it missed something.
	hello, _ := json.Marshal(map[string]any{"type": "hello",
		"version": s.state.Health().Version})
	fmt.Fprintf(writer, "data: %s\n\n", hello)
	flusher.Flush()

	ticker := time.NewTicker(Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case message := <-channel:
			fmt.Fprintf(writer, "data: %s\n\n", message)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(writer, ": latido\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) asset(writer http.ResponseWriter, request *http.Request) {
	name := filepath.Base(strings.TrimPrefix(request.URL.Path, "/assets/"))
	body, err := os.ReadFile(filepath.Join(s.opts.Assets, name))
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	switch filepath.Ext(name) {
	case ".css":
		writer.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		writer.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".png":
		writer.Header().Set("Content-Type", "image/png")
	case ".svg":
		writer.Header().Set("Content-Type", "image/svg+xml")
	case ".ico":
		writer.Header().Set("Content-Type", "image/x-icon")
	case ".html":
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	default:
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	_, _ = writer.Write(body)
}

// index serves the page.
func (s *Server) index(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	// No session: ask for one from the page itself rather than expecting somebody to ssh in
	// and drop a tokens.json next to the binary.
	if !HasSession() {
		slog.Warn("no session: serving the login page instead of the report")
		s.setup(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	if s.opts.Page == nil {
		fmt.Fprint(writer, "<title>Generando</title><p>Generando el primer informe…</p>")
		return
	}
	fmt.Fprint(writer, s.render())
}
