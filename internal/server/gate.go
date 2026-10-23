package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

type visitorKey struct{}

// Both doors' handlers must share one gate, or a session opened on one is unknown to the other.
type gate struct {
	sessions *sessionStore
	logins   *pendingLogins
	attempts *attempts
	links    *deviceLinks
}

func (s *Server) doors() *gate {
	s.gateOnce.Do(func() {
		path := ""
		if config.ConfigRoot != "" {
			path = filepath.Join(config.ConfigRoot, "sessions.json")
		}
		s.gate = &gate{sessions: newSessionStore(path), logins: newPendingLogins(),
			attempts: newAttempts(LoginAttempts, LoginWindow), links: newDeviceLinks()}
	})
	return s.gate
}

func signedOut(path string) bool {
	switch path {
	case "/healthz", "/api/health", "/metrics", "/favicon.ico", "/apple-touch-icon.png", "/api/session", "/api/logout",
		"/vincular", "/api/link/redeem":
		return true
	}
	return strings.HasPrefix(path, "/assets/")
}

var anyProfile = map[string]bool{
	"/": true, "/api/accounts": true, "/api/accounts/use": true, "/api/accounts/logout": true,
	"/api/accounts/avatar": true, "/api/devices": true, "/vincular/nuevo": true, "/api/link": true, "/api/link/status": true,
}

func visitorOf(request *http.Request) (visitor, bool) {
	who, ok := request.Context().Value(visitorKey{}).(visitor)
	return who, ok
}

func owns(who visitor, account string) bool {
	return account != "" && who.User != "" && auth.AccountUser(account) == who.User
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		doors := s.doors()
		who, ok := doors.sessions.find(cookieValue(request, SessionCookie))
		if ok && !admitted(s.opts.Allow, who.User, who.Email) {
			slog.Warn("session of a user no longer admitted", "user", who.User)
			doors.sessions.close(who.key)
			ok = false
		}
		if ok {
			doors.sessions.note(who.key, deviceOf(request), placeOf(request))
			request = request.WithContext(context.WithValue(request.Context(), visitorKey{}, who))
		}
		// Cleaned, so /assets/../api/state is judged as the route it reaches.
		path := path.Clean("/" + request.URL.Path)
		switch {
		case signedOut(path):
		case !ok && path == "/":
			s.setup(writer, request)
			return
		case !ok:
			s.json(writer, http.StatusUnauthorized, map[string]any{
				"error": "inicia sesion para seguir", "login": true})
			return
		case !anyProfile[path] && !owns(who, config.Account):
			s.json(writer, http.StatusForbidden, map[string]any{
				"error": "el panel esta siguiendo la cuenta de otra persona", "foreign": true})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(writer, request)
			return
		}
		source := request.Header.Get("Origin")
		if source == "" {
			source = request.Header.Get("Referer")
		}
		if !fromHere(source, request) {
			slog.Warn("cross-origin write refused", "path", request.URL.Path, "origin", source)
			http.Error(writer, `{"error":"origen no permitido"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func fromHere(source string, request *http.Request) bool {
	parsed, err := url.Parse(source)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	if strings.EqualFold(parsed.Host, request.Host) {
		return true
	}
	forwarded := request.Header.Get("X-Forwarded-Host")
	return forwarded != "" && trustedProxy(remoteHost(request)) && strings.EqualFold(parsed.Host, forwarded)
}

func (s *Server) signIn(writer http.ResponseWriter, request *http.Request, user, email string) error {
	doors := s.doors()
	if previous, ok := visitorOf(request); ok {
		doors.sessions.close(previous.key)
	}
	id, opened, err := doors.sessions.open(user, email)
	if err != nil {
		return err
	}
	doors.sessions.note(opened.key, deviceOf(request), placeOf(request))
	http.SetCookie(writer, cookie(request, SessionCookie, id, SessionLifetime))
	return nil
}

// The LaLiga tokens stay: the automatic engine still runs on them.
func (s *Server) signOut(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	var body struct {
		Everywhere bool `json:"everywhere"`
	}
	_ = json.NewDecoder(request.Body).Decode(&body)
	if who, ok := visitorOf(request); ok && body.Everywhere {
		slog.Info("signed out everywhere", "user", who.User, "sessions", s.doors().sessions.closeUser(who.User))
	} else if ok {
		s.doors().sessions.close(who.key)
		slog.Info("signed out", "user", who.User)
	}
	forget(writer, request, SessionCookie)
	s.json(writer, http.StatusOK, map[string]any{"ok": true})
}

