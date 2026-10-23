package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// deviceOf names the browser and system in the user agent the way a sessions list does,
// "iPhone · Safari": the full string means nothing to the person reading it.
func deviceOf(request *http.Request) string {
	agent := request.UserAgent()
	system := firstMatch(agent, [][2]string{{"iPhone", "iPhone"}, {"iPad", "iPad"},
		{"Android", "Android"}, {"CrOS", "ChromeOS"}, {"Macintosh", "Mac"},
		{"Windows", "Windows"}, {"Linux", "Linux"}})
	// Order matters: Edge and Chrome on iOS also say Safari, and Edge also says Chrome.
	browser := firstMatch(agent, [][2]string{{"Edg", "Edge"}, {"OPR", "Opera"},
		{"Firefox", "Firefox"}, {"FxiOS", "Firefox"}, {"CriOS", "Chrome"},
		{"Chrome", "Chrome"}, {"Safari", "Safari"}})
	switch {
	case system != "" && browser != "":
		return system + " · " + browser
	case system != "":
		return system
	case browser != "":
		return browser
	}
	return ""
}

func firstMatch(agent string, names [][2]string) string {
	for _, name := range names {
		if strings.Contains(agent, name[0]) {
			return name[1]
		}
	}
	return ""
}

var tailnet = mustCIDR("100.64.0.0/10")

func mustCIDR(block string) *net.IPNet {
	_, network, err := net.ParseCIDR(block)
	if err != nil {
		panic(err)
	}
	return network
}

// placeOf is where a request came from. Only the Access door is reached through Cloudflare,
// so only there are its location headers trusted; the city ones arrive only when the zone's
// "Add visitor location headers" managed transform is on.
func placeOf(request *http.Request) place {
	if doorOf(request) == "access" {
		return place{City: request.Header.Get("Cf-Ipcity"), Region: request.Header.Get("Cf-Region"),
			Country: strings.ToUpper(request.Header.Get("Cf-Ipcountry"))}
	}
	ip := net.ParseIP(clientIP(request))
	switch {
	case ip == nil:
		return place{}
	case tailnet.Contains(ip):
		return place{Network: "tailnet"}
	case ip.IsPrivate() || ip.IsLoopback():
		return place{Network: "lan"}
	}
	return place{}
}

// A session is named by the start of its key: the key is already a hash of the cookie, so
// showing it signs nobody in.
func sessionID(key string) string { return key[:16] }

func (s *Server) devices(writer http.ResponseWriter, request *http.Request) {
	who, _ := visitorOf(request)
	sessions := s.doors().sessions
	if request.Method == http.MethodPost {
		var body struct {
			Close  string `json:"close"`
			Others bool   `json:"others"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		closed := 0
		for _, open := range sessions.listUser(who.User) {
			if open.key == who.key {
				continue
			}
			if body.Others || (body.Close != "" && sessionID(open.key) == body.Close) {
				sessions.close(open.key)
				closed++
			}
		}
		if closed == 0 && !body.Others {
			s.json(writer, http.StatusNotFound, map[string]any{"error": "esa sesion ya no existe"})
			return
		}
		s.json(writer, http.StatusOK, map[string]any{"closed": closed})
		return
	}
	list := []map[string]any{}
	for _, open := range sessions.listUser(who.User) {
		list = append(list, map[string]any{"id": sessionID(open.key), "device": open.Device,
			"place": open.Place, "seen": time.Unix(open.Seen, 0).UTC().Format(time.RFC3339),
			"created": time.Unix(open.Created, 0).UTC().Format(time.RFC3339),
			"current": open.key == who.key})
	}
	s.json(writer, http.StatusOK, map[string]any{"count": len(list), "devices": list})
}

func (s *Server) devicesPage(writer http.ResponseWriter, request *http.Request) {
	if _, signed := visitorOf(request); !signed {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(writer, authPage("Dispositivos", devicesBody))
}

const devicesBody = `<h1>Dispositivos</h1>
<p class="lead">Dónde tienes abierto el panel. Cerrar uno lo saca al momento.</p>
<div class="device-list" id="devices"><p class="hint">Cargando…</p></div>
<div class="msg" id="devices-msg"></div>
<div class="row"><a class="btn quiet" href="/vincular/nuevo">Vincular un dispositivo</a>
<button class="btn quiet" id="close-others" type="button" hidden>Cerrar los demás</button></div>
<a class="back" href="/">← Volver al panel</a>
<script>
const list=document.getElementById('devices'), msg=document.getElementById('devices-msg'), others=document.getElementById('close-others');
const countries=(()=>{try{return new Intl.DisplayNames(['es'],{type:'region'});}catch(e){return null;}})();
const country=(code)=>{try{return code&&countries?countries.of(code):code;}catch(e){return code;}};
const placeText=(p)=>{p=p||{};if(p.network==='tailnet')return 'Tailnet';if(p.network==='lan')return 'Red local';
  return [p.city||p.region,country(p.country)].filter(Boolean).join(', ');};
const ago=(stamp)=>{const m=Math.floor((Date.now()-new Date(stamp))/60000);
  return m<10?'activo ahora':m<60?'hace '+m+' min':m<1440?'hace '+Math.floor(m/60)+' h':'hace '+Math.floor(m/1440)+' d';};
const icon=(d)=>/iPhone|iPad|Android/.test(d)
  ?'<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="7" y="2.5" width="10" height="19" rx="2"/><path d="M11 18.5h2"/></svg>'
  :'<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="5" width="16" height="11" rx="1.5"/><path d="M2 19h20"/></svg>';
const esc=(t)=>String(t).replace(/[&<>"]/g,(c)=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
async function post(body){const res=await fetch('/api/devices',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  const data=await res.json().catch(()=>({}));if(!res.ok)throw new Error(data.error||res.status);return data;}
async function load(){
  try{
    const data=await fetch('/api/devices',{cache:'no-store'}).then((r)=>r.json());
    const devices=data.devices||[];
    list.innerHTML=devices.map((d)=>'<div class="device-row">'+icon(d.device||'')+'<span class="device-who"><b>'+esc(d.device||'Dispositivo desconocido')+'</b><small>'+
      esc([placeText(d.place),d.current?'activo ahora':ago(d.seen)].filter(Boolean).join(' · '))+'</small></span>'+
      (d.current?'<span class="device-here">Este dispositivo</span>':'<button class="device-out" type="button" data-id="'+esc(d.id)+'">Cerrar</button>')+'</div>').join('');
    others.hidden=devices.length<2;
  }catch(e){msg.className='msg bad';msg.textContent='No he podido leer las sesiones.';}
}
list.onclick=async(e)=>{const id=e.target.dataset&&e.target.dataset.id;if(!id)return;
  try{await post({close:id});load();}catch(err){msg.className='msg bad';msg.textContent=err.message;}};
others.onclick=async()=>{try{const d=await post({others:true});msg.className='msg good';
  msg.textContent=d.closed===1?'Cerrada 1 sesión.':'Cerradas '+d.closed+' sesiones.';load();}catch(err){msg.className='msg bad';msg.textContent=err.message;}};
load();
</script>`
