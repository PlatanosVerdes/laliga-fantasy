// Session: the first-run flow. A fresh deploy has no tokens.json, and copying one in by hand
// means ssh and sudo on the box — so the page itself asks for the login.
package server

import (
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/files"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/secret"
)

// HasSession reports whether there is anything to authenticate with. A stored refresh token
// is enough: the access token is renewed on first use.
func HasSession() bool {
	tokens, err := auth.Load()
	// A session given up on is shown as none, so the page asks for the login that restarts it.
	return err == nil && tokens != nil && !tokens.Stopped() &&
		(tokens.RefreshToken != "" || tokens.AccessToken != "")
}

// locked is a session that exists but this key cannot open: kept, never taken for none.
func locked(err error) bool {
	return errors.Is(err, secret.ErrWrongKey) || errors.Is(err, secret.ErrNoKey)
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
	title, lead, back := "Inicia sesión", "Con tu cuenta de LaLiga Fantasy.", ""
	linkIn := linkInBox
	if adding {
		title, lead = "Añadir cuenta", "Queda guardada y el panel pasa a usarla."
		back, linkIn = `<a class="back" href="/">← Volver al panel</a>`, ""
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(writer, authPage(title, strings.NewReplacer("{{title}}", html.EscapeString(title),
		"{{lead}}", html.EscapeString(lead), "{{login}}", html.EscapeString(pending.URL),
		"{{back}}", back, "{{linkin}}", linkIn).Replace(setupBody)))
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
	if _, err := auth.Load(); locked(err) && !truthy(body["replace"]) && !truthy(body["add"]) {
		s.json(writer, http.StatusConflict, map[string]any{
			"error": err.Error() + "; para cambiarla por esta manda replace: true"})
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
<section id="choose" class="choose">
 <a class="btn wide" id="start" href="{{login}}" target="_blank" rel="noreferrer">Continuar con LaLiga Fantasy</a>
 <p class="hint phone-hint" data-for="phone" hidden>Pide las herramientas de red: mejor la primera vez desde un ordenador.</p>
 {{linkin}}
</section>
<section id="finish" hidden>
 <div class="closed" id="closed" hidden><b>Has cerrado la pestaña de LaLiga.</b> Si no llegaste a copiar la URL, vuelve a abrirla.
  <button class="btn wide" id="reopen-closed" type="button">Abrir el login de nuevo</button></div>
 <ol class="mini">
  <li>En la pestaña de LaLiga, antes de entrar: <kbd data-for="any">⌘⌥I · F12</kbd><kbd data-for="mac" hidden>⌘⌥I</kbd><kbd data-for="pc" hidden>F12</kbd><kbd data-for="phone" hidden>⌘⌥I · F12</kbd> → <b>Network</b> → <b>Preserve log</b>.</li>
  <li>Entra con tu cuenta.</li>
  <li>Copia la última fila, <code>(canceled)</code>: botón derecho → <b>Copy URL</b>.</li>
 </ol>
 <div class="code-row"><input class="field mono" id="paste" name="login-redirect" aria-label="Dirección de vuelta del login" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" data-1p-ignore data-lpignore="true" data-bwignore placeholder="Pega aquí authredirect://…"><button class="btn" id="go" type="button">Entrar</button></div>
 <div id="msg"></div>
 <details class="help"><summary>¿No encuentras la URL?</summary>
  <p>La pestaña se queda congelada en la página de Google o de LaLiga, en blanco o con <i>Unsupported URL scheme</i>: es lo normal, el login ha terminado.
  Escribe <code>code=</code> en el filtro de Network y la fila aparece. En Safari, activa antes Ajustes → Avanzado →
  <b>Mostrar funciones para desarrolladores web</b>; en Safari y Firefox la dirección suele salir también en la barra.
  El código caduca en unos minutos: si falla, vuelve a abrir el login.</p></details>
 <div class="row"><button class="text-btn" id="back-choose" type="button">← Volver</button><button class="btn quiet" id="reopen" type="button">Abrir el login de nuevo ↗</button></div>
</section>
{{back}}
<script>
const msg=document.getElementById('msg'), go=document.getElementById('go'), paste=document.getElementById('paste');
const choose=document.getElementById('choose'), finish=document.getElementById('finish');
(()=>{
  const ua=navigator.userAgent||'', hints=navigator.userAgentData;
  const platform=(hints&&hints.platform)||navigator.platform||ua;
  // iPadOS asks for the desktop site and reports itself as a Mac; only the touch screen gives it away.
  const phone=(hints&&hints.mobile)||/Android|iPhone|iPad|iPod/i.test(platform+' '+ua)||(/Mac/i.test(platform)&&navigator.maxTouchPoints>1);
  const os=phone?'phone':/Mac/i.test(platform)?'mac':'pc';
  document.querySelectorAll('[data-for]').forEach((e)=>{e.hidden=e.dataset.for!==os;});
  if(os!=='phone') return;
  const start=document.getElementById('start'), link=document.querySelector('.link-in');
  start.classList.add('quiet');
  if(link) choose.prepend(link);
})();
let loginTab=null, watch=null;
const closed=document.getElementById('closed');
// Opened by hand, so the page can see the tab close; the opener is cut first so LaLiga cannot steer this tab.
function openLogin(e){
  const url=document.getElementById('start').href;
  const tab=window.open('','_blank');
  if(tab){e.preventDefault(); tab.opener=null; tab.location=url; loginTab=tab;}
  else if(e.currentTarget.tagName!=='A') window.open(url,'_blank','noopener');
  choose.hidden=true; finish.hidden=false; closed.hidden=true; paste.focus();
  clearInterval(watch);
  watch=setInterval(()=>{if(loginTab&&loginTab.closed&&!go.disabled&&!paste.value.trim()){clearInterval(watch);closed.hidden=false;}},800);
}
document.getElementById('start').addEventListener('click',openLogin);
document.getElementById('reopen').onclick=openLogin;
document.getElementById('reopen-closed').onclick=openLogin;
document.getElementById('back-choose').onclick=()=>{finish.hidden=true;choose.hidden=false;};
const linkGo=document.getElementById('link-go'), linkCode=document.getElementById('link-code');
if(linkGo){linkGo.onclick=()=>linkHere(linkCode.value,document.getElementById('link-msg'),linkGo);
  linkCode.addEventListener('keydown',(e)=>{if(e.key==='Enter') linkGo.click();});}
paste.addEventListener('keydown',(e)=>{if(e.key==='Enter') go.click();});
paste.addEventListener('input',()=>{if(/code=/.test(paste.value)&&!go.disabled) go.click();});
go.onclick=async()=>{
  const pasted=paste.value.trim();
  if(!pasted) return;
  go.disabled=true; msg.className=''; msg.textContent='Comprobando…';
  try{
    const res=await fetch('/api/session',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({paste:pasted, add: new URLSearchParams(location.search).get('cuenta')==='nueva'})});
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
  finish.remove(); choose.remove();
  document.querySelector('.auth h1').textContent='Sin acceso';
  document.querySelector('.auth .lead').textContent=data.error;
  msg.className='';
  document.querySelector('.auth').insertAdjacentHTML('beforeend','<a class="btn quiet" href="/?nuevo=1">Entrar con otra cuenta</a>');
  throw new Error('');}
</script>
`

const linkInBox = `<div class="or"><span>o con un código</span></div>
<section class="link-in">
<div class="code-row"><input class="field code" id="link-code" name="device-link" aria-label="Código de vinculación" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" data-1p-ignore data-lpignore="true" data-bwignore placeholder="ABCD-EFGH" maxlength="12"><button class="btn quiet" id="link-go" type="button">Vincular</button></div>
<p class="hint">Desde un dispositivo donde ya hayas entrado: menú de la cuenta → Vincular un dispositivo.</p>
<div class="msg" id="link-msg"></div></section>`

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
