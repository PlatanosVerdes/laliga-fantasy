
// ---- usage: what is looked at, what is touched and for how long -----------
// What no server log can hold because it never becomes a request: the tab, the click inside it
// and the order of a table. The why is in internal/usage.
const usage=(()=>{
  const OFF={tab(){},click(){},sort(){},op(){}};
  // The static report has nowhere to send it, and a fetch that always fails is noise in the
  // console of whoever opened the file rather than a measurement.
  const mode=document.querySelector('.mode')?.dataset.mode||'';
  if(!mode||mode==='informe') return OFF;

  const phone=matchMedia('(max-width: 700px)').matches;
  let queue=[], timer=null, tab=null, since=0, seen=0;
  const nowMs=()=>Date.now();

  const send=(beacon)=>{
    if(!queue.length) return;
    const batch=queue.slice(0,200); queue=queue.slice(200);
    const body=JSON.stringify(batch);
    try{
      if(beacon&&navigator.sendBeacon){
        navigator.sendBeacon('/api/usage',new Blob([body],{type:'application/json'}));
      }else{
        fetch('/api/usage',{method:'POST',headers:{'Content-Type':'application/json'},
          body,keepalive:true}).catch(()=>{});
      }
    }catch(e){}
  };
  const push=(event)=>{
    queue.push({at:new Date().toISOString(),phone,...event});
    if(queue.length>=200){ send(false); return; }
    if(!timer) timer=setTimeout(()=>{ timer=null; send(false); },15000);
  };

  // The clock runs only while the page is visible: a fantasy panel sits open all afternoon, and
  // without this "an hour in Mercado" would be an hour of not looking at it.
  const stop=()=>{ if(tab&&since) seen+=nowMs()-since; since=0; };
  const start=()=>{ if(tab&&!since) since=nowMs(); };
  const close=()=>{
    stop();
    if(tab&&seen>=1000) push({kind:'tab',what:tab,seconds:Math.round(seen/1000)});
    tab=null; seen=0;
  };

  document.addEventListener('visibilitychange',()=>{ document.hidden?stop():start(); });
  addEventListener('pagehide',()=>{ close(); send(true); });

  return {
    tab(id){ if(id===tab) return; close(); tab=id; seen=0; start(); },
    click(where,label,what){ push({kind:'click',where,label,what}); },
    sort(where,column){ push({kind:'sort',where,what:column}); },
    op(name,where){ push({kind:'op',what:name,where:where||tab||''}); },
  };
})();

// The parts the browser draws from JSON (the player's card, Vender) live in /assets/ui. Without
// them (an old browser, the static file) everything below still draws the page on its own.
const uiScript=document.querySelector('script[type="module"][src*="/assets/ui/"]');
const uiModule=uiScript&&location.protocol.startsWith('http')
  ? import(uiScript.src).catch(()=>null) : Promise.resolve(null);

// ---- live countdowns -------------------------------------------------------
function tick(){
  const now=Date.now();
  document.querySelectorAll('[data-deadline]').forEach(el=>{
    const left=new Date(el.dataset.deadline).getTime()-now;
    if(isNaN(left)) return;
    const plain=el.dataset.plain==='1';
    if(el.dataset.chip){ chipTick(el,left); return; }
    const stat=el.closest('.stat');
    if(stat) stat.classList.toggle('hot',left>0&&left<6*3600000);
    if(left<=0){ el.textContent='ya'; if(!plain) el.className='pill-critical'; return; }
    const h=Math.floor(left/3600000), m=Math.floor(left%3600000/60000),
          s=Math.floor(left%60000/1000);
    el.textContent = h>=24 ? Math.floor(h/24)+'d '+(h%24)+'h'
                   : h>0   ? h+'h '+String(m).padStart(2,'0')+'m'
                           : m+'m '+String(s).padStart(2,'0')+'s';
    // A widget's value is not a pill: only the colour changes, not the whole class.
    if(stat) return;
    if(plain) el.style.color = h<1 ? 'var(--critical)' : h<6 ? 'var(--warning)' : '';
    else el.className = h<1 ? 'pill-critical' : h<24 ? 'pill-warning' : 'pill-neutral';
  });
}
setInterval(tick,1000);

// A row's countdown chip: "4 h 04 min", red in its last six hours, "cerrado" once it is past.
function chipTick(el,left){
  const target=el.querySelector('.left')||el;
  const minutes=Math.round(left/60000), d=Math.floor(minutes/1440),
        h=Math.floor(minutes%1440/60), m=minutes%60;
  target.textContent = left<=0 ? 'cerrado' : d ? `${d} d ${h} h`
                     : h ? `${h} h ${String(m).padStart(2,'0')} min` : `${m} min`;
  el.classList.toggle('soon',left>0&&left<6*3600000);
  el.classList.toggle('done',left<=0);
}

// ---- figures ---------------------------------------------------------------
const fmt=(n)=> n==null ? '—' :
  (Math.abs(n)>=1e6 ? (n/1e6).toFixed(2)+'M' : Math.abs(n)>=1e3 ? (n/1e3).toFixed(0)+'K' : String(n));
const exact=(n)=> n==null ? '—' : Number(n).toLocaleString('es-ES')+' €';

// ---- alineacion: campo, arrastrar y guardar --------------------------------
// Status icons: red card, first-aid kit, question mark. The reason comes from
// futbolfantasy (the API gives only the status code) and appears instantly through a tooltip
// of our own, because the native title takes nearly a second.
// Text glyphs, not SVG: at 12px a thin stroke disappears, and a cross of two rects
// o un caracter siempre salen.
const ICON_CARD='';              // la propia insignia ES la tarjeta
const ICON_KIT='<i class="kit"></i>';
const ICON_DOUBT='?';

// Red when the absence is confirmed, yellow when it is a doubt: an injury of unknown length
// (severity 0 on futbolfantasy) counts as confirmed, any other injury as a doubt.
function health(player){
  const st=player.status||'ok';
  const a=player.absence||{};
  if(st==='suspended'||st==='sanctioned'||a.kind==='sancionado')
    return {ring:'out', glyph:'card', label:'Sancionado'};
  if(st==='injured'||(a.kind==='lesionado'&&Number(a.severity)===0&&a.severity!=null))
    return {ring:'out', glyph:'cross', label:'Lesionado'};
  if(st==='doubtful'||a.kind==='lesionado'||a.kind==='duda')
    return {ring:'doubt', glyph:a.kind==='lesionado'?'cross':'', label:a.kind==='lesionado'?'Tocado':'Duda'};
  return null;
}

function healthTip(player){
  const s=health(player);
  if(!s) return '';
  const a=player.absence||{};
  const bits=[s.label, a.reason||'sin detalle en futbolfantasy'];
  if(a.since) bits.push(a.since);
  if(a.until) bits.push(a.until);
  return bits.join(' \u00b7 ').replace(/"/g,'&quot;');
}

function statusOf(player){
  const s=health(player);
  if(!s) return null;
  if(s.glyph==='card') return {cls:'st-sancionado', icon:ICON_CARD, label:s.label};
  if(s.glyph==='cross') return {cls:'st-lesionado'+(s.ring==='doubt'?' st-doubt':''), icon:ICON_KIT, label:s.label};
  return {cls:'st-duda', icon:ICON_DOUBT, label:s.label};
}

function statusRing(player){
  const s=health(player);
  return s ? ' ring-'+s.ring : '';
}

function statusBadge(player){
  const s=statusOf(player);
  if(!s) return '';
  // The floating tooltip, not a child of the badge: on a card in the top line the nested one
  // hung outside the pitch and read as an empty input box.
  return `<span class="badge-status ${s.cls}" data-tip="${healthTip(player)}">${s.icon}</span>`;
}

// A club's crest, from the stylesheet the page carries.
const crest=id=>id?`<span class="crest crest-${id}"></span>`:'';

// A round face with the status ring and its badge, the reason as the tooltip.
function faceOf(player,size='sm'){
  const s=health(player);
  const badge=!s?'':s.glyph==='card'?'<span class="hb hb-card"><svg viewBox="0 0 16 16" aria-hidden="true"><rect x="4.5" y="2.5" width="7" height="11" rx="1.3"/></svg></span>'
    :s.glyph==='cross'?'<span class="hb hb-cross"><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M6.4 2.5h3.2v3.9h3.9v3.2H9.6v3.9H6.4V9.6H2.5V6.4h3.9z"/></svg></span>':'';
  const inner=player.image
    ? `<img src="${player.image}" alt="" loading="lazy" onerror="this.remove()">`
    : `<span class="crest crest-${player.team_id}"></span>`;
  return `<span class="face face-${size}${s?' ring-'+s.ring:''}"${s?` data-tip="${healthTip(player)}"`:''}
    >${inner}${badge}</span>`;
}

// Odds of starting: the number that decides whether the xPts will materialise at all.
function titClass(p){
  return p>=75?'tit-hi':p>=50?'tit-mid':p>=30?'tit-lo':'tit-out';
}

// The thresholds are futbolfantasy's own scale, not chosen here: Clave 50, Importante 40, Rotacion 30.
function hierClass(rank){
  return rank>=50?'tit-hi':rank>=40?'tit-mid':rank>=30?'tit-lo':'tit-out';
}

function weekChip(w){
  const p=w.points;
  const cls = p==null?'wk-none': p<0?'wk-neg': p>=8?'wk-hi': p>=4?'wk-mid':'wk-lo';
  return `<span class="wk ${cls}" title="Jornada ${w.week}">${p==null?'–':p}</span>`;
}

function xClass(v){ return v>=6?'x-hi':v>=3.5?'x-mid':v>=2?'x-lo':'x-bad'; }

// ---- cajon de jugador: un nombre, todas sus acciones ----------------------
const drawer=document.getElementById('drawer');
// The server paints the mode into the header: reading it from there saves a request and stops
// the notice promising something this server does not do.
const MODE=(document.querySelector('.mode b')||{}).textContent||'manual';

// The drawer is a single space that gets rewritten whole, so opening a card from a squad
// destroyed the squad. This is the way back.
let drawerFrom=null;

// ---- direcciones: cada vista abierta tiene la suya --------------------------
// "#<tab>/<view>/<arg>": the tab part is what showTab already understood; the rest is the drawer.
// Every drawer opened is a history entry, so Back closes it instead of leaving the page.
const VIEWS={
  jugador: id=>openDetail(id),
  manager: id=>openManager(id),
  plantillas: week=>openMatchday(week),
  prevision: week=>typeof openForecast==='function'&&openForecast(week),
  jornada: week=>openWeek(week),
  alcance: team=>openReach(team),
  // The comparator was a drawer before it had its tab: old links land on the tab.
  comparar: ()=>openCompare({replace:true}),
};
let routing=false, routed=null, routedOnce=false;

function hashParts(hash=location.hash){
  const [base,view,arg]=(hash||'').replace(/^#/,'').split('/');
  return {base:base||'', view:view||'', arg:arg||''};
}

function markView(view,arg=''){
  if(routing) return;
  const base=hashParts().base||(document.querySelector('.tab.on')||{dataset:{}}).dataset.tab||'decidir';
  const target='#'+base+'/'+view+(arg!==''?'/'+arg:'');
  if(location.hash===target) return;
  const depth=(history.state&&history.state.depth)||0;
  history.pushState({depth:depth+1},'',target);
  routed=target;
}

// Back, forward, a pasted link: whatever the address says is what the page shows.
function route(){
  // Back fires both popstate and hashchange: the second one must not reload the drawer.
  if(routed===location.hash) return;
  routed=location.hash;
  const {base,view,arg}=hashParts();
  // Ids in the address only count when it is how the page was opened: on Back the tray,
  // which has moved on since that entry was written, is the truth.
  if(base==='comparador'&&view&&!VIEWS[view]&&!routedOnce) adoptCompareIds(view);
  routedOnce=true;
  const target=resolveTarget('#'+base);
  if(target){
    const active=document.querySelector('.tab.on');
    if(!active||active.dataset.tab!==target.tab||target.section)
      showTab(target.tab,{section:target.section,updateHash:false});
  }
  routing=true;
  try{
    if(view&&VIEWS[view]) VIEWS[view](arg);
    else if(drawer&&!drawer.hidden) shutDrawer();
  }finally{ routing=false; }
}

function closeDrawer(){
  const depth=(history.state&&history.state.depth)||0;
  // Opened from this page: step back to where it was. Opened from a pasted link: there is no
  // page behind it, so the address is rewritten instead.
  if(depth>0){ history.go(-depth); return; }
  if(hashParts().view) history.replaceState(null,'','#'+(hashParts().base||'decidir'));
  routed=location.hash;
  shutDrawer();
}

function shutDrawer(){
  if(!drawer) return;
  drawerFrom=null;
  drawer.hidden=true;
  panelWide(false);
}

// The browser writes the first value and tick() keeps it every second: the countdown
// of a clause is precisely the figure that expires while you look at it.
function leftUntil(stamp){
  const left=new Date(stamp).getTime()-Date.now();
  if(isNaN(left)) return '—';
  if(left<=0) return 'ya';
  const h=Math.floor(left/3600000), m=Math.floor(left%3600000/60000);
  return h>=24 ? Math.floor(h/24)+'d '+(h%24)+'h' : h>0 ? h+'h '+String(m).padStart(2,'0')+'m'
                                                        : m+'m';
}

// A team's four lines, in the order a pitch is read: from the back forwards.
const LINES=[{id:1,label:'POR'},{id:2,label:'DEF'},{id:3,label:'MED'},{id:4,label:'DEL'}];

// A squad reads by lines rather than as a list: grouped this way it shows at a glance whether
// alguien le falta un defensa o le sobran delanteros.
function byLine(squad){
  return LINES.map(line=>({
    ...line,
    players:(squad||[]).filter(p=>Number(p.position_id)===line.id)
  })).filter(line=>line.players.length);
}

// The player's face in miniature, with the crest behind it when there is no photo.
function chipFace(p){
  return faceOf(p,'xs');
}

// The pitch in miniature: the same four lines as the eleven, attack to keeper, because a squad
// is recognised by its shape before its names.
function miniPitch(m){
  // The eleven he fielded, not the squad: a pitch with two keepers is not a pitch. When that
  // matchday's lineup is unavailable it says so and falls back to the grouped squad.
  const fielded=m.lineup;
  if(!fielded){
    const lines=byLine(m.squad).slice().reverse();
    if(!lines.length) return '<p class="empty">Sin jugadores.</p>';
    return `<p class="drawer-note">No tengo la alineacion de esa jornada; esto es la plantilla
      que tenia.</p>` + `<div class="mini-pitch is-squad">${lines.map(line=>`
      <div class="mini-line">${line.players.map(p=>slotChip(p,line.label)).join('')}</div>`)
      .join('')}</div>`;
  }
  const order=['striker','midfield','defender','goalkeeper'];
  const label={goalkeeper:'POR',defender:'DEF',midfield:'MED',striker:'DEL'};
  return `<div class="mini-pitch">${order.map(line=>{
    const players=fielded[line]||[];
    if(!players.length) return '';
    return `<div class="mini-line">${players.map(p=>p
      ? slotChip(p,label[line],true)
      : `<span class="mini-hole" title="${label[line]} sin cubrir">⚠</span>`).join('')}</div>`;
  }).join('')}</div>` + benchStrip(m.bench);
}

// The slots he left empty: a 4-4-2 with ten is not a 4-4-2, it is a 4-4-2 missing a midfielder,
// and that is points given away.
function gapNote(fielded){
  let put=0, slots=0;
  Object.keys(fielded||{}).forEach(line=>(fielded[line]||[]).forEach(p=>{
    slots++; if(p) put++; }));
  return put<slots ? ` · <span class="md-gap">puso ${put} de ${slots}</span>` : '';
}

function slotChip(p,line,fielded){
  const points = fielded && p.points!=null ? `<span class="mini-points">${p.points}</span>` : '';
  const dim = (!fielded && !p.played) ? ' mini-out' : '';
  return `<button class="mini-slot${dim}" type="button" data-detail="${p.id}"
    title="${p.name} · ${p.team_short||''} · ${line}${(!fielded&&!p.played)?' · no jugaba esa jornada':''}">
    ${chipFace(p)}<span class="mini-name">${p.name}</span>${points}</button>`;
}

// That matchday's bench: what he had and did not field, the other half of the decision.
function benchStrip(bench){
  if(!bench || !bench.length) return '';
  return `<div class="mini-bench"><span class="mini-bench-label">banquillo</span>${
    bench.map(p=>`<button class="mini-benched" type="button" data-detail="${p.id}"
      title="${p.name} · ${p.team_short||''}">${chipFace(p)}${p.name}</button>`).join('')}</div>`;
}

// What each one held on a given matchday. The API keeps no history: this comes from unwinding
// the transfer log back to the first kick-off, so what is shown is what was, not what is.
async function openMatchday(week){
  if(!drawer) return;
  markView('plantillas',week);
  drawer.hidden=false;
  panelWide(false);
  const body=drawer.querySelector('.drawer-body');
  body.innerHTML='<p class="empty">Reconstruyendo…</p>';
  let d;
  try{
    const res=await fetch('/api/matchday/'+week);
    if(!res.ok) throw new Error(res.status);
    d=await res.json();
  }catch(e){
    body.innerHTML='<p class="empty">No he podido reconstruir esa jornada.</p>';
    return;
  }
  body.innerHTML=`
    <div class="drawer-head"><h3>Jornada ${d.week}</h3></div>
    <p class="sub">plantillas a ${String(d.kickoff).slice(0,10)} · ${String(d.kickoff).slice(11,16)}</p>
    ${(d.managers||[]).map(m=>`
      <div class="md-manager${m.is_me?' md-mine':''}">
        <div class="md-head">
          ${m.week_rank!=null?`<span class="md-rank${m.week_rank<=3?' md-podium':''}"
            >${m.week_rank}º</span>`:''}
          <button class="p-name" type="button" data-manager="${m.team_id}">${m.manager}</button>
          <span class="md-count">${m.lineup
            ? (m.week_points!=null?`<b>${Math.round(m.week_points)} pts</b> · `:'')
              +(m.formation||[]).join('-')+gapNote(m.lineup)
            : `${m.playing} de ${m.players} jugaron`}</span>
        </div>
        ${miniPitch(m)}
      </div>`).join('')}
    <p class="drawer-note">Reconstruido del log de traspasos: la API solo dice quien tiene a quien
      ahora. Los jugadores en gris no jugaban esa jornada.</p>`;
  wireDetails(body); wireManagers(body);
}

// What was expected of each eleven on a matchday and what it made. Recorded player by player
// before each kick-off, so the forecast is the one that stood when the ball rolled.
async function openForecast(week){
  if(!drawer) return;
  markView('prevision',week);
  drawer.hidden=false;
  panelWide(false);
  const body=drawer.querySelector('.drawer-body');
  body.innerHTML='<p class="empty">Cargando…</p>';
  let d;
  try{
    const res=await fetch('/api/forecast/'+week);
    if(!res.ok) throw new Error(res.status);
    d=await res.json();
  }catch(e){
    body.innerHTML='<p class="empty">No guarde la prevision de esa jornada.</p>';
    return;
  }
  const one=v=>v==null?'—':(Math.round(v*10)/10).toString();
  const diff=(real,planned)=>real==null?'<span class="fc-diff">—</span>'
    :`<span class="fc-diff ${real>=planned?'fc-up':'fc-down'}">${real>=planned?'+':''}${one(real-planned)}</span>`;
  const managers=[...(d.managers||[])].sort((a,b)=>b.actual-a.actual||b.planned-a.planned);
  // Level on points is level on the place, the way the game ranks a matchday.
  managers.forEach(m=>{ m.place=1+managers.filter(o=>o.actual>m.actual).length; });
  const chip=(real,scale)=>{
    if(real==null) return '<span>—</span>';
    const p=real/scale;
    const cls=p<0?'wk-neg':p>=8?'wk-hi':p>=4?'wk-mid':'wk-lo';
    return `<span><span class="wk ${cls}">${one(real)}</span></span>`;
  };
  // The fill says how far from the forecast, the chip how much: one glance per row.
  const tint=(real,forecast)=>{
    if(real==null) return '';
    const gap=real-forecast, strength=Math.min(Math.abs(gap)/10,1)*22+4;
    return ` style="--fc-tint:color-mix(in srgb,var(${gap>=0?'--good':'--critical'}) ${
      Math.round(strength)}%,transparent)"`;
  };
  const row=(name,planned,real,forecast,scale,place)=>`<span class="fc-place">${
    place?place+'º':''}</span><span class="fc-name">${name}</span>
    <span>${one(planned)}</span>${chip(real,scale)}${diff(real,forecast)}`;
  body.innerHTML=`
    <div class="drawer-head"><h3>Jornada ${d.week} · prevision</h3></div>
    <p class="sub">${d.complete?'terminada':'en juego: el real solo cuenta a quien ya ha jugado'}</p>
    <div class="fc-row fc-head"><span class="fc-place">#</span><span class="fc-name">Manager</span><span>Previsto</span>
      <span>Real</span><span>Dif.</span></div>
    <div class="fc-rows">${managers.map(m=>`
      <details class="fc-team${m.is_me?' fc-me':''}">
        <summary class="fc-row fc-tinted"${tint(m.counted?m.actual:null,m.forecast)}>${
          row(m.short?m.manager+' <span class="fc-short" title="Sin 11 alineados: la jornada cuenta 0">sin 11</span>':m.manager,m.planned,m.counted?m.actual:null,m.forecast,Math.max(m.counted,1),
            m.counted?m.place:null)}</summary>
        ${(m.players||[]).map(p=>`<button class="fc-row fc-player fc-tinted" type="button"
          data-detail="${p.id}"${tint(p.points,p.forecast)}>${
          row(p.name,p.forecast,p.points,p.forecast,1)}</button>`).join('')}
      </details>`).join('')}
    </div>
    <p class="drawer-note">Pulsa un manager para ver a sus jugadores. La prevision de cada uno es
      la que tenia justo antes de su partido.${d.counted?` De media se fallo por
      ${one(d.mean_abs_error)} puntos por jugador.`:''}</p>`;
  wireDetails(body);
}

// The league log in one list: newest first as the server sends it, or biggest first.
function wireMatchdays(root=document){
  root.querySelectorAll('button[data-matchday]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>openMatchday(button.dataset.matchday));
  });
  root.querySelectorAll('button[data-forecast]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>openForecast(button.dataset.forecast));
  });
}

function wireManagers(root=document){
  root.querySelectorAll('button[data-manager]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',(e)=>{ e.stopPropagation(); openManager(button.dataset.manager); });
  });
  root.querySelectorAll('button[data-back]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>{
      usage.click('ficha','volver a la plantilla');
      openManager(button.dataset.back);
    });
  });
}

// A rival's squad. It comes off the world already in memory, so it costs no request to LaLiga:
// it only had to be askable.
async function openManager(teamId){
  if(!drawer) return;
  markView('manager',teamId);
  drawer.hidden=false;
  panelWide(false);
  const body=drawer.querySelector('.drawer-body');
  body.innerHTML='<p class="empty">Cargando…</p>';
  let d;
  try{
    const res=await fetch('/api/manager/'+teamId);
    if(!res.ok) throw new Error(res.status);
    d=await res.json();
  }catch(e){
    body.innerHTML='<p class="empty">No he podido leer esa plantilla.</p>';
    return;
  }
  const pos=d.position?`${d.position}º`:'—';
  body.dataset.view='manager';
  body.dataset.manager=String(teamId);
  body.dataset.managerName=d.manager||'';
  drawerFrom=null;
  body.innerHTML=`
    <div class="drawer-head"><h3>${d.manager}</h3></div>
    <p class="sub">${d.team_name||''} · ${pos} con ${Math.round(d.points)} puntos</p>
    <dl class="drawer-stats">
      <div><dt>Caja estimada</dt><dd>${exact(Math.round(d.estimated_cash))}</dd></div>
      <div><dt>Valor de plantilla</dt><dd>${exact(Math.round(d.squad_value))}</dd></div>
      <div><dt>Suma de clausulas</dt><dd>${exact(Math.round(d.clause_total))}</dd></div>
      <div><dt>xPts de la plantilla</dt><dd>${(d.xpts_total||0).toFixed(1)}</dd></div>
      <div><dt>Jugadores</dt><dd>${d.players}${d.listed?` · ${d.listed} en venta`:''}</dd></div>
      <div><dt>Clausulas bloqueadas</dt><dd>${d.clauses_locked} de ${d.players}</dd></div>
    </dl>
    ${byLine(d.squad).map(line=>`
      <div class="squad-line">
        <span class="squad-line-label pos pos-${line.label.toLowerCase()}">${line.label}</span>
        <div class="squad-list">${line.players.map(managerRow).join('')}</div>
      </div>`).join('')}
    <p class="drawer-note">La caja es una estimacion reconstruida del log de traspasos, no un dato
      que publique el juego. Pulsa un jugador para su ficha.</p>`;
  wireDetails(body);
  tick();
}

// One row per player: what decides whether he can be reached, and for how much.
function managerRow(p){
  const listing=p.market||{};
  const chips=[];
  if(listing.market_id) chips.push(`<span class="chip">en venta ${fmt(listing.min_bid)}</span>`);
  if(p.shielded) chips.push(p.shielded_until
    ? `<span class="chip chip-warn">blindado <span
        data-deadline="${p.shielded_until}">${leftUntil(p.shielded_until)}</span></span>`
    : '<span class="chip chip-warn">blindado</span>');
  else if(p.clause_locked&&p.clause_locked_until)
    chips.push(`<span class="chip chip-warn">clausula en <span data-deadline="${p.clause_locked_until}">…</span></span>`);
  else if(p.clause) chips.push('<span class="chip chip-good">clausula pagable</span>');
  if(p.sale_locked) chips.push('<span class="chip chip-warn">🔒 recien fichado</span>');
  if(!p.available) chips.push('<span class="chip chip-bad">no puntua</span>');
  return `<div class="squad-row">
    <span class="squad-who">
      ${chipFace(p)}
      <button class="p-name" type="button" data-detail="${p.id}">${p.name}</button>
      <span class="pos pos-${String(p.position||'').toLowerCase().slice(0,3)}">${p.position}</span>
      <button class="cmp-add small" type="button" data-cmp="${p.id}" data-cmp-name="${p.name}"
        data-cmp-pos="${p.position||''}">+</button>
    </span>
    <span class="squad-nums">
      <b>${fmt(p.value)}</b>
      <span title="Clausula">${p.clause?fmt(p.clause):'—'}</span>
      <span title="xPts por jornada">${(p.xpts||0).toFixed(1)}</span>
    </span>
    <span class="squad-chips">${chips.join('')}</span>
  </div>`;
}

// The player's card, drawn by /assets/ui in the drawer as a centred popup.
async function openDetail(playerId){
  if(!drawer) return;
  markView('jugador',playerId);
  usage.click('ficha','abrir ficha',String(playerId));
  const body=drawer.querySelector('.drawer-body');
  // Read before writing: the drawer is the only thing that knows what was in it.
  drawerFrom = !drawer.hidden && body.dataset.view==='manager'
    ? {id:body.dataset.manager, label:body.dataset.managerName||'la plantilla'}
    : null;
  drawer.hidden=false;
  panelWide(false);
  drawer.classList.add('as-pop');
  body.dataset.view='player';
  body.innerHTML='<p class="empty">Cargando…</p>';
  const ui=await uiModule;
  if(ui) ui.mountPlayer(body,String(playerId),drawerFrom);
}

function wireDetails(root=document){
  root.querySelectorAll('button[data-detail]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>openDetail(button.dataset.detail));
  });
}

// ---- tooltip propio: el title nativo tarda casi un segundo -------------------
// These are figures read in passing (a padlock, a crest, an "est."), and a second of waiting is
// exactly what stops them being read. Floating and attached to the body rather than an ::after,
// which would be clipped inside a scrolling table.
let tipBox=null;
function showTip(target){
  const message=target.dataset.tip;
  if(!message) return;
  if(!tipBox){
    tipBox=document.createElement('div');
    tipBox.className='tip-float';
    document.body.appendChild(tipBox);
  }
  tipBox.textContent=message;
  tipBox.hidden=false;
  const anchor=target.getBoundingClientRect(), own=tipBox.getBoundingClientRect();
  const left=Math.max(8,Math.min(anchor.left+anchor.width/2-own.width/2,
    window.innerWidth-own.width-8));
  let top=anchor.top-own.height-8;
  if(top<8) top=anchor.bottom+8;
  tipBox.style.left=left+'px';
  tipBox.style.top=top+'px';
}
const hideTip=()=>{ if(tipBox) tipBox.hidden=true; };
document.addEventListener('mouseover',(event)=>{
  const target=event.target.closest('[data-tip]');
  if(target) showTip(target); else hideTip();
});
document.addEventListener('mouseout',(event)=>{
  if(event.target.closest&&event.target.closest('[data-tip]')) hideTip();
});
// Without a mouse there is no hovering, so a tap opens it and the next one closes it.
document.addEventListener('click',(event)=>{
  if(!window.matchMedia('(hover:none)').matches) return;
  const target=event.target.closest('[data-tip]');
  if(target) showTip(target); else hideTip();
});
window.addEventListener('scroll',hideTip,{passive:true});
document.addEventListener('focusin',(event)=>{
  const target=event.target.closest&&event.target.closest('[data-tip]');
  if(target) showTip(target);
});
document.addEventListener('focusout',hideTip);

// ---- comparador: un fichaje es siempre "en vez de quien" --------------------
// The tray and the comparator's tab are /assets/ui/compare.js; these reach it once it is loaded.
const cmpStore=()=>window.panelCompare;
const cmpHas=(id)=>!!cmpStore()&&cmpStore().has(id);
function cmpAdd(id,name,pos){ return cmpStore()?cmpStore().add(id,name,pos):false; }
function cmpDrop(id){ if(cmpStore()) cmpStore().drop(id); }
// The "+" buttons report.js still draws (a rival's squad in the drawer) follow the tray too.
function drawTray(){
  document.querySelectorAll('button[data-cmp]').forEach(button=>{
    const on=cmpHas(button.dataset.cmp), short=button.classList.contains('small');
    button.classList.toggle('on',on);
    button.textContent=on?(short?'✓':'✓ comparando'):(short?'+':'+ comparar');
    button.title=on?'Quitar del comparador':'Añadir al comparador';
  });
}
addEventListener('panel:tray',drawTray);
document.addEventListener('click',(event)=>{
  const add=event.target.closest('button[data-cmp]');
  if(!add) return;
  event.stopPropagation();
  if(cmpHas(add.dataset.cmp)) cmpDrop(add.dataset.cmp);
  else cmpAdd(add.dataset.cmp,add.dataset.cmpName,add.dataset.cmpPos);
});

function panelWide(on){
  const panel=drawer&&drawer.querySelector('.drawer-panel');
  if(panel) panel.classList.toggle('wide',!!on);
  // Only the player's card is a centred popup; every other view keeps the side drawer.
  if(drawer) drawer.classList.remove('as-pop');
}

// The comparator is a tab of its own; its address carries who is in it, so a link reopens
// the same comparison.
function compareHash(){
  return cmpStore()?cmpStore().hash():'#comparador';
}

function adoptCompareIds(list){
  if(cmpStore()) cmpStore().adopt(list); else window.__cmpAdopt=list;
}

function openCompare({replace=false}={}){
  if(drawer&&!drawer.hidden) shutDrawer();
  if(replace) history.replaceState(null,'',compareHash());
  else history.pushState(null,'',compareHash());
  routed=location.hash;
  showTab('comparador',{updateHash:false});
}

if(drawer){
  drawer.querySelector('.drawer-close').addEventListener('click',closeDrawer);
  drawer.addEventListener('click',(e)=>{ if(e.target===drawer) closeDrawer(); });
  // A dialog opened from the card closes first; the card waits for its own Escape.
  document.addEventListener('keydown',(e)=>{
    if(e.key==='Escape'&&!drawer.hidden&&!document.querySelector('.modal:not([hidden])')) closeDrawer();
  });
}

// ---- find a player and open his card --------------------------------------
// A card was reachable only by finding the player's row in one of the thirty sections. This runs
// off the same /api/compare the comparator uses, so there is no new index.
function wireFindPlayer(){
  const input=document.getElementById('find');
  if(!input||input.dataset.wired) return;
  input.dataset.wired='1';
  const list=input.parentElement.querySelector('.find-results');
  const pop=input.closest('.find-pop'), box=input.closest('.head-find');
  const trigger=box&&box.querySelector('.find-btn');
  let timer=null, found=[], cur=0;
  const hide=()=>{ list.hidden=true; list.innerHTML=''; found=[]; cur=0; };
  // The search is a magnifier until asked for: then the field opens over the end of the bar.
  const expand=()=>{ if(pop){ pop.hidden=false; box.classList.add('open'); } input.focus(); };
  const collapse=()=>{ hide(); input.value=''; input.blur(); if(pop){ pop.hidden=true; box.classList.remove('open'); } };
  if(trigger) trigger.addEventListener('click',()=>pop&&!pop.hidden?collapse():expand());
  const open=(id)=>{ collapse(); openDetail(id); };
  const paint=()=>{
    list.innerHTML=found.map((p,i)=>`
      <button class="cmp-hit${i===cur?' first':''}" type="button" data-find="${p.id}">
        <span class="cmp-hit-who">
          ${faceOf(p,'xs')}
          <b>${p.name}</b>
          <span class="pos pos-${String(p.position||'').toLowerCase().slice(0,3)}">${p.position}</span>
        </span>
        <span class="cmp-hit-num"><span class="crest crest-${p.team_id}"></span>${p.team_short||''} · ${p.is_mine?'tuyo':(p.owner||'libre')}
          <b>${fmt(p.value)}</b></span>
      </button>`).join('');
    list.hidden=false;
  };
  const run=async()=>{
    const query=input.value.trim();
    if(query.length<2){ hide(); return; }
    try{
      const res=await fetch('/api/compare?q='+encodeURIComponent(query));
      if(!res.ok) throw new Error(res.status);
      const matches=(await res.json()).matches||[];
      // Your own first: the card you look up most is one of yours.
      found=[...matches.filter(p=>p.is_mine),...matches.filter(p=>!p.is_mine)];
    }catch(e){ hide(); return; }
    cur=0;
    if(!found.length){
      list.innerHTML='<p class="cmp-none">Nadie con ese nombre</p>';
      list.hidden=false;
      return;
    }
    paint();
  };
  input.addEventListener('input',()=>{ clearTimeout(timer); timer=setTimeout(run,180); });
  input.addEventListener('keydown',(event)=>{
    if(event.key==='Escape'){ collapse(); trigger&&trigger.focus(); }
    if(!found.length) return;
    if(event.key==='ArrowDown'||event.key==='ArrowUp'){
      event.preventDefault();
      cur=Math.max(0,Math.min(found.length-1,cur+(event.key==='ArrowDown'?1:-1)));
      paint();
      list.querySelector('.cmp-hit.first')?.scrollIntoView({block:'nearest'});
    }
    if(event.key==='Enter'){ event.preventDefault(); open(found[cur].id); }
  });
  list.addEventListener('click',(event)=>{
    const hit=event.target.closest('button[data-find]');
    if(hit) open(hit.dataset.find);
  });
  document.addEventListener('click',(event)=>{
    if(!(box||input.parentElement).contains(event.target)) collapse();
  });
  document.addEventListener('keydown',(event)=>{
    if(event.key!=='/'||event.metaKey||event.ctrlKey) return;
    const at=document.activeElement;
    if(at&&(at.tagName==='INPUT'||at.tagName==='TEXTAREA'||at.isContentEditable)) return;
    event.preventDefault();
    expand();
  });
}

// ---- tabs: one view at a time ---------------------------------------------
const TABS=[
  {id:'decidir', label:'Decidir', sections:['ahora']},
  // By direction: a bid sits with the market it was made in, an offer with the sale it answers.
  {id:'comprar', label:'Comprar', sections:['v-comprar']},
  {id:'vender', label:'Vender', sections:['v-vender']},
  {id:'clausulas', label:'Cláusulas', sections:['v-clausulas']},
  {id:'plantilla', label:'Plantilla', sections:['once','v-plantilla','plantilla']},
  {id:'partidos', label:'Partidos', sections:['v-partidos']},
  // Everyone else's in its place: their whole squads and what they can pay for yours.
  {id:'rivales', label:'Rivales', sections:['v-rivales']},
  {id:'liga', label:'Liga', sections:['evolucion','movimientos','normas']},
  {id:'ranking', label:'Ranking', sections:['v-ranking']},
  {id:'comparador', label:'Comparador', sections:['comparador']},
];

// A hash can be a tab (#comprar) or a section (#v-clausulas), and the second is what the
// links carry, so it has to be resolved to the tab that owns it.
// The tabs before they were split by direction, and the sections of the old tables, so old
// links and bookmarks still land on the tab that took their place.
const TAB_ALIASES={mercado:'comprar', misofertas:'vender',
  plan:'decidir', acciones:'decidir', caja:'decidir', chollos:'comprar',
  fichajes:'comprar', enventa:'comprar', mispujas:'comprar', seguimiento:'comprar', resueltas:'comprar',
  misventas:'vender', ofertas:'vender', siempre:'vender', ventas:'plantilla',
  subir:'clausulas', programados:'clausulas', calendario:'clausulas', vencimientos:'clausulas',
  oportunidades:'clausulas', jornada:'partidos', pinta:'rivales', rentabilidad:'ranking'};

function resolveTarget(hash){
  let id=(hash||'').replace(/^#/,'');
  id=TAB_ALIASES[id]||id;
  if(!id) return null;
  if(TABS.some(t=>t.id===id)) return {tab:id, section:null};
  const own=document.getElementById(id);
  if(own&&own.dataset.tab) return {tab:own.dataset.tab, section:id};
  const owner=TABS.find(t=>t.sections.includes(id));
  return owner ? {tab:owner.id, section:id} : null;
}

// One rival squad at a time: twelve stacked is a lot of scrolling for a question about one.
const RIVAL_KEY='fantasy:rival';
function applyRivalPick(){
  const sections=[...document.querySelectorAll('section[data-tab="rivales"][id^="rival-"]')];
  if(!sections.length) return;
  // Outside its tab showTab is in charge: nothing can be shown again from here.
  const active=document.querySelector('.tab.on');
  if(!active||active.dataset.tab!=='rivales') return;
  const ids=sections.map(s=>s.id);
  let choice=null;
  try{ choice=localStorage.getItem(RIVAL_KEY); }catch(e){}
  if(choice!=='all'&&!ids.includes(choice)) choice=ids[0];
  sections.forEach(s=>{ s.hidden = choice!=='all'&&s.id!==choice; });
  const select=document.getElementById('rival-pick');
  if(select&&select.value!==choice) select.value=choice;
}

document.addEventListener('change',(event)=>{
  const select=event.target.closest&&event.target.closest('#rival-pick');
  if(!select) return;
  try{ localStorage.setItem(RIVAL_KEY,select.value); }catch(e){}
  applyRivalPick();
});

// Faces, names and rows in the views open the player's card; a button inside them does its own.
document.addEventListener('click',(event)=>{
  const target=event.target;
  if(!target.closest) return;
  if(target.closest('button, a, input, select, label')) return;
  const team=target.closest('.mk [data-team]');
  if(team&&!target.closest('[data-pid]')){ openManager(team.dataset.team); return; }
  const host=target.closest('.mk [data-pid], .md-xi [data-pid]');
  if(host&&host.dataset.pid){ event.preventDefault(); openDetail(host.dataset.pid); return; }
  const week=target.closest('.mk .hist[data-week]');
  if(week) openWeek(week.dataset.week);
});
document.addEventListener('keydown',(event)=>{
  const week=event.target.closest&&event.target.closest('.mk .hist[data-week]');
  if(week&&(event.key==='Enter'||event.key===' ')){ event.preventDefault(); openWeek(week.dataset.week); }
});

// ---- Partidos: my points per matchday, and each matchday's whole table -------------
// Which of my players a rival's cash reaches: drawn on the server, kept in the page.
async function openReach(teamId){
  if(!drawer) return;
  markView('alcance',teamId);
  drawer.hidden=false;
  panelWide(false);
  drawer.classList.add('as-pop');
  const body=drawer.querySelector('.drawer-body');
  body.dataset.view='reach';
  const ui=await uiModule;
  if(ui) ui.mountDrawer(body,'reach',{team:String(teamId)});
}
document.addEventListener('click',(event)=>{
  const button=event.target.closest&&event.target.closest('button[data-reach]');
  if(!button) return;
  event.stopPropagation();
  usage.click('rivales','alcance',button.dataset.reach);
  openReach(button.dataset.reach);
},true);

async function openWeek(week){
  if(!drawer) return;
  markView('jornada',week);
  drawer.hidden=false;
  panelWide(false);
  drawer.classList.add('as-pop');
  const body=drawer.querySelector('.drawer-body');
  body.dataset.view='week';
  body.innerHTML='<p class="empty">Cargando…</p>';
  const ui=await uiModule;
  if(ui) ui.mountDrawer(body,'week',{week:String(week)});
}

function showTab(id,{section=null,updateHash=true}={}){
  const tab=TABS.find(t=>t.id===id)||TABS[0];
  const was=(document.querySelector('.tab.on')||{dataset:{}}).dataset.tab;
  // Un enlace a un rival concreto manda sobre lo elegido en el desplegable.
  if(section&&section.startsWith('rival-')){
    try{ localStorage.setItem(RIVAL_KEY,section); }catch(e){}
  }
  document.querySelectorAll('section[id]').forEach(s=>{
    // A section can name its own tab: the ones born at runtime (one per rival) cannot be in
    // a list written here.
    s.hidden = s.dataset.tab ? s.dataset.tab!==tab.id : !tab.sections.includes(s.id);
  });
  document.querySelectorAll('.tab').forEach(b=>{
    const on=b.dataset.tab===tab.id;
    b.classList.toggle('on',on);
    b.setAttribute('aria-selected',on?'true':'false');
    // The strip scrolls, so the tab that just lit up can be outside it. 'nearest' keeps
    // the page itself where it was.
    if(on) b.scrollIntoView({block:'nearest',inline:'center'});
  });
  try{ localStorage.setItem('fantasy-tab',tab.id); }catch(e){}
  dispatchEvent(new Event('panel:tab'));
  usage.tab(tab.id);
  if(updateHash){
    // replaceState, not assignment: we want neither a history entry per click nor
    // disparar hashchange sobre nosotros mismos.
    history.replaceState(null,'','#'+(section||tab.id));
  }
  applyRivalPick();
  drawTray();
  if(section){
    const node=document.getElementById(section);
    if(node) node.scrollIntoView({behavior:'smooth',block:'start'});
  }
}

function wireTabs(){
  wireFindPlayer();
  const bar=document.getElementById('tabs');
  if(!bar||bar.dataset.wired) return;
  bar.dataset.wired='1';
  bar.querySelectorAll('.tab').forEach(b=>
    b.addEventListener('click',()=>{
      if(drawer&&!drawer.hidden) shutDrawer();
      history.pushState(null,'','#'+b.dataset.tab);
      routed=location.hash;
      showTab(b.dataset.tab,{updateHash:false});
    }));
  window.addEventListener('popstate',route);
  // A plain <a href="#..."> link moves the hash without popstate in some browsers.
  window.addEventListener('hashchange',route);
  let saved=null;
  try{ saved=localStorage.getItem('fantasy-tab'); }catch(e){}
  if(resolveTarget('#'+hashParts().base)) route();
  else showTab(TAB_ALIASES[saved]||saved||'decidir');
}

// ---- push: swap out only what changed -------------------------------------
let currentVersion=null;

// Sections whose content the browser paints rather than the server: the fragment that arrives
// is an empty shell, so swapping it in would wipe what is inside (and any unsaved lineup
// changes).
const CLIENT_OWNED=new Set(['once','comparador']);

// The balance lives in the Caja card; the live refresh rewrites it there and keeps the exact
// figure for the arithmetic a typed amount needs.
let myCash=null;

function showCash(amount){
  if(typeof amount!=='number') return;
  myCash=amount;
  const kpi=document.getElementById('kpi-cash');
  if(kpi){ kpi.textContent=amount>=1e6?(amount/1e6).toFixed(1).replace('.',',')+'M':Math.round(amount/1e3)+'K'; kpi.title='Tu saldo ahora mismo: '+exact(amount); }
  const exactLine=kpi&&kpi.parentElement.querySelector('.s');
  if(exactLine&&/€$/.test(exactLine.textContent.trim())) exactLine.textContent=exact(amount);
}

async function swap(){
  const res=await fetch('/api/fragments');
  if(!res.ok) return;
  const data=await res.json();
  if(data.version===currentVersion) return;
  // A section that does not exist yet in this page cannot appear on its own: the browser has
  // the old HTML (nowhere to put it) and the old JS (no idea which tab it belongs to), so until
  // now it was ignored in silence and went unseen until a manual reload.
  const missing=Object.keys(data.sections)
    .filter(id=>!CLIENT_OWNED.has(id)&&!document.getElementById(id));
  if(missing.length){
    // Except mid-operation: reloading with the dialog open would take it down
    // delante. Se reintenta en el siguiente aviso, y la version no se toca hasta entonces.
    if((drawer&&!drawer.hidden)||document.querySelector('.modal')) return;
    location.reload();
    return;
  }
  currentVersion=data.version;
  dispatchEvent(new CustomEvent('panel:version',{detail:data.version}));
  showCash(data.cash);
  liveTip();
  Object.entries(data.sections).forEach(([id,inner])=>{
    if(CLIENT_OWNED.has(id)) return;
    const node=document.getElementById(id);
    if(node&&(node.dataset.view||node.dataset.ui)) return;
    if(node && node.innerHTML!==inner) node.innerHTML=inner;
  });
  wireDetails(); wireManagers(); wireMatchdays(); tick();
  showTab(document.querySelector('.tab.on')?.dataset.tab||'decidir',
          {updateHash:false});
  const stamp=document.getElementById('live-stamp');
  if(stamp) stamp.textContent='actualizado '+new Date().toLocaleTimeString('es-ES');
}

const EFFECT_LABELS={cash:'Saldo',squad:'Jugadores',squad_value:'Valor de la plantilla',
                     listed:'En el mercado',offers:'Ofertas recibidas',
                     points:'Puntos de la plantilla',absences:'Bajas'};
const OPERATION_LABELS={sell_to_market:'Puesto en venta',accept_offer:'Oferta aceptada',
  decline_offer:'Oferta rechazada',withdraw:'Retirado del mercado',bid:'Puja enviada',
  modify_bid:'Puja modificada',cancel_bid:'Puja cancelada',direct_offer:'Oferta directa',
  pay_clause:'Clausulazo pagado',raise_clause:'Clausula subida',
  save_lineup:'Alineacion guardada',policy:'Instruccion ejecutada',
  shield_player:'Jugador blindado',shield:'Blindaje programado',
  traspaso:'Se ha movido la liga',mercado:'Cambios en el mercado',
  partido:'Partido en juego',
  vencimiento:'Ha vencido algo',refresco:'Actualizado'};

function showEffect(message){
  // What an operation actually moves: the before and the after, not a "done".
  const rows=Object.entries(message.changed||{}).map(([key,change])=>{
    const money=key==='cash'||key==='squad_value';
    const fmt=(n)=> money?exact(n||0):String(n??0);
    const worse=key==='absences';
    const sign=change.delta>0?(worse?'down':'up'):(change.delta<0?(worse?'up':'down'):'');
    return `<tr><th>${EFFECT_LABELS[key]||key}</th><td>${fmt(change.before)}</td>`
      +`<td class="arrow">→</td><td>${fmt(change.after)}</td>`
      +`<td class="delta ${sign}">${change.delta>0?'+':''}${fmt(change.delta)}</td></tr>`;
  }).join('');
  if(!rows) return;
  const box=document.createElement('div');
  box.className='effect';
  box.innerHTML=`<button class="effect-close" aria-label="Cerrar">×</button>`
    +`<h4>${OPERATION_LABELS[message.operation]||message.operation}</h4>`
    +`<table>${rows}</table>`;
  document.body.appendChild(box);
  box.querySelector('.effect-close').addEventListener('click',()=>box.remove());
  requestAnimationFrame(()=>box.classList.add('in'));
  setTimeout(()=>{box.classList.remove('in');setTimeout(()=>box.remove(),400);},12000);
}

// The live dot says the connection, the build and when the page last changed.
let liveState='Sin conexión en vivo', liveAt=new Date();
function liveTip(state){
  if(state) liveState=state; else liveAt=new Date();
  const dot=document.getElementById('live-dot');
  if(!dot) return;
  const hm=String(liveAt.getHours()).padStart(2,'0')+':'+String(liveAt.getMinutes()).padStart(2,'0');
  dot.dataset.tip=[liveState, dot.dataset.build, 'actualizado '+hm].filter(Boolean).join(' · ');
}

function connect(){
  const dot=document.getElementById('live-dot');
  const source=new EventSource('/api/events');
  source.onopen=()=>{ if(dot){ dot.className='live-on'; liveTip('En vivo'); } };
  source.onmessage=(event)=>{
    const message=JSON.parse(event.data);
    if(message.type==='effect'){
      showEffect(message);
      if(message.version!==currentVersion) swap();
      return;
    }
    if(message.type==='state'||message.type==='hello'){
      if(message.version!==currentVersion) swap();
    }
  };
  source.onerror=()=>{
    if(dot){ dot.className='live-off'; liveTip('Sin conexión: reintentando'); }
    source.close(); setTimeout(connect,5000);
  };
}

// What /assets/ui borrows from this file: the dialogs it does not redraw and the drawer's routing.
window.panel={openCompare, isView:(name)=>!!VIEWS[name], routedTo:(hash)=>{ routed=hash; },
  openDetail, openManager, openWeek, openReach, applyRivalPick, openMatchday, openForecast, closeDrawer,
  cmpHas, cmpAdd, cmpDrop, usage,
  goto:(where)=>{ const target=resolveTarget(where);
    if(target) showTab(target.tab,{section:target.section}); else showTab(where); }};

wireDetails(); wireManagers(); wireMatchdays();
wireTabs(); tick(); drawTray();
{
  const stamped=document.querySelector('.topbar[data-cash]');
  if(stamped) myCash=+stamped.dataset.cash;
  liveTip();
}
if(window.EventSource && location.protocol.startsWith('http')) connect();

// ---- legacy (fichero estatico) ----
