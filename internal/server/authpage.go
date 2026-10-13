package server

import (
	"html"
	"strings"
)

// authPage is the frame of the pages shown before the panel (logging in, choosing a league):
// the panel's own stylesheet for its colours and type, light or dark like the rest, and a card.
func authPage(title, body string) string {
	return strings.NewReplacer("{{title}}", html.EscapeString(title), "{{body}}", body).Replace(authFrame)
}

const authFrame = `<!doctype html><html lang="es"><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Fantasy · {{title}}</title>
<link rel="stylesheet" href="/assets/report.css">
<style>
.auth-wrap{position:fixed;inset:0;overflow:auto;display:grid;place-items:center;padding:24px 16px;box-sizing:border-box}
.auth{width:min(520px,100%);background:var(--surface);border:1px solid var(--line);border-radius:16px;
  padding:28px 28px 24px;box-shadow:0 10px 40px rgba(0,0,0,.18)}
.auth-brand{display:flex;align-items:center;gap:8px;color:var(--muted);font-size:12px;font-weight:600;
  letter-spacing:.08em;text-transform:uppercase;margin-bottom:18px}
.auth-brand i{width:8px;height:8px;border-radius:50%;background:var(--accent)}
.auth h1{margin:0 0 6px;font-size:22px;letter-spacing:-.01em}
.auth .lead{margin:0 0 22px;color:var(--ink-2)}
.steps{list-style:none;margin:0;padding:0;display:grid;gap:18px;counter-reset:step}
.steps li{display:grid;grid-template-columns:28px minmax(0,1fr);gap:12px;counter-increment:step}
.steps li::before{content:counter(step);width:28px;height:28px;border-radius:50%;display:grid;
  place-items:center;background:var(--mid);color:var(--ink-2);font-size:13px;font-weight:700}
.steps p{margin:3px 0 0;color:var(--ink-2);font-size:14px}
.steps code{font-size:12.5px;background:var(--mid);padding:1px 5px;border-radius:5px}
.auth .btn{display:inline-flex;align-items:center;gap:6px;margin-top:10px;padding:10px 16px;border-radius:10px;
  border:1px solid var(--accent);background:var(--accent);color:#fff;font:inherit;font-weight:650;
  font-size:14px;text-decoration:none;cursor:pointer}
.auth .btn{text-transform:none;letter-spacing:0;height:auto;box-shadow:none;margin-right:8px}
.auth .btn.quiet{background:transparent;color:var(--ink);border-color:var(--line)}
.auth .btn[disabled]{opacity:.5;cursor:default}
textarea{width:100%;box-sizing:border-box;min-height:84px;margin-top:10px;padding:10px 12px;
  border-radius:10px;border:1px solid var(--line);background:var(--plane);color:var(--ink);
  font:12.5px/1.45 ui-monospace,SFMono-Regular,Menlo,monospace;resize:vertical}
textarea:focus{outline:2px solid var(--accent);outline-offset:1px;border-color:transparent}
#msg{margin-top:14px;min-height:20px;font-size:14px;color:var(--ink-2)}
#msg.bad{color:var(--critical)} #msg.good{color:var(--good)}
.choices{display:grid;gap:8px}
.choices button{font:inherit;text-align:left;padding:13px 15px;border:1px solid var(--line);border-radius:12px;
  background:var(--plane);color:var(--ink);cursor:pointer;display:flex;justify-content:space-between;
  align-items:center}
.choices button:hover{border-color:var(--accent)}
.choices button span{color:var(--muted);font-size:13px}
.back{display:inline-block;margin-top:18px;color:var(--muted);font-size:13px}
.load{margin-top:8px}
.load-track{height:8px;border-radius:999px;background:var(--mid);overflow:hidden}
.load-track i{display:block;height:100%;width:2%;border-radius:999px;background:var(--accent);
  transition:width .5s ease}
.load-meta{display:flex;justify-content:space-between;margin-top:8px;font-size:13px;color:var(--muted);
  font-variant-numeric:tabular-nums}
.centered{text-align:center}
.centered .choices button{justify-content:center;gap:10px}
.centered .btn-row{display:flex;flex-wrap:wrap;justify-content:center;gap:8px}
.centered .btn-row .btn{margin-right:0}
</style>
<script>
const health=async()=>{try{return await(await fetch('/healthz',{cache:'no-store'})).json();}catch(e){return null;}};
// The bar and the step the build is on, put in place of the message when the page has none.
function showProgress(p){
  if(!document.querySelector('.load-track')){
    const msg=document.getElementById('msg');
    if(!msg) return;
    msg.innerHTML='<div class="load"><div class="load-track"><i></i></div><div class="load-meta">'+
      '<span class="load-step">Reiniciando el panel…</span><span class="load-pct"></span></div></div>';
  }
  if(!p) return;
  document.querySelector('.load-track i').style.width=Math.max(2,p.percent)+'%';
  document.querySelector('.load-step').textContent=p.step+(p.total>0?' · '+p.done+' de '+p.total:'');
  document.querySelector('.load-pct').textContent=p.percent+' %';
}
// Reload once the build is over: a new world, a new failure, or a new process that has finished
// either way, since a league that fails again fails with the same error as before.
async function reloadWhenReady(was){
  was=was||(await health())||{};
  showProgress(null);
  const poll=setInterval(async()=>{const now=await health();
    if(!now) return;
    if(now.building){showProgress(now.building);return;}
    const restarted=now.started!==was.started&&(now.generated_at||now.last_error);
    const built=now.generated_at&&now.generated_at!==was.generated_at;
    const failed=now.last_error&&now.last_error!==was.last_error;
    if(restarted||built||failed){clearInterval(poll);
      showProgress({step:now.last_error?'No ha cargado':'Listo',done:0,total:0,percent:100});
      setTimeout(()=>{location.href='/';},450);}},700);
  setTimeout(()=>{clearInterval(poll);location.href='/';},60000);
}
// A switch: note the process before asking for it, so a restart quicker than the first poll
// still counts as one.
async function switchTo(url,body){
  const was=(await health())||{};
  await fetch(url,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  reloadWhenReady(was);
}
</script>
<div class="auth-wrap"><main class="auth"><div class="auth-brand"><i></i>Fantasy</div>{{body}}</main></div></html>`
