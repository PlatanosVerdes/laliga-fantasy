package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/files"
)

// LeagueChoice is one league the active account plays in.
type LeagueChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type accountView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	// Manager is the name the account plays under in its league: the login's own name is
	// often "Google user".
	Manager string         `json:"manager"`
	Active  bool           `json:"active"`
	Leagues []LeagueChoice `json:"leagues"`
}

// accounts lists the saved accounts and their leagues: the active account's live from the API,
// the others as they were last used, since asking needs their session.
func (s *Server) accounts(writer http.ResponseWriter, _ *http.Request) {
	live := []LeagueChoice{}
	if s.opts.Leagues != nil && config.Account != "" {
		if leagues, err := s.opts.Leagues(); err == nil {
			live = leagues
		} else {
			slog.Warn("leagues unreadable", "reason", err.Error())
		}
	}
	out := []accountView{}
	for _, id := range config.Accounts() {
		view := accountView{ID: id, Active: id == config.Account, Leagues: []LeagueChoice{}}
		var tokens struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if body, err := os.ReadFile(config.AccountTokenFile(id)); err == nil {
			_ = json.Unmarshal(body, &tokens)
		}
		view.Email, view.Name = tokens.Email, tokens.Name
		view.Manager = s.managerName(id)
		known := map[string]bool{}
		if view.Active {
			for _, league := range live {
				view.Leagues = append(view.Leagues, league)
				known[league.ID] = true
			}
		}
		for league, name := range config.AccountLeagues(id) {
			if !known[league] {
				view.Leagues = append(view.Leagues, LeagueChoice{ID: league, Name: name})
			}
		}
		out = append(out, view)
	}
	s.json(writer, http.StatusOK, map[string]any{"account": config.Account,
		"league": config.League, "accounts": out})
}

// managerName is the account's manager name: read from the world for the active account and
// remembered beside its session, so the others show theirs too.
func (s *Server) managerName(account string) string {
	path := filepath.Join(config.AccountDir(account), "profile.json")
	var profile struct {
		Manager string `json:"manager"`
	}
	if body, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(body, &profile)
	}
	if account != config.Account {
		return profile.Manager
	}
	universe := s.state.Universe()
	if universe == nil || universe.MyTeamID == nil {
		return profile.Manager
	}
	if team := universe.LeagueTeams[*universe.MyTeamID]; team != nil && team.Manager != nil &&
		*team.Manager != "" && *team.Manager != profile.Manager {
		profile.Manager = *team.Manager
		if blob, err := json.Marshal(profile); err == nil {
			_ = files.WriteAtomic(path, blob, 0o600)
		}
	}
	return profile.Manager
}

// useAccount switches to another account or league. Every path in the process points at the
// active one, so the switch is a restart: nothing reads a half-switched profile.
func (s *Server) useAccount(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	body := s.body(request)
	account, league := text(body["account"]), text(body["league"])
	if account == "" {
		account = config.Account
	}
	saved := false
	for _, id := range config.Accounts() {
		saved = saved || id == account
	}
	if !saved {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "esa cuenta no esta guardada"})
		return
	}
	if strings.ContainsAny(league, `/\.`) {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "liga no valida"})
		return
	}
	if err := config.SetActive(account, league); err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("profile switched", "account", account, "league", league)
	s.json(writer, http.StatusOK, map[string]any{"ok": true, "restarting": true})
	s.restartSoon()
}

// logout forgets an account's session. Its leagues' instructions and history stay, for when
// it logs in again. The panel moves to another saved account, or to the login page.
func (s *Server) logout(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	account := text(s.body(request)["account"])
	if account == "" {
		account = config.Account
	}
	saved := false
	for _, id := range config.Accounts() {
		saved = saved || id == account
	}
	if !saved {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "esa cuenta no esta guardada"})
		return
	}
	if err := os.Remove(config.AccountTokenFile(account)); err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	slog.Info("logged out", "account", account)
	if account != config.Account {
		s.json(writer, http.StatusOK, map[string]any{"ok": true})
		return
	}
	next, league := "", ""
	if left := config.Accounts(); len(left) > 0 {
		next = left[0]
		for id := range config.AccountLeagues(next) {
			league = id
			break
		}
	}
	if err := config.SetActive(next, league); err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.json(writer, http.StatusOK, map[string]any{"ok": true, "restarting": true})
	s.restartSoon()
}

// AvatarStyle is the DiceBear style the accounts are drawn in.
const AvatarStyle = "thumbs"

// avatar is an account's picture, from DiceBear, fetched once and kept: the panel does not
// depend on it after the first time, and what it is sent is a hash of the account, not the email.
func (s *Server) avatar(writer http.ResponseWriter, request *http.Request) {
	account := request.URL.Query().Get("account")
	saved := false
	for _, id := range config.Accounts() {
		saved = saved || id == account
	}
	if !saved {
		http.NotFound(writer, request)
		return
	}
	path := filepath.Join(config.CacheRoot, "avatars", AvatarStyle+"-"+account+".svg")
	body, err := os.ReadFile(path)
	if err != nil {
		if body, err = fetchAvatar(account); err != nil {
			slog.Warn("avatar unavailable", "reason", err.Error())
			http.NotFound(writer, request)
			return
		}
		if os.MkdirAll(filepath.Dir(path), 0o700) == nil {
			_ = files.WriteAtomic(path, body, 0o600)
		}
	}
	// A picture from somebody else served from this origin: as an image it runs nothing, and
	// opened on its own the policy stops any script it might carry.
	writer.Header().Set("Content-Type", "image/svg+xml")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Cache-Control", "max-age=86400")
	writer.Write(body)
}

func fetchAvatar(account string) ([]byte, error) {
	sum := sha256.Sum256([]byte(account))
	url := fmt.Sprintf("https://api.dicebear.com/9.x/%s/svg?seed=%s", AvatarStyle,
		hex.EncodeToString(sum[:8]))
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		!strings.HasPrefix(response.Header.Get("Content-Type"), "image/svg+xml") {
		return nil, fmt.Errorf("dicebear answered %d %s", response.StatusCode,
			response.Header.Get("Content-Type"))
	}
	return io.ReadAll(io.LimitReader(response.Body, 64<<10))
}

// restartSoon restarts after the answer has left, so the page hears the switch went through.
func (s *Server) restartSoon() {
	if s.opts.Restart == nil {
		return
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.opts.Restart()
	}()
}

// leaguePicker is the page shown when the account plays in more than one league and none has
// been chosen yet.
func (s *Server) leaguePicker(writer http.ResponseWriter) {
	leagues := []LeagueChoice{}
	if s.opts.Leagues != nil {
		if found, err := s.opts.Leagues(); err == nil {
			leagues = found
		}
	}
	var buttons strings.Builder
	for _, league := range leagues {
		fmt.Fprintf(&buttons, `<button type="button" data-league="%s">%s<span>→</span></button>`,
			html.EscapeString(league.ID), html.EscapeString(fallbackName(league)))
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(writer, authPage("Elige liga", strings.Replace(leagueBody, "{{leagues}}", buttons.String(), 1)))
}

func fallbackName(league LeagueChoice) string {
	if league.Name != "" {
		return league.Name
	}
	return league.ID
}

// leagueFailed is the page for a league that cannot be read: what went wrong and where to go.
func (s *Server) leagueFailed(writer http.ResponseWriter, reason string) {
	var buttons strings.Builder
	if s.opts.Leagues != nil {
		if leagues, err := s.opts.Leagues(); err == nil {
			for _, league := range leagues {
				if league.ID != config.League {
					fmt.Fprintf(&buttons, `<button type="button" data-league="%s">%s<span>→</span></button>`,
						html.EscapeString(league.ID), html.EscapeString(fallbackName(league)))
				}
			}
		}
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(writer, authPage("Liga no disponible", strings.NewReplacer(
		"{{reason}}", html.EscapeString(plainReason(reason)), "{{leagues}}", buttons.String()).Replace(failedBody)))
}

var httpStatus = regexp.MustCompile(`http (\d{3})`)

// plainReason is the build error in words: the raw one carries the API's whole address.
func plainReason(reason string) string {
	switch status := httpStatus.FindStringSubmatch(reason); {
	case status == nil:
		return reason
	case status[1] == "404":
		return "LaLiga responde 404: esa liga no existe o ya no estás en ella."
	case status[1] == "401" || status[1] == "403":
		return "LaLiga no te deja verla (" + status[1] + "): la sesión no tiene acceso a esta liga."
	default:
		return "LaLiga responde " + status[1] + "."
	}
}

const failedBody = `<div class="centered"><h1>No puedo leer esta liga</h1><div class="sad-ball" aria-hidden="true">
<svg viewBox="0 0 220 140" width="220" height="140">
 <defs><clipPath id="ballclip"><circle cx="88" cy="90" r="34"/></clipPath></defs>
 <ellipse cx="88" cy="128" rx="44" ry="6" fill="var(--line)"/>
 <g class="ball">
  <circle cx="88" cy="90" r="34" fill="#fff"/>
  <g clip-path="url(#ballclip)" fill="#1b1b1b"><polygon points="102.1,70.6 98.5,59.4 108.0,52.5 117.5,59.4 113.9,70.6"/><polygon points="110.8,97.4 120.3,90.5 129.8,97.4 126.2,108.6 114.5,108.6"/><polygon points="88.0,114.0 97.5,120.9 93.9,132.1 82.1,132.1 78.5,120.9"/><polygon points="65.2,97.4 61.5,108.6 49.8,108.6 46.2,97.4 55.7,90.5"/><polygon points="73.9,70.6 62.1,70.6 58.5,59.4 68.0,52.5 77.5,59.4"/></g>
  <path d="M88.0 79.0L88.0 56.0M98.5 86.6L120.3 79.5M94.5 98.9L108.0 117.5M81.5 98.9L68.0 117.5M77.5 86.6L55.7 79.5" stroke="#1b1b1b" stroke-width="2"/>
  <polygon points="88.0,79.0 98.5,86.6 94.5,98.9 81.5,98.9 77.5,86.6" fill="#1b1b1b"/>
  <circle cx="88" cy="90" r="34" fill="none" stroke="var(--ink)" stroke-width="3"/>
 </g>
 <g class="puff" fill="none" stroke="var(--muted)" stroke-width="2.5" stroke-linecap="round">
  <path d="M46 84q-8-4-14 0"/><path d="M44 96q-10 0-16 6"/><path d="M48 72q-6-8-14-8"/>
 </g>
 <g stroke="var(--ink)" stroke-width="3.5" stroke-linecap="round" fill="none">
  <circle cx="172" cy="56" r="9" fill="var(--surface)"/>
  <path d="M172 66v34M172 100l-9 26M172 100l9 26M172 76l-12 14M172 76l14-20"/>
 </g>
 <rect class="card" x="180" y="22" width="14" height="20" rx="2" fill="var(--critical)" transform="rotate(10 187 32)"/>
</svg></div>
<style>
.sad-ball{display:flex;justify-content:center;margin:8px 0 10px}
.sad-ball .ball{animation:flop 2.4s ease-in-out infinite;transform-origin:88px 124px;transform:scaleY(.9)}
.sad-ball .puff{animation:hiss 2.4s ease-in-out infinite}
.sad-ball .card{animation:blink 2.4s ease-in-out infinite}
@keyframes flop{0%,100%{transform:scaleY(.9)}45%{transform:scaleY(.82) translateY(3px)}}
@keyframes hiss{0%,100%{opacity:.2;transform:translateX(0)}45%{opacity:1;transform:translateX(-4px)}}
@keyframes blink{0%,100%{opacity:1}50%{opacity:.7}}
@media (prefers-reduced-motion:reduce){.sad-ball .ball,.sad-ball .card,.sad-ball .puff{animation:none}}
</style>
<p class="lead">LaLiga no la devuelve: puede que ya no estés en ella. Elige otra o cambia de cuenta.</p>
<div class="choices">{{leagues}}</div>
<p class="lead" style="margin-top:16px;font-size:13px">Detalle: {{reason}}</p>
<div class="btn-row"><a class="btn quiet" href="/?cuenta=nueva">Entrar con otra cuenta</a><button class="btn quiet" id="out" type="button">Cerrar sesión</button></div><div id="msg"></div></div>
<script>
const go=(url,body)=>{document.querySelectorAll('.auth button').forEach((x)=>x.disabled=true);
  switchTo(url,body);};
document.querySelectorAll('[data-league]').forEach((b)=>b.onclick=()=>go('/api/accounts/use',{league:b.dataset.league}));
document.getElementById('out').onclick=()=>go('/api/accounts/logout',{});
</script>`

const leagueBody = `<h1>¿Con qué liga trabajo?</h1>
<p class="lead">Tu cuenta está en varias. Podrás cambiar cuando quieras desde el menú de arriba.</p>
<div class="choices">{{leagues}}</div><div id="msg"></div>
<script>
document.querySelectorAll('[data-league]').forEach((b)=>b.onclick=async()=>{
  document.querySelectorAll('[data-league]').forEach((x)=>x.disabled=true);
  switchTo('/api/accounts/use',{league:b.dataset.league});
});
</script>`
