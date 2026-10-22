package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

// Nothing in this package's tests may read or write the real profile on the machine running them.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "fantasy-server-test")
	if err != nil {
		panic(err)
	}
	config.ConfigRoot, config.StateRoot, config.CacheRoot = root, root, filepath.Join(root, "cache")
	config.Use("", "")
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func freshRoot(t *testing.T) {
	t.Helper()
	root, account, league := config.ConfigRoot, config.Account, config.League
	t.Cleanup(func() { config.ConfigRoot, config.Account, config.League = root, account, league })
	config.ConfigRoot = t.TempDir()
	config.Account, config.League = "", ""
}

func saveAccount(t *testing.T, email, user string) string {
	t.Helper()
	account, err := auth.Keep(&auth.Tokens{AccessToken: "a", RefreshToken: "r", Email: email,
		UserID: user, ExpiresOn: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func sessionFor(t *testing.T, server *Server, user string) *http.Cookie {
	t.Helper()
	id, _, err := server.doors().sessions.open(user, "")
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: SessionCookie, Value: id}
}

func ownerSession(t *testing.T, server *Server) *http.Cookie {
	t.Helper()
	freshRoot(t)
	config.Account = saveAccount(t, "me@x.com", "user-me")
	return sessionFor(t, server, "user-me")
}

func asOwner(t *testing.T, server *Server) http.Handler {
	t.Helper()
	session := ownerSession(t, server)
	handler := server.Handler()
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.AddCookie(session)
		if request.Header.Get("Origin") == "" {
			request.Header.Set("Origin", "http://"+request.Host)
		}
		handler.ServeHTTP(writer, request)
	})
}

func visit(handler http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://x"+path, strings.NewReader(body))
	if method != http.MethodGet {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://x")
	}
	for _, c := range cookies {
		request.AddCookie(c)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func cookieNamed(recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range recorder.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func fakeExchange(t *testing.T, user, email string) *int {
	t.Helper()
	calls := new(int)
	was := exchange
	t.Cleanup(func() { exchange = was })
	exchange = func(code, verifier string) (*auth.Tokens, error) {
		*calls++
		return &auth.Tokens{AccessToken: "a", RefreshToken: "r", Email: email, UserID: user,
			ExpiresOn: time.Now().Add(time.Hour).Unix()}, nil
	}
	return calls
}

func login(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	page := visit(handler, http.MethodGet, "/", "")
	started := cookieNamed(page, LoginCookie)
	if started == nil {
		t.Fatalf("the login page set no login cookie: %d", page.Code)
	}
	return visit(handler, http.MethodPost, "/api/session", `{"paste":"authredirect://x?code=c"}`, started)
}

func TestWithoutASessionOnlyTheLoginPageAnswers(t *testing.T) {
	freshRoot(t)
	handler := New(nil, Options{}).Handler()
	if page := visit(handler, http.MethodGet, "/", ""); page.Code != http.StatusOK ||
		!strings.Contains(page.Body.String(), "Inicia sesión") {
		t.Errorf("the panel without a session: %d %.200s", page.Code, page.Body.String())
	}
	for _, path := range []string{"/api/raid", "/api/bid/confirm", "/api/accounts/use", "/refresh"} {
		if answer := visit(handler, http.MethodPost, path, `{}`); answer.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without a session: %d", path, answer.Code)
		}
	}
	for _, path := range []string{"/api/state", "/api/views", "/api/accounts", "/api/events"} {
		if answer := visit(handler, http.MethodGet, path, ""); answer.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a session: %d", path, answer.Code)
		}
	}
}

func TestAnOpenPrefixDoesNotOpenTheRouteItLeadsTo(t *testing.T) {
	freshRoot(t)
	handler := New(nil, Options{}).Handler()
	for _, path := range []string{"/assets/../api/state", "/assets/%2e%2e/api/views", "//api/state"} {
		request := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
		answer := httptest.NewRecorder()
		handler.ServeHTTP(answer, request)
		if answer.Code == http.StatusOK {
			t.Errorf("GET %s without a session: %d %.120s", path, answer.Code, answer.Body.String())
		}
	}
}

func TestTheSessionCookieIsHttpOnlySecureAndLax(t *testing.T) {
	freshRoot(t)
	fakeExchange(t, "user-me", "me@x.com")
	handler := New(nil, Options{}).Handler()
	answer := login(t, handler)
	session := cookieNamed(answer, SessionCookie)
	if answer.Code != http.StatusOK || session == nil {
		t.Fatalf("login: %d %s", answer.Code, answer.Body.String())
	}
	if !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode ||
		session.Path != "/" || session.MaxAge <= 0 {
		t.Errorf("cookie flags: %+v", session)
	}
	if len(session.Value) < 43 {
		t.Errorf("session id too short: %d characters", len(session.Value))
	}
	stored, _ := os.ReadFile(filepath.Join(config.ConfigRoot, "sessions.json"))
	if strings.Contains(string(stored), session.Value) || !strings.Contains(string(stored), hashOf(session.Value)) {
		t.Errorf("the file must hold the hash, never the id: %s", stored)
	}
	if info, err := os.Stat(filepath.Join(config.ConfigRoot, "sessions.json")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("sessions file: %v %v", info, err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://localhost:8000/", nil)
	if cookie(request, SessionCookie, "id", time.Hour).Secure {
		t.Error("on localhost the cookie has to work over plain HTTP")
	}
	request = httptest.NewRequest(http.MethodGet, "http://fantasy.example/", nil)
	if !cookie(request, SessionCookie, "id", time.Hour).Secure {
		t.Error("anywhere else the cookie is Secure")
	}
}

func TestExpiredUnknownAndRevokedSessionsAreRejected(t *testing.T) {
	server := New(nil, Options{})
	handler := asOwner(t, server)
	store := server.doors().sessions
	now := time.Now()
	store.now = func() time.Time { return now }
	if answer := visit(handler, http.MethodGet, "/no-such-page", ""); answer.Code != http.StatusNotFound {
		t.Fatalf("a live session: %d %s", answer.Code, answer.Body.String())
	}
	raw := server.Handler()
	unknown := &http.Cookie{Name: SessionCookie, Value: "made-up"}
	if answer := visit(raw, http.MethodGet, "/no-such-page", "", unknown); answer.Code != http.StatusUnauthorized {
		t.Errorf("an unknown session: %d", answer.Code)
	}

	idle := sessionFor(t, server, "user-me")
	now = now.Add(SessionIdle + time.Minute)
	if answer := visit(raw, http.MethodGet, "/no-such-page", "", idle); answer.Code != http.StatusUnauthorized {
		t.Errorf("a session idle for 30 days: %d", answer.Code)
	}

	busy := sessionFor(t, server, "user-me")
	for day := 0; day < 91; day++ {
		now = now.Add(24 * time.Hour)
		visit(raw, http.MethodGet, "/no-such-page", "", busy)
	}
	if answer := visit(raw, http.MethodGet, "/no-such-page", "", busy); answer.Code != http.StatusUnauthorized {
		t.Errorf("a session past 90 days: %d", answer.Code)
	}

	revoked := sessionFor(t, server, "user-me")
	if answer := visit(raw, http.MethodPost, "/api/logout", `{}`, revoked); answer.Code != http.StatusOK {
		t.Fatalf("logout: %d", answer.Code)
	}
	if answer := visit(raw, http.MethodGet, "/no-such-page", "", revoked); answer.Code != http.StatusUnauthorized {
		t.Errorf("a session logged out: %d", answer.Code)
	}
}

func TestSessionsSurviveARestart(t *testing.T) {
	freshRoot(t)
	first := New(nil, Options{})
	session := sessionFor(t, first, "user-me")
	if _, ok := New(nil, Options{}).doors().sessions.find(session.Value); !ok {
		t.Error("a restarted panel forgot the session")
	}
}

func TestALoginFromABrowserThatDidNotStartItIsRejected(t *testing.T) {
	freshRoot(t)
	calls := fakeExchange(t, "user-me", "me@x.com")
	handler := New(nil, Options{}).Handler()
	page := visit(handler, http.MethodGet, "/", "")
	started := cookieNamed(page, LoginCookie)
	if started == nil {
		t.Fatal("no login cookie")
	}
	for name, cookies := range map[string][]*http.Cookie{
		"no cookie":    nil,
		"forged one":   {{Name: LoginCookie, Value: "guess"}},
		"another page": {cookieNamed(visit(handler, http.MethodGet, "/", ""), LoginCookie)},
	} {
		answer := visit(handler, http.MethodPost, "/api/session",
			`{"paste":"authredirect://x?code=c&state=`+stateOf(t, page)+`"}`, cookies...)
		if answer.Code == http.StatusOK || cookieNamed(answer, SessionCookie) != nil {
			t.Errorf("%s: signed in with a code started elsewhere: %d", name, answer.Code)
		}
	}
	if *calls != 0 {
		t.Errorf("the code was exchanged %d times", *calls)
	}
	if answer := visit(handler, http.MethodPost, "/api/session",
		`{"paste":"authredirect://x?code=c&state=`+stateOf(t, page)+`"}`, started); answer.Code != http.StatusOK {
		t.Errorf("the browser that started it: %d %s", answer.Code, answer.Body.String())
	}
}

func stateOf(t *testing.T, page *httptest.ResponseRecorder) string {
	t.Helper()
	body := page.Body.String()
	index := strings.Index(body, "state=")
	if index < 0 {
		t.Fatal("no state in the login link")
	}
	state := body[index+len("state="):]
	return state[:strings.IndexAny(state, "&\"")]
}

func TestOnlyTheUsersFantasyAllowNamesSignIn(t *testing.T) {
	for _, c := range []struct {
		rule, user, email string
		want              bool
	}{
		{"user-1, user-2", "user-2", "b@x.com", true},
		{"user-1,b@x.com", "user-9", "B@X.com", true},
		{"user-1", "user-2", "b@x.com", false},
		{"*", "anybody", "", true},
		{"*", "", "", false},
	} {
		freshRoot(t)
		fakeExchange(t, c.user, c.email)
		answer := login(t, New(nil, Options{Allow: c.rule}).Handler())
		if signed := cookieNamed(answer, SessionCookie) != nil; signed != c.want || (answer.Code == http.StatusOK) != c.want {
			t.Errorf("FANTASY_ALLOW=%q user %q: %d signed in %v, want %v", c.rule, c.user, answer.Code, signed, c.want)
		}
		if !c.want && len(config.Accounts()) != 0 {
			t.Errorf("FANTASY_ALLOW=%q stored the refused tokens", c.rule)
		}
	}
}

func TestWithoutFantasyAllowTheSavedAccountsSignIn(t *testing.T) {
	freshRoot(t)
	saveAccount(t, "me@x.com", "user-me")
	fakeExchange(t, "user-me", "me@x.com")
	if answer := login(t, New(nil, Options{}).Handler()); answer.Code != http.StatusOK {
		t.Errorf("the saved account's user: %d %s", answer.Code, answer.Body.String())
	}
	fakeExchange(t, "stranger", "who@x.com")
	answer := login(t, New(nil, Options{}).Handler())
	var body map[string]any
	_ = json.Unmarshal(answer.Body.Bytes(), &body)
	if answer.Code != http.StatusForbidden || body["refused"] != true || cookieNamed(answer, SessionCookie) != nil {
		t.Errorf("a stranger: %d %s", answer.Code, answer.Body.String())
	}
}

func TestOnAFreshInstallTheFirstLoginBecomesTheOwner(t *testing.T) {
	freshRoot(t)
	server := New(nil, Options{})
	fakeExchange(t, "user-first", "first@x.com")
	if answer := login(t, server.Handler()); answer.Code != http.StatusOK {
		t.Fatalf("the first login: %d %s", answer.Code, answer.Body.String())
	}
	if err := os.Remove(config.AccountTokenFile(config.AccountID("first@x.com"))); err != nil {
		t.Fatal(err)
	}
	fakeExchange(t, "user-second", "second@x.com")
	if answer := login(t, server.Handler()); answer.Code != http.StatusForbidden {
		t.Errorf("the second, a stranger: %d %s", answer.Code, answer.Body.String())
	}
	fakeExchange(t, "user-first", "first@x.com")
	if answer := login(t, server.Handler()); answer.Code != http.StatusOK {
		t.Errorf("the owner coming back: %d %s", answer.Code, answer.Body.String())
	}
}

func TestASessionCannotSeeOrActOnAnotherUsersAccount(t *testing.T) {
	freshRoot(t)
	mine := saveAccount(t, "a@x.com", "user-a")
	theirs := saveAccount(t, "b@x.com", "user-b")
	config.Account, config.League = theirs, "L"
	server := New(nil, Options{AllowWrites: true, Guard: writes.NewGuard(nil)})
	handler := server.Handler()
	a := sessionFor(t, server, "user-a")

	answer := visit(handler, http.MethodGet, "/api/accounts", "", a)
	var listed struct {
		Account  string `json:"account"`
		Accounts []struct {
			ID string `json:"id"`
		} `json:"accounts"`
	}
	_ = json.Unmarshal(answer.Body.Bytes(), &listed)
	if answer.Code != http.StatusOK || listed.Account != "" || len(listed.Accounts) != 1 || listed.Accounts[0].ID != mine {
		t.Errorf("A's account list: %d %s", answer.Code, answer.Body.String())
	}
	if answer := visit(handler, http.MethodPost, "/api/accounts/use", `{"account":"`+theirs+`"}`, a); answer.Code != http.StatusForbidden {
		t.Errorf("A switching to B's account: %d %s", answer.Code, answer.Body.String())
	}
	if answer := visit(handler, http.MethodPost, "/api/accounts/use", `{}`, a); answer.Code != http.StatusForbidden {
		t.Errorf("A switching league on B's account: %d", answer.Code)
	}
	if answer := visit(handler, http.MethodPost, "/api/accounts/logout", `{"account":"`+theirs+`"}`, a); answer.Code != http.StatusForbidden {
		t.Errorf("A logging B out: %d", answer.Code)
	}
	if answer := visit(handler, http.MethodGet, "/api/accounts/avatar?account="+theirs, "", a); answer.Code != http.StatusNotFound {
		t.Errorf("A reading B's avatar: %d", answer.Code)
	}
	for _, path := range []string{"/api/state", "/api/views", "/api/events", "/api/lineup"} {
		if answer := visit(handler, http.MethodGet, path, "", a); answer.Code != http.StatusForbidden {
			t.Errorf("A reading %s of B's profile: %d", path, answer.Code)
		}
	}
	for _, path := range []string{"/api/raid", "/api/bid/prepare", "/refresh"} {
		if answer := visit(handler, http.MethodPost, path, `{"id":"1","amount":1}`, a); answer.Code != http.StatusForbidden {
			t.Errorf("A acting through %s on B's profile: %d", path, answer.Code)
		}
	}
	page := visit(handler, http.MethodGet, "/", "", a)
	if !strings.Contains(page.Body.String(), "El panel sigue otra cuenta") || strings.Contains(page.Body.String(), "b@x.com") {
		t.Errorf("A's page on B's profile: %.300s", page.Body.String())
	}
	if !strings.Contains(page.Body.String(), "a@x.com") {
		t.Errorf("A's own account is not offered: %.300s", page.Body.String())
	}
	if _, err := os.Stat(config.AccountTokenFile(theirs)); err != nil {
		t.Errorf("B's session was touched: %v", err)
	}
}

func TestACrossOriginWriteIsRejected(t *testing.T) {
	server := New(nil, Options{})
	session := ownerSession(t, server)
	handler := server.Handler()
	for _, c := range []struct {
		origin, referer string
		want            int
	}{
		{"https://evil.example", "", http.StatusForbidden},
		{"null", "", http.StatusForbidden},
		{"", "https://evil.example/page", http.StatusForbidden},
		{"", "", http.StatusForbidden},
		{"http://x", "", http.StatusOK},
		{"", "http://x/", http.StatusOK},
	} {
		request := httptest.NewRequest(http.MethodPost, "http://x/api/logout", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		if c.origin != "" {
			request.Header.Set("Origin", c.origin)
		}
		if c.referer != "" {
			request.Header.Set("Referer", c.referer)
		}
		request.AddCookie(session)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != c.want {
			t.Errorf("origin %q referer %q: %d, want %d", c.origin, c.referer, recorder.Code, c.want)
		}
		session = sessionFor(t, server, "user-me")
	}
}

func TestAConfirmationTokenFromAnotherSessionIsRejected(t *testing.T) {
	profile := tempProfile(t)
	bids := 0
	world := state.New(profile, func() (*model.Universe, error) {
		universe := &model.Universe{
			Market:  []model.Listing{{MarketID: "m", MinBid: 1_000_000, Bids: &bids}},
			Players: []model.Player{{ID: "p", Name: "Barato", Value: 1_000_000}},
		}
		universe.Players[0].MarketEntry = &universe.Market[0]
		return universe, nil
	})
	if err := world.Refresh("arranque"); err != nil {
		t.Fatal(err)
	}
	guard := writes.NewGuard(nil)
	guard.Cash = func(string) (int64, error) { return 50_000_000, nil }
	server := New(world, Options{AllowWrites: true, Guard: guard, LeagueID: "L", MyTeamID: "me",
		Profile: profile})
	a := ownerSession(t, server)
	b := sessionFor(t, server, "user-me")
	handler := server.Handler()
	prepared := visit(handler, http.MethodPost, "/api/bid/prepare",
		`{"operation":"bid","amount":1000000,"market_id":"m","player_id":"p"}`, a)
	var summary map[string]any
	_ = json.Unmarshal(prepared.Body.Bytes(), &summary)
	token, _ := summary["token"].(string)
	if prepared.Code != http.StatusOK || token == "" {
		t.Fatalf("prepare: %d %s", prepared.Code, prepared.Body.String())
	}
	confirm := `{"token":"` + token + `","dry_run":true}`
	if answer := visit(handler, http.MethodPost, "/api/bid/confirm", confirm, b); answer.Code == http.StatusOK {
		t.Errorf("session B confirmed A's token: %s", answer.Body.String())
	}
	if answer := visit(handler, http.MethodPost, "/api/bid/confirm", confirm, a); answer.Code != http.StatusOK {
		t.Errorf("session A, after B tried: %d %s", answer.Code, answer.Body.String())
	}
}

func TestLoginAttemptsAreLimitedPerClient(t *testing.T) {
	freshRoot(t)
	handler := New(nil, Options{}).Handler()
	submit := func(remote, forwarded string) int {
		request := httptest.NewRequest(http.MethodPost, "http://x/api/session", strings.NewReader(`{"paste":"nope"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://x")
		request.RemoteAddr = remote
		if forwarded != "" {
			request.Header.Set("X-Forwarded-For", forwarded)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}
	for attempt := 1; attempt <= LoginAttempts; attempt++ {
		if status := submit("203.0.113.7:4000", ""); status == http.StatusTooManyRequests {
			t.Fatalf("limited at attempt %d", attempt)
		}
	}
	if status := submit("203.0.113.7:4000", ""); status != http.StatusTooManyRequests {
		t.Errorf("attempt %d: %d, want 429", LoginAttempts+1, status)
	}
	if status := submit("203.0.113.7:4000", "198.51.100.1"); status != http.StatusTooManyRequests {
		t.Errorf("a forged X-Forwarded-For escaped the limit: %d", status)
	}
	for attempt := 1; attempt <= LoginAttempts; attempt++ {
		submit("172.18.0.2:5000", "spoof, 198.51.100.9")
	}
	if status := submit("172.18.0.2:5000", "198.51.100.9"); status != http.StatusTooManyRequests {
		t.Errorf("the client behind the proxy was not limited: %d", status)
	}
	if status := submit("172.18.0.2:5000", "198.51.100.10"); status == http.StatusTooManyRequests {
		t.Error("another client behind the same proxy was limited too")
	}
}

func TestSigningOutEverywhereEndsOnlyThatUsersSessions(t *testing.T) {
	server := New(nil, Options{Allow: "*"})
	here := ownerSession(t, server)
	phone := sessionFor(t, server, "user-me")
	stranger := sessionFor(t, server, "user-other")
	handler := server.Handler()
	if answer := visit(handler, http.MethodPost, "/api/logout", `{"everywhere":true}`, here); answer.Code != http.StatusOK {
		t.Fatalf("signing out everywhere: %d %s", answer.Code, answer.Body.String())
	}
	for name, cookie := range map[string]*http.Cookie{"this browser": here, "the linked phone": phone} {
		if _, ok := server.doors().sessions.find(cookie.Value); ok {
			t.Errorf("%s is still signed in", name)
		}
	}
	if _, ok := server.doors().sessions.find(stranger.Value); !ok {
		t.Error("another user's session ended too")
	}
}

func TestSigningOutHereLeavesTheOtherDevices(t *testing.T) {
	server := New(nil, Options{})
	here := ownerSession(t, server)
	phone := sessionFor(t, server, "user-me")
	visit(server.Handler(), http.MethodPost, "/api/logout", `{}`, here)
	if _, ok := server.doors().sessions.find(here.Value); ok {
		t.Error("this browser is still signed in")
	}
	if _, ok := server.doors().sessions.find(phone.Value); !ok {
		t.Error("the phone was signed out too")
	}
}

func TestDevicesCountsOnlyTheVisitorsOwnSessions(t *testing.T) {
	server := New(nil, Options{Allow: "*"})
	here := ownerSession(t, server)
	sessionFor(t, server, "user-me")
	sessionFor(t, server, "user-other")
	answer := visit(server.Handler(), http.MethodGet, "/api/devices", "", here)
	if answer.Code != http.StatusOK || !strings.Contains(answer.Body.String(), `"count":2`) {
		t.Errorf("devices: %d %s", answer.Code, answer.Body.String())
	}
}
