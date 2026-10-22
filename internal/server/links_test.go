package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func createLink(t *testing.T, handler http.Handler, session *http.Cookie) string {
	t.Helper()
	answer := visit(handler, http.MethodPost, "/api/link", `{}`, session)
	var body struct {
		Code, URL, QR string
	}
	_ = json.Unmarshal(answer.Body.Bytes(), &body)
	if answer.Code != http.StatusOK || body.Code == "" {
		t.Fatalf("creating a link code: %d %s", answer.Code, answer.Body.String())
	}
	if body.URL != "http://x/vincular#"+body.Code || !strings.HasPrefix(body.QR, "<svg") {
		t.Errorf("link address %q, picture %.40q", body.URL, body.QR)
	}
	return body.Code
}

func redeemFrom(handler http.Handler, ip, code string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "http://x/api/link/redeem", strings.NewReader(`{"code":"`+code+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://x")
	request.RemoteAddr = ip + ":4000"
	for _, c := range cookies {
		request.AddCookie(c)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func linkStatusOf(t *testing.T, handler http.Handler, session *http.Cookie, code string) string {
	t.Helper()
	answer := visit(handler, http.MethodPost, "/api/link/status", `{"code":"`+code+`"}`, session)
	var body struct{ Status string }
	_ = json.Unmarshal(answer.Body.Bytes(), &body)
	return body.Status
}

func TestALinkCodeRedeemsOnceIntoANewSessionForTheSameUser(t *testing.T) {
	server := New(nil, Options{})
	creator := ownerSession(t, server)
	handler := server.Handler()
	code := createLink(t, handler, creator)
	if status := linkStatusOf(t, handler, creator, code); status != "waiting" {
		t.Errorf("before it is used: %q", status)
	}

	answer := redeemFrom(handler, "203.0.113.5", strings.ToLower(code))
	linked := cookieNamed(answer, SessionCookie)
	if answer.Code != http.StatusOK || linked == nil {
		t.Fatalf("redeeming: %d %s", answer.Code, answer.Body.String())
	}
	if linked.Value == creator.Value || !linked.HttpOnly || !linked.Secure || linked.SameSite != http.SameSiteLaxMode {
		t.Errorf("the new browser's cookie: %+v", linked)
	}
	if who, ok := server.doors().sessions.find(linked.Value); !ok || who.User != "user-me" {
		t.Errorf("the new session belongs to %q (%v)", who.User, ok)
	}
	for name, session := range map[string]*http.Cookie{"the new session": linked, "the creator's": creator} {
		if got := visit(handler, http.MethodGet, "/no-such-page", "", session); got.Code != http.StatusNotFound {
			t.Errorf("%s on a protected route: %d", name, got.Code)
		}
	}
	if status := linkStatusOf(t, handler, creator, code); status != "linked" {
		t.Errorf("the creator's page is told %q", status)
	}

	again := redeemFrom(handler, "203.0.113.6", code)
	if again.Code != http.StatusBadRequest || cookieNamed(again, SessionCookie) != nil {
		t.Errorf("a second use: %d %s", again.Code, again.Body.String())
	}
}

func TestAnExpiredLinkCodeIsRefused(t *testing.T) {
	server := New(nil, Options{})
	creator := ownerSession(t, server)
	handler := server.Handler()
	code := createLink(t, handler, creator)
	now := time.Now()
	server.doors().links.now = func() time.Time { return now.Add(LinkTTL + time.Second) }
	if answer := redeemFrom(handler, "203.0.113.5", code); answer.Code != http.StatusBadRequest ||
		cookieNamed(answer, SessionCookie) != nil {
		t.Errorf("an expired code: %d %s", answer.Code, answer.Body.String())
	}
	if status := linkStatusOf(t, handler, creator, code); status != "expired" {
		t.Errorf("the creator's page is told %q", status)
	}
}

func TestAStrangersLinkCodeRevealsNothing(t *testing.T) {
	server := New(nil, Options{Allow: "user-me,user-b"})
	mine := ownerSession(t, server)
	theirs := sessionFor(t, server, "user-b")
	handler := server.Handler()
	code := createLink(t, handler, theirs)
	if got, unknown := linkStatusOf(t, handler, mine, code), linkStatusOf(t, handler, mine, "ZZZZ-ZZZZ"); got != unknown {
		t.Errorf("another user's code reads %q, an unknown one %q", got, unknown)
	}
	if status := linkStatusOf(t, handler, theirs, code); status != "waiting" {
		t.Errorf("its own creator: %q", status)
	}

	used := createLink(t, handler, mine)
	redeemFrom(handler, "203.0.113.5", used)
	madeUp := redeemFrom(handler, "203.0.113.6", "ABCD-EFGH")
	spent := redeemFrom(handler, "203.0.113.7", used)
	if madeUp.Code != spent.Code || madeUp.Body.String() != spent.Body.String() {
		t.Errorf("a made-up code %d %s, a spent one %d %s", madeUp.Code, madeUp.Body.String(), spent.Code, spent.Body.String())
	}
}

func TestAStrangersWrongTriesDoNotVoidAnotherUsersCode(t *testing.T) {
	server := New(nil, Options{})
	creator := ownerSession(t, server)
	handler := server.Handler()
	code := createLink(t, handler, creator)
	for try := range LoginAttempts {
		if answer := redeemFrom(handler, "203.0.113.5", "WRNG-MMMM"); answer.Code != http.StatusBadRequest {
			t.Fatalf("wrong try %d: %d", try+1, answer.Code)
		}
	}
	if answer := redeemFrom(handler, "203.0.113.9", code); answer.Code != http.StatusOK ||
		cookieNamed(answer, SessionCookie) == nil {
		t.Errorf("the right code after a stranger's wrong tries: %d %s", answer.Code, answer.Body.String())
	}
}

func TestWrongLinkCodesCountTowardThePerClientLimit(t *testing.T) {
	freshRoot(t)
	handler := New(nil, Options{}).Handler()
	for try := 1; try <= LoginAttempts; try++ {
		if answer := redeemFrom(handler, "203.0.113.5", "NOPE-NOPE"); answer.Code == http.StatusTooManyRequests {
			t.Fatalf("limited at try %d", try)
		}
	}
	if answer := redeemFrom(handler, "203.0.113.5", "NOPE-NOPE"); answer.Code != http.StatusTooManyRequests {
		t.Errorf("try %d: %d, want 429", LoginAttempts+1, answer.Code)
	}
	if answer := submitLogin(handler, "203.0.113.5"); answer != http.StatusTooManyRequests {
		t.Errorf("the login from the same client after the wrong codes: %d", answer)
	}
}

func submitLogin(handler http.Handler, ip string) int {
	request := httptest.NewRequest(http.MethodPost, "http://x/api/session", strings.NewReader(`{"paste":"nope"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://x")
	request.RemoteAddr = ip + ":4000"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func TestAUserHasAtMostThreeLiveLinkCodes(t *testing.T) {
	server := New(nil, Options{})
	creator := ownerSession(t, server)
	handler := server.Handler()
	for range LinksPerUser {
		createLink(t, handler, creator)
	}
	if answer := visit(handler, http.MethodPost, "/api/link", `{}`, creator); answer.Code != http.StatusTooManyRequests {
		t.Errorf("a fourth live code: %d %s", answer.Code, answer.Body.String())
	}
	now := time.Now()
	server.doors().links.now = func() time.Time { return now.Add(LinkTTL) }
	createLink(t, handler, creator)
}

func TestCreatingALinkCodeNeedsASessionAndTheSameOrigin(t *testing.T) {
	server := New(nil, Options{})
	session := ownerSession(t, server)
	handler := server.Handler()
	if answer := visit(handler, http.MethodPost, "/api/link", `{}`); answer.Code != http.StatusUnauthorized {
		t.Errorf("without a session: %d", answer.Code)
	}
	if answer := visit(handler, http.MethodGet, "/api/link", "", session); answer.Code != http.StatusMethodNotAllowed {
		t.Errorf("a GET: %d", answer.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "http://x/api/link", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	request.AddCookie(session)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("from another origin: %d", recorder.Code)
	}
	if answer := visit(handler, http.MethodGet, "/vincular/nuevo", ""); answer.Code == http.StatusOK {
		t.Errorf("the page that makes codes, signed out: %d", answer.Code)
	}
	if answer := visit(handler, http.MethodGet, "/vincular", ""); answer.Code != http.StatusOK ||
		!strings.Contains(answer.Body.String(), "link-code") {
		t.Errorf("the page that takes a code, signed out: %d", answer.Code)
	}
}

func TestAUserNoLongerAdmittedCannotRedeemALinkCode(t *testing.T) {
	server := New(nil, Options{Allow: "user-me"})
	creator := ownerSession(t, server)
	handler := server.Handler()
	code := createLink(t, handler, creator)
	server.opts.Allow = "someone-else"
	if answer := redeemFrom(handler, "203.0.113.5", code); answer.Code != http.StatusForbidden ||
		cookieNamed(answer, SessionCookie) != nil {
		t.Errorf("a user taken off FANTASY_ALLOW: %d %s", answer.Code, answer.Body.String())
	}
}

func TestTheLoginPageCarriesTheStepsForEveryPlatformAndTheLinkBox(t *testing.T) {
	freshRoot(t)
	server := New(nil, Options{})
	page := visit(server.Handler(), http.MethodGet, "/", "").Body.String()
	for _, part := range []string{`data-for="any"`, `data-for="mac" hidden`, `data-for="pc" hidden`,
		`data-for="phone" hidden`, `F12`, `⌘⌥I`, `id="link-code"`} {
		if !strings.Contains(page, part) {
			t.Errorf("the login page lacks %s", part)
		}
	}
	session := ownerSession(t, server)
	adding := visit(server.Handler(), http.MethodGet, "/?cuenta=nueva", "", session).Body.String()
	if !strings.Contains(adding, "Añadir cuenta") || strings.Contains(adding, `id="link-code"`) {
		t.Errorf("adding an account offers a link code: %.200s", adding)
	}
}
