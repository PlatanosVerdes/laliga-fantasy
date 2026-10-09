
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

function sectionOf(node){
  return node?.closest?.('section[id]')?.id||'';
}

// ---- filter state, so it survives a section being swapped out --------------
const filterState = {pos:'all', price:'', text:''};

function wireTables(root=document){
  root.querySelectorAll('table.sortable').forEach(table=>{
    if(table.dataset.wired) return;
    table.dataset.wired='1';
    table.querySelectorAll('th').forEach((th,index)=>{
      th.addEventListener('click',()=>{
        const body=table.tBodies[0], rows=[...body.rows];
        // The matchday ones too: the sort key they carry is a number, and without being
        // here they would sort as text (48 before 9).
        const numeric=['money','pct','num','num1','int','pct_plain','spark','verdict','mag',
                       'ideal','hours','ratio','live_points','waiting','projection']
                      .includes(th.dataset.kind);
        const desc=!th.classList.contains('sorted-desc');
        table.querySelectorAll('th').forEach(h=>h.classList.remove('sorted-asc','sorted-desc'));
        th.classList.add(desc?'sorted-desc':'sorted-asc');
        rows.sort((a,b)=>{
          const x=a.cells[index].dataset.sort, y=b.cells[index].dataset.sort;
          const cmp=numeric?(parseFloat(x||0)-parseFloat(y||0)):String(x).localeCompare(String(y),'es');
          return desc?-cmp:cmp;
        });
        rows.forEach(r=>body.appendChild(r));
        usage.sort(sectionOf(table),th.textContent.trim()||th.dataset.kind||'');
      });
    });
  });
}

// What people type as a price: "20", "20M", "20,5", "20.000.000". Small numbers are millions;
// empty is no limit.
function parsePrice(raw){
  let text=String(raw||'').trim().toLowerCase().replace(/\s|€/g,'');
  if(!text) return Infinity;
  const millions=/m$/.test(text);
  text=text.replace(/m$/,'');
  if(/^\d{1,3}(\.\d{3})+$/.test(text)) text=text.replace(/\./g,'');
  const n=parseFloat(text.replace(',','.'));
  if(!isFinite(n)) return Infinity;
  return millions||n<1000 ? n*1e6 : n;
}
const plain=t=>String(t||'').normalize('NFD').replace(/[̀-ͯ]/g,'').toLowerCase();

function applyFilters(){
  const maxPrice=parsePrice(filterState.price);
  const needle=plain(filterState.text.trim());
  const active=filterState.pos!=='all'||maxPrice!==Infinity||!!needle;
  document.querySelectorAll('.filters').forEach(bar=>{
    const scope=bar.closest('section');
    let shown=0,total=0;
    // A list keeps its rest folded; a filter has to look through all of it.
    if(active)
      scope.querySelectorAll('details.fold').forEach(d=>{ if(d.querySelector('li[data-position]')) d.open=true; });
    scope.querySelectorAll('tr[data-position], li[data-position]').forEach(row=>{
      total++;
      const ok=(filterState.pos==='all'||row.dataset.position===filterState.pos)
        && parseFloat(row.dataset.price)<=maxPrice
        && (!needle||plain(row.dataset.find||row.dataset.name).includes(needle));
      row.hidden=!ok; if(ok) shown++;
    });
    // Each box says how many it shows, and says so when the filter leaves it empty.
    scope.querySelectorAll('.block').forEach(box=>{
      const rows=[...box.querySelectorAll('li[data-position]')];
      if(!rows.length) return;
      const badge=box.querySelector('.sec-head .count');
      if(badge){
        if(!badge.dataset.total) badge.dataset.total=badge.textContent;
        badge.textContent=active?rows.filter(r=>!r.hidden).length:badge.dataset.total;
      }
      let none=box.querySelector('.f-none');
      const empty=active&&rows.every(r=>r.hidden);
      if(empty&&!none){
        none=document.createElement('p');
        none.className='mk-empty f-none';
        none.textContent='Ninguno con este filtro.';
        box.appendChild(none);
      }
      if(none) none.hidden=!empty;
      const list=box.querySelector('.scrollbox, .rows');
      if(list) list.hidden=empty;
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
    // Without its controls it is not a filter bar: an exception here takes the rest of the
    // start-up with it (the remaining wiring and the live connection), and the whole page is
    // left dead with nothing clickable.
    if(!pos||!price||!text||!reset) return;
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

// ---- bidding, confirmed twice ----------------------------------------------
const modal=document.getElementById('bid-modal');
let pending=null;

const fmt=(n)=> n==null ? '—' :
  (Math.abs(n)>=1e6 ? (n/1e6).toFixed(2)+'M' : Math.abs(n)>=1e3 ? (n/1e3).toFixed(0)+'K' : String(n));
// The amount is typed with thousands separators so nobody has to count zeros.
const group=(n)=> (n==null||isNaN(n)) ? '' : Number(n).toLocaleString('es-ES');
const digits=(s)=> parseInt(String(s).replace(/[^0-9]/g,''),10);
const exact=(n)=> n==null ? '—' : Number(n).toLocaleString('es-ES')+' €';

function closeModal(){ modal.hidden=true; pending=null; }

// One place decides which step is shown: the buttons used to share a class with the blocks
// and querySelector only reached the first, so the content moved on while the buttons stayed
// on step one.
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
  // :not([data-op]) because the accept-an-offer button carries .bid for the colour alone, and
  // it was catching this handler: pressing Aceptar opened the bid dialog with a minimum of NaN.
  // Colour cannot decide what a button does.
  root.querySelectorAll('button.bid:not([data-op])').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>openBid(button.dataset));
  });
}

function openBid(data){
  // The modal is reused, so whatever a clause raise hid has to be given back.
  modal.querySelector('.bid-refs').hidden=false;
  modal.querySelector('#bid-clause').hidden=true;
  modal.querySelector('#bid-amount-label').textContent='Importe de la puja';
  // With a bid already placed the operation is to change it: the API refuses a second with 400.
  const existing=data.bid||null;
  // The button dictates the operation: the free market takes a bid and a rival's listing takes
  // an offer, and the API answers 404 to the wrong one.
  const operation=existing?'modify_bid':(data.operation||'bid');
  pending={market_id:data.market, player_id:data.player, name:data.name,
           min_bid:+data.min, ideal:+data.ideal||0, value:+data.value,
           bid_id:existing, operation};
  modal.hidden=false;
  modal.querySelector('.bid-action').textContent =
    existing ? 'Cambiar tu puja por' : (operation==='buy_offer' ? 'Ofertar por' : 'Pujar por');
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
                            || pending.operation==='buy_offer' || !pending.operation);
  wrap.hidden = !isBid;
  if(!isBid) return;
  // One of those bids can be yours, and counting it as a rival's is counting wrong: it says so.
  const mine = pending && pending.bid_id ? 1 : 0;
  const others = Math.max(0, count - mine);
  node.textContent = !count ? 'ninguna'
    : mine ? (others ? `${count} · ${others} de rivales y la tuya` : 'solo la tuya')
           : String(count);
  node.className = 'bid-rivals'+(others?' rivals-on':'');
}

// What doubles is the rise, not what you pay: you pay 8,555 and the clause gains 17,110. It
// lives here because it is the only operation where the amount you type is not what changes.
const CLAUSE_FACTOR=2;

// When the money comes in rather than goes out. Both steps of the dialog read it, the one
// where the amount is typed and the one where it is confirmed, so they cannot name the same
// thing two ways: paying a clause leaves the bank now, and "si sale" was a lie in that row. A
// sale pays nothing today, it pays if somebody buys, which is why the line says so instead of
// adding it up in silence.
const CASH_IN=new Set(['sell_to_market','accept_offer']);
// What has not happened yet: a bid takes nothing until it is won, and an offer to a rival
// takes nothing until he accepts.
const CASH_WHEN={bid:'si la ganas', modify_bid:'si la ganas', buy_offer:'si te la aceptan',
                 direct_offer:'si te la aceptan', sell_to_market:'si te lo compran',
                 accept_offer:'al aceptarla'};

// How the balance ends up, while the amount is still being typed. It is the one reference that
// decides whether the operation can happen at all, and it only appeared on step two, by which
// point the amount was already decided.
function showBalance(amount){
  const box=modal.querySelector('.bid-balance');
  if(!box) return;
  const op=pending.operation||'bid';
  if(myCash==null||!amount){ box.hidden=true; return; }
  const after=myCash+(CASH_IN.has(op)?amount:-amount);
  const when=CASH_WHEN[op];
  box.innerHTML='Saldo <b>'+exact(myCash)+'</b> → <b class="'
    +(after<0?'balance-bad':'balance-after')+'">'+exact(after)+'</b>'
    +(when?' <span class="muted">'+when+'</span>':'')
    +(after<0?' <span class="balance-bad">no te llega</span>':'');
  box.hidden=false;
}

function clauseSums(amount){
  const box=modal.querySelector('#bid-clause');
  if(!box) return;
  const rise=amount*CLAUSE_FACTOR;
  const next=(pending.clause||0)+rise;
  const times=pending.value ? next/pending.value : 0;
  const safe=pending.safe||0;
  // The same line the advice uses, said here while you type: above it, paying the clause is a
  // bad deal for whoever pays it, and that is the whole of the defence it buys.
  let verdict='';
  if(safe && times){
    verdict = times>=safe
      ? `<dt></dt><dd class="clause-safe">por encima de ${safe.toFixed(2)}x: a nadie le renta pagarla</dd>`
      : `<dt></dt><dd class="clause-open">por debajo de ${safe.toFixed(2)}x: sigue siendo negocio para quien pueda pagarla</dd>`;
  }
  box.innerHTML=
    `<dt>Multiplicador</dt><dd>${CLAUSE_FACTOR}x</dd>`
    +`<dt>Sube la clausula</dt><dd>${exact(rise)}</dd>`
    +`<dt>Clausula ahora</dt><dd>${exact(pending.clause||0)}</dd>`
    +`<dt>Clausula nueva</dt><dd class="clause-new">${exact(next)}`
    +`${times?` · ${times.toFixed(2)}x su valor`:''}</dd>`
    +verdict;
  box.hidden=false;
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
  // Raising a clause has no minimum bid and no futbolfantasy ceiling: that ceiling is what is
  // worth paying *for the player*, and nobody is being bought here. It said "no le ve
  // rentabilidad".
  showBalance(amount);
  if(pending.raise){
    clauseSums(amount);
    if(!amount){
      const now=pending.value ? (pending.clause||0)/pending.value : 0;
      // Suggesting zero is an answer, not a blank: the clause is already where it should be.
      text = pending.safe && now>=pending.safe
        ? `Ya esta a ${now.toFixed(2)}x su valor, por encima de ${pending.safe.toFixed(2)}x: `
          +'no hace falta subirla. Si aun asi quieres, escribe un importe.'
        : 'Escribe lo que quieres pagar.';
    }
    warn.textContent=text;
    warn.hidden=!text;
    return;
  }
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

  // step 1: ask the server to validate and hand back the summary and a token
  modal.querySelector('.bid-next').addEventListener('click', async ()=>{
    const amount=digits(modal.querySelector('.bid-amount').value);
    modal.querySelector('.bid-error').textContent='';
    usage.op('empezar '+(pending.operation||'bid'));
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
      const movesCash=['bid','modify_bid','buy_offer','direct_offer','pay_clause',
                       'accept_offer','raise_clause','sell_to_market'].includes(op);
      modal.querySelector('.bid-summary').innerHTML =
        `<dl class="bid-dl">
           <dt>Jugador</dt><dd>${data.player_name||pending.name}</dd>
           <dt>${AMOUNT_LABEL[op]||'Importe'}</dt>
             <dd><strong>${exact(data.amount)}</strong></dd>
           ${data.new_clause?`<dt>Clausula</dt>
             <dd>${exact(data.clause)} → <strong>${exact(data.new_clause)}</strong></dd>`:''}
           <dt>Saldo ahora</dt><dd>${exact(data.cash_before)}</dd>
           ${movesCash?`<dt>Saldo ${CASH_WHEN[op]||'despues'}</dt>
             <dd><strong>${exact(data.cash_after)}</strong></dd>`:''}
         </dl>` +
        (data.warnings||[]).map(w=>`<p class="bid-warn-line">⚠ ${w}</p>`).join('');
      showStep(2,{confirmLabel:CONFIRM_LABEL[pending.operation]||'Aceptar'});
    }catch(err){
      modal.querySelector('.bid-error').textContent=err.message;
    }
  });

  // withdrawing a bid already placed, through the same two steps
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

  // step 2: actually confirm
  modal.querySelector('.bid-confirm').addEventListener('click', async ()=>{
    const button=modal.querySelector('.bid-confirm');
    button.disabled=true; button.textContent='Enviando…';
    usage.op('confirmar '+(pending.operation||'bid'));
    try{
      const res=await fetch('/api/bid/confirm',{method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify({token:pending.token})});
      const data=await res.json();
      if(!res.ok) throw new Error(data.error||res.status);
      const done=DONE_LABEL[pending&&pending.operation]||'Hecho';
      modal.querySelector('.bid-summary').innerHTML =
        `<p class="bid-ok">${done}${data.dry_run?' (simulacro)':''}.</p>`;
      modal.querySelector('.bid-confirm').hidden=true;
      modal.querySelector('.bid-cancel').textContent='Cerrar';
      // The server has already done it: the row goes and the notice appears now, without
      // waiting for the world to be rebuilt. By the time the refresh lands the table agrees.
      // In a dry run nothing moved, so the summary stays where it can be read.
      if(!data.dry_run){ settled(pending,done); closeModal(); }
    }catch(err){
      modal.querySelector('.bid-error').textContent=err.message;
    }finally{
      button.disabled=false; button.textContent='Aceptar';
    }
  });
}

// Operations that end the row: an offer accepted or refused no longer exists, nor does a bid
// withdrawn. Listing or bidding delete nothing, so there only the notice appears.
const OP_ENDS_ROW=new Set(['accept_offer','decline_offer','withdraw','cancel_bid',
                           'cancel_offer','pay_clause']);

function settled(op,label){
  if(!op) return;
  if(OP_ENDS_ROW.has(op.operation)){
    // The row is found by what identifies it, most specific first: an offer
    // concreta, si no la entrada de mercado, si no el jugador.
    const key=op.offer_id?`[data-op-offer="${op.offer_id}"]`
      :(op.market_id?`[data-op-market="${op.market_id}"]`
      :(op.player_id?`[data-op-player="${op.player_id}"]`:''));
    if(key) document.querySelectorAll('button.op'+key).forEach(button=>{
      const row=button.closest('tr');
      if(row) row.classList.add('row-gone');
      button.disabled=true;
    });
  }
  flash(label,op.name);
}

// The same notice the server sends when something moves, but said here and at once.
function flash(title,detail){
  const box=document.createElement('div');
  box.className='effect';
  box.innerHTML=`<button class="effect-close" aria-label="Cerrar">×</button>`
    +`<h4>${title}</h4>`+(detail?`<p class="effect-line">${detail}</p>`:'');
  document.body.appendChild(box);
  box.querySelector('.effect-close').addEventListener('click',()=>box.remove());
  requestAnimationFrame(()=>box.classList.add('in'));
  setTimeout(()=>{box.classList.remove('in');setTimeout(()=>box.remove(),400);},7000);
}

// ---- operaciones genericas (aceptar/rechazar oferta, retirar) --------------
// Every operation is called by its own name on the final button and in the summary: "Pujas" and
// "Saldo si ganas" means nothing when what you are doing is selling.
const CONFIRM_LABEL={};   // el boton dice simplemente Aceptar
const DONE_LABEL={bid:'Puja enviada',sell_to_market:'Puesto en venta',
  accept_offer:'Oferta aceptada',decline_offer:'Oferta rechazada',
  withdraw:'Retirado del mercado',direct_offer:'Oferta enviada',
  pay_clause:'Clausula pagada',raise_clause:'Clausula subida',
  cancel_bid:'Puja retirada',modify_bid:'Puja cambiada',buy_offer:'Oferta enviada',
  cancel_offer:'Oferta retirada',shield_player:'Blindado 24h'};
const AMOUNT_LABEL={bid:'Pujas',modify_bid:'Nueva puja',buy_offer:'Ofreces',
  sell_to_market:'Precio de venta',
  accept_offer:'Cobras',direct_offer:'Ofreces',pay_clause:'Pagas',
  raise_clause:'Pagas'};

const OP_LABELS={accept_offer:'Aceptar oferta por',decline_offer:'Rechazar oferta por',
                 withdraw:'Retirar del mercado a',sell_to_market:'Poner en venta a',
                 cancel_offer:'Retirar tu oferta por',cancel_raid:'Cancelar el clausulazo de',
                 drop_always:'Quitar de siempre-en-mercado a',
                 pay_clause:'Pagar la cláusula de',
                 shield_player:'Blindar 24h a'};

function wireOps(root=document){
  root.querySelectorAll('button.op').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click', async ()=>{
      const d=button.dataset;
      // Cancelling a raid is not an operation against LaLiga: it deletes an instruction of
      // ours, so it skips the two-step confirmation, which exists for money. Dropping a
      // standing instruction spends nothing either: it stops spending.
      if(d.op==='drop_always'){
        if(!confirm('Quitar '+d.opName+' de siempre-en-mercado?')) return;
        try{
          const res=await fetch('/api/always',{method:'POST',
            headers:{'Content-Type':'application/json'},
            body:JSON.stringify({id:d.opPlayer,name:d.opName})});
          const data=await res.json();
          if(!res.ok) throw new Error(data.error||res.status);
          if(data.always_listed){
            // The toggle would have put it back: leave it as it was and say so.
            await fetch('/api/always',{method:'POST',headers:{'Content-Type':'application/json'},
              body:JSON.stringify({id:d.opPlayer,name:d.opName})});
            throw new Error('no estaba armado');
          }
          button.closest('tr')?.classList.add('row-gone');
          button.disabled=true; button.textContent='quitado';
        }catch(err){ alert('No he podido quitarlo: '+err.message); }
        return;
      }
      if(d.op==='cancel_raid'){
        if(!confirm('Cancelar el clausulazo programado de '+d.opName+'?')) return;
        try{
          const res=await fetch('/api/raid/cancel',{method:'POST',
            headers:{'Content-Type':'application/json'},
            body:JSON.stringify({id:d.opPlayer,name:d.opName})});
          if(!res.ok) throw new Error((await res.json()).error||res.status);
          button.closest('tr')?.classList.add('row-gone');
          button.disabled=true; button.textContent='cancelado';
        }catch(err){ alert('No he podido cancelarlo: '+err.message); }
        return;
      }
      confirmOp({op:d.op, name:d.opName, player_id:d.opPlayer, market_id:d.opMarket,
                 offer_id:d.opOffer, amount:+d.opAmount||null});
    });
  });
}

// The two-step confirmation: the summary the server gives, its single-use token and the final
// button. It lives apart because two places ask for it, the tables and the drawer, and the
// drawer has no table button to hang off.
async function confirmOp(op){
  const name=op.name||'';
  pending={operation:op.op, market_id:op.market_id||'', offer_id:op.offer_id||'',
           player_id:op.player_id, name, amount:op.amount||null};
  modal.hidden=false;
  modal.querySelector('.bid-who').textContent=name;
  modal.querySelector('.bid-action').textContent=OP_LABELS[op.op]||'Confirmar';
  modal.querySelector('.bid-drop').hidden=true;
  modal.querySelector('.bid-error').textContent='';
  modal.querySelector('.bid-summary').innerHTML='<p>Comprobando…</p>';
  showStep(2,{confirmLabel:CONFIRM_LABEL[op.op]||'Aceptar'});
  try{
    const res=await fetch('/api/bid/prepare',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({operation:op.op, market_id:op.market_id, offer_id:op.offer_id,
                           player_id:op.player_id, amount:op.amount||undefined})});
    const data=await res.json();
    if(!res.ok) throw new Error(data.error||res.status);
    pending.token=data.token;
    modal.querySelector('.bid-summary').innerHTML=
      `<dl class="bid-dl">
         <dt>Operacion</dt><dd>${data.label}</dd>
         <dt>Jugador</dt><dd>${data.player_name||name}</dd>
         ${data.amount?`<dt>Importe</dt><dd><strong>${exact(data.amount)}</strong></dd>`:''}
         <dt>Saldo</dt><dd>${exact(data.cash_before)}</dd>
       </dl>` +
      (data.warnings||[]).map(w=>`<p class="bid-warn-line">⚠ ${w}</p>`).join('');
  }catch(err){
    modal.querySelector('.bid-summary').innerHTML='';
    modal.querySelector('.bid-error').textContent=err.message;
    modal.querySelector('.bid-confirm').hidden=true;
  }
}



// ---- alineacion: campo, arrastrar y guardar --------------------------------
const LINE_ORDER=['striker','midfield','defender','goalkeeper'];   // arriba -> abajo
const LINE_LABEL={goalkeeper:'POR',defender:'DEF',midfield:'MED',striker:'DEL'};
const LINE_POS={goalkeeper:1,defender:2,midfield:3,striker:4};
let pitchState=null, pitchDirty=false, dragged=null;


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

function shirtHtml(player,line,index){
  if(!player) return `<div class="slot empty gap" data-line="${line}" data-index="${index}"
    title="No tienes con quien cubrir esta plaza">⚠<br>${LINE_LABEL[line]}<br>sin cubrir</div>`;
  const listed=player.listed_for?`<span class="tok-flag" title="en venta por ${mny(player.listed_for)}"><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M2.2 8.6V3.2a1 1 0 0 1 1-1h5.4l5.2 5.2a1 1 0 0 1 0 1.4l-4.4 4.4a1 1 0 0 1-1.4 0z"/><circle cx="5.4" cy="5.4" r="1.2"/></svg></span>`:'';
  return `<div class="slot tokslot" draggable="true" data-line="${line}"
    data-index="${index}" data-player="${player.id}" data-pt="${player.player_team_id}"
    title="${player.name}${player.next_rival?(' · vs '+player.next_rival
      +(player.next_home?' (en casa)':' (fuera)')):''}">
    ${gripHtml()}${listed}
    ${faceOf(player,'md')}
    <span class="tok-name">${player.name}${shieldMark(player)}</span>
    <span class="tok-x ${xClass(player.xpts||0)}">${dec(player.xpts||0)}</span>
  </div>`;
}

// How a reserve stands for selling: the hold rule first, then what is already on the table.
function sellState(p){
  if(p.sale_locked&&p.hold_until) return `🔒 hasta ${whenShort(p.hold_until)}`;
  if(p.best_offer) return `oferta de ${mny(p.best_offer)}`;
  if(p.listed_for) return `en venta por ${mny(p.listed_for)}`;
  return 'se puede vender';
}

function benchHtml(player){
  const pos={1:'POR',2:'DEF',3:'MED',4:'DEL'}[player.position_id]||'ENT';
  return `<div class="bench-item" draggable="true" data-player="${player.id}"
    data-pt="${player.player_team_id}" data-from="bench" title="${player.name}">
    ${gripHtml()}
    ${faceOf(player,'sm')}
    <span class="bench-who"><span class="bench-name">${player.name}${shieldMark(player)}</span>
      <span class="bench-sell">${sellState(player)}</span></span>
    <span class="pos pos-${pos.toLowerCase()}">${pos}</span>
    <span class="tx ${xClass(player.xpts||0)}">${dec(player.xpts||0)}</span>
  </div>`;
}

// The saved eleven against the best one the squad allows, and the way to load the latter.
function pitchBest(){
  const box=document.getElementById('pitch-best');
  if(!box||!pitchState) return;
  const best=pitchState.best;
  const saved=LINE_ORDER.reduce((sum,l)=>sum+(pitchState.lines[l]||[])
    .reduce((t,p)=>t+(p?(p.xpts||0):0),0),0);
  if(!best||best.xpts-saved<0.05){ box.hidden=true; return; }
  box.hidden=false;
  box.innerHTML=`Tu mejor once suma <b>${dec(best.xpts)}</b>; el que tienes, <b>${dec(saved)}</b>. `
    +`<button type="button" class="pitch-apply">Poner el mejor</button>`;
  box.querySelector('.pitch-apply').onclick=applyBest;
}

function applyBest(){
  const best=pitchState&&pitchState.best;
  if(!best) return;
  const everyone=[...LINE_ORDER.flatMap(l=>(pitchState.lines[l]||[]).filter(Boolean)),
                  ...(pitchState.bench||[])];
  const byId=Object.fromEntries(everyone.map(p=>[String(p.id),p]));
  const used=new Set();
  const lines={};
  LINE_ORDER.forEach(l=>{
    lines[l]=(best.lines[l]||[]).map(id=>{ used.add(String(id)); return byId[String(id)]||null; });
  });
  pitchState.lines=lines;
  pitchState.bench=everyone.filter(p=>!used.has(String(p.id)));
  pitchState.formation=best.formation;
  const select=document.getElementById('pitch-formation-select');
  if(select) select.value=best.formation.join(',');
  pitchDirty=true;
  usage.click('alineacion','poner el mejor once');
  renderPitch();
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
  pitchAlert();
  pitchBest();
  wireDrag();
}

const LINE_WORD={goalkeeper:['portero','porteros'],defender:['defensa','defensas'],
                 midfield:['medio','medios'],striker:['delantero','delanteros']};

// A hole in the eleven is points not played, so it is said at the top with the way out beside
// it: the formation that does fit the players who can play, if any fits.
//
// Counting shirts will not do. Eleven shirts with a suspended player among them are ten
// players and one named hole, and this warning offered the formation change that lines them up
// as if that
// would fix anything.
// Only a confirmed absence keeps a starter from scoring; a doubt or a knock still plays.
const cannotPlay=p=>!!p&&(p.available===false||(health(p)||{}).ring==='out');

function pitchAlert(){
  const box=document.getElementById('pitch-alert');
  if(!box) return;
  const holes=[]; let missing=0; const idle=[], doubts=[];
  LINE_ORDER.forEach(line=>{
    const slots=pitchState.lines[line]||[];
    slots.forEach(p=>{ if(cannotPlay(p)) idle.push(p); else if(p&&health(p)) doubts.push(p); });
    const empty=slots.filter(p=>!p).length;
    if(!empty) return;
    missing+=empty;
    holes.push(`${empty} ${LINE_WORD[line][empty>1?1:0]}`);
  });
  const doubtLine=doubts.length?`<span class="pitch-doubt">${doubts.map(p=>
    `<b>${p.name}</b>: ${health(p).label.toLowerCase()}${p.start_probability!=null?` (${p.start_probability} %)`:''}`)
    .join(' · ')}</span>`:'';
  // No holes and nobody standing there for nothing: at most the doubts, said softly.
  if(!missing&&!idle.length){
    box.innerHTML=doubtLine; box.hidden=!doubtLine; box.classList.toggle('soft',!!doubtLine);
    return;
  }
  box.classList.remove('soft');

  const have={1:0,2:0,3:0,4:0}, can={1:0,2:0,3:0,4:0};
  const tally=p=>{ if(!p) return; have[p.position_id]++; if(!cannotPlay(p)) can[p.position_id]++; };
  LINE_ORDER.forEach(line=>(pitchState.lines[line]||[]).forEach(tally));
  (pitchState.bench||[]).forEach(tally);
  const squad=have[1]+have[2]+have[3]+have[4];
  const playable=can[1]+can[2]+can[3]+can[4];
  const fits=f=>{ const [d,m,s]=f.split(',').map(Number);
    return can[1]>=1&&can[2]>=d&&can[3]>=m&&can[4]>=s; };
  const free=(pitchState.formations.free||[]).find(fits);
  const premium=free?null:(pitchState.formations.premium||[]).find(fits);
  const option=free||premium;
  const shape=(pitchState.formation||[]).join('-');

  let out='';
  if(missing) out+=`<span>⚠ <b>Once incompleto</b>: el ${shape} pide 11 y sales con `
    +`${11-missing}. Falta${missing>1?'n':''} ${holes.join(' y ')}.</span>`;
  if(idle.length){
    const who=idle.map(p=>{ const s=statusOf(p);
      return `<b>${p.name}</b>${s?` (${s.label.toLowerCase()})`:''}`; }).join(', ');
    out+=`<span>⚠ ${who} en el campo sin poder jugar: esa plaza no puntua.</span>`;
  }
  if(option) out+=`<span>Con <b>${option.replace(/,/g,'-')}</b>`
    +`${premium?' (premium)':''} cuadras el once sin contar a quien no puede jugar.</span>`
    +`<button type="button" data-formation="${option}">Cambiar a ${option.replace(/,/g,'-')}</button>`;
  else if(playable<11) out+=`<span>Hoy solo pueden jugar ${playable} de tus ${squad}: `
    +`ninguna formacion cuadra el once, y cambiarla no lo arregla. Toca fichar.</span>`;
  else out+=`<span>Ninguna formacion cuadra con ${squad} jugadores: toca fichar.</span>`;
  box.innerHTML=out+doubtLine;
  box.hidden=false;
  const button=box.querySelector('button[data-formation]');
  if(button) button.addEventListener('click',()=>applyFormation(button.dataset.formation));
}

let justDragged=false;

// A touchscreen emits no dragstart, so the grip lifts a player and the next tap drops him.
// The body of a shirt still opens his card, which is what gets tapped nine times out of ten.
let lifted=null;

const gripHtml=()=>'<button class="slot-grip" type="button" title="Mover">⇅</button>';

function lift(next){
  lifted=next;
  document.querySelectorAll('.lifted').forEach(n=>n.classList.remove('lifted'));
  const status=document.getElementById('pitch-status');
  if(!lifted){ if(status) status.textContent=''; return; }
  const node=document.querySelector(`.slot[data-player="${lifted.id}"],`
    +`.bench-item[data-player="${lifted.id}"]`);
  if(node) node.classList.add('lifted');
  if(status) status.textContent='Toca el hueco donde va';
}

function placeOn(node){
  if(!lifted) return false;
  dragged=lifted;
  lift(null);
  if(node.id==='bench') dropOnBench();
  else dropOnSlot(node.dataset.line, +node.dataset.index);
  dragged=null;
  // dropOnSlot returns without repainting when the hole is the same one, and the player
  // would stay marked as lifted.
  renderPitch();
  return true;
}

function wireDrag(){
  document.querySelectorAll('.slot[draggable], .bench-item[draggable]').forEach(node=>{
    // Dragging and clicking start the same way, so a drop must not open the card.
    node.addEventListener('click',(event)=>{
      if(event.target.closest('.slot-grip')) return;
      if(placeOn(node)) return;
      if(justDragged) return;
      if(node.dataset.player) openDetail(node.dataset.player);
    });
    const grip=node.querySelector('.slot-grip');
    if(grip) grip.addEventListener('click',(event)=>{
      event.stopPropagation();
      if(lifted&&lifted.id===node.dataset.player){ lift(null); return; }
      lift({id:node.dataset.player, pt:node.dataset.pt,
            from:node.dataset.from||'pitch',
            line:node.dataset.line, index:+node.dataset.index});
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
    node.addEventListener('click',()=>{ placeOn(node); });
    node.addEventListener('dragover',e=>{ e.preventDefault(); node.classList.add('drop-target'); });
    node.addEventListener('dragleave',()=>node.classList.remove('drop-target'));
    node.addEventListener('drop',e=>{
      e.preventDefault(); node.classList.remove('drop-target');
      if(!dragged) return;
      if(node.id==='bench') dropOnBench();
      else dropOnSlot(node.dataset.line, +node.dataset.index);
    });
  });
  if(lifted) lift(lifted);
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
  // A line takes only its own position; the keeper cannot be moved at all.
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
  // fill the holes with reserves of that position, the rest to the bench
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
  // The picker is changed from the warning too, and has to be left telling the truth.
  const select=document.getElementById('pitch-formation-select');
  if(select) select.value=text;
  pitchDirty=true; renderPitch();
}

// ---- la liga jornada a jornada --------------------------------------------
// One line per manager over the finished matchdays: the place each of them held after each one.
// The server rebuilds it from the elevens, so it is asked for once, when the tab is opened.
let seasonData=null, seasonAsked=false;

async function loadSeason(){
  const box=document.querySelector('.evo');
  if(!box) return;
  if(seasonData){ box.innerHTML=seasonChart(seasonData,box.clientWidth); return; }
  if(seasonAsked) return;
  seasonAsked=true;
  box.innerHTML='<p class="empty">Reconstruyendo la clasificacion…</p>';
  try{
    const res=await fetch('/api/season');
    if(!res.ok) throw new Error(res.status);
    seasonData=await res.json();
    // A live refresh during the wait puts a new, empty frame in place of the one asked for.
    const frame=document.querySelector('.evo')||box;
    frame.innerHTML=seasonChart(seasonData,frame.clientWidth);
  }catch(e){
    seasonAsked=false;
    box.innerHTML='<p class="empty">No he podido reconstruir la clasificacion.</p>';
  }
}

// Drawn at the width it has, so turning the phone has to redraw it.
let seasonRedraw;
window.addEventListener('resize',()=>{
  if(!seasonData) return;
  clearTimeout(seasonRedraw);
  seasonRedraw=setTimeout(loadSeason,200);
});

// The season: every manager in a colour of his own and me in the accent. Picking managers on
// the chips leaves only those (and me) coloured, the rest thin and grey. Rank or points.
const EVO_KEY='fantasy:evo', EVO_HUES=11;
function evoState(){
  try{ const saved=JSON.parse(localStorage.getItem(EVO_KEY)||'{}');
       return {mode:saved.mode==='points'?'points':'place', picked:saved.picked||[]}; }
  catch(e){ return {mode:'place',picked:[]}; }
}
function evoSave(state){ try{ localStorage.setItem(EVO_KEY,JSON.stringify(state)); }catch(e){} }

function seasonChart(d,width){
  const weeks=d.weeks||[];
  const managers=(d.managers||[]).filter(m=>(m.place||[]).some(p=>p!=null));
  if(weeks.length<1||!managers.length) return '<p class="empty">Aun no hay jornadas terminadas.</p>';
  const state=evoState();
  const picked=state.picked.filter(id=>managers.some(m=>m.team_id===id));
  // Each manager's colour is fixed, by his place in a stable order, so it never moves.
  const hue=new Map([...managers].filter(m=>!m.is_me).sort((a,b)=>String(a.team_id).localeCompare(b.team_id))
    .map((m,i)=>[m.team_id,i%EVO_HUES+1]));
  const shown=id=>!picked.length||picked.includes(id);
  const byPoints=state.mode==='points';

  const w=Math.max(300,width||760), narrow=w<560;
  const padL=byPoints?(narrow?50:58):(narrow?34:44), padR=narrow?78:130, padT=14, padB=24;
  const rows=managers.length, h=narrow?Math.round(w*0.75):Math.max(260,padT+padB+(rows-1)*22);
  const step=weeks.length>1?(w-padL-padR)/(weeks.length-1):0;
  const x=i=>padL+step*i;
  const top=Math.max(1,...managers.flatMap(m=>(m.total||[]).filter(v=>v!=null)));
  const y=byPoints
    ? v=>padT+(h-padT-padB)*(1-v/top)
    : p=>padT+(h-padT-padB)*(rows>1?(p-1)/(rows-1):0);
  const valueOf=(m,i)=>byPoints?m.total[i]:m.place[i];

  let grid='';
  weeks.forEach((week,i)=>{
    grid+=`<line class="evo-grid" x1="${x(i)}" y1="${padT-6}" x2="${x(i)}" y2="${h-padB+4}"></line>`
      +`<text class="evo-axis" x="${x(i)}" y="${h-padB+16}" text-anchor="middle">J${week}</text>`;
  });
  const ticks=byPoints?[0,Math.round(top/2),Math.round(top)].map(v=>[v,v+' pts']):[[1,'1º'],[rows,rows+'º']];
  ticks.forEach(([v,label])=>{
    grid+=`<text class="evo-axis" x="${padL-8}" y="${y(v)+3}" text-anchor="end">${label}</text>`;
  });

  const kind=m=>!shown(m.team_id)?'rest':m.is_me?'me':'pick';
  const order={rest:0,pick:1,me:2};
  const painted=[...managers].sort((a,b)=>order[kind(a)]-order[kind(b)]);
  const labels=[];
  const lines=painted.map(m=>{
    const points=[];
    (m.place||[]).forEach((place,i)=>{
      const v=valueOf(m,i);
      if(place!=null&&v!=null) points.push([x(i),y(v),i,place]);
    });
    if(!points.length) return '';
    const k=kind(m);
    const cls=k==='me'?'evo-me':k==='pick'?`evo-pick evo-h${hue.get(m.team_id)}`:'evo-rest';
    const dots=points.map(([px,py,i,place])=>
      `<circle class="evo-dot" cx="${px}" cy="${py}" r="${k==='rest'?3:4}" tabindex="0" data-tip="${
        m.manager} · J${weeks[i]} · ${place}º · ${Math.round(m.points[i]||0)} pts · ${
        Math.round(m.total[i]||0)} acumulados"></circle>`).join('');
    if(k!=='rest'){
      const last=points[points.length-1];
      labels.push({x:last[0]+9,y:last[1]+3.5,name:m.manager,cls});
    }
    return `<g class="evo-row ${cls}" data-evo-team="${m.team_id}">
      <polyline class="evo-line" points="${points.map(p=>p[0]+','+p[1]).join(' ')}"></polyline>
      <polyline class="evo-hit" points="${points.map(p=>p[0]+','+p[1]).join(' ')}"></polyline>${dots}</g>`;
  }).join('');
  // The labels at the right end, pushed apart so no two overlap.
  labels.sort((a,b)=>a.y-b.y);
  labels.forEach((label,i)=>{ if(i&&label.y<labels[i-1].y+12) label.y=labels[i-1].y+12; });
  const cut=name=>narrow&&name.length>9?name.slice(0,9)+'…':name;
  const names=labels.map(l=>`<text class="evo-name ${l.cls}" x="${l.x}" y="${l.y}">${cut(l.name)}</text>`).join('');

  const chips=[...managers].sort((a,b)=>(b.is_me?1:0)-(a.is_me?1:0)||String(a.manager).localeCompare(b.manager,'es'))
    .map(m=>{
      const k=kind(m);
      const sw=m.is_me?'evo-me':`evo-h${hue.get(m.team_id)}`;
      return `<button type="button" class="evo-chip ${sw}${picked.includes(m.team_id)?' on':''}" data-evo-pick="${m.team_id}"`
        +` aria-pressed="${k!=='rest'}"><i class="evo-sw"></i>${m.manager}</button>`;
    }).join('');
  const controls=`<div class="evo-controls"><div class="evo-mode" role="group">`
    +`<button type="button" data-evo-mode="place" class="${byPoints?'':'on'}">Puesto</button>`
    +`<button type="button" data-evo-mode="points" class="${byPoints?'on':''}">Puntos</button></div>`
    +`<div class="evo-chips">${chips}${picked.length?'<button type="button" class="evo-clear" data-evo-clear>Todos</button>':''}</div>`
    +`<p class="evo-hint">${picked.length?'Toca más managers para añadirlos o quitarlos.':'Toca un manager para ver solo su línea.'}</p></div>`;
  return controls+`<svg class="evo-svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}"
    role="img" aria-label="${byPoints?'Puntos acumulados':'Puesto'} de cada manager jornada a jornada">${grid}${lines}${names}</svg>`;
}

document.addEventListener('click',(event)=>{
  const t=event.target.closest&&event.target.closest('[data-evo-pick],[data-evo-mode],[data-evo-clear]');
  if(!t||!t.closest('.evo')) return;
  const state=evoState();
  if(t.dataset.evoMode) state.mode=t.dataset.evoMode;
  else if(t.hasAttribute('data-evo-clear')) state.picked=[];
  else{
    const id=t.dataset.evoPick;
    state.picked=state.picked.includes(id)?state.picked.filter(x=>x!==id):[...state.picked,id];
  }
  evoSave(state);
  usage.click('liga','evolucion',t.dataset.evoMode||t.dataset.evoPick||'limpiar');
  loadSeason();
});
// Hovering a chip brings his line forward.
document.addEventListener('mouseover',(event)=>{
  const chip=event.target.closest&&event.target.closest('.evo [data-evo-pick]');
  document.querySelectorAll('.evo-row.hl').forEach(g=>g.classList.remove('hl'));
  if(chip) document.querySelector(`.evo-row[data-evo-team="${chip.dataset.evoPick}"]`)?.classList.add('hl');
});

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
    // The ids do not change on save, so repainting what we already have is enough: reloading
    // from the API is a whole round trip for the same result.
    pitchState.formation=data.formation||pitchState.formation;
    renderPitch();
    document.getElementById('pitch-status').textContent=
      'guardada ' + new Date().toLocaleTimeString('es-ES');
  }catch(err){ flashPitch('No se ha guardado: '+err.message); }
  finally{ button.textContent='Guardar alineación'; }
}

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
    if(base==='comparador'&&!VIEWS[view]) renderCompare();
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
// How long ago something happened, in the same units as the countdown: a signing from three
// days ago and one from three hours ago are different decisions.
function since(stamp){
  const gone=Date.now()-new Date(stamp).getTime();
  if(isNaN(gone)||gone<0) return '—';
  const h=Math.floor(gone/3600000), m=Math.floor(gone%3600000/60000);
  return h>=24 ? 'hace '+Math.floor(h/24)+'d '+(h%24)+'h'
       : h>0   ? 'hace '+h+'h '+String(m).padStart(2,'0')+'m'
               : 'hace '+m+'m';
}

// The shield form: a day and an hour, prefilled with when the cover is worth starting, and the
// matchday's two shields counted. "Ahora" goes through the two-step confirmation.
function shieldDialog(a,player){
  const box=document.getElementById('shield-modal');
  const day=box.querySelector('#shield-day'), hour=box.querySelector('#shield-time');
  const error=box.querySelector('.shield-error');
  const pad=n=>String(n).padStart(2,'0');
  const start=a.suggested?new Date(a.suggested):new Date();
  const today=new Date();
  box.querySelector('.shield-who').textContent=player.name;
  box.querySelector('.shield-help').textContent=
    a.because==='round'?'Los dos blindajes de esta jornada ya están usados: te propongo '
      +stampText(a.suggested)+', cuando empieza la siguiente.'
    :a.because==='shield'?'Su blindaje acaba el '+stampText(a.suggested)+': te propongo esa hora '
      +'para encadenar el siguiente.'
    :a.suggested?'Las clausulas estan cerradas hasta '+stampText(a.suggested)+': te propongo '
      +'esa hora, antes no protege de nada.'
    :'Las clausulas se pueden pagar ahora mismo: blindarlo ya protege.';
  const quota=box.querySelector('.shield-quota'), budget=a.budget||{};
  quota.textContent='';
  quota.classList.remove('full');
  if(budget.known){
    const left=budget.limit-(budget.used||[]).length-(budget.booked||[]).length;
    const who=[...(budget.used||[]).map(u=>u.player+' '+stampText(u.at)),
               ...(budget.booked||[]).map(u=>u.player+' '+stampText(u.at)+' programado')];
    quota.textContent=`Jornada ${budget.round.week}: te quedan ${Math.max(left,0)} de ${budget.limit}`
      +(who.length?' ('+who.join(', ')+')':'');
    quota.classList.toggle('full',left<=0);
  }
  box.querySelector('.shield-now').hidden=!a.now_allowed;
  day.min=`${today.getFullYear()}-${pad(today.getMonth()+1)}-${pad(today.getDate())}`;
  day.value=`${start.getFullYear()}-${pad(start.getMonth()+1)}-${pad(start.getDate())}`;
  hour.value=`${pad(start.getHours())}:${pad(start.getMinutes())}`;
  error.textContent='';
  box.hidden=false;

  const close=()=>{ box.hidden=true; box.onclick=null; document.removeEventListener('keydown',onKey); };
  const onKey=(e)=>{ if(e.key==='Escape') close(); };
  document.addEventListener('keydown',onKey);
  box.onclick=async(e)=>{
    if(e.target===box||e.target.closest('.shield-cancel')){ close(); return; }
    if(e.target.closest('.shield-now')){
      close(); closeDrawer();
      confirmOp({op:'shield_player', name:player.name, player_id:player.id});
      return;
    }
    if(!e.target.closest('.shield-save')) return;
    const when=new Date(`${day.value}T${hour.value}`);
    if(!day.value||!hour.value||isNaN(when.getTime())){ error.textContent='Elige dia y hora.'; return; }
    if(when<=new Date()){ error.textContent='Esa hora ya ha pasado: usa "Ahora".'; return; }
    const res=await fetch('/api/shield',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({id:player.id,name:player.name,at:when.toISOString()})});
    if(!res.ok){
      const data=await res.json().catch(()=>({}));
      error.textContent=data.error||'No he podido programarlo.';
      return;
    }
    close();
    openDetail(player.id);
  };
}

// The day and time of an ISO stamp, local and without the year: how it is typed and read.
function stampText(stamp){
  const when=new Date(stamp);
  if(isNaN(when.getTime())) return '';
  const pad=n=>String(n).padStart(2,'0');
  return `${pad(when.getDate())}/${pad(when.getMonth()+1)} ${pad(when.getHours())}:${pad(when.getMinutes())}`;
}

function leftUntil(stamp){
  const left=new Date(stamp).getTime()-Date.now();
  if(isNaN(left)) return '—';
  if(left<=0) return 'ya';
  const h=Math.floor(left/3600000), m=Math.floor(left%3600000/60000);
  return h>=24 ? Math.floor(h/24)+'d '+(h%24)+'h' : h>0 ? h+'h '+String(m).padStart(2,'0')+'m'
                                                        : m+'m';
}

// A curve without figures says only "up" or "down". Under the cursor it says how much and on
// what day, which is the question being asked of it.
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
  <p class="drawer-note">Valor diario, últimos ${points.length} días ·
    mín ${fmt(lo)} · máx ${fmt(hi)} · pasa el cursor para ver cada día</p>`;
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
    // The viewBox is stretched with preserveAspectRatio="none", so the x on screen is the
    // proportion and not the viewBox's: the point and the line go in viewBox coordinates.
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
          row(m.manager,m.planned,m.counted?m.actual:null,m.forecast,Math.max(m.counted,1),
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
const FEED_SORT_KEY='fantasy:feed-sort';
function sortFeed(order){
  const rail=document.querySelector('.feed-rail');
  if(!rail) return;
  const rows=[...rail.children];
  rows.forEach((row,i)=>{ if(row.dataset.i==null) row.dataset.i=i; });
  rows.sort(order==='amount'
    ? (a,b)=>(+b.dataset.amount||0)-(+a.dataset.amount||0)||a.dataset.i-b.dataset.i
    : (a,b)=>a.dataset.i-b.dataset.i);
  rows.forEach(row=>rail.appendChild(row));
  rail.scrollTop=0;
  document.querySelectorAll('[data-feed-sort]').forEach(b=>
    b.classList.toggle('on',b.dataset.feedSort===order));
}

function wireFeedSort(root=document){
  root.querySelectorAll('[data-feed-sort]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>{
      try{ localStorage.setItem(FEED_SORT_KEY,button.dataset.feedSort); }catch(e){}
      sortFeed(button.dataset.feedSort);
    });
  });
  let saved=null;
  try{ saved=localStorage.getItem(FEED_SORT_KEY); }catch(e){}
  if(saved==='amount') sortFeed('amount');
}

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

// Figures written for the card, the Spanish way: 40,2M and 6,6.
const dec=(v,d=1)=> v==null||isNaN(v) ? '—' : Number(v).toFixed(d).replace('.',',').replace('-','−');
const mny=(v)=> v==null||isNaN(v) ? '—'
  : Math.abs(v)>=1e6 ? dec(v/1e6)+'M' : Math.abs(v)>=1e3 ? Math.round(v/1e3)+'K' : String(v);
const signed=(v,d=1)=> (v>=0?'+':'−')+dec(Math.abs(v),d);

function popWeeks(weeks){
  if(!weeks||!weeks.length) return '';
  const chips=weeks.map(w=>{
    const p=w.points;
    const cls=p==null?(w.forecast!=null?'fc':'na'):p<0?'rd':p>=8?'g':p>=4?'bl':'br';
    const shown=p==null?(w.forecast!=null?dec(w.forecast):'–'):p;
    const below=p!=null&&w.forecast!=null?`<small>${dec(w.forecast)}</small>`:'';
    const tip=`Jornada ${w.week}${w.rival?' · '+w.rival:''}${w.ideal?' · once ideal':''}`
      +(w.forecast!=null?' · previsto '+dec(w.forecast):'');
    return `<span class="pc-wk${w.ideal?' ideal':''}" title="${tip}"><b class="${cls}">${shown}</b>${below}<i>J${w.week}</i></span>`;
  }).join('');
  const legend=weeks.some(w=>w.forecast!=null)?' · en pequeño, lo previsto':'';
  return `<div class="pc-h">Puntos por jornada<span class="pc-h-note">${legend}</span></div><div class="pc-wks">${chips}</div>`;
}

// How much his value moved in the last seven days of the series, the same thing the
// projection beside it guesses forwards.
function lastWeekPct(history){
  const days=(history||[]).filter(h=>h.value!=null);
  if(days.length<8) return null;
  const now=days[days.length-1].value, then=days[days.length-8].value;
  return then ? (now/then-1)*100 : null;
}

// What the server stamped about the league in the page: the clause window and the hold rule.
const pageFacts=(()=>{
  const node=document.getElementById('page-facts');
  if(!node) return {};
  const d=node.dataset;
  return {windowOpen:d.windowOpen==='1'?true:d.windowOpen==='0'?false:null,
          opens:d.opens||'', closes:d.closes||'', holdExcept:d.holdExcept||''};
})();

function whenShort(stamp){
  const t=new Date(stamp);
  if(isNaN(t)) return '';
  const day=['dom','lun','mar','mié','jue','vie','sáb'][t.getDay()];
  const hm=String(t.getHours()).padStart(2,'0')+':'+String(t.getMinutes()).padStart(2,'0');
  const soon=t-Date.now()<6*86400000;
  return soon?`${day} ${hm}`:`${day} ${t.getDate()} ${['ene','feb','mar','abr','may','jun','jul','ago','sep','oct','nov','dic'][t.getMonth()]} ${hm}`;
}

// futbolfantasy's category for a player in his club, in the colour of their icon.
// The editors' note shows on hover or focus, and the role leads to the club's hierarchy page.
function roleChip(r){
  if(!r||!r.key) return '';
  const tip=r.note?` data-tip="${String(r.note).replace(/"/g,'&quot;')}"`:'';
  const inner=`<i class="rdot"></i>${r.label}`;
  return r.team_url
    ? `<a class="role role-${r.key}" href="${r.team_url}" target="_blank" rel="noopener"${tip}>${inner}</a>`
    : `<span class="role role-${r.key}"${tip}>${inner}</span>`;
}

function shieldMark(p){
  return p.shielded?` <span class="shield-mark" title="blindado${p.shielded_until?' hasta '+whenShort(p.shielded_until):''}">🛡</span>`:'';
}

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
  if(ui){ ui.mountPlayer(body,String(playerId),drawerFrom); return; }
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
  // The owner is a link: what the rival holds decides whether his clause is worth paying and
  // whether his offer is worth taking.
  const owner=p.is_mine ? 'tuyo'
    : (p.owner && p.owner_team_id
        ? `<button class="p-name" type="button" data-manager="${p.owner_team_id}">${p.owner}</button>`
        : (p.owner||'libre'));
  // The same tags as the rows: club, owner, role, starting odds, then the states that apply.
  const tags=`<span class="pl"><span class="pl-team">${crest(p.team_id)}${p.team_short||p.team||''}</span>`
    +`<span class="pl-owner">${owner}</span>${p.role?roleChip(p.role):''}`
    +`${p.start_probability!=null?`<span>${p.start_probability} %</span>`:''}${p.starred?'<span>★</span>':''}</span>`
    +(p.is_mine&&p.sale_locked&&p.hold_until?`<span class="tg tg-warn">🔒 hasta ${whenShort(p.hold_until)}</span>`:'')
    +(p.role&&p.role.change==='down'?`<span class="tg tg-warn">bajó a ${p.role.label}</span>`:'');
  const h=health(p), a=p.absence||{};
  const status=h?`<div class="pc-status ${h.ring}">${h.glyph==='card'?'':'✚ '}${
    [h.label,a.reason,a.since,a.until].filter(Boolean).join(' · ')}</div>`:'';
  const countdown=(stamp)=>`<span data-deadline="${stamp}" data-plain="1">${leftUntil(stamp)}</span>`;
  const past=lastWeekPct(data.history);
  const projected=p.projected_pct;
  const trend=past!=null?past:projected;
  const starts=p.start_probability;
  const xp=p.xpts||0;
  // Tiles by meaning, one row each: performance, money, availability, the season. A tile with
  // nothing to say is left out rather than drawn empty.
  const tile=(k,v,sm,cls,href)=>({k,v,sm,cls,href});
  const xpTile=tile('xPts / jornada', dec(p.xpts), p.rank?`score #${p.rank}`:'',
    xp>=6?'t-good':xp>=3.5?'t-info':xp>=2?'t-warn':'t-bad');
  const startsTile=starts!=null?tile('Titular', starts+' %',
    p.role?roleChip(p.role):p.hierarchy?p.hierarchy:(p.start_probability_source==='ficha'?`J${p.start_week||''} en su ficha`:''),
    starts>=75?'t-good':starts>=50?'t-warn':'t-bad'):null;
  const nextTile=p.next_rival?tile('Próximo', crest(p.next_rival_id)+p.next_rival, p.next_home?'en casa':'fuera'):null;
  const valueTile=tile('Valor', mny(p.value), l.market_id?`en venta por ${mny(l.min_bid)}`:'');
  const ceilingTile=p.is_mine?null:tile('Techo rentable', p.ideal_bid?mny(p.ideal_bid):'sin margen',
    p.ff_url?'↗ futbolfantasy':(p.ideal_bid?'futbolfantasy':''), 't-ceiling', p.ff_url);
  const clauseTile=p.clause?tile('Cláusula', mny(p.clause),
    p.value?`${dec(p.clause/p.value,2)}x su valor`:''):null;
  // Whether anybody can pay his clause right now, and if not, until when.
  let payableTile=null;
  if(p.clause){
    const shut=pageFacts.windowOpen===false||(pageFacts.closes&&new Date(pageFacts.closes)<=new Date());
    if(p.shielded&&p.shielded_until) payableTile=tile('Clausulable',`blindado hasta ${whenShort(p.shielded_until)}`,'','t-info');
    else if(p.clause_locked&&p.clause_locked_until) payableTile=tile('Clausulable',`se libera en ${countdown(p.clause_locked_until)}`,
      whenShort(p.clause_locked_until),'t-warn');
    else if(shut&&pageFacts.opens) payableTile=tile('Clausulable',`se abre ${whenShort(pageFacts.opens)}`,
      'ventana de cláusulas cerrada','t-warn');
    else payableTile=tile('Clausulable','pagable ya','','t-good');
  }
  const bidsTile=(l.kind==='libre'||l.expires)?tile('Pujas', l.bids||'ninguna',
    l.expires?'cierra '+String(l.expires).slice(11,16):''):null;
  let sellTile=null;
  if(p.is_mine){
    const rule=pageFacts.holdExcept?` title="excepción: ${pageFacts.holdExcept.replace(/"/g,'&quot;')}"`:'';
    sellTile=p.sale_locked&&p.hold_until
      ? tile('Puedes venderlo',`<span${rule}>🔒 en ${countdown(p.hold_until)}</span>`,'norma de la liga','t-warn')
      : tile('Puedes venderlo',`<span${rule}>ya</span>`,'','t-good');
  }
  const boughtTile=p.bought_at?tile('Fichado', since(p.bought_at), ''):null;
  const seasonTile=p.season_points!=null?tile('Puntos temporada', p.season_points,
    p.last_season_points?`25/26: ${p.last_season_points}`:''):null;
  const trendTile=trend!=null?tile('Valor 7d', `${signed(trend)} %`,
    past!=null&&projected!=null?`prevé ${signed(projected)} % en 7 días`:(past==null?'previsión':''),
    trend>=0?'t-good':'t-bad'):null;
  const rowsOfTiles=[
    [xpTile, startsTile, nextTile],
    [valueTile, ceilingTile, clauseTile],
    [payableTile, bidsTile, sellTile||boughtTile],
    [seasonTile, trendTile, sellTile?boughtTile:null],
  ];
  const drawTile=t=>{
    const inner=`<span>${t.k}</span><b>${t.v}</b>${t.sm?`<small>${t.sm}</small>`:''}`;
    return t.href
      ? `<a class="${t.cls||''} t-link" href="${t.href}" target="_blank" rel="noopener" title="Su ficha en futbolfantasy">${inner}</a>`
      : `<div class="${t.cls||''}">${inner}</div>`;
  };
  const grid=rowsOfTiles.map(group=>group.filter(Boolean)).filter(group=>group.length)
    .map(group=>`<div class="pc-grid">${group.map(drawTile).join('')}</div>`).join('');
  const actions=data.actions||[];
  const notes=actions.filter(x=>x.kind==='note'), buttons=actions.filter(x=>x.kind!=='note');
  // An offer's pair, always Aceptar then Rechazar: only the recommended one is filled, in the
  // accent colour whichever it is, and says why.
  const recommended=new Map();
  buttons.filter(x=>x.op==='accept_offer'&&x.why).forEach(x=>{
    const decline=buttons.find(y=>y.op==='decline_offer'&&y.offer_id===x.offer_id);
    if(x.take) recommended.set(x,{tone:'primary',why:x.why});
    else if(decline) recommended.set(decline,{tone:'primary',why:x.why});
  });
  // Buying, blue means recommended: he improves the eleven at a price within the ceiling.
  const BUY_OPS=['bid','buy_offer','direct_offer'];
  const primary=p.is_mine
    ? buttons.find(x=>!isDanger(x)&&x.op!=='always'&&!x.blocked&&x.op!=='accept_offer')
    : (data.recommended&&buttons.find(x=>BUY_OPS.includes(x.op)&&!x.blocked))
      ||buttons.find(x=>x.recommended&&!x.blocked);
  body.innerHTML=`
    ${drawerFrom?`<button class="drawer-back" type="button" data-back="${drawerFrom.id}"
      >← ${drawerFrom.label}</button>`:''}
    <div class="pc-head">${faceOf(p,'xl')}<div class="pc-who"><h3>${p.name}${shieldMark(p)}</h3>
      <div class="pc-sub"><span class="pos pos-${(p.position||'').toLowerCase().slice(0,3)}">${p.position}</span>
        <span class="tags">${tags}</span>
        <button class="cmp-add" type="button" data-cmp="${p.id}" data-cmp-name="${p.name}"
          data-cmp-pos="${p.position||''}">+ comparar</button></div></div></div>
    ${status}
    <div class="pc-tiles">${grid}</div>
    ${popWeeks(data.weeks||[])}
    ${(data.history||[]).filter(x=>x.value!=null).length>=3
      ?`<div class="pc-h">Valor · ${(data.history||[]).filter(x=>x.value!=null).length} días</div>`:''}
    ${sparkSvg(data.history||[])}
    ${actions.length?'<div class="pc-h">Acciones</div>':''}
    ${notes.map(actionButton).join('')}
    <div class="drawer-actions pc-acts">${buttons.map(x=>actionButton(x,x===primary,recommended.get(x))).join('')}</div>
    ${data.writes_enabled?'':'<p class="drawer-note">Servidor en modo solo lectura: '
      +'las operaciones estan desactivadas.</p>'}`;
  body.querySelectorAll('button[data-action]').forEach(button=>
    button.addEventListener('click',()=>runAction(JSON.parse(button.dataset.action),p)));
  wireAlways(body,p);
  wireChart(body);
  wireManagers(body);
  tick();
  drawTray();
}

// The panel's footer says in words what is about to happen: going from "no vende solo" to
// "vendo desde X" is exactly what needs to be seen confirmed.
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
  // Empty means do not sell on your own. It is the only way to say it, so the placeholder says
  // it in words instead of leaving a blank that looks like "no limit".
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

function isDanger(a){
  return !!a.danger||a.op==='decline_offer'||a.op==='withdraw';
}

function actionButton(a,primary=false,rec=null){
  if(a.kind==='note') return `<p class="pc-info">${a.label}${a.deadline
    ? ` · quedan <span data-deadline="${a.deadline}" data-plain="1">${leftUntil(a.deadline)}</span>` : ''}</p>`;
  if(rec) return `<button class="act act-${rec.tone}" type="button" title="recomendado: ${rec.why}" `
    +`data-action='${JSON.stringify(a).replace(/'/g,"&#39;")}'>${a.label}</button>`;
  const cls='act'+(isDanger(a)&&!/^(accept|decline)_offer$/.test(a.op)?' act-danger':primary?' act-primary':'')
    +((a.op==='always'||a.op==='raid')&&a.on?' on':'');
  const off=a.blocked?' disabled':'';
  const button=`<button class="${cls}" type="button" data-action='${JSON.stringify(a).replace(/'/g,"&#39;")}'${off}`
    +`${a.note?` title="${String(a.note).replace(/"/g,'&quot;')}"`:''}>`
    +`${a.label}${a.blocked?' — no te llega':''}</button>`;
  return a.op==='always'&&a.on ? button+alwaysPanel(a) : button;
}

async function runAction(a,player,from=null){
  if(a.op==='note') return;
  if(a.op==='raid'){
    raidDialog({id:player.id, name:player.name, suggested:a.suggested, clause:player.clause,
                opens:player.clause_locked_until},()=>openDetail(player.id));
    return;
  }
  if(a.op==='shield'){
    shieldDialog(a,player);
    return;
  }
  if(a.op==='cancel_shield'){
    if(!confirm('Cancelar el blindaje de '+player.name+' del '+stampText(a.at)+'?')) return;
    const res=await fetch('/api/shield/cancel',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({id:player.id,at:a.at})});
    if(!res.ok){ alert('No he podido cancelarlo.'); return; }
    openDetail(player.id);
    return;
  }
  if(a.op==='always'){
    // Paint before asking: the server confirms in milliseconds, but reloading the whole card
    // made the button look dead.
    const button=from||[...document.querySelectorAll('.drawer-actions button')]
      .find(b=>b.textContent.includes('mercado'));
    const label=on=>from?(on?'● Siempre en mercado':'Siempre en mercado')
                        :(on?'Quitar de siempre-en-mercado':'Siempre en mercado');
    const turningOn=!a.on;
    if(button){
      button.classList.toggle('on',turningOn);
      button.textContent=label(turningOn);
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
        button.textContent=label(!!data.always_listed);
      }
      a.on=!!data.always_listed;
      const existing=document.querySelector('.always-panel');
      if(from) return;
      if(a.on&&!existing&&button){
        button.insertAdjacentHTML('afterend',alwaysPanel(a));
        wireAlways(button.parentElement,player);
      }else if(!a.on&&existing){ existing.remove(); }
    }catch(e){
      if(button){ button.classList.toggle('on',a.on); button.textContent=label(a.on); }
      if(from) alert('No he podido cambiarlo: '+(e.message||e));
    }finally{ if(button) button.disabled=false; }
    return;
  }
  closeDrawer();
  if(a.kind==='amount'){
    openAmount(a,player);
  }else{
    confirmOp({op:a.op, name:player.name, player_id:a.player_id||player.id,
               market_id:a.market_id, offer_id:a.offer_id, amount:a.amount||null});
  }
}

// The amount modal: a bid, a sale or a clause raise. The drawer and the table buttons both ask
// for it, so it lives apart and is handed the player already resolved.
function openAmount(a,player){
  const raise=a.op==='raise_clause';
  pending={operation:a.op, market_id:a.market_id, player_id:a.player_id||player.id,
           player_team_id:a.player_team_id||player.player_team_id,
           offer_id:a.offer_id, bid_id:a.bid_id, name:player.name, min_bid:a.min||0,
           ideal:player.ideal_bid||0, value:player.value,
           raise, clause:+player.clause||0, safe:+a.safe_margin||0};
  modal.hidden=false;
  modal.querySelector('.bid-action').textContent=a.label+' —';
  modal.querySelector('.bid-who').textContent=player.name;
  // A suggested zero is left blank on purpose: the note below explains why.
  modal.querySelector('.bid-amount').value =
    raise && !a.suggested ? '' : group(a.suggested||a.min||0);
  modal.querySelector('#bid-amount-label').textContent=
    raise ? 'Importe a pagar (se descuenta de tu saldo)'
          : a.op==='pay_clause' ? 'Importe de la clausula (se descuenta de tu saldo)'
          : a.op==='sell_to_market' ? 'Precio de venta'
          : 'Importe de la puja';
  // The bid references say nothing about a clause, and futbolfantasy's ceiling is
  // sobre comprar al jugador, no sobre proteger al tuyo.
  modal.querySelector('.bid-refs').hidden=raise;
  modal.querySelector('#bid-clause').hidden=!raise;
  modal.querySelector('.bid-min').textContent=a.min?exact(a.min):'sin minimo';
  modal.querySelector('.bid-ideal').textContent=player.ideal_bid?exact(player.ideal_bid):'sin margen';
  modal.querySelector('.bid-value').textContent=exact(player.value);
  showRivals(+a.bids||0, a.expires);
  modal.querySelector('.bid-drop').hidden=true;
  showStep(1);
  modal.querySelector('.bid-error').textContent='';
  checkAmount();
}

// The button in the clause-raise table: the amount is already worked out, so it opens the modal
// with the figure filled in and the clause it would leave in plain sight.
function wireRaises(root=document){
  root.querySelectorAll('button.raise[data-raise]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>{
      const d=button.dataset;
      openAmount({op:'raise_clause', kind:'amount', label:'Subir cláusula',
                  player_id:d.raise, player_team_id:d.raiseSlot,
                  suggested:+d.raisePay||0},
                 {id:d.raise, name:d.raiseName, clause:+d.raiseClause||0,
                  value:+d.raiseClause||0, ideal_bid:0});
    });
  });
}

function scheduleRaid(dataset){
  raidDialog({id:dataset.raid, name:dataset.raidName, suggested:+dataset.raidMax||0,
              clause:+dataset.raidClause||0},()=>swap());
}

// The scheduled clausulazo asks for one number, the most you would pay; it is grouped as it is
// typed so a million and ten millions cannot be told apart by counting zeros.
function raidDialog(p,done){
  const box=document.getElementById('raid-modal');
  if(!box) return;
  const input=box.querySelector('#raid-max'), error=box.querySelector('.raid-error');
  box.querySelector('.raid-who').textContent=p.name;
  box.querySelector('.raid-facts').innerHTML=
    (p.clause?`<dt>Cláusula ahora</dt><dd><strong>${exact(p.clause)}</strong></dd>`:'')
    +(p.opens&&new Date(p.opens)>new Date()?`<dt>Se libera</dt><dd>${stampText(p.opens)}</dd>`:'')
    +(myCash!=null?`<dt>Tu saldo</dt><dd>${exact(myCash)}</dd>`:'')
    +(MODE!=='auto'?`<dt></dt><dd class="clause-open">este servidor está en modo ${MODE}: `
      +'lo guardará pero no lo pagará solo</dd>':'');
  input.value=group(p.suggested||p.clause||0);
  error.textContent='';
  box.hidden=false;
  input.focus(); input.select();

  const format=()=>{
    const caret=input.selectionStart, before=input.value.length;
    input.value=group(digits(input.value));
    const shift=input.value.length-before;
    input.setSelectionRange(Math.max(0,caret+shift),Math.max(0,caret+shift));
  };
  const close=()=>{ box.hidden=true; box.onclick=null; input.oninput=null;
    document.removeEventListener('keydown',onKey); };
  const save=async()=>{
    const max_pay=digits(input.value);
    if(!max_pay){ error.textContent='Escribe un importe.'; return; }
    if(p.clause&&max_pay<p.clause){ error.textContent='Por debajo de la cláusula actual: no se pagaría.'; return; }
    const res=await fetch('/api/raid',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({id:p.id,name:p.name,max_pay})});
    if(!res.ok){ error.textContent='No se ha podido programar.'; return; }
    close();
    if(done) done();
  };
  const onKey=(e)=>{ if(e.key==='Escape') close(); if(e.key==='Enter') save(); };
  document.addEventListener('keydown',onKey);
  input.oninput=format;
  box.onclick=(e)=>{
    if(e.target===box||e.target.closest('.raid-cancel')) close();
    else if(e.target.closest('.raid-save')) save();
  };
}

function wireRaids(root=document){
  root.querySelectorAll('button.raid-btn, .cal-chip[data-raid]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>scheduleRaid(button.dataset));
  });
  // Your own chips in the calendar are not raidable: they open the card.
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
// The tray lives in localStorage because the panel swaps itself out live, and losing a
// half-built comparison to a refresh would mean nobody used it.
const CMP_MAX=8, CMP_KEY='fantasy:compare';
let tray=[];
try{ tray=(JSON.parse(localStorage.getItem(CMP_KEY))||[]).slice(0,CMP_MAX); }catch(e){ tray=[]; }

const cmpSave=()=>{ try{ localStorage.setItem(CMP_KEY,JSON.stringify(tray)); }catch(e){} };
const cmpHas=(id)=> tray.some(p=>p.id===String(id));
function cmpAdd(id,name,pos){
  id=String(id);
  if(cmpHas(id)) return true;
  if(tray.length>=CMP_MAX){ trayMsg(`El comparador ya lleva ${CMP_MAX}`); return false; }
  tray.push({id,name:name||id,pos:pos||''});
  cmpSave(); drawTray();
  if(comparing()) renderCompare();
  return true;
}
function cmpDrop(id){
  tray=tray.filter(p=>p.id!==String(id));
  cmpSave(); drawTray();
  // Dropping one with the table in front of you has to drop him from the table, not just the bar.
  if(comparing()) renderCompare();
}

const comparing=()=> (document.querySelector('.tab.on')||{dataset:{}}).dataset.tab==='comparador';

function trayBox(){
  let box=document.getElementById('cmp-tray');
  if(box) return box;
  box=document.createElement('div');
  box.id='cmp-tray'; box.className='cmp-tray'; box.hidden=true;
  box.innerHTML=`<div class="cmp-find-wrap">
      <input class="cmp-find" type="search" autocomplete="off" spellcheck="false"
        placeholder="buscar jugador…" aria-label="Buscar jugador para comparar">
      <div class="cmp-results" hidden></div>
    </div>
    <div class="cmp-chips"></div>
    <span class="cmp-msg"></span>
    <div class="cmp-acts">
      <div class="cmp-mine-wrap">
        <button type="button" class="cmp-mine">Mi plantilla</button>
        <div class="cmp-results cmp-mine-list" hidden></div>
      </div>
      <button type="button" class="cmp-go primary"></button>
      <button type="button" class="cmp-close" title="Quitar todos" aria-label="Quitar todos">✕</button>
    </div>`;
  document.body.appendChild(box);
  box.querySelector('.cmp-go').addEventListener('click',openCompare);
  box.querySelector('.cmp-close').addEventListener('click',clearCompare);
  wireFind(box);
  wireMine(box);
  return box;
}

// The tray's search. It comes off the same request the comparator uses, so no new index is
// needed: the whole world is already in memory on the server.
function wireFind(box){
  const input=box.querySelector('.cmp-find'), list=box.querySelector('.cmp-results');
  let timer=null, found=[];
  const hide=()=>{ list.hidden=true; list.innerHTML=''; found=[]; };
  const paint=()=>{
    if(!found.length){ hide(); return; }
    list.innerHTML=found.map((p,i)=>`
      <button class="cmp-hit${i===0?' first':''}" type="button" data-cmp="${p.id}"
        data-cmp-name="${p.name}" data-cmp-pos="${p.position||''}">
        <span class="cmp-hit-who">
          ${p.image
            ? `<img src="${p.image}" alt="" loading="lazy" onerror="this.remove()">`
            : `<span class="crest crest-${p.team_id}"></span>`}
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
      const data=await res.json();
      found=(data.matches||[]).filter(p=>!cmpHas(p.id));
      if(!found.length){
        list.innerHTML='<p class="cmp-none">Nadie con ese nombre</p>';
        list.hidden=false;
        return;
      }
      paint();
    }catch(e){ hide(); }
  };
  input.addEventListener('input',()=>{ clearTimeout(timer); timer=setTimeout(run,180); });
  input.addEventListener('keydown',(event)=>{
    if(event.key==='Escape'){ collapse(); trigger&&trigger.focus(); }
    // Enter adds the first: three letters and Enter is the short way through.
    if(event.key==='Enter'&&found.length){
      event.preventDefault();
      const first=found[0];
      cmpAdd(first.id,first.name,first.position);
      input.value=''; hide();
    }
  });
  // Adding from the list closes it on its own: the document picks the click up.
  list.addEventListener('click',()=>{ input.value=''; setTimeout(hide,0); });
  document.addEventListener('click',(event)=>{
    if(!box.contains(event.target)) hide();
  });
}

function trayMsg(text){
  const line=trayBox().querySelector('.cmp-msg');
  line.textContent=text;
  setTimeout(()=>{ if(line.textContent===text) line.textContent=''; },4000);
}

// The tray's common line: comparing a forward with your keepers says nothing, so the shortcut
// to the squad is offered by position only when they all agree.
function trayLine(){
  const lines=[...new Set(tray.map(p=>p.pos).filter(Boolean))];
  return lines.length===1?lines[0]:'';
}

function drawTray(){
  dispatchEvent(new Event('panel:tray'));
  const box=trayBox();
  const visible=tray.length>0&&!comparing();
  box.hidden=!visible;
  document.body.classList.toggle('tray-on',visible);
  box.querySelector('.cmp-chips').innerHTML=tray.map(p=>
    `<span class="cmp-chip">${p.pos
        ? `<span class="pos pos-${p.pos.toLowerCase().slice(0,3)}">${p.pos}</span>`:''}
      <button class="p-name" type="button" data-detail="${p.id}">${p.name}</button>
      <button class="cmp-x" type="button" data-cmp-drop="${p.id}" aria-label="Quitar">&times;</button>
    </span>`).join('');
  const line=trayLine();
  const mineButton=box.querySelector('.cmp-mine');
  mineButton.title=line?`Meter tus ${line} para verlos al lado`:'Meter jugadores tuyos';
  const go=box.querySelector('.cmp-go');
  go.textContent=`Comparar (${tray.length})`;
  wireDetails(box);
  // The card's button has to say which state it is in: "+" invites, "✓" reminds.
  document.querySelectorAll('button[data-cmp]').forEach(button=>{
    const on=cmpHas(button.dataset.cmp), short=button.classList.contains('small');
    button.classList.toggle('on',on);
    button.textContent=on?(short?'✓':'✓ comparando'):(short?'+':'+ comparar');
    button.title=on?'Quitar del comparador':'Añadir al comparador';
  });
}

// The buttons are born in content that gets swapped out (cards, squads, the tray itself), so
// they are listened for on the document and nothing ever has to be rewired.
document.addEventListener('click',(event)=>{
  const add=event.target.closest('button[data-cmp]');
  if(add){
    event.stopPropagation();
    if(cmpHas(add.dataset.cmp)) cmpDrop(add.dataset.cmp);
    else cmpAdd(add.dataset.cmp,add.dataset.cmpName,add.dataset.cmpPos);
    return;
  }
  const drop=event.target.closest('button[data-cmp-drop]');
  if(drop){ event.stopPropagation(); cmpDrop(drop.dataset.cmpDrop); }
});

// Your squad as a menu: add whoever you want, or that whole line at once. Better than a button
// reading "+ mis MED" that decides for you.
function wireMine(box){
  const button=box.querySelector('.cmp-mine'), list=box.querySelector('.cmp-mine-list');
  let squad=null;
  const hide=()=>{ list.hidden=true; };
  const row=(p)=>`
    <button class="cmp-hit" type="button" data-cmp="${p.id}" data-cmp-name="${p.name}"
      data-cmp-pos="${p.position||''}">
      <span class="cmp-hit-who">
        ${p.image
          ? `<img src="${p.image}" alt="" loading="lazy" onerror="this.remove()">`
          : `<span class="crest crest-${p.team_id}"></span>`}
        <b>${p.name}</b>
        <span class="pos pos-${String(p.position||'').toLowerCase().slice(0,3)}">${p.position}</span>
      </span>
      <span class="cmp-hit-num">${(p.xpts||0).toFixed(2)} xPts · <b>${fmt(p.value)}</b></span>
    </button>`;
  const paint=()=>{
    const line=trayLine();
    const free=(squad||[]).filter(p=>!cmpHas(p.id));
    if(!free.length){ list.innerHTML='<p class="cmp-none">Ya estan todos</p>'; list.hidden=false; return; }
    const same=line?free.filter(p=>p.position===line):[];
    const rest=free.filter(p=>!same.includes(p));
    list.innerHTML=(same.length>1
        ? `<button class="cmp-hit cmp-all" type="button">Añadir mis ${same.length} ${line}</button>`
        : '')
      + (same.length?`<p class="cmp-group">Tus ${line}</p>`+same.map(row).join(''):'')
      + (rest.length?`<p class="cmp-group">${same.length?'El resto':'Tu plantilla'}</p>`
          +rest.map(row).join(''):'');
    list.hidden=false;
    const all=list.querySelector('.cmp-all');
    if(all) all.addEventListener('click',()=>{
      for(const p of same){ if(!cmpAdd(p.id,p.name,p.position)) break; }
      hide();
      if(tray.length>1&&!comparing()) openCompare();
    });
  };
  button.addEventListener('click',async()=>{
    if(!list.hidden){ hide(); return; }
    if(!squad){
      try{
        const res=await fetch('/api/compare');
        if(!res.ok) throw new Error(res.status);
        squad=(await res.json()).mine||[];
      }catch(e){ trayMsg('No he podido leer tu plantilla'); return; }
    }
    paint();
  });
  list.addEventListener('click',(event)=>{ if(event.target.closest('button[data-cmp]')) hide(); });
  document.addEventListener('click',(event)=>{
    if(!box.querySelector('.cmp-mine-wrap').contains(event.target)) hide();
  });
}

const CMP_ROWS=[
  {label:'Valor', get:p=>p.value, fmt:v=>exact(v), best:'min', cost:true},
  {label:'Clausula', get:p=>p.clause, fmt:v=>v?exact(v):'—', best:'min', cost:true},
  {label:'Techo rentable', get:p=>p.ideal_bid, fmt:v=>v?exact(v):'sin margen', best:'max'},
  {label:'xPts por jornada', get:p=>p.xpts, fmt:v=>(v||0).toFixed(2), best:'max'},
  {label:'Pts por millon', get:p=>p.points_value, fmt:v=>(v||0).toFixed(3), best:'max'},
  {label:'Score', get:p=>p.score, fmt:v=>((v||0)>=0?'+':'')+(v||0).toFixed(2), best:'max'},
  {label:'Titularidad', get:p=>p.start_probability, fmt:v=>v==null?'—':v+'%', best:'max'},
  {label:'Puntos temporada', get:p=>p.season_points, fmt:v=>Math.round(v||0), best:'max'},
  {label:'Media', get:p=>p.season_avg, fmt:v=>(v||0).toFixed(1), best:'max'},
  {label:'Puntos 25/26', get:p=>p.last_season_points, fmt:v=>Math.round(v||0), best:'max'},
  {label:'Valor 7d', get:p=>p.projected_pct,
    fmt:v=>((v||0)>=0?'+':'')+(v||0).toFixed(2)+'%', best:'max'},
  {label:'Proximo rival', get:p=>p.next_rival,
    fmt:(v,p)=>v?`${crest(p.next_rival_id)}${v} · ${p.next_home?'en casa':'fuera'}`:'—', text:true},
];

function cmpChips(p){
  const listing=p.market||{};
  const chips=[];
  if(listing.market_id) chips.push(`<span class="chip">en venta ${fmt(listing.min_bid)}</span>`);
  if(p.shielded) chips.push(p.shielded_until
    ? `<span class="chip chip-warn">blindado <span
        data-deadline="${p.shielded_until}">${leftUntil(p.shielded_until)}</span></span>`
    : '<span class="chip chip-warn">blindado</span>');
  else if(p.clause_locked&&p.clause_locked_until)
    chips.push(`<span class="chip chip-warn">clausula en <span
      data-deadline="${p.clause_locked_until}">…</span></span>`);
  else if(p.clause&&!p.is_mine) chips.push('<span class="chip chip-good">clausula pagable</span>');
  if(p.sale_locked) chips.push('<span class="chip chip-warn">🔒 recien fichado</span>');
  if(!p.available) chips.push('<span class="chip chip-bad">no puntua</span>');
  return chips.join('');
}

// A table decides nothing on its own: this line says whether the one you are looking at
// improves on what you have, which is the only reason to be comparing.
function cmpVerdict(list){
  const outside=list.filter(p=>!p.is_mine), ours=list.filter(p=>p.is_mine);
  const by=(arr,key)=>arr.slice().sort((a,b)=>(b[key]||0)-(a[key]||0));
  if(outside.length!==1||!ours.length){
    const top=by(list,'xpts')[0], eff=by(list,'points_value')[0];
    if(!top) return '';
    return `<p class="cmp-verdict">Mas xPts: <b>${top.name}</b> (${(top.xpts||0).toFixed(2)}).
      Mas puntos por millon: <b>${eff.name}</b> (${(eff.points_value||0).toFixed(3)}).</p>`;
  }
  const him=outside[0], line=him.position||'esa posicion';
  const ranked=by(ours,'xpts'), best=ranked[0], worst=ranked[ranked.length-1];
  const gap=(a,b)=>((a.xpts||0)-(b.xpts||0));
  const money=(a,b)=>{
    const diff=(a.value||0)-(b.value||0);
    return diff===0?'y cuesta lo mismo'
      : `y cuesta ${fmt(Math.abs(Math.round(diff)))} ${diff>0?'mas':'menos'}`;
  };
  // Mejorar a uno sancionado no es merito: decirlo evita leer el veredicto al reves.
  const why=(p)=> p.available===false?' (que ahora no puntua)':'';
  let verdict;
  if(gap(him,best)>0){
    verdict = `<b>${him.name}</b> mejora a tu mejor ${line}: <b>+${gap(him,best).toFixed(2)}
      xPts</b> sobre ${best.name} ${money(him,best)}.`;
  }else if(gap(him,worst)>0){
    verdict = `<b>${him.name}</b> no llega a ${best.name}, pero si mejora a
      <b>${worst.name}</b>${why(worst)}: +${gap(him,worst).toFixed(2)} xPts ${money(him,worst)}.`;
  }else{
    verdict = `<b>${him.name}</b> no mejora a ninguno de tus ${line}:
      ${worst.name} ya le saca ${Math.abs(gap(him,worst)).toFixed(2)} xPts.`;
  }
  return `<p class="cmp-verdict">${verdict}</p>`;
}

function panelWide(on){
  const panel=drawer&&drawer.querySelector('.drawer-panel');
  if(panel) panel.classList.toggle('wide',!!on);
  // Only the player's card is a centred popup; every other view keeps the side drawer.
  if(drawer) drawer.classList.remove('as-pop');
}

// The comparator is a tab of its own; its address carries who is in it, so a link reopens
// the same comparison.
function compareHash(){
  return '#comparador'+(tray.length?'/'+tray.map(p=>p.id).join(','):'');
}

function adoptCompareIds(list){
  const ids=String(list).split(',').map(x=>x.trim()).filter(Boolean).slice(0,CMP_MAX);
  if(ids.join(',')===tray.map(p=>p.id).join(',')) return;
  tray=ids.map(id=>tray.find(p=>p.id===id)||{id,name:id,pos:''});
  cmpSave(); drawTray();
}

function openCompare({replace=false}={}){
  if(drawer&&!drawer.hidden) shutDrawer();
  if(replace) history.replaceState(null,'',compareHash());
  else history.pushState(null,'',compareHash());
  routed=location.hash;
  showTab('comparador',{updateHash:false});
}

let compareRun=0;
async function renderCompare(){
  const section=document.getElementById('comparador');
  if(!section) return;
  wireCompareSection(section);
  const body=section.querySelector('.cmp-body');
  // Only the bare tab address follows the tray: with a card open on top it is that card's.
  const at=hashParts();
  if(at.base==='comparador'&&!VIEWS[at.view]&&location.hash!==compareHash()){
    history.replaceState(history.state,'',compareHash());
    routed=location.hash;
  }
  if(!tray.length){
    body.innerHTML=`<p class="empty">Busca jugadores arriba y ve añadiéndolos, pulsa
      <b>Mi plantilla</b> para meter a los tuyos, o usa el <b>+ comparar</b> de cada ficha.</p>`;
    return;
  }
  const run=++compareRun;
  if(!body.querySelector('.cmp-view')) body.innerHTML='<p class="empty">Comparando…</p>';
  let data;
  try{
    const res=await fetch('/api/compare?ids='+tray.map(p=>p.id).join(','));
    if(!res.ok) throw new Error(res.status);
    data=await res.json();
  }catch(e){
    if(run===compareRun) body.innerHTML='<p class="empty">No he podido comparar.</p>';
    return;
  }
  if(run!==compareRun) return;
  const list=data.players||[];
  if(!list.length){ body.innerHTML='<p class="empty">No conozco a ninguno de esos.</p>'; return; }
  // Ids that came in an address have no name yet: the answer brings them.
  let named=false;
  tray.forEach(t=>{ const p=list.find(x=>String(x.id)===t.id);
    if(p&&(t.name!==p.name||!t.pos)){ t.name=p.name; t.pos=p.position||''; named=true; } });
  if(named){ cmpSave(); drawTray(); }
  const head=list.map(p=>`<th>
    <div class="cmp-who">
      ${faceOf(p,'md')}
      <span class="cmp-name">
        <button class="p-name" type="button" data-detail="${p.id}">${p.name}</button>
        <button class="cmp-x" type="button" data-cmp-drop="${p.id}" aria-label="Quitar">&times;</button>
      </span>
      <span class="cmp-sub"><span class="pos pos-${String(p.position||'').toLowerCase().slice(0,3)}"
        >${p.position}</span> ${crest(p.team_id)}${p.team_short||p.team||''} ·
        ${p.is_mine?'tuyo':(p.owner&&p.owner_team_id
          ? `<button class="p-name" type="button" data-manager="${p.owner_team_id}">${p.owner}</button>`
          : (p.owner||'libre'))}</span>
    </div></th>`).join('');
  const rows=CMP_ROWS.map(row=>{
    const values=list.map(row.get);
    let target=null;
    if(!row.text){
      const numbers=values.filter(v=>v!=null&&isFinite(v)&&v!==0);
      if(numbers.length>1)
        target=row.best==='min'?Math.min(...numbers):Math.max(...numbers);
      // All equal teaches nothing: highlighting there only stains the table.
      if(numbers.length&&numbers.every(v=>v===numbers[0])) target=null;
    }
    const cells=list.map((p,i)=>{
      const value=values[i];
      const win=target!=null&&value===target;
      const css=win?(row.cost?'cmp-cheap':'cmp-best'):'';
      return `<td class="${css}">${row.fmt(value,p)}</td>`;
    }).join('');
    return `<tr><td>${row.label}</td>${cells}</tr>`;
  }).join('');
  body.innerHTML=`
    <div class="cmp-view">
    <p class="note">${list.length} jugadores · lo mejor de cada fila en verde</p>
    ${cmpVerdict(list)}
    <div class="cmp-wrap"><table class="cmp">
      <thead><tr><th></th>${head}</tr></thead>
      <tbody>${rows}
        <tr><td>Estado</td>${list.map(p=>
          `<td><span class="cmp-state">${cmpChips(p)||'<span class="cmp-quiet">sin nada</span>'}</span></td>`
          ).join('')}</tr>
      </tbody>
    </table></div>
    <p class="drawer-note">Valor y cláusula en verde marcan el más barato, no el mejor.
      Pulsa un nombre para su ficha.</p>
    </div>`;
  wireDetails(body); wireManagers(body); tick();
}


function clearCompare(){
  tray=[]; cmpSave(); drawTray();
  if(comparing()) renderCompare();
}

function wireCompareSection(section){
  if(section.dataset.wired) return;
  section.dataset.wired='1';
  wireFind(section);
  wireMine(section);
  section.querySelector('.cmp-clear').addEventListener('click',clearCompare);
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

// A row's button that is one of the player's own actions runs it exactly as his card would.
document.addEventListener('click',async(event)=>{
  const button=event.target.closest&&event.target.closest('button[data-act]');
  if(!button) return;
  button.disabled=true;
  try{
    const res=await fetch('/api/player/'+button.dataset.actPlayer);
    if(!res.ok) throw new Error(res.status);
    const data=await res.json();
    const action=(data.actions||[]).find(a=>a.op===button.dataset.act);
    if(!action){ alert('Ahora mismo no se puede: abre su ficha para ver por qué.'); return; }
    await runAction(action,data.player,action.op==='always'?button:null);
  }catch(e){
    alert('Solo disponible en la versión servida.');
  }finally{ button.disabled=false; }
});

// ---- Partidos: my points per matchday, and each matchday's whole table -------------
let seasonCache=null;
async function fillHistory(){
  const list=document.getElementById('pv-history');
  if(!list) return;
  const week=+list.dataset.week, plannedHere=+list.dataset.planned||0;
  try{
    if(!seasonCache){
      const [season,forecast]=await Promise.all([
        fetch('/api/season').then(r=>r.ok?r.json():null),
        fetch('/api/forecast/'+week).then(r=>r.ok?r.json():null)]);
      seasonCache={season,forecast};
    }
  }catch(e){ seasonCache={season:null,forecast:null}; }
  const target=document.getElementById('pv-history');
  if(!target) return;
  const {season,forecast}=seasonCache;
  const me=((season||{}).managers||[]).find(m=>m.is_me);
  const weeks=(season||{}).weeks||[];
  const planned=((forecast||{}).mine||{}).planned||plannedHere;
  const real=weeks.map((w,i)=>({w, pts:me?me.points[i]:null, rank:me&&me.week_rank?me.week_rank[i]:null}));
  const scale=Math.max(planned,...real.map(r=>r.pts||0))*1.05||1;
  const rows=real.map(r=>r.pts==null
    ? `<li class="hist"><span class="hj">J${r.w}</span><span class="bars"></span><span class="hv">—</span></li>`
    : `<li class="hist" data-week="${r.w}" tabindex="0"><span class="hj">J${r.w}</span><span class="bars">`
      +`<span class="bar real" style="width:${Math.max(r.pts,0)/scale*100}%"></span></span>`
      +`<span class="hv">${r.pts}<i>${r.rank?r.rank+'º':''}</i></span></li>`);
  rows.push(`<li class="hist now" data-week="${week}" tabindex="0"><span class="hj">J${week}</span><span class="bars">`
    +`<span class="bar fore" style="width:${planned/scale*100}%"></span></span>`
    +`<span class="hv">${dec(planned)}<i>prev.</i></span></li>`);
  target.innerHTML=rows.join('');
  const sub=document.getElementById('pv-sub');
  const total=real.reduce((sum,r)=>sum+(r.pts||0),0), played=real.filter(r=>r.pts!=null).length;
  if(sub) sub.textContent=played?`${total} pts en ${played} jornadas`:'';
}

const XI_LINES=[['striker','DEL'],['midfield','MED'],['defender','DEF'],['goalkeeper','POR']];
function ptsClass(p){ return p==null?'':p>=8?'x-hi':p>=4?'x-mid':p<0?'x-bad':'x-lo'; }
function fcClass(v){ return v>=6?'x-hi':v>=3.5?'x-mid':v>=2?'x-lo':'x-bad'; }

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
  if(ui){ ui.mountDrawer(body,'week',{week:String(week)}); return; }
  let md=null, fc=null;
  try{
    [md,fc]=await Promise.all([
      fetch('/api/matchday/'+week).then(r=>r.ok?r.json():null),
      fetch('/api/forecast/'+week).then(r=>r.ok?r.json():null)]);
  }catch(e){}
  if(!md||!md.managers){ body.innerHTML='<p class="empty">No he podido leer esa jornada.</p>'; return; }
  const current=+((document.getElementById('pv-history')||{dataset:{}}).dataset.week||0);
  const future=current&&+week>=current;
  const forecasts={}, plans={};
  [...((fc||{}).managers||[]), ...((fc||{}).mine?[fc.mine]:[])].forEach(m=>{
    plans[m.team_id]=m.planned;
    (m.players||[]).forEach(p=>{ forecasts[p.id]=p.forecast; });
  });
  const score=m=>future?(plans[m.team_id]||0):(m.week_points||0);
  const managers=md.managers.slice().sort((a,b)=>score(b)-score(a));
  const rows=managers.map((m,i)=>{
    const lineup=m.lineup||{};
    const lines=XI_LINES.map(([key,label])=>{
      let players=(lineup[key]||[]).filter(Boolean);
      if(!m.lineup) players=(m.squad||[]).filter(p=>({1:'POR',2:'DEF',3:'MED',4:'DEL'})[p.position_id]===label);
      const chips=players.map(p=>{
        const f=forecasts[p.id];
        const value=future?(f!=null?dec(f):'–'):(p.points??'–');
        const cls=future?fcClass(f||0):ptsClass(p.points);
        const small=!future&&f!=null?` <span class="fcs">${dec(f)}</span>`:'';
        return `<span class="tchip ${cls}" data-pid="${p.id}">${faceOf(p,'xs')}`
          +`<span class="tname">${p.name}</span><span class="tx">${value}</span>${small}</span>`;
      }).join('');
      return chips?`<div class="line"><span class="pos pos-${label.toLowerCase()}">${label}</span><div class="chips">${chips}</div></div>`:'';
    }).join('');
    const total=future?`${dec(score(m))} <span class="mf">previsto</span>`:`${m.week_points??'–'} <span class="mf">pts</span>`;
    return `<details class="md-row${m.is_me?' me':''}"${m.is_me?' open':''}><summary><span class="rk">${i+1}º</span>`
      +`<span>${m.manager}</span><span class="mp">${total}</span></summary><div class="md-xi">${lines}</div></details>`;
  }).join('');
  const k=md.kickoff?new Date(md.kickoff):null;
  const when=k?['dom','lun','mar','mié','jue','vie','sáb'][k.getDay()]+' '+k.getDate()+' '
    +['ene','feb','mar','abr','may','jun','jul','ago','sep','oct','nov','dic'][k.getMonth()]:'';
  body.innerHTML=`<h3 class="pc-title">J${week}${future?' · previsión de cada once':when?' · '+when:''}</h3>`
    +`<p class="drawer-note" style="margin:0 0 8px">Toca un manager para ver su once${future?'':'; en pequeño, lo previsto si se guardó'}.</p>${rows}`
    +`<p class="drawer-note"><button class="j-squads" type="button" data-matchday="${week}">plantillas</button>`
    +`${fc&&!future?` <button class="j-squads" type="button" data-forecast="${week}">previsión</button>`:''}</p>`;
  wireMatchdays(body);
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
  usage.tab(tab.id);
  applyFilters();
  if(updateHash){
    // replaceState, not assignment: we want neither a history entry per click nor
    // disparar hashchange sobre nosotros mismos.
    history.replaceState(null,'','#'+(section||tab.id));
  }
  applyRivalPick();
  if(tab.sections.includes('once') && !pitchState) loadPitch();
  if(tab.sections.includes('evolucion')) loadSeason();
  if(tab.id==='comparador'&&was!=='comparador') renderCompare();
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
  if(kpi){ kpi.textContent=mny(amount); kpi.title='Tu saldo ahora mismo: '+exact(amount); }
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
    if((drawer&&!drawer.hidden)||(modal&&!modal.hidden)) return;
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
    if(node&&node.dataset.view) return;
    if(node && node.innerHTML!==inner) node.innerHTML=inner;
  });
  wireTables(); wireFilters(); wireStars(); wireBids(); wireOps();
  wireDetails(); wireRaids();
  wireRaises();
  wireManagers(); wireMatchdays(); wireFeedSort(); tick();
  // The chart is the client's, and the rebuild has just put the empty frame back in its place.
  if(seasonData) loadSeason();
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
window.panel={openDetail, openManager, openWeek, openReach, applyRivalPick, openMatchday, openForecast, closeDrawer, openAmount, openBid, shieldDialog, raidDialog,
  flash, cmpHas, cmpAdd, cmpDrop, usage,
  goto:(where)=>{ const target=resolveTarget(where);
    if(target) showTab(target.tab,{section:target.section}); else showTab(where); }};

wireTables(); wireFilters(); wireStars(); wireBids(); wireOps();
wireDetails(); wireRaids();
wireRaises(); wireManagers(); wireMatchdays(); wireFeedSort();
wireTabs(); tick(); drawTray();
{
  const stamped=document.querySelector('.topbar[data-cash]');
  if(stamped) myCash=+stamped.dataset.cash;
  liveTip();
}
if(window.EventSource && location.protocol.startsWith('http')) connect();

// ---- legacy (fichero estatico) ----
