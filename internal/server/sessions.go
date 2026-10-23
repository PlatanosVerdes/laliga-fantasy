package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/files"
)

const (
	SessionCookie   = "fantasy_session"
	LoginCookie     = "fantasy_login"
	SessionIdle     = 30 * 24 * time.Hour
	SessionLifetime = 90 * 24 * time.Hour
	LoginAttempts   = 10
	LoginWindow     = 10 * time.Minute
	seenEvery       = 10 * time.Minute
	maxPending      = 256
)

type visitor struct {
	User    string `json:"user"`
	Email   string `json:"email,omitempty"`
	Created int64  `json:"created"`
	Seen    int64  `json:"seen"`
	Device  string `json:"device,omitempty"`
	Place   place  `json:"place,omitempty"`
	key     string
}

// place is where a session was last used from. The country is a code: the browser names it.
type place struct {
	City    string `json:"city,omitempty"`
	Region  string `json:"region,omitempty"`
	Country string `json:"country,omitempty"`
	Network string `json:"network,omitempty"`
}

// Keyed by the SHA-256 of the id, so the file alone signs nobody in.
type sessionStore struct {
	mu     sync.Mutex
	path   string
	now    func() time.Time
	loaded bool
	byKey  map[string]*visitor
}

func newSessionStore(path string) *sessionStore {
	return &sessionStore{path: path, now: time.Now, byKey: map[string]*visitor{}}
}

func hashOf(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func randomID() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *sessionStore) load() {
	if s.loaded || s.path == "" {
		s.loaded = true
		return
	}
	s.loaded = true
	body, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	stored := map[string]*visitor{}
	if err := json.Unmarshal(body, &stored); err != nil {
		slog.Warn("sessions unreadable, starting empty", "reason", err.Error())
		return
	}
	for key, who := range stored {
		who.key = key
		if s.live(who) {
			s.byKey[key] = who
		}
	}
}

func (s *sessionStore) save() {
	if s.path == "" {
		return
	}
	blob, err := json.Marshal(s.byKey)
	if err == nil {
		if err = os.MkdirAll(filepath.Dir(s.path), 0o700); err == nil {
			err = files.WriteAtomic(s.path, blob, 0o600)
		}
	}
	if err != nil {
		slog.Error("sessions not saved", "reason", err.Error())
	}
}

func (s *sessionStore) live(who *visitor) bool {
	now := s.now()
	return now.Sub(time.Unix(who.Seen, 0)) < SessionIdle &&
		now.Sub(time.Unix(who.Created, 0)) < SessionLifetime
}

func (s *sessionStore) open(user, email string) (string, *visitor, error) {
	id, err := randomID()
	if err != nil {
		return "", nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	now := s.now().Unix()
	who := &visitor{User: user, Email: email, Created: now, Seen: now, key: hashOf(id)}
	s.byKey[who.key] = who
	s.save()
	return id, who, nil
}

func (s *sessionStore) find(id string) (visitor, bool) {
	if id == "" {
		return visitor{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	key := hashOf(id)
	who, ok := s.byKey[key]
	if !ok {
		return visitor{}, false
	}
	if !s.live(who) {
		delete(s.byKey, key)
		s.save()
		return visitor{}, false
	}
	now := s.now()
	if now.Sub(time.Unix(who.Seen, 0)) >= seenEvery {
		who.Seen = now.Unix()
		s.save()
	}
	return *who, true
}

// note records where a session was last used from, writing only when that changed.
func (s *sessionStore) note(key, device string, where place) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	who, ok := s.byKey[key]
	if !ok || (who.Device == device && who.Place == where) {
		return
	}
	who.Device, who.Place = device, where
	s.save()
}

// listUser is the user's live sessions, the most recently used first.
func (s *sessionStore) listUser(user string) []visitor {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	var out []visitor
	for _, who := range s.byKey {
		if who.User == user && s.live(who) {
			out = append(out, *who)
		}
	}
	slices.SortFunc(out, func(a, b visitor) int { return int(b.Seen - a.Seen) })
	return out
}

func (s *sessionStore) closeUser(user string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	closed := 0
	for key, who := range s.byKey {
		if who.User == user {
			delete(s.byKey, key)
			closed++
		}
	}
	if closed > 0 {
		s.save()
	}
	return closed
}

func (s *sessionStore) close(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if _, ok := s.byKey[key]; ok {
		delete(s.byKey, key)
		s.save()
	}
}

type pendingLogins struct {
	mu    sync.Mutex
	now   func() time.Time
	byKey map[string]*auth.Pending
}

func newPendingLogins() *pendingLogins {
	return &pendingLogins{now: time.Now, byKey: map[string]*auth.Pending{}}
}

func (p *pendingLogins) fresh(pending *auth.Pending) bool {
	return p.now().Sub(time.Unix(int64(pending.Started), 0)) < auth.PendingTTL
}

func (p *pendingLogins) start() (string, *auth.Pending, error) {
	pending, err := auth.NewLogin()
	if err != nil {
		return "", nil, err
	}
	pending.Started = float64(p.now().Unix())
	id, err := randomID()
	if err != nil {
		return "", nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, other := range p.byKey {
		if !p.fresh(other) {
			delete(p.byKey, key)
		}
	}
	for len(p.byKey) >= maxPending {
		oldest := ""
		for key, other := range p.byKey {
			if oldest == "" || other.Started < p.byKey[oldest].Started {
				oldest = key
			}
		}
		delete(p.byKey, oldest)
	}
	p.byKey[hashOf(id)] = pending
	return id, pending, nil
}

func (p *pendingLogins) find(id string) (*auth.Pending, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pending, ok := p.byKey[hashOf(id)]
	if !ok || !p.fresh(pending) {
		return nil, false
	}
	return pending, true
}

func (p *pendingLogins) drop(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.byKey, hashOf(id))
}

type attempts struct {
	mu    sync.Mutex
	now   func() time.Time
	byIP  map[string][]time.Time
	limit int
	every time.Duration
}

func newAttempts(limit int, every time.Duration) *attempts {
	return &attempts{now: time.Now, byIP: map[string][]time.Time{}, limit: limit, every: every}
}

func (a *attempts) allow(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	recent := slices.DeleteFunc(a.byIP[ip], func(at time.Time) bool { return now.Sub(at) >= a.every })
	if len(recent) >= a.limit {
		a.byIP[ip] = recent
		return false
	}
	a.byIP[ip] = append(recent, now)
	for other, times := range a.byIP {
		if len(times) == 0 || now.Sub(times[len(times)-1]) >= a.every {
			delete(a.byIP, other)
		}
	}
	return true
}

func trustedProxy(address string) bool {
	ip := net.ParseIP(address)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func remoteHost(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

// Read from the right and only past trusted proxies, so a client cannot pick its own address.
func clientIP(request *http.Request) string {
	address := remoteHost(request)
	if !trustedProxy(address) {
		return address
	}
	hops := strings.Split(strings.Join(request.Header.Values("X-Forwarded-For"), ","), ",")
	for index := len(hops) - 1; index >= 0; index-- {
		hop := strings.TrimSpace(hops[index])
		if net.ParseIP(hop) == nil {
			break
		}
		address = hop
		if !trustedProxy(hop) {
			break
		}
	}
	return address
}

func localHost(request *http.Request) bool {
	host := request.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func cookie(request *http.Request, name, value string, maxAge time.Duration) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true,
		Secure: !localHost(request), SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds())}
}

func forget(writer http.ResponseWriter, request *http.Request, name string) {
	gone := cookie(request, name, "", 0)
	gone.MaxAge = -1
	http.SetCookie(writer, gone)
}

func cookieValue(request *http.Request, name string) string {
	if found, err := request.Cookie(name); err == nil {
		return found.Value
	}
	return ""
}

func ownersFile() string { return filepath.Join(config.ConfigRoot, "owners.json") }

// Recorded so that logging out of every account does not open the install to the next stranger.
func owners() []string {
	var stored struct {
		Users []string `json:"users"`
	}
	if body, err := os.ReadFile(ownersFile()); err == nil {
		_ = json.Unmarshal(body, &stored)
	}
	return stored.Users
}

func addOwner(user string) error {
	known := owners()
	if slices.Contains(known, user) {
		return nil
	}
	blob, err := json.MarshalIndent(map[string]any{"users": append(known, user)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.ConfigRoot, 0o700); err != nil {
		return err
	}
	return files.WriteAtomic(ownersFile(), blob, 0o600)
}

func accountUsers() map[string]bool {
	users := map[string]bool{}
	for _, account := range config.Accounts() {
		if user := auth.AccountUser(account); user != "" {
			users[user] = true
		}
	}
	return users
}

func admitted(rule, user, email string) bool {
	if user == "" {
		return false
	}
	if rule = strings.TrimSpace(rule); rule != "" {
		for _, entry := range strings.Split(rule, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "*" || entry == user || (email != "" && strings.EqualFold(entry, email)) {
				return true
			}
		}
		return false
	}
	known := owners()
	if slices.Contains(known, user) {
		return true
	}
	users := accountUsers()
	return users[user] || (len(known) == 0 && len(users) == 0)
}
