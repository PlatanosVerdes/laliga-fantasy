// Session: the first-run flow. A fresh deploy has no tokens.json, and copying one in by hand
// means ssh and sudo on the box — so the page itself asks for the login.
package server

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/files"
)

// HasSession reports whether there is anything to authenticate with. A stored refresh token
// is enough: the access token is renewed on first use.
func HasSession() bool {
	tokens, err := auth.Load()
	// A session given up on is shown as none, so the page asks for the login that restarts it.
	return err == nil && tokens != nil && !tokens.Stopped() &&
		(tokens.RefreshToken != "" || tokens.AccessToken != "")
}

var exchange = auth.Exchange

// setup is the page shown when there is no session: the login link and a box to paste the
// address bar into. The redirect goes to authredirect://com.lfp.laligafantasy, which the
// browser cannot open — it just leaves the code in the address bar, which is the whole trick.
func (s *Server) setup(writer http.ResponseWriter, request *http.Request) {
	logins := s.doors().logins
	id := cookieValue(request, LoginCookie)
	pending, ok := logins.find(id)
	if !ok || request.URL.Query().Get("nuevo") != "" {
		var err error
		id, pending, err = logins.start()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		http.SetCookie(writer, cookie(request, LoginCookie, id, auth.PendingTTL))
	}

	_, signed := visitorOf(request)
	adding := request.URL.Query().Get("cuenta") == "nueva" && signed
	title, lead, back := "Inicia sesión", "Entra con tu cuenta de LaLiga Fantasy y el panel se pone en marcha.", ""
	if adding {
		title, lead = "Añadir cuenta", "Entra con otra cuenta de LaLiga Fantasy. Queda guardada y el panel pasa a usarla."
		back = `<a class="back" href="/">← Volver al panel</a>`
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(writer, authPage(title, strings.NewReplacer("{{title}}", html.EscapeString(title),
		"{{lead}}", html.EscapeString(lead), "{{login}}", html.EscapeString(pending.URL),
		"{{back}}", back).Replace(setupBody)))
}

// session finishes the login: it accepts the pasted redirect URL, a bare code, or a whole
// tokens.json, because the fastest path out of a broken session should never be blocked on
// which of the three you happen to have.
func (s *Server) session(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	if !s.doors().attempts.allow(clientIP(request)) {
		slog.Warn("login attempts limited", "ip", clientIP(request))
		writer.Header().Set("Retry-After", fmt.Sprint(int(LoginWindow.Seconds())))
		s.json(writer, http.StatusTooManyRequests, map[string]any{
			"error": "demasiados intentos: espera unos minutos y vuelve a probar"})
		return
	}
	body := s.body(request)
	pasted := strings.TrimSpace(text(body["paste"]))
	if pasted == "" {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "no has pegado nada"})
		return
	}
	if strings.HasPrefix(pasted, "{") {
		s.adopt(writer, request, pasted, body)
		return
	}

	id := cookieValue(request, LoginCookie)
	pending, ok := s.doors().logins.find(id)
	if !ok {
		s.json(writer, http.StatusBadRequest, map[string]any{
			"error": "este navegador no ha empezado ningun login: recarga la pagina y usa el enlace"})
		return
	}
	code, err := auth.ExtractCode(pasted, pending.State)
	if err != nil {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.doors().logins.drop(id)
	forget(writer, request, LoginCookie)
	tokens, err := exchange(code, pending.Verifier)
	if err != nil {
		s.json(writer, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	user := tokens.User()

	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if !admitted(s.opts.Allow, user, tokens.Email) {
		slog.Warn("login refused", "user", user, "email", tokens.Email)
		s.json(writer, http.StatusForbidden, map[string]any{"refused": true,
			"error": "La cuenta " + tokens.Email + " no tiene acceso a este panel. " +
				"Pide a quien lo aloja que la añada a FANTASY_ALLOW."})
		return
	}
	account := config.AccountID(tokens.Email)
	if held := auth.AccountUser(account); held != "" && held != user {
		s.json(writer, http.StatusConflict, map[string]any{
			"error": "esa cuenta ya esta guardada para otro usuario de LaLiga"})
		return
	}
	if _, err := auth.Keep(tokens); err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if strings.TrimSpace(s.opts.Allow) == "" {
		if err := addOwner(user); err != nil {
			slog.Error("owner not recorded", "reason", err.Error())
		}
	}
	if err := s.signIn(writer, request, user, tokens.Email); err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.sessionReady(writer, tokens.Email, user)
}

// A pasted tokens.json cannot be verified offline, so it only mends the signed-in user's account.
func (s *Server) adopt(writer http.ResponseWriter, request *http.Request, pasted string, body map[string]any) {
	who, signed := visitorOf(request)
	if !signed {
		s.json(writer, http.StatusUnauthorized, map[string]any{
			"error": "entra con el enlace de LaLiga; pegar un tokens.json solo sirve para reparar tu cuenta"})
		return
	}
	// A working session is replaced only on purpose: a stray post used to overwrite it, and a
	// failed check then deleted the file.
	if HasSession() && !truthy(body["replace"]) && !truthy(body["add"]) {
		s.json(writer, http.StatusConflict, map[string]any{
			"error": "ya hay una sesion que funciona; para cambiarla manda replace: true"})
		return
	}
	tokens, err := auth.Parse(pasted)
	if err != nil {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	account := config.AccountID(tokens.Email)
	if tokens.User() != who.User || (auth.AccountUser(account) != "" && auth.AccountUser(account) != who.User) {
		s.json(writer, http.StatusForbidden, map[string]any{"error": "ese tokens.json no es de tu cuenta"})
		return
	}
	file := config.AccountTokenFile(account)
	previous, readErr := os.ReadFile(file)
	restore := func() {
		if readErr == nil {
			_ = files.WriteAtomic(file, previous, 0o600)
		} else {
			_ = os.Remove(file)
		}
	}
	if _, err := auth.Keep(tokens); err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	// Prove it works before saying it does: a pasted session that cannot read anything
	// looks identical to a good one until the first request fails, hours later. Only the
	// active account can be asked from here; another one is proved by starting on it.
	if s.opts.Client != nil && account == config.Account {
		if _, err := s.opts.Client.Me(0); err != nil {
			restore()
			s.json(writer, http.StatusBadRequest,
				map[string]any{"error": "esa sesion no sirve: " + err.Error()})
			return
		}
	}
	s.sessionReady(writer, tokens.Email, who.User)
}

// Another user's profile is left running: switching it away is that page's explicit choice.
func (s *Server) sessionReady(writer http.ResponseWriter, who, user string) {
	slog.Info("session stored", "email", who)
	account := config.AccountID(who)
	if account == config.Account && config.League != "" {
		// The world is empty until something reads the API with the new session, and that
		// read can take a while, so it happens in the background and the page waits on /healthz.
		if s.opts.Adopted != nil {
			go s.opts.Adopted()
		}
		s.json(writer, http.StatusOK, map[string]any{"ok": true, "email": who})
		return
	}
	if held := auth.AccountUser(config.Account); config.Account != account && held != "" && held != user {
		s.json(writer, http.StatusOK, map[string]any{"ok": true, "email": who, "foreign": true})
		return
	}
	// Another account, or one with no league yet: every path has to point at it, so the
	// process starts again on it and the page reloads into the panel or the league choice.
	if err := config.SetActive(account, ""); err != nil {
		s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	s.json(writer, http.StatusOK, map[string]any{"ok": true, "email": who, "restarting": true})
	s.restartSoon()
}

func (s *Server) foreign(writer http.ResponseWriter, who visitor) {
	var buttons strings.Builder
	for _, account := range config.Accounts() {
		if !owns(who, account) {
			continue
		}
		league := ""
		for id := range config.AccountLeagues(account) {
			league = id
			break
		}
		fmt.Fprintf(&buttons, `<button type="button" data-account="%s" data-league="%s">%s<span>→</span></button>`,
			html.EscapeString(account), html.EscapeString(league), html.EscapeString(accountEmail(account)))
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(writer, authPage("Otra cuenta", strings.Replace(foreignBody, "{{accounts}}", buttons.String(), 1)))
}

const setupBody = `<h1>{{title}}</h1><p class="lead">{{lead}}</p>
<ol class="steps">
 <li><div><b>Abre el login de LaLiga</b>
  <p>Entra como en la app: Google, Apple o correo.</p>
  <a class="btn" href="{{login}}" target="_blank" rel="noreferrer">Abrir el login ↗</a></div></li>
 <li><div><b>Antes de entrar, abre las herramientas de red</b>
  <p>En la pestaña del login: <code>Cmd+Opt+I</code> (Mac) o <code>Ctrl+Shift+I</code> →
  pestaña <b>Network</b> (Red) → marca <b>Preserve log</b> (Conservar registro). Ahora entra.</p></div></li>
 <li><div><b>Copia la última petición</b>
  <p>Al terminar, la página se queda en blanco y la barra no cambia: es lo normal. En Network, la
  última fila, en rojo y <code>(canceled)</code>, es <code>authredirect://…?code=…</code>:
  botón derecho → <b>Copy → Copy URL</b>. En Safari y Firefox suele salir también en la barra.</p></div></li>
 <li><div><b>Pégala aquí</b>
  <textarea id="paste" placeholder="authredirect://com.lfp.laligafantasy?code=…"></textarea>
  <button class="btn" id="go">Entrar</button></div></li>
</ol>
<div id="msg"></div>{{back}}
<script>
const msg=document.getElementById('msg'), go=document.getElementById('go');
go.onclick=async()=>{
  const paste=document.getElementById('paste').value.trim();
  if(!paste) return;
  go.disabled=true; msg.className=''; msg.textContent='Comprobando…';
  try{
    const res=await fetch('/api/session',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({paste, add: new URLSearchParams(location.search).get('cuenta')==='nueva'})});
    await refused(res);
    const data=await res.json();
    if(!res.ok) throw new Error(data.error||res.status);
    msg.className='good';
    msg.textContent='Sesión guardada'+(data.email?' ('+data.email+')':'')+'. Cargando…';
    if(data.foreign){location.href='/';return;}
    // A new account restarts the panel on it: reload once it is back, into the panel or the
    // choice of league.
    if(data.restarting){reloadWhenReady();return;}
    // The first build reads the whole league, so wait for it instead of showing a broken page.
    const wait=setInterval(async()=>{
      try{
        const health=await(await fetch('/healthz')).json();
        if(health.status==='ok'){clearInterval(wait);location.href='/';}
      }catch(e){}
    },3000);
  }catch(e){ if(!e.message) return; msg.className='bad'; msg.textContent=e.message; go.disabled=false; }
};
async function refused(res){const data=await res.clone().json().catch(()=>({}));
  if(!data.refused) return;
  document.querySelector('.steps').remove();
  document.querySelector('.auth h1').textContent='Sin acceso';
  document.querySelector('.auth .lead').textContent=data.error;
  msg.className='';msg.innerHTML='<a class="btn quiet" href="/?nuevo=1">Entrar con otra cuenta</a>';
  throw new Error('');}
</script>
`

const foreignBody = `<h1>El panel sigue otra cuenta</h1>
<p class="lead">Ahora mismo está con la cuenta de otra persona, y eso no se enseña. Puedes pasarlo a una tuya:
la otra persona deja de verlo hasta que lo cambie de vuelta.</p>
<div class="choices">{{accounts}}</div>
<div class="btn-row"><a class="btn quiet" href="/?cuenta=nueva">Entrar con otra cuenta</a><button class="btn quiet" id="out" type="button">Salir</button></div><div id="msg"></div>
<script>
document.querySelectorAll('[data-account]').forEach((b)=>b.onclick=()=>{
  document.querySelectorAll('.auth button').forEach((x)=>x.disabled=true);
  switchTo('/api/accounts/use',{account:b.dataset.account,league:b.dataset.league});});
document.getElementById('out').onclick=async()=>{
  await fetch('/api/logout',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});
  location.href='/';};
</script>`
