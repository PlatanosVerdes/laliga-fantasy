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

// setup is the page shown when there is no session: the login link and a box to paste the
// address bar into. The redirect goes to authredirect://com.lfp.laligafantasy, which the
// browser cannot open — it just leaves the code in the address bar, which is the whole trick.
func (s *Server) setup(writer http.ResponseWriter, request *http.Request) {
	pending, err := auth.LoadPending()
	if err != nil || request.URL.Query().Get("nuevo") != "" {
		pending, err = auth.StartLogin()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	adding := request.URL.Query().Get("cuenta") == "nueva" && HasSession()
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
	body := s.body(request)
	pasted := strings.TrimSpace(text(body["paste"]))
	if pasted == "" {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "no has pegado nada"})
		return
	}
	// A working session is replaced only on purpose: a stray post used to overwrite it, and a
	// failed check then deleted the file.
	if HasSession() && !truthy(body["replace"]) && !truthy(body["add"]) {
		s.json(writer, http.StatusConflict, map[string]any{
			"error": "ya hay una sesion que funciona; para cambiarla manda replace: true"})
		return
	}
	previous, readErr := os.ReadFile(config.TokenFile)
	restore := func() {
		if readErr == nil {
			_ = files.WriteAtomic(config.TokenFile, previous, 0o600)
		} else {
			_ = os.Remove(config.TokenFile)
		}
	}

	if strings.HasPrefix(pasted, "{") {
		tokens, err := auth.Adopt(pasted)
		if err != nil {
			s.json(writer, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		// Prove it works before saying it does: a pasted session that cannot read anything
		// looks identical to a good one until the first request fails, hours later. Only the
		// active account can be asked from here; another one is proved by starting on it.
		if s.opts.Client != nil && config.AccountID(tokens.Email) == config.Account {
			if _, err := s.opts.Client.Me(0); err != nil {
				restore()
				s.json(writer, http.StatusBadRequest,
					map[string]any{"error": "esa sesion no sirve: " + err.Error()})
				return
			}
		}
		s.sessionReady(writer, tokens.Email)
		return
	}

	pending, err := auth.LoadPending()
	if err != nil {
		s.json(writer, http.StatusBadRequest, map[string]any{
			"error": "no hay login empezado: recarga la pagina y usa el enlace"})
		return
	}
	code, err := auth.ExtractCode(pasted, pending.State)
	if err != nil {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	tokens, err := auth.ExchangeCode(code, pending.Verifier)
	if err != nil {
		s.json(writer, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	s.sessionReady(writer, tokens.Email)
}

func (s *Server) sessionReady(writer http.ResponseWriter, who string) {
	slog.Info("session stored", "email", who)
	// Another account, or one with no league yet: every path has to point at it, so the
	// process starts again on it and the page reloads into the panel or the league choice.
	if config.AccountID(who) != config.Account || config.League == "" {
		s.json(writer, http.StatusOK, map[string]any{"ok": true, "email": who, "restarting": true})
		s.restartSoon()
		return
	}
	// The world is empty until something reads the API with the new session, and that read
	// can take a while, so it happens in the background and the page waits on /healthz.
	if s.opts.Adopted != nil {
		go s.opts.Adopted()
	}
	s.json(writer, http.StatusOK, map[string]any{"ok": true, "email": who})
}

const setupBody = `<h1>{{title}}</h1><p class="lead">{{lead}}</p>
<ol class="steps">
 <li><div><b>Abre el login de LaLiga</b>
  <p>Entra como en la app: Google, Apple o correo.</p>
  <a class="btn" href="{{login}}" target="_blank" rel="noreferrer">Abrir el login ↗</a></div></li>
 <li><div><b>Copia la dirección final</b>
  <p>Al terminar, el navegador intentará abrir <code>authredirect://…</code> y dará error: es lo
  normal. Copia la dirección completa de la barra.</p></div></li>
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
    const data=await res.json();
    if(!res.ok) throw new Error(data.error||res.status);
    msg.className='good';
    msg.textContent='Sesión guardada'+(data.email?' ('+data.email+')':'')+'. Cargando…';
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
  }catch(e){ msg.className='bad'; msg.textContent=e.message; go.disabled=false; }
};
</script>
`
