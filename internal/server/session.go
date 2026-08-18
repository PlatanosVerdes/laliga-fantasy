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
)

// HasSession reports whether there is anything to authenticate with. A stored refresh token
// is enough: the access token is renewed on first use.
func HasSession() bool {
	tokens, err := auth.Load()
	return err == nil && tokens != nil && (tokens.RefreshToken != "" || tokens.AccessToken != "")
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

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(writer, setupPage, html.EscapeString(pending.URL))
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

	if strings.HasPrefix(pasted, "{") {
		tokens, err := auth.Adopt(pasted)
		if err != nil {
			s.json(writer, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		// Prove it works before saying it does: a pasted session that cannot read anything
		// looks identical to a good one until the first request fails, hours later.
		if s.opts.Client != nil {
			if _, err := s.opts.Client.Me(0); err != nil {
				_ = os.Remove(config.TokenFile)
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
	// The world is empty until something reads the API with the new session, and that read
	// can take a while, so it happens in the background and the page waits on /healthz.
	if s.opts.Adopted != nil {
		go s.opts.Adopted()
	}
	s.json(writer, http.StatusOK, map[string]any{"ok": true, "email": who})
}

const setupPage = `<!doctype html><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Fantasy · sesion</title>
<style>
 :root{color-scheme:dark}
 body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0f1115;
      color:#e6e6e6;font:15px/1.55 system-ui,sans-serif}
 main{width:min(680px,92vw);background:#161a21;border:1px solid #262c37;border-radius:14px;
      padding:28px}
 h1{margin:0 0 4px;font-size:20px}
 p{color:#9aa4b2}
 ol{padding-left:20px} li{margin:10px 0}
 a.link{color:#6ea8fe}
 textarea{width:100%%;box-sizing:border-box;min-height:88px;margin-top:6px;padding:10px;
          border-radius:8px;border:1px solid #2c3442;background:#0f1319;color:#e6e6e6;
          font:13px/1.4 ui-monospace,monospace}
 button{margin-top:12px;padding:10px 16px;border:0;border-radius:8px;background:#2f6feb;
        color:#fff;font-weight:600;cursor:pointer}
 button[disabled]{opacity:.5;cursor:default}
 #msg{margin-top:12px;min-height:20px}
 .bad{color:#ff8080} .good{color:#79d67a}
</style>
<main>
 <h1>Falta la sesion</h1>
 <p>Entra con tu cuenta de LaLiga Fantasy y pega aqui la direccion a la que te redirija.</p>
 <ol>
  <li><a class="link" href="%s" target="_blank" rel="noreferrer">Abrir el login de LaLiga</a></li>
  <li>Al terminar el navegador intentara ir a <code>authredirect://…</code> y dara error:
      eso es lo normal. Copia la direccion completa de la barra.</li>
  <li>Pegala aqui:
   <textarea id="paste" placeholder="authredirect://com.lfp.laligafantasy?code=..."></textarea>
   <button id="go">Guardar sesion</button>
  </li>
 </ol>
 <div id="msg"></div>
</main>
<script>
const msg=document.getElementById('msg'), go=document.getElementById('go');
go.onclick=async()=>{
  const paste=document.getElementById('paste').value.trim();
  if(!paste) return;
  go.disabled=true; msg.className=''; msg.textContent='Canjeando…';
  try{
    const res=await fetch('/api/session',{method:'POST',
      headers:{'Content-Type':'application/json'},body:JSON.stringify({paste})});
    const data=await res.json();
    if(!res.ok) throw new Error(data.error||res.status);
    msg.className='good';
    msg.textContent='Sesion guardada'+(data.email?' ('+data.email+')':'')+'. Cargando…';
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
