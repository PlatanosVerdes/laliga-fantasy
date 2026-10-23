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

// loadBar is the progress bar of the pages waiting on a build or a render; the script fills it.
const loadBar = `<div class="load"><div class="load-track"><i></i></div>
<div class="load-meta" role="status" aria-live="polite"><span class="load-step">Empezando…</span><span class="load-pct"></span></div></div>
`

const authFrame = `<!doctype html><html lang="es"><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Fantasy · {{title}}</title>
<link rel="stylesheet" href="/assets/report.css">
<style>
[hidden]{display:none!important}
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
.auth .btn.danger{color:var(--critical);border-color:color-mix(in srgb,var(--critical) 45%,transparent)}
textarea{width:100%;box-sizing:border-box;min-height:84px;margin-top:10px;padding:10px 12px;
  border-radius:10px;border:1px solid var(--line);background:var(--plane);color:var(--ink);
  font:12.5px/1.45 ui-monospace,SFMono-Regular,Menlo,monospace;resize:vertical}
textarea:focus{outline:2px solid var(--accent);outline-offset:1px;border-color:transparent}
#msg,.msg{margin-top:14px;min-height:20px;font-size:14px;color:var(--ink-2)}
#msg.bad,.msg.bad{color:var(--critical)} #msg.good,.msg.good{color:var(--good)}
.auth input.code{width:100%;box-sizing:border-box;margin-top:10px;padding:10px 12px;border-radius:10px;
  border:1px solid var(--line);background:var(--plane);color:var(--ink);text-transform:uppercase;
  font:600 18px/1.3 ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:.12em}
.auth input.code:focus{outline:2px solid var(--accent);outline-offset:1px;border-color:transparent}
.auth .btn.wide{display:flex;justify-content:center;width:100%;box-sizing:border-box;margin:0;padding:12px 16px;font-size:15px}
.hint{margin:10px 0 0;color:var(--muted);font-size:13px}
.or{display:flex;align-items:center;gap:12px;margin:22px 0 14px;color:var(--muted);font-size:12px}
.or::before,.or::after{content:"";flex:1;height:1px;background:var(--line)}
.choose .link-in:first-child+.or,.choose .link-in:first-child{margin-top:0}
.code-row{display:flex;gap:8px;align-items:stretch}
.code-row .btn{margin:0;flex:none}
.auth .field{flex:1;min-width:0;box-sizing:border-box;padding:10px 12px;border-radius:10px;border:1px solid var(--line);
  background:var(--plane);color:var(--ink);font:14px/1.3 inherit}
.auth .field.mono{font:13px/1.3 ui-monospace,SFMono-Regular,Menlo,monospace}
.auth .field:focus{outline:2px solid var(--accent);outline-offset:1px;border-color:transparent}
.auth .field.code{text-transform:uppercase;font:600 16px/1.3 ui-monospace,SFMono-Regular,Menlo,monospace;letter-spacing:.12em}
.mini{margin:0 0 16px;padding-left:20px;display:grid;gap:8px;color:var(--ink-2);font-size:14px}
.mini code,.help code{font-size:12.5px;background:var(--mid);padding:1px 5px;border-radius:5px}
kbd{font:600 12px/1 ui-monospace,SFMono-Regular,Menlo,monospace;padding:3px 6px;border:1px solid var(--line);
  border-bottom-width:2px;border-radius:6px;background:var(--plane)}
.help{margin-top:16px;font-size:13px;color:var(--ink-2)}
.help summary{cursor:pointer;color:var(--muted)}
.help p{margin:8px 0 0;line-height:1.5}
.row{display:flex;justify-content:space-between;gap:12px;margin-top:18px}
.text-btn{background:none;border:0;padding:0;font:inherit;font-size:13px;color:var(--muted);cursor:pointer;text-decoration:none}
.text-btn:hover{color:var(--ink)}
.row .btn{margin:0}
.closed{margin:0 0 16px;padding:14px;border:1px solid var(--accent);border-radius:12px;font-size:14px;color:var(--ink-2)}
.closed b{color:var(--ink)}
.closed .btn.wide{margin-top:12px}
.link-in p,.aside{margin:3px 0 0;color:var(--ink-2);font-size:14px}
.aside{margin-top:18px}
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
.auth-wrap:has(.pv){grid-template-columns:minmax(0,min(1040px,100%));justify-content:center}
.auth.pv{width:100%;min-width:0;box-sizing:border-box}
.pv-sec{margin-top:26px;overflow-x:auto}
.pv-sec h2{margin:0 0 10px;font-size:16px}
.pv-wait{margin:0;padding:14px 16px;border:1px dashed var(--line);border-radius:12px;color:var(--muted);font-size:14px}
.pv-chips{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px}
.pv-chip{display:grid;gap:2px;padding:8px 12px;border:1px solid var(--line);border-radius:10px;background:var(--plane)}
.pv-chip i{font-style:normal;font-size:12px;color:var(--muted)}
.pv-chip b{font-variant-numeric:tabular-nums}
.pv-table{width:100%;border-collapse:collapse;font-size:13.5px}
.pv-table th{text-align:left;font-weight:600;color:var(--muted);font-size:12px;padding:6px 8px;border-bottom:1px solid var(--line)}
.pv-table td{padding:6px 8px;border-bottom:1px solid var(--line);white-space:nowrap}
.pv-table .num{text-align:right;font-variant-numeric:tabular-nums}
.pv-tag{font-size:11px;padding:1px 6px;border-radius:999px;background:var(--mid);color:var(--ink-2)}
.pv-bad{color:var(--critical)}
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
  document.querySelector('.load-step').textContent=p.step+(p.total>1?' · '+p.done+' de '+p.total:'');
  document.querySelector('.load-pct').textContent=p.percent+' %';
}
// The step to show: the page being drawn, inside the build when it is the first build's last step.
function progressOf(h){
  const b=h.building,r=h.rendering;
  if(!r) return b||null;
  const base=b?b.percent:0;
  return {step:r.step,done:r.done,total:r.total,percent:Math.round(base+(100-base)*r.percent/100)};
}
// Reload once the build is over: a new world, a new failure, or a new process that has finished
// either way, since a league that fails again fails with the same error as before.
async function reloadWhenReady(was){
  was=was||(await health())||{};
  showProgress(null);
  const poll=setInterval(async()=>{const now=await health();
    if(!now) return;
    if(now.loading&&now.preview>0){clearInterval(poll);location.href='/';return;}
    if(now.building){showProgress(progressOf(now));return;}
    const restarted=now.started!==was.started&&(now.generated_at||now.last_error);
    const built=now.generated_at&&now.generated_at!==was.generated_at;
    const failed=now.last_error&&now.last_error!==was.last_error;
    if(restarted||built||failed){clearInterval(poll);
      showProgress({step:now.last_error?'No ha cargado':'Listo',done:0,total:0,percent:100});
      setTimeout(()=>{location.href='/';},450);}},700);
  setTimeout(()=>{clearInterval(poll);location.href='/';},60000);
}
// Reload once the panel is drawn; reload rather than going to /, so the tab in the address stays.
function reloadWhenDrawn(){
  showProgress(null);
  let idle=0,poll=null;
  const check=async()=>{const now=await health();
    if(!now) return;
    // Nothing drawing and nothing ready: the world moved under the render, and asking again starts one.
    idle=now.rendering||now.building?0:idle+1;
    if(!now.page_ready&&idle<4){showProgress(progressOf(now));return;}
    clearInterval(poll);
    showProgress({step:'Listo',done:0,total:0,percent:100});
    setTimeout(()=>{location.reload();},300);};
  poll=setInterval(check,700);
  check();
  setTimeout(()=>{clearInterval(poll);location.reload();},90000);
}
// Follow a cold start: swap in each stage of the preview as it is read, and open the panel once
// it is drawn. Swapped rather than reloaded, so the page does not jump while it is being read.
function followPreview(){
  const box=document.getElementById('preview');
  let stage=+box.dataset.stage,idle=0,busy=false,poll=null;
  const done=()=>{clearInterval(poll);location.reload();};
  const check=async()=>{const now=await health();
    if(!now||busy) return;
    showProgress(progressOf(now));
    if(now.page_ready||now.last_error&&now.loading) return done();
    idle=now.rendering||now.building?0:idle+1;
    if(idle>=4) return done();
    if(now.preview<=stage) return;
    busy=true;
    try{
      const page=await(await fetch('/',{cache:'no-store'})).text();
      const next=new DOMParser().parseFromString(page,'text/html').getElementById('preview');
      if(!next) return done();
      box.replaceChildren(...next.childNodes);
      stage=+next.dataset.stage;
      box.dataset.stage=stage;
    }catch(e){}finally{busy=false;}};
  poll=setInterval(check,700);
  check();
}
async function linkHere(code,msg,button){
  if(!code.trim()) return;
  button.disabled=true; msg.className='msg'; msg.textContent='Comprobando…';
  try{
    const res=await fetch('/api/link/redeem',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({code})});
    const data=await res.json().catch(()=>({}));
    if(!res.ok) throw new Error(data.error||res.status);
    msg.className='msg good'; msg.textContent='Dispositivo vinculado. Cargando…';
    location.href='/';
  }catch(e){msg.className='msg bad'; msg.textContent=e.message; button.disabled=false;}
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
