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
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/rules"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/services"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

// Heartbeat keeps the SSE connection alive through proxies that close idle sockets.
const Heartbeat = 20 * time.Second

type Options struct {
	Host        string
	Port        int
	AllowWrites bool
	// Rehearse is writes.Rehearsal seen from here: nothing is sent, so nothing is rebuilt after.
	Rehearse bool
	// Mode is what this server may do, in one word: auto, manual or solo lectura. Shown on the
	// page and answered by /api/state, because "can it act by itself" is the first question.
	Mode   string
	Assets string
	// Nudge lets a request tighten the refresh cadence (a browser connecting, a write
	// landing) without the server knowing what the engine is.
	Nudge func(string)
	// Page renders the current world. Injected so the server does not need to know what a
	// document is, and so a run without a session can still answer /healthz.
	Page func() Rendered
	// Refresh forces a rebuild, for /refresh.
	Refresh func(cause string, force bool) error
	// Client reads the live half of a request: the lineup, the value history, the balance.
	Client *api.Client
	// Guard is the two-step write path. Nil means the binary was built to only read.
	Guard *writes.Guard
	// Which league and team every write belongs to. Identity, when set, wins: a server
	// started without a session only learns them once one is adopted.
	LeagueID string
	MyTeamID string
	Identity func() (league, team string)
	// HoldExceptions is what the league agreed as an escape hatch to its own hold rule,
	// quoted verbatim when an operation is refused because of it.
	HoldExceptions string
	// Restart starts the process again on the active profile, and Leagues lists the active
	// account's leagues. Both optional: without them switching is not offered.
	Restart func()
	Leagues func() ([]LeagueChoice, error)
	// MaxDebtPct is the league's debt cap for market bids, in percent of the squad's value.
	MaxDebtPct float64
	// Settle makes the world catch up after a write, and is expected to block.
	Settle func(cause string)
	// Adopted runs once a session has just been stored: the world has to be built from
	// scratch, and the league resolved, before the page can show anything.
	Adopted func()
	// Necroporra holds the side game's vote credentials: the external session cookie, the
	// server-action id and the ballot's gameweek. Empty cookie disables the vote.
	NecroCookie   string
	NecroAction   string
	NecroGameweek int
	// Services is the registry of job clocks behind /api/services. Nil leaves the heartbeat
	// at its default and the endpoint answering 501.
	Services *services.Registry
	// Access, when enabled, opens a second door for the Cloudflare tunnel. Port stays the
	// private one (tailnet, Caddy), so the panel is still reachable when Cloudflare is
	// blocked or the proxy is down.
	Access Access
	// Allow is FANTASY_ALLOW; empty lets in only the users already known to the install.
	Allow string
}

// Rendered is one build of the world: the page, and the views the client draws from JSON.
// Both come out of the same pass, so a view can never be a build ahead of the page around it.
type Rendered struct {
	HTML  string
	Views map[string]any
}

type Server struct {
	state *state.State
	opts  Options
	// The rendered page, keyed by what the world holds. Rendering it costs seconds — the
	// advice layer, the futbolfantasy details, the whole document — and every repaint was
	// paying that again for a page that had not changed. One entry: the only page worth
	// keeping is the current one.
	pageMu      sync.Mutex
	pageKey     string
	pageVersion int
	page        Rendered
	// drawing is the render on its way for each key, so a request arriving during one waits for
	// it instead of queueing another. drawMu keeps it to one render at a time.
	drawing map[string]chan struct{}
	drawMu  sync.Mutex
	season  season

	gateOnce sync.Once
	gate     *gate
	// Held from admitting to recording the owner, or two first logins would both own the install.
	loginMu sync.Mutex
}

// Rendering is how far the page being drawn has got. Options.Page reports its steps here; a
// page waiting on it reads them from /healthz. The futbolfantasy pages are only the ones in
// hand: the rest fill in after the page is up.
var Rendering = model.NewTracker(
	model.Stage{Name: "Saldo", Key: "cash", Weight: 5},
	model.Stage{Name: "Calculando consejos", Key: "advice", Weight: 25},
	model.Stage{Name: "Fichas de futbolfantasy", Key: "details", Weight: 2},
	model.Stage{Name: "Plan y cláusulas", Key: "plan", Weight: 20},
	model.Stage{Name: "Necroporra", Key: "necro", Weight: 8},
	model.Stage{Name: "Dibujando la página", Key: "document", Weight: 40},
)

// patience is how long the page request waits for a render before answering with a page that
// shows its progress. Most misses take a second or three, and a blank tab for longer reads as a
// panel that is down.
var patience = 1500 * time.Millisecond

// cached is the page in the cache with the key and world version it was drawn for.
func (s *Server) cached() (Rendered, string, int) {
	s.pageMu.Lock()
	defer s.pageMu.Unlock()
	return s.page, s.pageKey, s.pageVersion
}

// draw starts a render of the world at key and version unless one for the same key is on its
// way, and returns what is closed once it is over.
func (s *Server) draw(key string, version int) <-chan struct{} {
	s.pageMu.Lock()
	if done, ok := s.drawing[key]; ok {
		s.pageMu.Unlock()
		return done
	}
	if s.drawing == nil {
		s.drawing = map[string]chan struct{}{}
	}
	done := make(chan struct{})
	s.drawing[key] = done
	s.pageMu.Unlock()
	go func() {
		defer close(done)
		s.drawMu.Lock()
		defer s.drawMu.Unlock()
		if page, at, _ := s.cached(); at != key || page.HTML == "" {
			s.paint(key, version)
		}
		s.pageMu.Lock()
		delete(s.drawing, key)
		s.pageMu.Unlock()
	}()
	return done
}

// paint renders the world into the cache under key. Only ever run by draw, one at a time.
func (s *Server) paint(key string, version int) {
	started := time.Now()
	Rendering.Start()
	page := s.opts.Page()
	phases := Rendering.End()
	if page.HTML == "" {
		return
	}
	s.pageMu.Lock()
	s.pageKey, s.pageVersion, s.page = key, version, page
	s.pageMu.Unlock()
	slog.Info("page rendered", append([]any{"key", key, "bytes", len(page.HTML),
		"ms", time.Since(started).Milliseconds()}, phases...)...)
}

// render returns the current page, rendering it only when the world has moved since the last
// time. Two requests arriving together wait for one render, not run two.
func (s *Server) render() Rendered {
	if s.opts.Page == nil {
		return Rendered{}
	}
	key, version := s.state.RenderStamp()
	if page, at, _ := s.cached(); at == key && page.HTML != "" {
		return page
	}
	<-s.draw(key, version)
	page, _, _ := s.cached()
	return page
}

// current is the page for a browser opening the panel, without making it wait for a render it
// does not need. A page drawn for the same version of the world is served at once while the
// new one is drawn behind it: the version only stays put when no rebuild found anything worth
// telling an open browser about, and the views the page asks for as soon as it connects are
// the fresh ones. Otherwise it waits up to patience, and false means the render is still on.
func (s *Server) current() (Rendered, bool) {
	key, version := s.state.RenderStamp()
	page, at, drawnFor := s.cached()
	if at == key && page.HTML != "" {
		return page, true
	}
	done := s.draw(key, version)
	if page.HTML != "" && drawnFor == version {
		slog.Info("page served while redrawing", "drawn", at, "key", key)
		return page, true
	}
	select {
	case <-done:
		page, _, _ = s.cached()
		return page, page.HTML != ""
	case <-time.After(patience):
		return Rendered{}, false
	}
}

// pageReady says whether opening the panel now would be served at once.
func (s *Server) pageReady() bool {
	_, version := s.state.RenderStamp()
	page, _, drawnFor := s.cached()
	return page.HTML != "" && drawnFor == version
}

// drawnOnce is whether any page is in the cache, of whatever version.
func (s *Server) drawnOnce() bool {
	page, _, _ := s.cached()
	return page.HTML != ""
}

// Warm builds the page into the cache. Called before a change is announced, so the refresh
// the browser does next reads a page that is already there.
func (s *Server) Warm() { s.render() }

func New(world *state.State, opts Options) *Server {
	if opts.Assets == "" {
		opts.Assets = "assets"
	}
	return &Server{state: world, opts: opts}
}

func (s *Server) identity() (string, string) {
	if s.opts.Identity != nil {
		if league, team := s.opts.Identity(); league != "" && team != "" {
			return league, team
		}
	}
	return s.opts.LeagueID, s.opts.MyTeamID
}

func (s *Server) leagueID() string {
	league, _ := s.identity()
	return league
}

func (s *Server) myTeamID() string {
	_, team := s.identity()
	return team
}

func (s *Server) holdExceptions() string {
	if s.opts.HoldExceptions != "" {
		return s.opts.HoldExceptions
	}
	if league := s.leagueID(); league != "" {
		return rules.For(league).HoldExceptions
	}
	return ""
}

// maxDebtPct is the league's debt cap for bids, read from its rules once the league is known.
func (s *Server) maxDebtPct() float64 {
	if s.opts.MaxDebtPct != 0 {
		return s.opts.MaxDebtPct
	}
	if league := s.leagueID(); league != "" {
		return rules.For(league).MaxDebtPct
	}
	return 0
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/state", s.payload)
	mux.HandleFunc("/api/events", s.events)
	mux.HandleFunc("/api/player/", s.detail)
	mux.HandleFunc("/api/manager/", s.manager)
	mux.HandleFunc("/api/compare", s.compare)
	mux.HandleFunc("/api/matchday/", s.matchday)
	mux.HandleFunc("/api/forecast/", s.forecastWeek)
	mux.HandleFunc("/api/season", s.seasonTable)
	mux.HandleFunc("/api/view/", s.view)
	mux.HandleFunc("/api/views", s.views)
	mux.HandleFunc("/api/lineup", s.lineup)
	mux.HandleFunc("/api/session", s.session)
	mux.HandleFunc("/api/favourite", s.favourite)
	mux.HandleFunc("/api/usage", s.usage)
	mux.HandleFunc("/api/services", s.services)
	mux.HandleFunc("/favourite", s.favourite)
	mux.HandleFunc("/api/always", s.always)
	mux.HandleFunc("/api/raid", s.raid)
	mux.HandleFunc("/api/raid/cancel", s.cancelRaid)
	mux.HandleFunc("/api/dismiss", s.dismiss)
	mux.HandleFunc("/api/shield", s.shield)
	mux.HandleFunc("/api/shield/cancel", s.cancelShield)
	mux.HandleFunc("/api/bid/prepare", s.prepare)
	mux.HandleFunc("/api/bid/confirm", s.confirm)
	mux.HandleFunc("/api/necroporra/vote", s.necroVote)
	mux.HandleFunc("/refresh", s.refresh)
	mux.HandleFunc("/assets/", s.asset)
	mux.HandleFunc("/favicon.ico", s.icon)
	mux.HandleFunc("/apple-touch-icon.png", s.icon)
	mux.HandleFunc("/api/accounts", s.accounts)
	mux.HandleFunc("/api/accounts/use", s.useAccount)
	mux.HandleFunc("/api/accounts/logout", s.logout)
	mux.HandleFunc("/api/accounts/avatar", s.avatar)
	mux.HandleFunc("/api/logout", s.signOut)
	mux.HandleFunc("/api/devices", s.devices)
	mux.HandleFunc("/vincular", s.linkPage)
	mux.HandleFunc("/vincular/nuevo", s.newLinkPage)
	mux.HandleFunc("/api/link", s.createLink)
	mux.HandleFunc("/api/link/status", s.linkStatus)
	mux.HandleFunc("/api/link/redeem", s.redeemLink)
	mux.HandleFunc("/", s.index)
	return logging(sameOrigin(sameSiteOnly(s.requireSession(s.whileLoading(mux)))))
}

// AccessHandler is Handler behind the Access check, for the tunnel's door.
func (s *Server) AccessHandler() http.Handler {
	return requireAccess(s.opts.Access, s.Handler())
}

func (s *Server) ListenAndServe() error {
	failed := make(chan error, 2)
	address := fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Port)
	go func() { failed <- (&http.Server{Addr: address, Handler: s.Handler()}).ListenAndServe() }()
	slog.Info("serving", "address", address, "writes", s.opts.AllowWrites)
	fmt.Printf("Sirviendo en http://%s\n", address)
	if s.opts.Access.Enabled() {
		access := fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Access.Port)
		go func() { failed <- (&http.Server{Addr: access, Handler: s.AccessHandler()}).ListenAndServe() }()
		slog.Info("serving behind Cloudflare Access", "address", access)
	}
	return <-failed
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

// sameSiteOnly refuses an API write that is not JSON. A page on another site can make your
// browser post a form or plain text here, but not JSON without this server's consent, which it
// never gives; so an armed clausulazo or a swapped session can only come from the panel.
// The usage beacon is exempt: it records clicks and moves nothing.
func sameSiteOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		write := request.Method != http.MethodGet && request.Method != http.MethodHead
		api := strings.HasPrefix(request.URL.Path, "/api/") && request.URL.Path != "/api/usage"
		if write && api && !strings.HasPrefix(request.Header.Get("Content-Type"), "application/json") {
			http.Error(writer, `{"error":"solo JSON"}`, http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(writer, request)
	})
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
		// The usage beacon is a POST that changes nothing but arrives every fifteen seconds,
		// so counting it as mutating would bury the log it is filed next to.
		mutating := request.Method != http.MethodGet && request.Method != http.MethodHead &&
			request.URL.Path != "/api/usage"
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

// health is the world's health plus the page's: a page waiting on a render polls it to show the
// step the render is on and to know when the panel is ready.
type health struct {
	state.Health
	Rendering *model.Progress `json:"rendering,omitempty"`
	PageReady bool            `json:"page_ready"`
}

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	answer := health{Health: s.state.Health(), PageReady: s.pageReady()}
	if current, running := Rendering.Current(); running {
		answer.Rendering = &current
	}
	status := http.StatusOK
	if answer.Status == "degraded" {
		status = http.StatusServiceUnavailable
	}
	s.json(writer, status, answer)
}

func (s *Server) payload(writer http.ResponseWriter, _ *http.Request) {
	payload := s.state.Payload()
	// The page reads this to decide whether to offer an operation at all, so it travels
	// with the world rather than being asked for separately.
	payload["writes_enabled"] = s.opts.AllowWrites
	payload["mode"] = s.opts.Mode
	s.json(writer, http.StatusOK, payload)
}

func (s *Server) refresh(writer http.ResponseWriter, request *http.Request) {
	// A forced rebuild reads the whole league: not something a link preview should set off.
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
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
		"version": s.state.Health().Version, "loading": s.state.Loading()})
	fmt.Fprintf(writer, "data: %s\n\n", hello)
	flusher.Flush()

	beat := s.heartbeat()
	ticker := time.NewTicker(beat)
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
			if next := s.heartbeat(); next != beat {
				beat = next
				ticker.Reset(beat)
			}
		}
	}
}

func (s *Server) asset(writer http.ResponseWriter, request *http.Request) {
	name := path.Clean(strings.TrimPrefix(request.URL.Path, "/assets/"))
	if name == "." || strings.HasPrefix(name, "..") || strings.HasPrefix(name, "/") ||
		strings.Contains(name, "\\") {
		http.NotFound(writer, request)
		return
	}
	file, err := os.Open(filepath.Join(s.opts.Assets, filepath.FromSlash(name)))
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
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
	// The modules import each other by plain path, so a deploy is only picked up if every load
	// asks again; Last-Modified keeps that a 304.
	writer.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(writer, request, name, info.ModTime(), file)
}

// icon answers the paths browsers ask on their own, which never read the page's embedded icon.
func (s *Server) icon(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("Cache-Control", "max-age=86400")
	http.ServeFile(writer, request, filepath.Join(s.opts.Assets, "favicon.png"))
}

// index serves the page.
func (s *Server) index(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	// No session: ask for one from the page itself rather than expecting somebody to ssh in
	// and drop a tokens.json next to the binary.
	if !HasSession() || request.URL.Query().Get("cuenta") == "nueva" {
		slog.Warn("no session or adding one: serving the login page")
		s.setup(writer, request)
		return
	}
	if who, _ := visitorOf(request); !owns(who, config.Account) {
		s.foreign(writer, who)
		return
	}
	if config.League == "" {
		s.leaguePicker(writer)
		return
	}
	// The league never loaded (left it, or it is gone): say so and offer the way out, rather
	// than an empty page with no menu to leave it by.
	if health := s.state.Health(); s.state.Universe() == nil {
		if health.LastError != nil {
			s.leagueFailed(writer, *health.LastError)
			return
		}
		// The first build reads the whole league: a page that says so rather than a blank one,
		// and that shows what is already read once there is something.
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		if preview := s.state.Preview(); preview != nil {
			fmt.Fprint(writer, previewPage(preview))
			return
		}
		fmt.Fprint(writer, authPage("Preparando", `<h1>Preparando la liga…</h1>
<p class="lead">La primera vez se lee la liga entera: plantillas, mercado, calendario y movimientos.</p>
`+loadBar+`<script>reloadWhenReady();</script>`))
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	if s.opts.Page == nil {
		fmt.Fprint(writer, "<title>Generando</title><p>Generando el primer informe…</p>")
		return
	}
	page, ready := s.current()
	// The first page of a cold start: the preview stays up while the panel is drawn.
	if preview := s.state.Preview(); !ready && preview != nil && !s.drawnOnce() {
		slog.Info("page still rendering: serving the preview")
		fmt.Fprint(writer, previewPage(preview))
		return
	}
	if !ready {
		slog.Info("page still rendering: serving the wait page")
		fmt.Fprint(writer, drawingPage)
		return
	}
	fmt.Fprint(writer, page.HTML)
}

// drawingPage is what a browser gets while the panel is drawn: the league is already read, so
// it says that, and shows the step the render is on until the panel is ready.
var drawingPage = authPage("Generando", `<h1>Generando la página…</h1>
<p class="lead">La liga ya está leída. Falta dibujar el panel: consejos, plan y cláusulas.</p>
`+loadBar+`<script>reloadWhenDrawn();</script>`)

// views is every tab the browser draws, in one answer: a live refresh asks once, not per tab.
func (s *Server) views(writer http.ResponseWriter, _ *http.Request) {
	// The live balance rides along: it is what every amount is judged against.
	s.json(writer, http.StatusOK, map[string]any{"version": s.state.Health().Version,
		"cash": s.budget(), "views": s.render().Views})
}

// view is one tab as data, for the tabs the browser draws itself.
func (s *Server) view(writer http.ResponseWriter, request *http.Request) {
	name := strings.TrimPrefix(request.URL.Path, "/api/view/")
	built := s.render()
	data, ok := built.Views[name]
	if !ok {
		s.json(writer, http.StatusNotFound, map[string]any{"error": "sin vista " + name})
		return
	}
	s.json(writer, http.StatusOK, map[string]any{"version": s.state.Health().Version,
		"view": data})
}
