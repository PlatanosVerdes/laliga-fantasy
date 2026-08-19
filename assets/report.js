
// ---- estado de filtros, para que sobreviva a un recambio de seccion --------
const filterState = {pos:'all', price:'', text:''};

function wireTables(root=document){
  root.querySelectorAll('table.sortable').forEach(table=>{
    if(table.dataset.wired) return;
    table.dataset.wired='1';
    table.querySelectorAll('th').forEach((th,index)=>{
      th.addEventListener('click',()=>{
        const body=table.tBodies[0], rows=[...body.rows];
        const numeric=['money','pct','num','int','pct_plain','spark','verdict','mag','ideal','hours','ratio'].includes(th.dataset.kind);
        const desc=!th.classList.contains('sorted-desc');
        table.querySelectorAll('th').forEach(h=>h.classList.remove('sorted-asc','sorted-desc'));
        th.classList.add(desc?'sorted-desc':'sorted-asc');
        rows.sort((a,b)=>{
          const x=a.cells[index].dataset.sort, y=b.cells[index].dataset.sort;
          const cmp=numeric?(parseFloat(x||0)-parseFloat(y||0)):String(x).localeCompare(String(y),'es');
          return desc?-cmp:cmp;
        });
        rows.forEach(r=>body.appendChild(r));
      });
    });
  });
}

function applyFilters(){
  const maxPrice=parseFloat(filterState.price)||Infinity;
  const needle=filterState.text.trim().toLowerCase();
  document.querySelectorAll('.filters').forEach(bar=>{
    const scope=bar.closest('section');
    let shown=0,total=0;
    scope.querySelectorAll('tr[data-position]').forEach(row=>{
      total++;
      const ok=(filterState.pos==='all'||row.dataset.position===filterState.pos)
        && parseFloat(row.dataset.price)<=maxPrice
        && (!needle||row.dataset.name.includes(needle));
      row.hidden=!ok; if(ok) shown++;
    });
    const counter=bar.querySelector('.f-count');
    if(counter) counter.textContent=shown+' de '+total+' filas';
  });
}

function wireFilters(root=document){
  root.querySelectorAll('.filters').forEach(bar=>{
    if(bar.dataset.wired) return;
    bar.dataset.wired='1';
    const pos=bar.querySelector('.f-pos'), price=bar.querySelector('.f-price'),
          text=bar.querySelector('.f-text'), reset=bar.querySelector('.f-reset');
    pos.value=filterState.pos; price.value=filterState.price; text.value=filterState.text;
    const sync=()=>{ filterState.pos=pos.value; filterState.price=price.value;
                     filterState.text=text.value; applyFilters(); };
    [pos,price,text].forEach(el=>el.addEventListener('input',sync));
    reset.addEventListener('click',()=>{ filterState.pos='all'; filterState.price='';
      filterState.text=''; pos.value='all'; price.value=''; text.value=''; applyFilters(); });
  });
  applyFilters();
}

// ---- favoritos -------------------------------------------------------------
function wireStars(root=document){
  root.querySelectorAll('button.star').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click', async ()=>{
      const on=button.classList.contains('on');
      const paint=(state,el)=>{ el.classList.toggle('on',state);
        el.textContent=state?'★':'☆';
        el.setAttribute('aria-pressed',state?'true':'false'); };
      paint(!on,button);
      try{
        const res=await fetch('/api/favourite',{method:'POST',
          headers:{'Content-Type':'application/json'},
          body:JSON.stringify({id:button.dataset.player,name:button.dataset.name})});
        if(!res.ok) throw new Error(res.status);
        const data=await res.json();
        document.querySelectorAll(`button.star[data-player="${button.dataset.player}"]`)
          .forEach(el=>paint(!!data.starred,el));
      }catch(e){
        paint(on,button);
        button.title='Solo se puede cambiar en la version servida (fantasy serve)';
      }
    });
  });
}

// ---- cuentas atras en vivo -------------------------------------------------
function tick(){
  const now=Date.now();
  document.querySelectorAll('[data-deadline]').forEach(el=>{
    const left=new Date(el.dataset.deadline).getTime()-now;
    if(isNaN(left)) return;
    const plain=el.dataset.plain==='1';
    if(left<=0){ el.textContent='ya'; if(!plain) el.className='pill-critical'; return; }
    const h=Math.floor(left/3600000), m=Math.floor(left%3600000/60000),
          s=Math.floor(left%60000/1000);
    el.textContent = h>=24 ? Math.floor(h/24)+'d '+(h%24)+'h'
                   : h>0   ? h+'h '+String(m).padStart(2,'0')+'m'
                           : m+'m '+String(s).padStart(2,'0')+'s';
    // El valor de un widget no es una pastilla: solo cambia el color, no la clase entera.
    if(plain) el.style.color = h<1 ? 'var(--critical)' : h<6 ? 'var(--warning)' : '';
    else el.className = h<1 ? 'pill-critical' : h<24 ? 'pill-warning' : 'pill-neutral';
  });
}
setInterval(tick,1000);

// ---- puja con doble confirmacion ------------------------------------------
const modal=document.getElementById('bid-modal');
let pending=null;

const fmt=(n)=> n==null ? '—' :
  (Math.abs(n)>=1e6 ? (n/1e6).toFixed(2)+'M' : Math.abs(n)>=1e3 ? (n/1e3).toFixed(0)+'K' : String(n));
// El importe se escribe con puntos de millar para que no haya que contar ceros.
const group=(n)=> (n==null||isNaN(n)) ? '' : Number(n).toLocaleString('es-ES');
const digits=(s)=> parseInt(String(s).replace(/[^0-9]/g,''),10);
const exact=(n)=> n==null ? '—' : Number(n).toLocaleString('es-ES')+' €';

function closeModal(){ modal.hidden=true; pending=null; }

// Un unico sitio decide que paso se ve: antes los botones compartian clase con los
// bloques y querySelector solo alcanzaba al primero, asi que el contenido avanzaba
// y los botones se quedaban en el paso uno.
function showStep(step,{confirmLabel='Aceptar'}={}){
  modal.querySelector('#bid-amount-step').hidden = step!==1;
  modal.querySelector('#bid-summary-step').hidden = step!==2;
  modal.querySelector('.bid-next').hidden = step!==1;
  const confirm=modal.querySelector('.bid-confirm');
  confirm.hidden = step!==2;
  confirm.textContent = confirmLabel;
  confirm.disabled = false;
}

function wireBids(root=document){
  // :not([data-op]) porque el boton de aceptar una oferta lleva la clase .bid solo por el
  // color, y se estaba quedando con este manejador: al pulsar Aceptar se abria el dialogo de
  // pujar, con importe minimo NaN. El color no puede decidir que hace un boton.
  root.querySelectorAll('button.bid:not([data-op])').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>openBid(button.dataset));
  });
}

function openBid(data){
  // Con una puja puesta la operacion es cambiarla: la API rechaza una segunda con un 400.
  const existing=data.bid||null;
  pending={market_id:data.market, player_id:data.player, name:data.name,
           min_bid:+data.min, ideal:+data.ideal||0, value:+data.value,
           bid_id:existing, operation:existing?'modify_bid':'bid'};
  modal.hidden=false;
  modal.querySelector('.bid-action').textContent=existing?'Cambiar tu puja por':'Pujar por';
  modal.querySelector('.bid-who').textContent=data.name;
  const suggested = pending.ideal && pending.ideal>=pending.min_bid ? pending.ideal : pending.min_bid;
  const input=modal.querySelector('.bid-amount');
  input.value=group(suggested);
  modal.querySelector('.bid-min').textContent=exact(pending.min_bid);
  modal.querySelector('.bid-ideal').textContent=pending.ideal?exact(pending.ideal):'sin margen';
  modal.querySelector('.bid-value').textContent=exact(pending.value);
  showRivals(+data.bids||0, data.expires);
  const drop=modal.querySelector('.bid-drop');
  drop.hidden=!pending.bid_id;
  showStep(1);
  modal.querySelector('.bid-error').textContent='';
  checkAmount();
  input.focus();
}

function showRivals(count, expires){
  const wrap=modal.querySelector('.bid-rivals-wrap');
  const node=modal.querySelector('.bid-rivals');
  if(!wrap) return;
  const isBid = pending && (pending.operation==='bid' || pending.operation==='modify_bid'
                            || !pending.operation);
  wrap.hidden = !isBid;
  if(!isBid) return;
  node.textContent = count ? String(count) : 'ninguna';
  node.className = 'bid-rivals'+(count?' rivals-on':'');
}

function checkAmount(){
  const input=modal.querySelector('.bid-amount');
  const warn=modal.querySelector('.bid-warn');
  const amount=digits(input.value);
  const caret=input.selectionStart, before=input.value.length;
  input.value=group(amount);
  if(document.activeElement===input){
    const shift=input.value.length-before;
    input.setSelectionRange(Math.max(0,caret+shift), Math.max(0,caret+shift));
  }
  let text='';
  if(!amount) text='Escribe un importe.';
  else if(amount<pending.min_bid) text='Por debajo de la puja minima ('+exact(pending.min_bid)+').';
  if(pending.ideal && amount>pending.ideal) text='Por encima del techo rentable de futbolfantasy.';
  else if(!pending.ideal) text='futbolfantasy no le ve rentabilidad a este precio.';
  warn.textContent=text;
  warn.hidden=!text;
}

if(modal){
  modal.querySelector('.bid-amount').addEventListener('input',checkAmount);
  modal.querySelector('.bid-cancel').addEventListener('click',closeModal);
  modal.addEventListener('click',(e)=>{ if(e.target===modal) closeModal(); });
  document.addEventListener('keydown',(e)=>{ if(e.key==='Escape'&&!modal.hidden) closeModal(); });

  // paso 1: pedir al servidor que valide y devuelva el resumen + token
  modal.querySelector('.bid-next').addEventListener('click', async ()=>{
    const amount=digits(modal.querySelector('.bid-amount').value);
    modal.querySelector('.bid-error').textContent='';
    try{
      const res=await fetch('/api/bid/prepare',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({operation:pending.operation||'bid', amount,
                             market_id:pending.market_id, player_id:pending.player_id,
                             player_team_id:pending.player_team_id,
                             offer_id:pending.offer_id, bid_id:pending.bid_id})});
      const data=await res.json();
      if(!res.ok) throw new Error(data.error||res.status);
      pending.token=data.token;
      const op=pending.operation||'bid';
      const movesCash=['bid','modify_bid','direct_offer','pay_clause',
                       'accept_offer'].includes(op);
      modal.querySelector('.bid-summary').innerHTML =
        `<dl class="bid-dl">
           <dt>Jugador</dt><dd>${data.player_name||pending.name}</dd>
           <dt>${AMOUNT_LABEL[op]||'Importe'}</dt>
             <dd><strong>${exact(data.amount)}</strong></dd>
           <dt>Saldo ahora</dt><dd>${exact(data.cash_before)}</dd>
           ${movesCash?`<dt>${op==='accept_offer'?'Saldo despues':'Saldo si sale'}</dt>
             <dd><strong>${exact(data.cash_after)}</strong></dd>`:''}
         </dl>` +
        (data.warnings||[]).map(w=>`<p class="bid-warn-line">⚠ ${w}</p>`).join('');
      showStep(2,{confirmLabel:CONFIRM_LABEL[pending.operation]||'Aceptar'});
    }catch(err){
      modal.querySelector('.bid-error').textContent=err.message;
    }
  });

  // retirar una puja ya puesta, con el mismo doble paso
  modal.querySelector('.bid-drop').addEventListener('click', async ()=>{
    modal.querySelector('.bid-error').textContent='';
    try{
      const res=await fetch('/api/bid/prepare',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({operation:'cancel_bid',market_id:pending.market_id,
                             bid_id:pending.bid_id,player_id:pending.player_id})});
      const data=await res.json();
      if(!res.ok) throw new Error(data.error||res.status);
      pending.token=data.token;
      modal.querySelector('.bid-summary').innerHTML =
        `<p>Vas a <strong>retirar tu puja</strong> por ${pending.name}.</p>`;
      modal.querySelector('.bid-drop').hidden=true;
      showStep(2);
    }catch(err){ modal.querySelector('.bid-error').textContent=err.message; }
  });

  // paso 2: confirmar de verdad
  modal.querySelector('.bid-confirm').addEventListener('click', async ()=>{
    const button=modal.querySelector('.bid-confirm');
    button.disabled=true; button.textContent='Enviando…';
    try{
      const res=await fetch('/api/bid/confirm',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({token:pending.token})});
      const data=await res.json();
      if(!res.ok) throw new Error(data.error||res.status);
      modal.querySelector('.bid-summary').innerHTML =
        `<p class="bid-ok">${DONE_LABEL[pending&&pending.operation]||'Hecho'}${
          data.dry_run?' (simulacro)':''}.</p>`;
      modal.querySelector('.bid-confirm').hidden=true;
      modal.querySelector('.bid-cancel').textContent='Cerrar';
    }catch(err){
      modal.querySelector('.bid-error').textContent=err.message;
    }finally{
      button.disabled=false; button.textContent='Aceptar';
    }
  });
}

// ---- operaciones genericas (aceptar/rechazar oferta, retirar) --------------
// Cada operacion se llama por su nombre en el boton final y en el resumen: "Pujas" y
// "Saldo si ganas" no significan nada cuando lo que haces es vender.
const CONFIRM_LABEL={};   // el boton dice simplemente Aceptar
const DONE_LABEL={bid:'Puja enviada',sell_to_market:'Puesto en venta',
  accept_offer:'Oferta aceptada',decline_offer:'Oferta rechazada',
  withdraw:'Retirado del mercado',direct_offer:'Oferta enviada',
  pay_clause:'Clausula pagada',raise_clause:'Clausula subida',
  cancel_bid:'Puja retirada',modify_bid:'Puja cambiada'};
const AMOUNT_LABEL={bid:'Pujas',modify_bid:'Nueva puja',sell_to_market:'Precio de venta',
  accept_offer:'Cobras',direct_offer:'Ofreces',pay_clause:'Pagas',
  raise_clause:'Subes la clausula'};

const OP_LABELS={accept_offer:'Aceptar oferta por',decline_offer:'Rechazar oferta por',
                 withdraw:'Retirar del mercado a',sell_to_market:'Poner en venta a'};

function wireOps(root=document){
  root.querySelectorAll('button.op').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click', async ()=>{
      const d=button.dataset;
      pending={operation:d.op, market_id:d.opMarket, offer_id:d.opOffer,
               player_id:d.opPlayer, name:d.opName, amount:+d.opAmount||null};
      modal.hidden=false;
      modal.querySelector('.bid-who').textContent=d.opName;
      modal.querySelector('.bid-action').textContent=OP_LABELS[d.op]||'Confirmar';
      modal.querySelector('.bid-drop').hidden=true;
      modal.querySelector('.bid-error').textContent='';
      modal.querySelector('.bid-summary').innerHTML='<p>Comprobando…</p>';
      showStep(2,{confirmLabel:CONFIRM_LABEL[d.op]||'Aceptar'});
      try{
        const res=await fetch('/api/bid/prepare',{method:'POST',
          headers:{'Content-Type':'application/json'},
          body:JSON.stringify({operation:d.op,market_id:d.opMarket,offer_id:d.opOffer,
                               player_id:d.opPlayer,amount:+d.opAmount||undefined})});
        const data=await res.json();
        if(!res.ok) throw new Error(data.error||res.status);
        pending.token=data.token;
        modal.querySelector('.bid-summary').innerHTML=
          `<dl class="bid-dl">
             <dt>Operacion</dt><dd>${data.label}</dd>
             <dt>Jugador</dt><dd>${data.player_name||d.opName}</dd>
             ${data.amount?`<dt>Importe</dt><dd><strong>${exact(data.amount)}</strong></dd>`:''}
             <dt>Saldo</dt><dd>${exact(data.cash_before)}</dd>
           </dl>` +
          (data.warnings||[]).map(w=>`<p class="bid-warn-line">⚠ ${w}</p>`).join('');
      }catch(err){
        modal.querySelector('.bid-summary').innerHTML='';
        modal.querySelector('.bid-error').textContent=err.message;
        modal.querySelector('.bid-confirm').hidden=true;
      }
    });
  });
}



// ---- alineacion: campo, arrastrar y guardar --------------------------------
const LINE_ORDER=['striker','midfield','defender','goalkeeper'];   // arriba -> abajo
const LINE_LABEL={goalkeeper:'POR',defender:'DEF',midfield:'MED',striker:'DEL'};
const LINE_POS={goalkeeper:1,defender:2,midfield:3,striker:4};
let pitchState=null, pitchDirty=false, dragged=null;


// Iconos de estado: tarjeta roja, botiquin, incognita. El motivo lo pone
// futbolfantasy (la API solo da el codigo de estado) y sale al instante con un
// tooltip propio, porque el title nativo tarda casi un segundo.
// Glifos de texto, no SVG: a 12px un trazo fino no se ve, y una cruz de dos rects
// o un caracter siempre salen.
const ICON_CARD='';              // la propia insignia ES la tarjeta
const ICON_KIT='<i class="kit"></i>';
const ICON_DOUBT='?';

function statusOf(player){
  const st=player.status||'ok';
  const a=player.absence||{};
  if(st==='suspended'||st==='sanctioned'||a.kind==='sancionado')
    return {cls:'st-sancionado', icon:ICON_CARD, label:'Sancionado'};
  if(st==='injured'||a.kind==='lesionado')
    return {cls:'st-lesionado', icon:ICON_KIT, label:'Lesionado'};
  if(st==='doubtful'||a.kind==='duda')
    return {cls:'st-duda', icon:ICON_DOUBT, label:'Duda'};
  return null;
}

function statusRing(player){
  const s=statusOf(player);
  return s ? ' ring-'+(s.cls==='st-sancionado'?'red':'amber') : '';
}

function statusBadge(player){
  const s=statusOf(player);
  if(!s) return '';
  const a=player.absence||{};
  const bits=[`<strong>${s.label}</strong>`];
  if(a.reason) bits.push(a.reason);
  if(a.since) bits.push(`<em>${a.since}</em>`);
  if(a.until) bits.push(`<em>${a.until}</em>`);
  if(!a.reason) bits.push('<em>sin detalle en futbolfantasy</em>');
  return `<span class="badge-status ${s.cls}">${s.icon}`
    +`<span class="tip">${bits.join('<br>')}</span></span>`;
}

// Titularidad: el numero que decide si los xPts se van a materializar.
function titClass(p){
  return p>=75?'tit-hi':p>=50?'tit-mid':p>=30?'tit-lo':'tit-out';
}

function weekChip(w){
  const p=w.points;
  const cls = p==null?'wk-none': p<0?'wk-neg': p>=8?'wk-hi': p>=4?'wk-mid':'wk-lo';
  return `<span class="wk ${cls}" title="Jornada ${w.week}">${p==null?'–':p}</span>`;
}

// La cara identifica mas rapido que el nombre; el escudo se queda en la esquina porque el
// rival de la jornada se lee por equipo. Si la imagen no carga, queda el escudo solo.
function faceHtml(player){
  const crest=`<span class="crest crest-${player.team_id}"></span>`;
  if(!player.image) return crest;
  return `<span class="slot-avatar"><img class="slot-face" src="${player.image}" alt=""
    loading="lazy" onerror="this.remove()">${crest}</span>`;
}

function shirtHtml(player,line,index){
  if(!player) return `<div class="slot empty" data-line="${line}" data-index="${index}">`
    +`${LINE_LABEL[line]}<br>libre</div>`;
  const trend=player.projected_pct||0;
  const played=(player.weeks||[]).slice(-5);
  const weeks=played.length
    ? '<span class="wk-label">J</span>'+played.map(weekChip).join('')
    : '<span class="wk wk-none">sin jornadas</span>';
  return `<div class="slot${statusRing(player)}" draggable="true" data-line="${line}"
    data-index="${index}" data-player="${player.id}" data-pt="${player.player_team_id}"
    title="${player.name}${player.next_rival?(' · vs '+player.next_rival
      +(player.next_home?' (en casa)':' (fuera)')):''}">
    ${statusBadge(player)}
    ${faceHtml(player)}
    <span class="slot-name">${player.name}</span>
    <span class="slot-weeks">${weeks}</span>
    <span class="slot-meta">
      <span>${(player.xpts||0).toFixed(1)} xPts</span>
      ${player.start_probability!=null?`<span class="tit ${titClass(player.start_probability)}"
        >${player.start_probability}%</span>`:''}
      <span class="slot-trend ${trend>=0?'up':'down'}">${trend>=0?'▲':'▼'}${Math.abs(trend).toFixed(1)}%</span>
    </span>
  </div>`;
}

function benchHtml(player){
  const trend=player.projected_pct||0;
  return `<div class="bench-item${statusRing(player)}" draggable="true" data-player="${player.id}"
    data-pt="${player.player_team_id}" data-from="bench" title="${player.name}">
    ${statusBadge(player)}
    ${faceHtml(player)}
    <span class="pos pos-${(LINE_LABEL[Object.keys(LINE_POS).find(k=>LINE_POS[k]===player.position_id)]||'ENT').toLowerCase()}">${
      {1:'POR',2:'DEF',3:'MED',4:'DEL'}[player.position_id]||'ENT'}</span>
    <span class="bench-name">${player.name}</span>
    <span class="slot-trend ${trend>=0?'up':'down'}" style="margin-left:auto">${
      (player.xpts||0).toFixed(1)}</span>
  </div>`;
}

function renderPitch(){
  if(!pitchState) return;
  const pitch=document.getElementById('pitch');
  const benchList=document.getElementById('bench-list');
  if(!pitch) return;
  pitch.innerHTML=LINE_ORDER.map(line=>{
    const slots=pitchState.lines[line]||[];
    return `<div class="pitch-line" data-line="${line}">`
      + slots.map((p,i)=>shirtHtml(p,line,i)).join('') + '</div>';
  }).join('');
  benchList.innerHTML=(pitchState.bench||[]).map(benchHtml).join('')
    || '<p class="bench-empty">Sin reservas</p>';
  document.getElementById('pitch-formation').textContent=
    (pitchState.formation||[]).join('-');
  const save=document.getElementById('pitch-save');
  save.disabled=!pitchDirty||!pitchState.writes_enabled;
  document.getElementById('pitch-status').textContent = pitchDirty
    ? 'cambios sin guardar' : (pitchState.writes_enabled?'':'servidor en solo lectura');
  wireDrag();
}

let justDragged=false;

function wireDrag(){
  document.querySelectorAll('.slot[draggable], .bench-item[draggable]').forEach(node=>{
    // Arrastrar y clicar empiezan igual, asi que un drop no puede abrir la ficha.
    node.addEventListener('click',()=>{
      if(justDragged) return;
      if(node.dataset.player) openDetail(node.dataset.player);
    });
    node.addEventListener('dragstart',e=>{
      dragged={id:node.dataset.player, pt:node.dataset.pt,
               from:node.dataset.from||'pitch',
               line:node.dataset.line, index:+node.dataset.index};
      node.classList.add('dragging');
      e.dataTransfer.effectAllowed='move';
      e.dataTransfer.setData('text/plain',node.dataset.player);
    });
    node.addEventListener('dragend',()=>{
      node.classList.remove('dragging'); dragged=null;
      justDragged=true; setTimeout(()=>{ justDragged=false; },250);
      document.querySelectorAll('.drop-target').forEach(n=>n.classList.remove('drop-target')); });
  });
  const targets=[...document.querySelectorAll('.slot'), document.getElementById('bench')];
  targets.forEach(node=>{
    if(!node) return;
    node.addEventListener('dragover',e=>{ e.preventDefault(); node.classList.add('drop-target'); });
    node.addEventListener('dragleave',()=>node.classList.remove('drop-target'));
    node.addEventListener('drop',e=>{
      e.preventDefault(); node.classList.remove('drop-target');
      if(!dragged) return;
      if(node.id==='bench') dropOnBench();
      else dropOnSlot(node.dataset.line, +node.dataset.index);
    });
  });
}

function takeFrom(source){
  if(source.from==='bench'){
    const i=pitchState.bench.findIndex(p=>p&&p.id===source.id);
    return i<0?null:pitchState.bench.splice(i,1)[0];
  }
  const arr=pitchState.lines[source.line];
  const player=arr[source.index]; arr[source.index]=null;
  return player;
}

function dropOnSlot(line,index){
  const moving=dragged;
  if(moving.from==='pitch'&&moving.line===line&&moving.index===index) return;
  const target=pitchState.lines[line][index]||null;
  const player=takeFrom(moving);
  if(!player) return;
  // Una linea solo acepta su propia posicion; el portero es intransferible.
  if(player.position_id!==LINE_POS[line]){
    // devolver y avisar
    if(moving.from==='bench') pitchState.bench.push(player);
    else pitchState.lines[moving.line][moving.index]=player;
    flashPitch(`${player.name} es ${{1:'portero',2:'defensa',3:'medio',4:'delantero'}[player.position_id]}`
      +`, no puede jugar de ${{goalkeeper:'portero',defender:'defensa',midfield:'medio',striker:'delantero'}[line]}.`);
    return;
  }
  pitchState.lines[line][index]=player;
  if(target){
    if(moving.from==='bench') pitchState.bench.push(target);
    else pitchState.lines[moving.line][moving.index]=target;   // intercambio
  }
  pitchDirty=true; renderPitch();
}

function dropOnBench(){
  if(dragged.from==='bench') return;
  const player=takeFrom(dragged);
  if(player) pitchState.bench.push(player);
  pitchDirty=true; renderPitch();
}

function flashPitch(message){
  const status=document.getElementById('pitch-status');
  status.textContent=message;
  status.style.color='var(--warning)';
  setTimeout(()=>{ status.style.color=''; renderPitch(); },2600);
}

function applyFormation(text){
  const [d,m,s]=text.split(',').map(Number);
  const want={goalkeeper:1,defender:d,midfield:m,striker:s};
  const spare=[];
  LINE_ORDER.forEach(line=>{
    const arr=pitchState.lines[line]||[];
    while(arr.length>want[line]){ const p=arr.pop(); if(p) spare.push(p); }
    while(arr.length<want[line]) arr.push(null);
    pitchState.lines[line]=arr;
  });
  // rellenar huecos con reservas de esa posicion, el resto al banquillo
  LINE_ORDER.forEach(line=>{
    pitchState.lines[line]=pitchState.lines[line].map(slot=>{
      if(slot) return slot;
      const pool=spare.concat(pitchState.bench);
      const i=pool.findIndex(p=>p&&p.position_id===LINE_POS[line]);
      if(i<0) return null;
      const chosen=pool[i];
      const inSpare=spare.indexOf(chosen);
      if(inSpare>=0) spare.splice(inSpare,1);
      else pitchState.bench.splice(pitchState.bench.indexOf(chosen),1);
      return chosen;
    });
  });
  pitchState.bench=pitchState.bench.concat(spare);
  pitchState.formation=[d,m,s];
  pitchDirty=true; renderPitch();
}

async function loadPitch(){
  const pitch=document.getElementById('pitch');
  if(!pitch) return;
  try{
    const res=await fetch('/api/lineup');
    if(!res.ok) throw new Error(res.status);
    pitchState=await res.json();
  }catch(e){
    pitch.innerHTML='<p class="slot empty" style="width:auto">Solo disponible en la '
      +'version servida</p>';
    return;
  }
  pitchDirty=false;
  const select=document.getElementById('pitch-formation-select');
  const all=[...(pitchState.formations.free||[]),...(pitchState.formations.premium||[])];
  const current=(pitchState.formation||[]).join(',');
  select.innerHTML=all.map(f=>{
    const premium=(pitchState.formations.premium||[]).includes(f);
    return `<option value="${f}"${f===current?' selected':''}>${f.replace(/,/g,'-')}`
      +`${premium?' (premium)':''}</option>`;
  }).join('');
  if(!select.dataset.wired){
    select.dataset.wired='1';
    select.addEventListener('change',()=>applyFormation(select.value));
    document.getElementById('pitch-reset').addEventListener('click',loadPitch);
    document.getElementById('pitch-save').addEventListener('click',savePitch);
  }
  renderPitch();
}

async function savePitch(){
  const missing=LINE_ORDER.some(l=>(pitchState.lines[l]||[]).some(p=>!p));
  if(missing){ flashPitch('Hay huecos sin cubrir: completa el once antes de guardar.');
    return; }
  const ids=l=>pitchState.lines[l].map(p=>p.player_team_id);
  const button=document.getElementById('pitch-save');
  button.disabled=true; button.textContent='Guardando…';
  try{
    const res=await fetch('/api/lineup',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({goalkeeper:ids('goalkeeper')[0], defender:ids('defender'),
                           midfield:ids('midfield'), striker:ids('striker'),
                           formation:pitchState.formation})});
    const data=await res.json();
    if(!res.ok) throw new Error(data.error||res.status);
    pitchDirty=false;
    // Los ids no cambian al guardar, asi que basta con repintar lo que ya tenemos:
    // recargar del API es una vuelta entera para el mismo resultado.
    pitchState.formation=data.formation||pitchState.formation;
    renderPitch();
    document.getElementById('pitch-status').textContent=
      'guardada ' + new Date().toLocaleTimeString('es-ES');
  }catch(err){ flashPitch('No se ha guardado: '+err.message); }
  finally{ button.textContent='Guardar alineación'; }
}

// ---- cajon de jugador: un nombre, todas sus acciones ----------------------
const drawer=document.getElementById('drawer');

function closeDrawer(){ if(drawer) drawer.hidden=true; }

// El primer valor lo escribe el navegador y tick() lo mantiene cada segundo: la cuenta atras
// de una clausula es justo el dato que caduca mientras lo miras.
// Cuanto lleva algo hecho, en las mismas unidades que la cuenta atras: un fichaje de hace tres
// dias y uno de hace tres horas son decisiones distintas.
function since(stamp){
  const gone=Date.now()-new Date(stamp).getTime();
  if(isNaN(gone)||gone<0) return '—';
  const h=Math.floor(gone/3600000), m=Math.floor(gone%3600000/60000);
  return h>=24 ? 'hace '+Math.floor(h/24)+'d '+(h%24)+'h'
       : h>0   ? 'hace '+h+'h '+String(m).padStart(2,'0')+'m'
               : 'hace '+m+'m';
}

function leftUntil(stamp){
  const left=new Date(stamp).getTime()-Date.now();
  if(isNaN(left)) return '—';
  if(left<=0) return 'ya';
  const h=Math.floor(left/3600000), m=Math.floor(left%3600000/60000);
  return h>=24 ? Math.floor(h/24)+'d '+(h%24)+'h' : h>0 ? h+'h '+String(m).padStart(2,'0')+'m'
                                                        : m+'m';
}

// Una curva sin cifras solo dice "sube" o "baja". Con el cursor encima dice cuanto y que dia,
// que es la pregunta que se hace mirandola.
let chartDays=[];

function sparkSvg(history){
  const days=history.filter(h=>h.value!=null);
  const points=days.map(h=>h.value);
  if(points.length<3) return '';
  chartDays=days;
  const w=440,h=90,lo=Math.min(...points),hi=Math.max(...points),span=(hi-lo)||1;
  const step=w/(points.length-1);
  const xy=(v,i)=>[i*step, h-4-(v-lo)/span*(h-12)];
  const path=points.map((v,i)=>xy(v,i).map(n=>n.toFixed(1)).join(',')).join(' ');
  const rising=points[points.length-1]>=points[0];
  return `<div class="chart-wrap">
    <svg class="drawer-chart" width="100%" height="${h}" viewBox="0 0 ${w} ${h}"
      preserveAspectRatio="none" aria-label="Historico de valor">
      <polyline points="${path}" fill="none" stroke="var(--${rising?'pole-pos':'pole-neg'})"
        stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
      <line class="chart-cross" x1="0" y1="0" x2="0" y2="${h}" stroke="var(--muted)"
        stroke-width="1" stroke-dasharray="3 3" style="opacity:0"/>
      <circle class="chart-dot" r="3.5" fill="var(--${rising?'pole-pos':'pole-neg'})"
        style="opacity:0"/>
    </svg>
    <div class="chart-tip" hidden></div>
  </div>
  <p class="drawer-note">Valor diario, ultimos ${points.length} dias ·
    min ${fmt(lo)} · max ${fmt(hi)} · pasa el cursor para ver cada dia</p>`;
}

function wireChart(root){
  const wrap=root.querySelector('.chart-wrap');
  if(!wrap || chartDays.length<3) return;
  const svg=wrap.querySelector('.drawer-chart'), tip=wrap.querySelector('.chart-tip'),
        cross=wrap.querySelector('.chart-cross'), dot=wrap.querySelector('.chart-dot');
  const values=chartDays.map(d=>d.value);
  const lo=Math.min(...values), hi=Math.max(...values), span=(hi-lo)||1;
  const w=440, h=90, step=w/(values.length-1);

  const move=(event)=>{
    const box=svg.getBoundingClientRect();
    const ratio=Math.min(1,Math.max(0,(event.clientX-box.left)/box.width));
    const index=Math.round(ratio*(values.length-1));
    const day=chartDays[index];
    // El viewBox se estira con preserveAspectRatio="none", asi que la x en pantalla es la
    // proporcion, no la del viewBox: el punto y la linea van en coordenadas del viewBox.
    const x=index*step, y=h-4-(values[index]-lo)/span*(h-12);
    cross.setAttribute('x1',x); cross.setAttribute('x2',x); cross.style.opacity='.6';
    dot.setAttribute('cx',x); dot.setAttribute('cy',y); dot.style.opacity='1';
    const first=values[0], change=first?((values[index]-first)/first*100):0;
    tip.hidden=false;
    tip.innerHTML=`<b>${exact(day.value)}</b><span>${day.date||''}</span>`+
      `<span class="${change>=0?'up':'down'}">${change>=0?'+':''}${change.toFixed(1)}% desde el inicio</span>`;
    const left=Math.min(box.width-120, Math.max(0, ratio*box.width-60));
    tip.style.left=left+'px';
  };
  svg.addEventListener('mousemove',move);
  svg.addEventListener('touchmove',(e)=>{ if(e.touches[0]) move(e.touches[0]); });
  svg.addEventListener('mouseleave',()=>{
    tip.hidden=true; cross.style.opacity='0'; dot.style.opacity='0';
  });
}

async function openDetail(playerId){
  if(!drawer) return;
  drawer.hidden=false;
  const body=drawer.querySelector('.drawer-body');
  body.innerHTML='<p class="empty">Cargando…</p>';
  let data;
  try{
    const res=await fetch('/api/player/'+playerId);
    if(!res.ok) throw new Error(res.status);
    data=await res.json();
  }catch(e){
    body.innerHTML='<p class="empty">Solo disponible en la version servida '
      +'(<code>fantasy serve</code>).</p>';
    return;
  }
  const p=data.player, l=data.listing||{};
  // 🏠 en casa, ✈️ fuera: dos palabras repetidas en cada fila se leen como ruido.
  const where=(home)=>home?'<span title="en casa">🏠</span>':'<span title="fuera">✈️</span>';
  const rival=p.next_rival?`${p.next_rival} ${where(p.next_home)}`:'—';
  const owner=p.is_mine?'tu':(p.owner||'libre');
  // La foto identifica antes que el nombre; si no hay, queda el escudo del equipo.
  const face=p.image
    ? `<img class="drawer-face" src="${p.image}" alt="" loading="lazy" onerror="this.remove()">`
    : `<span class="drawer-face crest crest-${p.team_id}"></span>`;
  body.innerHTML=`
    <div class="drawer-head">${face}<h3>${p.name}</h3></div>
    <p class="sub"><span class="pos pos-${(p.position||'').toLowerCase().slice(0,3)}">${p.position}</span>
      ${p.team||''} · ${owner}${p.starred?' · ★':''}</p>
    <dl class="drawer-stats">
      <div><dt>Valor de mercado</dt><dd>${exact(p.value)}</dd></div>
      <div><dt>xPts por jornada</dt><dd>${(p.xpts||0).toFixed(2)}</dd></div>
      <div><dt>Puntos por millon</dt><dd>${(p.points_value||0).toFixed(3)}</dd></div>
      <div><dt>Score</dt><dd>${(p.score||0)>=0?'+':''}${(p.score||0).toFixed(2)}
        <span style="color:var(--muted);font-weight:400">· #${p.rank||'?'}</span></dd></div>
      <div><dt>Puntos 25/26</dt><dd>${p.last_season_points||0}</dd></div>
      <div><dt>Puntos temporada</dt><dd>${p.season_points||0}</dd></div>
      <div><dt>Titularidad</dt><dd class="${p.start_probability!=null?titClass(p.start_probability):''}"
        >${p.start_probability!=null?p.start_probability+'%':'—'}</dd></div>
      <div><dt>Proximo rival</dt><dd>${rival}</dd></div>
      <div><dt>Valor 7d</dt><dd style="color:var(--${(p.projected_pct||0)>=0?'pole-pos':'pole-neg'})"
        >${(p.projected_pct||0)>=0?'+':''}${(p.projected_pct||0).toFixed(2)}%</dd></div>
      <div><dt>Techo rentable</dt><dd${p.ideal_bid?'':' style="color:var(--warning)"'}
        >${p.ideal_bid?exact(p.ideal_bid):'sin margen'}</dd></div>
      <div><dt>Clausula</dt><dd>${p.clause?exact(p.clause):'—'}${p.clause_locked?' 🔒':''}</dd></div>
      ${p.clause_locked&&p.clause_locked_until?`<div><dt>${p.is_mine?'Blindada hasta':'Se libera en'}</dt>
        <dd><span data-deadline="${p.clause_locked_until}">${leftUntil(p.clause_locked_until)}</span>
        <span style="color:var(--muted);font-weight:400"> · ${String(p.clause_locked_until).slice(0,10)}</span></dd></div>`:''}
      ${!p.clause_locked&&p.clause&&!p.is_mine?`<div><dt>Clausula</dt><dd
        style="color:var(--pole-pos)">pagable ya</dd></div>`:''}
      ${p.bought_at?`<div><dt>Fichado</dt><dd>${since(p.bought_at)}
        <span style="color:var(--muted);font-weight:400"> · ${String(p.bought_at).slice(0,10)}</span></dd></div>`:''}
      ${p.sale_locked&&p.hold_until?`<div><dt>${p.is_mine?'Puedes venderlo en':'Puede venderlo en'}</dt>
        <dd style="color:var(--warning)"><span data-deadline="${p.hold_until}">${leftUntil(p.hold_until)}</span>
        <span style="color:var(--muted);font-weight:400"> · norma de la liga</span></dd></div>`:''}
      ${l.market_id?`<div><dt>En mercado</dt><dd>${exact(l.min_bid)}</dd></div>`:''}
      ${l.kind==='libre'?`<div><dt>Pujas vigentes</dt><dd${l.bids?' style="color:var(--warning)"':''}>${l.bids||'ninguna'}</dd></div>`:''}
      ${l.expires?`<div><dt>Cierra</dt><dd>${String(l.expires).slice(11,16)}</dd></div>`:''}
      ${p.status&&p.status!=='ok'?`<div><dt>Estado</dt><dd style="color:var(--${
        p.status==='suspended'||p.status==='sanctioned'?'critical':'warning'})">${
        {injured:'lesionado',doubtful:'duda',suspended:'sancionado',
         sanctioned:'sancionado'}[p.status]||p.status}</dd></div>`:''}
      ${p.absence&&p.absence.reason?`<div style="grid-column:1/-1"><dt>Motivo</dt><dd
        style="text-align:left;font-weight:400">${p.absence.reason}${
        p.absence.since?' · '+p.absence.since:''}${
        p.absence.until?' · '+p.absence.until:''}</dd></div>`:''}
    </dl>
    ${sparkSvg(data.history||[])}
    <div class="drawer-actions">${(data.actions||[]).map(actionButton).join('')}</div>
    ${data.writes_enabled?'':'<p class="drawer-note">Servidor en modo solo lectura: '
      +'las operaciones estan desactivadas.</p>'}`;
  body.querySelectorAll('button[data-action]').forEach(button=>
    button.addEventListener('click',()=>runAction(JSON.parse(button.dataset.action),p)));
  wireAlways(body,p);
  wireChart(body);
}

// El pie del panel dice en palabras que va a pasar: cambiar de "no vende solo" a
// "vendo desde X" es justo lo que hay que ver confirmado.
function note(panel,data){
  const line=panel.querySelector('.always-foot p');
  line.innerHTML = data.accept_above
    ? '<b>Vendo desde ese importe</b>, sin preguntar. El importe manda sobre el '
      +'interruptor de arriba.'
    : (data.auto_sell
        ? '<b>Vendo cuando la oferta sea buena.</b> Si prefieres decidir el numero tu, '
          +'ponlo en «aceptar desde».'
        : 'No vende solo: si llega una oferta buena <b>te aviso</b> y decides tu.');
}

function wireAlways(scope,player){
  const panel=scope.querySelector('.always-panel');
  if(!panel) return;
  const min=panel.querySelector('.always-min');
  const accept=panel.querySelector('.always-accept');
  const save=panel.querySelector('.always-save');
  const auto=panel.querySelector('.always-auto');
  auto.addEventListener('change',async()=>{
    auto.disabled=true;
    try{
      const res=await fetch('/api/always',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({id:player.id,name:player.name,auto_sell:auto.checked})});
      if(!res.ok) throw new Error(res.status);
      const data=await res.json();
      auto.checked=!!data.auto_sell;
      note(panel,data);
    }catch(e){ auto.checked=!auto.checked; }
    finally{ auto.disabled=false; }
  });
  [min,accept].forEach(input=>input.addEventListener('input',()=>{
    const n=digits(input.value);
    input.value=isNaN(n)?'':group(n);
    save.disabled=false; save.textContent='Guardar';
  }));
  save.addEventListener('click',async()=>{
    save.disabled=true; save.textContent='Guardando…';
    try{
      const res=await fetch('/api/always',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({id:player.id,name:player.name,
                             min_price:digits(min.value)||0,
                             accept_above:digits(accept.value)||0})});
      if(!res.ok) throw new Error(res.status);
      const data=await res.json();
      min.value=data.min_price?group(data.min_price):'';
      accept.value=data.accept_above?group(data.accept_above):'';
      note(panel,{...data,auto_sell:auto.checked});
      save.textContent='Guardado'; save.classList.add('always-saved');
      setTimeout(()=>{save.classList.remove('always-saved');
                      save.textContent='Guardar'; save.disabled=false;},1600);
    }catch(e){
      save.textContent='No se ha guardado'; save.disabled=false;
    }
  });
}

function alwaysPanel(a){
  // Vacio = no vender solo. Es la unica forma de decirlo, asi que el placeholder
  // lo dice con palabras en vez de dejar un hueco que parece "sin limite".
  const min=a.min_price?group(a.min_price):'';
  const acc=a.accept_above?group(a.accept_above):'';
  const floor=a.good_floor||0;
  return `<div class="always-panel">
    <h4>Siempre en mercado</h4>
    <label class="always-check"><input type="checkbox" class="always-auto"
      ${a.auto_sell?'checked':''}>
      <span><b>Vender si la oferta es buena</b>${floor?`: desde ${exact(floor)}`:''}<br>
      <i>${floor?`el mayor de lo que pides, un 2% sobre su valor y el techo rentable de `
        +`futbolfantasy — aqui manda ${a.good_source}`
        :'para jugadores que te dan igual'}</i>
      ${a.room<=0?'<br><i class="always-warn">ojo: es tu ultimo '
        +'jugador de esa posicion, no lo vendere solo</i>':''}</span>
    </label>
    <div class="always-grid">
      <label>Precio de listado
        <input class="always-min" type="text" inputmode="numeric" autocomplete="off"
               value="${min}" placeholder="${a.value?group(a.value):'valor de mercado'}"></label>
      <label>Aceptar desde
        <input class="always-accept" type="text" inputmode="numeric" autocomplete="off"
               value="${acc}" placeholder="no vendo solo"></label>
    </div>
    <div class="always-foot">
      <p>${acc?'<b>Vendo desde ese importe</b>, sin preguntar. El importe manda sobre el '
             +'interruptor de arriba.'
            :(a.auto_sell?'<b>Vendo si llegan a tu precio de venta.</b> Si prefieres otro '
                          +'numero, ponlo en «aceptar desde».'
                        :'No vende solo: si llega una oferta buena <b>te aviso</b> y decides tu.')}</p>
      <button class="always-save">Guardar</button>
    </div></div>`;
}

function actionButton(a){
  if(a.kind==='note') return `<p class="drawer-note">${a.label}</p>`;
  const cls=a.op==='decline_offer'||a.op==='withdraw' ? 'danger-full'
          : (a.op==='always'||a.op==='raid') ? (a.on?'on':'') : 'primary';
  const off=a.blocked?' disabled':'';
  const button=`<button class="${cls}" data-action='${JSON.stringify(a).replace(/'/g,"&#39;")}'${off}>`
    +`${a.label}${a.blocked?' — no te llega':''}</button>`;
  return a.op==='always'&&a.on ? button+alwaysPanel(a) : button;
}

async function runAction(a,player){
  if(a.op==='note') return;
  if(a.op==='raid'){
    const current=group(a.suggested);
    const answer=prompt('Clausulazo programado para '+player.name+'.\n\n'
      +'Se pagara en cuanto se libere la clausula, y SOLO si entonces sigue por debajo '
      +'del importe que pongas aqui. Si el dueño la sube o le pone blindaje, se cancela.\n\n'
      +'Pago maximo (€):', current);
    if(answer===null) return;
    const max_pay=digits(answer);
    if(!max_pay) return;
    const res=await fetch('/api/raid',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({id:player.id,name:player.name,max_pay})});
    if(res.ok) openDetail(player.id);
    return;
  }
  if(a.op==='always'){
    // Pintar antes de preguntar: el servidor confirma en milisegundos, pero volver a
    // cargar la ficha entera hacia que el boton pareciera muerto.
    const button=[...document.querySelectorAll('.drawer-actions button')]
      .find(b=>b.textContent.includes('mercado'));
    const turningOn=!a.on;
    if(button){
      button.classList.toggle('on',turningOn);
      button.textContent=turningOn?'Quitar de siempre-en-mercado':'Siempre en mercado';
      button.disabled=true;
    }
    try{
      const res=await fetch('/api/always',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({id:player.id,name:player.name})});
      if(!res.ok) throw new Error(res.status);
      const data=await res.json();
      if(button){
        button.classList.toggle('on',!!data.always_listed);
        button.textContent=data.always_listed?'Quitar de siempre-en-mercado'
                                             :'Siempre en mercado';
      }
      a.on=!!data.always_listed;
      const existing=document.querySelector('.always-panel');
      if(a.on&&!existing&&button){
        button.insertAdjacentHTML('afterend',alwaysPanel(a));
        wireAlways(button.parentElement,player);
      }else if(!a.on&&existing){ existing.remove(); }
    }catch(e){
      if(button){ button.classList.toggle('on',a.on);
        button.textContent=a.on?'Quitar de siempre-en-mercado':'Siempre en mercado'; }
    }finally{ if(button) button.disabled=false; }
    return;
  }
  closeDrawer();
  if(a.kind==='amount'){
    pending={operation:a.op, market_id:a.market_id, player_id:a.player_id||player.id,
             player_team_id:a.player_team_id||player.player_team_id,
             offer_id:a.offer_id, bid_id:a.bid_id, name:player.name, min_bid:a.min||0,
             ideal:player.ideal_bid||0, value:player.value};
    modal.hidden=false;
    modal.querySelector('.bid-action').textContent=a.label+' —';
    modal.querySelector('.bid-who').textContent=player.name;
    modal.querySelector('.bid-amount').value=group(a.suggested||a.min||0);
    modal.querySelector('.bid-min').textContent=a.min?exact(a.min):'sin minimo';
    modal.querySelector('.bid-ideal').textContent=player.ideal_bid?exact(player.ideal_bid):'sin margen';
    modal.querySelector('.bid-value').textContent=exact(player.value);
    showRivals(+a.bids||0, a.expires);
    modal.querySelector('.bid-drop').hidden=true;
    showStep(1);
    modal.querySelector('.bid-error').textContent='';
    checkAmount();
  }else{
    const button=document.createElement('button');
    button.className='op'; button.dataset.op=a.op;
    button.dataset.opMarket=a.market_id||''; button.dataset.opOffer=a.offer_id||'';
    button.dataset.opPlayer=a.player_id||player.id; button.dataset.opName=player.name;
    button.dataset.opAmount=a.amount||'';
    document.body.appendChild(button); wireOps(document.body); button.click();
    button.remove();
  }
}

async function scheduleRaid(dataset){
  const suggested=group(+dataset.raidMax||0);
  const clause=+dataset.raidClause||0;
  const answer=prompt('Clausulazo programado para '+dataset.raidName+'.\n\n'
    +'Se pagara SOLO en el momento en que la clausula se libere, y solo si entonces '
    +'sigue por debajo del importe que pongas aqui.\n'
    +(clause?('Clausula ahora: '+clause.toLocaleString('es-ES')+' €\n'):'')
    +'Si el dueño la sube por encima de tu limite, o blinda al jugador, se cancela '
    +'sola y no se paga nada.\n\n'
    +'Pago maximo (€):', suggested);
  if(answer===null) return;
  const max_pay=digits(answer);
  if(!max_pay) return;
  const res=await fetch('/api/raid',{method:'POST',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify({id:dataset.raid,name:dataset.raidName,max_pay})});
  if(!res.ok){ alert('No se ha podido programar.'); return; }
  alert(dataset.raidName+': programado con limite '+max_pay.toLocaleString('es-ES')+' €.\n'
    +'Se ejecutara solo si el servidor corre con --auto.');
  swap();
}

function wireRaids(root=document){
  root.querySelectorAll('button.raid-btn, .cal-chip[data-raid]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>scheduleRaid(button.dataset));
  });
  // Tus propios chips del calendario no se clausulan: abren su ficha.
  root.querySelectorAll('.cal-chip:not([data-raid])').forEach(chip=>{
    if(chip.dataset.wired) return;
    chip.dataset.wired='1';
    chip.addEventListener('click',()=>openDetail(chip.dataset.detailAlt));
  });
  root.querySelectorAll('button[data-goto]').forEach(card=>{
    if(card.dataset.wired) return;
    card.dataset.wired='1';
    card.addEventListener('click',()=>{
      const target=resolveTarget(card.dataset.goto);
      if(target) showTab(target.tab,{section:target.section});
      else showTab(card.dataset.goto);
    });
  });
}

function wireDetails(root=document){
  root.querySelectorAll('button[data-detail]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>openDetail(button.dataset.detail));
  });
}

if(drawer){
  drawer.querySelector('.drawer-close').addEventListener('click',closeDrawer);
  drawer.addEventListener('click',(e)=>{ if(e.target===drawer) closeDrawer(); });
  document.addEventListener('keydown',(e)=>{ if(e.key==='Escape') closeDrawer(); });
}

// ---- pestañas: una vista a la vez ------------------------------------------
const TABS=[
  {id:'decidir', label:'Decidir', sections:['plan','acciones','ofertas']},
  {id:'mercado', label:'Mercado', sections:['fichajes','enventa','misventas','siempre','seguimiento']},
  {id:'clausulas', label:'Cláusulas', sections:['programados','calendario','vencimientos','oportunidades','riesgo','clausulas']},
  {id:'plantilla', label:'Plantilla', sections:['once','plantilla','ventas']},
  {id:'partidos', label:'Partidos', sections:['partidos']},
  {id:'liga', label:'Liga', sections:['rivales','movimientos','normas']},
  {id:'ranking', label:'Ranking', sections:['ranking','rentabilidad']},
];

// Un hash puede ser una pestaña (#mercado) o una seccion (#oportunidades): lo
// segundo es lo que hay en los enlaces, asi que hay que resolverlo a su pestaña.
function resolveTarget(hash){
  const id=(hash||'').replace(/^#/,'');
  if(!id) return null;
  if(TABS.some(t=>t.id===id)) return {tab:id, section:null};
  const owner=TABS.find(t=>t.sections.includes(id));
  return owner ? {tab:owner.id, section:id} : null;
}

function showTab(id,{section=null,updateHash=true}={}){
  const tab=TABS.find(t=>t.id===id)||TABS[0];
  document.querySelectorAll('section[id]').forEach(s=>{
    s.hidden=!tab.sections.includes(s.id);
  });
  document.querySelectorAll('.tab').forEach(b=>{
    const on=b.dataset.tab===tab.id;
    b.classList.toggle('on',on);
    b.setAttribute('aria-selected',on?'true':'false');
  });
  try{ localStorage.setItem('fantasy-tab',tab.id); }catch(e){}
  applyFilters();
  if(updateHash){
    // replaceState, no assignment: no queremos una entrada de historial por clic ni
    // disparar hashchange sobre nosotros mismos.
    history.replaceState(null,'','#'+(section||tab.id));
  }
  if(tab.sections.includes('once') && !pitchState) loadPitch();
  if(section){
    const node=document.getElementById(section);
    if(node) node.scrollIntoView({behavior:'smooth',block:'start'});
  }
}

function wireTabs(){
  const bar=document.getElementById('tabs');
  if(!bar||bar.dataset.wired) return;
  bar.dataset.wired='1';
  bar.querySelectorAll('.tab').forEach(b=>
    b.addEventListener('click',()=>showTab(b.dataset.tab)));
  window.addEventListener('hashchange',()=>{
    const target=resolveTarget(location.hash);
    if(target) showTab(target.tab,{section:target.section,updateHash:false});
  });
  let saved=null;
  try{ saved=localStorage.getItem('fantasy-tab'); }catch(e){}
  const target=resolveTarget(location.hash);
  if(target) showTab(target.tab,{section:target.section,updateHash:false});
  else showTab(saved||'decidir');
}

// ---- push: recambiar solo lo que cambia -----------------------------------
let currentVersion=null;

// Secciones cuyo contenido lo pinta el navegador, no el servidor: el fragmento que
// llega es una carcasa vacia, asi que recambiarla borraria lo que hay dentro (y los
// cambios de alineacion sin guardar).
const CLIENT_OWNED=new Set(['once']);

async function swap(){
  const res=await fetch('/api/fragments');
  if(!res.ok) return;
  const data=await res.json();
  if(data.version===currentVersion) return;
  currentVersion=data.version;
  Object.entries(data.sections).forEach(([id,inner])=>{
    if(CLIENT_OWNED.has(id)) return;
    const node=document.getElementById(id);
    if(node && node.innerHTML!==inner) node.innerHTML=inner;
  });
  wireTables(); wireFilters(); wireStars(); wireBids(); wireOps(); wireDetails(); wireRaids(); tick();
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
  traspaso:'Se ha movido la liga',mercado:'Cambios en el mercado',
  partido:'Partido en juego',
  vencimiento:'Ha vencido algo',refresco:'Actualizado'};

function showEffect(message){
  // Lo que una operacion mueve de verdad: el antes y el despues, no un "hecho".
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

function connect(){
  const dot=document.getElementById('live-dot');
  const source=new EventSource('/api/events');
  source.onopen=()=>{ if(dot){ dot.className='live-on'; dot.title='En vivo'; } };
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
    if(dot){ dot.className='live-off'; dot.title='Sin conexion: reintentando'; }
    source.close(); setTimeout(connect,5000);
  };
}

wireTables(); wireFilters(); wireStars(); wireBids(); wireOps(); wireDetails(); wireRaids();
wireTabs(); tick();
if(window.EventSource && location.protocol.startsWith('http')) connect();

// ---- legacy (fichero estatico) ----
