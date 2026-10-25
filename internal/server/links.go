package server

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/qr"
)

const (
	LinkTTL      = 2 * time.Minute
	LinksPerUser = 3
	linkAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	linkLength   = 8
)

var errTooManyLinks = errors.New("too many live link codes")

type link struct {
	user, email string
	expires     time.Time
	used        bool
}

func (l *link) live(now time.Time) bool {
	return !l.used && now.Before(l.expires)
}

// Memory only, keyed by the code's SHA-256: a restart drops every code.
type deviceLinks struct {
	mu    sync.Mutex
	now   func() time.Time
	byKey map[string]*link
}

func newDeviceLinks() *deviceLinks {
	return &deviceLinks{now: time.Now, byKey: map[string]*link{}}
}

func linkCode() (string, error) {
	raw := make([]byte, linkLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	for i, b := range raw {
		raw[i] = linkAlphabet[int(b)%len(linkAlphabet)]
	}
	return string(raw), nil
}

func normalCode(typed string) string {
	var code strings.Builder
	for _, r := range strings.ToUpper(typed) {
		if strings.ContainsRune(linkAlphabet, r) {
			code.WriteRune(r)
		}
	}
	return code.String()
}

func grouped(code string) string { return code[:4] + "-" + code[4:] }

func (d *deviceLinks) prune(now time.Time) {
	for key, entry := range d.byKey {
		if now.Sub(entry.expires) >= LinkTTL {
			delete(d.byKey, key)
		}
	}
}

func (d *deviceLinks) create(user, email string) (string, error) {
	code, err := linkCode()
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	d.prune(now)
	live := 0
	for _, entry := range d.byKey {
		if entry.user == user && entry.live(now) {
			live++
		}
	}
	if live >= LinksPerUser {
		return "", errTooManyLinks
	}
	d.byKey[hashOf(code)] = &link{user: user, email: email, expires: now.Add(LinkTTL)}
	return code, nil
}

// A miss burns no code, or anyone could void everyone's: the per-IP limit stops guessing.
func (d *deviceLinks) redeem(typed string) (link, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	d.prune(now)
	code := normalCode(typed)
	if entry, ok := d.byKey[hashOf(code)]; ok && len(code) == linkLength && entry.live(now) {
		entry.used = true
		return *entry, true
	}
	return link{}, false
}

func (d *deviceLinks) status(typed, user string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	entry, ok := d.byKey[hashOf(normalCode(typed))]
	switch {
	case !ok || entry.user != user:
		return "expired"
	case entry.used:
		return "linked"
	case entry.live(now):
		return "waiting"
	}
	return "expired"
}

func (a *attempts) limited(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	recent := 0
	for _, at := range a.byIP[ip] {
		if now.Sub(at) < a.every {
			recent++
		}
	}
	return recent >= a.limit
}

func linkURL(request *http.Request, code string) string {
	base := "https://" + request.Host
	if localHost(request) {
		base = "http://" + request.Host
	}
	if source := request.Header.Get("Origin"); fromHere(source, request) {
		if parsed, err := url.Parse(source); err == nil {
			base = parsed.Scheme + "://" + parsed.Host
		}
	}
	return base + "/vincular#" + grouped(code)
}

func (s *Server) createLink(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	who, ok := visitorOf(request)
	if !ok {
		s.json(writer, http.StatusUnauthorized, map[string]any{"error": "inicia sesion para seguir", "login": true})
		return
	}
	code, err := s.doors().links.create(who.User, who.Email)
	if errors.Is(err, errTooManyLinks) {
		s.json(writer, http.StatusTooManyRequests, map[string]any{
			"error": fmt.Sprintf("ya tienes %d códigos sin usar: espera a que caduquen", LinksPerUser)})
		return
	}
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	address := linkURL(request, code)
	picture, err := qr.Encode([]byte(address))
	if err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("device link created", "user", who.User)
	s.json(writer, http.StatusOK, map[string]any{"code": grouped(code), "url": address,
		"qr": picture.SVG(), "expires_in": int(LinkTTL.Seconds())})
}

func (s *Server) linkStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	who, ok := visitorOf(request)
	if !ok {
		s.json(writer, http.StatusUnauthorized, map[string]any{"error": "inicia sesion para seguir", "login": true})
		return
	}
	code := text(s.body(request)["code"])
	s.json(writer, http.StatusOK, map[string]any{"status": s.doors().links.status(code, who.User)})
}

func (s *Server) redeemLink(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	doors, ip := s.doors(), clientIP(request)
	if doors.attempts.limited(ip) {
		slog.Warn("device link attempts limited", "ip", ip)
		writer.Header().Set("Retry-After", fmt.Sprint(int(LoginWindow.Seconds())))
		s.json(writer, http.StatusTooManyRequests, map[string]any{
			"error": "demasiados intentos: espera unos minutos y vuelve a probar"})
		return
	}
	entry, ok := doors.links.redeem(text(s.body(request)["code"]))
	if !ok {
		doors.attempts.allow(ip)
		slog.Warn("device link refused", "ip", ip, "reason", "unknown, used, expired or locked")
		s.json(writer, http.StatusBadRequest, map[string]any{
			"error": "ese código no sirve: puede haber caducado o estar ya usado. Pide otro"})
		return
	}
	if !admitted(s.opts.Allow, entry.user, entry.email) {
		slog.Warn("device link refused", "user", entry.user, "reason", "no longer admitted")
		s.json(writer, http.StatusForbidden, map[string]any{"refused": true,
			"error": "esa cuenta ya no tiene acceso a este panel"})
		return
	}
	if err := s.signIn(writer, request, entry.user, entry.email); err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("device linked", "user", entry.user)
	s.json(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) linkPage(writer http.ResponseWriter, request *http.Request) {
	_, signed := visitorOf(request)
	body := linkInBody
	if signed {
		body = strings.Replace(body, "{{other}}",
			`<p class="aside">¿Quieres traer otro dispositivo a este? <a href="/vincular/nuevo">Vincular un dispositivo</a></p>`, 1)
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(writer, authPage("Vincular este dispositivo", strings.Replace(body, "{{other}}", "", 1)))
}

// The QR is a window over the panel now; the old address opens it there.
func (s *Server) newLinkPage(writer http.ResponseWriter, request *http.Request) {
	http.Redirect(writer, request, "/?abrir=vincular", http.StatusSeeOther)
}

const linkInBody = `<h1>Vincular este dispositivo</h1>
<p class="lead">El código del otro dispositivo (menú de la cuenta → Vincular un dispositivo): escríbelo o escanea su QR con el botón de la cámara.</p>
<div class="code-row"><input class="field code" id="link-code" name="device-link" aria-label="Código de vinculación" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" data-1p-ignore data-lpignore="true" data-bwignore placeholder="ABCD-EFGH" maxlength="9"><button class="btn" id="link-go">Vincular</button></div>
<div class="msg" id="link-msg"></div>{{other}}
<a class="back" href="/">← Volver</a>
<script>
const box=document.getElementById('link-code'), go=document.getElementById('link-go');
// Taken from the fragment and wiped from the address, but only sent on a click: a link alone never signs anybody in.
if(location.hash.length>1){box.value=decodeURIComponent(location.hash.slice(1));history.replaceState(null,'',location.pathname);}
go.onclick=()=>linkHere(box.value,document.getElementById('link-msg'),go);
box.onkeydown=(e)=>{if(e.key==='Enter') go.click();};
</script>`
