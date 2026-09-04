
// ---- estado de filtros, para que sobreviva a un recambio de seccion --------
const filterState = {pos:'all', price:'', text:''};

function wireTables(root=document){
  root.querySelectorAll('table.sortable').forEach(table=>{
    if(table.dataset.wired) return;
    table.dataset.wired='1';
    table.querySelectorAll('th').forEach((th,index)=>{
      th.addEventListener('click',()=>{
        const body=table.tBodies[0], rows=[...body.rows];
        // Los de la jornada tambien: la clave de orden que traen es un numero, y sin estar
        // aqui se ordenarian como texto (48 delante de 9).
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
    // Sin sus controles no es una barra de filtros: una excepcion aqui se lleva por delante
    // el resto del arranque (el resto de cables y la conexion en vivo), y la pagina entera
    // se queda muerta sin poder pulsar nada.
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
  // El modal se reutiliza, asi que lo que escondio una subida de clausula hay que devolverlo.
  modal.querySelector('.bid-refs').hidden=false;
  modal.querySelector('#bid-clause').hidden=true;
  modal.querySelector('#bid-amount-label').textContent='Importe de la puja';
  // Con una puja puesta la operacion es cambiarla: la API rechaza una segunda con un 400.
  const existing=data.bid||null;
  // La operacion la manda el boton: por el mercado libre se puja y por la venta de un rival se
  // oferta, y la API contesta 404 a la equivocada.
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
  // Una de esas pujas puede ser la tuya, y contarla como rival es contar mal: se dice.
  const mine = pending && pending.bid_id ? 1 : 0;
  const others = Math.max(0, count - mine);
  node.textContent = !count ? 'ninguna'
    : mine ? (others ? `${count} · ${others} de rivales y la tuya` : 'solo la tuya')
           : String(count);
  node.className = 'bid-rivals'+(others?' rivals-on':'');
}

// Lo que se multiplica es la subida, no lo que pagas: pagas 8.555 y la clausula gana 17.110.
// Vive aqui porque es la unica operacion en la que el importe que escribes no es lo que cambia.
const CLAUSE_FACTOR=2;

function clauseSums(amount){
  const box=modal.querySelector('#bid-clause');
  if(!box) return;
  const rise=amount*CLAUSE_FACTOR;
  const next=(pending.clause||0)+rise;
  const times=pending.value ? next/pending.value : 0;
  const safe=pending.safe||0;
  // La misma linea que usa el consejo, dicha aqui mientras escribes: por encima de ella pagar
  // la clausula es mal negocio para quien la paga, y eso es toda la defensa que compra.
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
  // Subir una clausula no tiene puja minima ni techo de futbolfantasy: ese techo es lo que
  // renta pagar *por el jugador*, y aqui no se compra a nadie. Decia "no le ve rentabilidad".
  if(pending.raise){
    clauseSums(amount);
    if(!amount){
      const now=pending.value ? (pending.clause||0)/pending.value : 0;
      // Sugerir cero es una respuesta, no un hueco: la clausula ya esta donde tiene que estar.
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
      const movesCash=['bid','modify_bid','buy_offer','direct_offer','pay_clause',
                       'accept_offer','raise_clause'].includes(op);
      modal.querySelector('.bid-summary').innerHTML =
        `<dl class="bid-dl">
           <dt>Jugador</dt><dd>${data.player_name||pending.name}</dd>
           <dt>${AMOUNT_LABEL[op]||'Importe'}</dt>
             <dd><strong>${exact(data.amount)}</strong></dd>
           ${data.new_clause?`<dt>Clausula</dt>
             <dd>${exact(data.clause)} → <strong>${exact(data.new_clause)}</strong></dd>`:''}
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
      const done=DONE_LABEL[pending&&pending.operation]||'Hecho';
      modal.querySelector('.bid-summary').innerHTML =
        `<p class="bid-ok">${done}${data.dry_run?' (simulacro)':''}.</p>`;
      modal.querySelector('.bid-confirm').hidden=true;
      modal.querySelector('.bid-cancel').textContent='Cerrar';
      // El servidor ya lo ha hecho: la fila se va y el aviso sale ahora, sin esperar a que
      // se reconstruya el mundo. Cuando llegue el refresco, la tabla ya coincide.
      // En simulacro no se ha movido nada: el resumen se queda para poder leerlo.
      if(!data.dry_run){ settled(pending,done); closeModal(); }
    }catch(err){
      modal.querySelector('.bid-error').textContent=err.message;
    }finally{
      button.disabled=false; button.textContent='Aceptar';
    }
  });
}

// Operaciones que terminan con la fila: la oferta aceptada o rechazada ya no existe, la puja
// retirada tampoco. Poner en venta o pujar no borran nada, asi que ahi solo sale el aviso.
const OP_ENDS_ROW=new Set(['accept_offer','decline_offer','withdraw','cancel_bid',
                           'cancel_offer','pay_clause']);

function settled(op,label){
  if(!op) return;
  if(OP_ENDS_ROW.has(op.operation)){
    // La fila se busca por lo que la identifica, de lo mas concreto a lo menos: una oferta
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

// El mismo aviso que manda el servidor cuando algo se mueve, pero dicho aqui y al instante.
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
// Cada operacion se llama por su nombre en el boton final y en el resumen: "Pujas" y
// "Saldo si ganas" no significan nada cuando lo que haces es vender.
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
                 shield_player:'Blindar 24h a'};

function wireOps(root=document){
  root.querySelectorAll('button.op').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click', async ()=>{
      const d=button.dataset;
      // Cancelar un clausulazo no es una operacion contra LaLiga: es borrar una instruccion
      // nuestra, asi que no pasa por la confirmacion de dos pasos, que existe para el dinero.
      // Quitar una instruccion permanente tampoco gasta: deja de hacerlo.
      if(d.op==='drop_always'){
        if(!confirm('Quitar '+d.opName+' de siempre-en-mercado?')) return;
        try{
          const res=await fetch('/api/always',{method:'POST',
            headers:{'Content-Type':'application/json'},
            body:JSON.stringify({id:d.opPlayer,name:d.opName})});
          const data=await res.json();
          if(!res.ok) throw new Error(data.error||res.status);
          if(data.always_listed){
            // El toggle lo habria vuelto a poner: lo dejamos como estaba y lo decimos.
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
  const bits=[s.label];
  if(a.reason) bits.push(a.reason); else bits.push('sin detalle en futbolfantasy');
  if(a.since) bits.push(a.since);
  if(a.until) bits.push(a.until);
  // The floating tooltip, not a child of the badge: on a card in the top line the nested one
  // hung outside the pitch and read as an empty input box.
  const tip=bits.join(' \u00b7 ').replace(/"/g,'&quot;');
  return `<span class="badge-status ${s.cls}" data-tip="${tip}">${s.icon}</span>`;
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
  if(!player) return `<div class="slot empty gap" data-line="${line}" data-index="${index}"
    title="No tienes con quien cubrir esta plaza">⚠<br>${LINE_LABEL[line]}<br>sin cubrir</div>`;
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
    ${faceHtml(player)}
    <span class="pos pos-${(LINE_LABEL[Object.keys(LINE_POS).find(k=>LINE_POS[k]===player.position_id)]||'ENT').toLowerCase()}">${
      {1:'POR',2:'DEF',3:'MED',4:'DEL'}[player.position_id]||'ENT'}</span>
    <span class="bench-name">${player.name}</span>
    ${statusBadge(player)}
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
  pitchAlert();
  wireDrag();
}

const LINE_WORD={goalkeeper:['portero','porteros'],defender:['defensa','defensas'],
                 midfield:['medio','medios'],striker:['delantero','delanteros']};

// Un hueco en el once son puntos que no se juegan, asi que se dice arriba y con la salida
// puesta: la formacion que si cuadra con los que pueden jugar, si alguna cuadra.
//
// Contar camisetas no vale. Once camisetas con un sancionado dentro son diez jugadores y una
// plaza con nombre, y este aviso ofrecia el cambio de formacion que las junta como si eso
// arreglara algo.
const cannotPlay=p=>{
  if(!p) return false;
  if(p.available===false) return true;
  const s=statusOf(p);
  return !!s&&s.cls!=='st-duda';
};

function pitchAlert(){
  const box=document.getElementById('pitch-alert');
  if(!box) return;
  const holes=[]; let missing=0; const idle=[];
  LINE_ORDER.forEach(line=>{
    const slots=pitchState.lines[line]||[];
    slots.forEach(p=>{ if(cannotPlay(p)) idle.push(p); });
    const empty=slots.filter(p=>!p).length;
    if(!empty) return;
    missing+=empty;
    holes.push(`${empty} ${LINE_WORD[line][empty>1?1:0]}`);
  });
  // Ni huecos ni nadie ahi puesto para nada: no hay nada que decir.
  if(!missing&&!idle.length){ box.hidden=true; box.innerHTML=''; return; }

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
  box.innerHTML=out;
  box.hidden=false;
  const button=box.querySelector('button[data-formation]');
  if(button) button.addEventListener('click',()=>applyFormation(button.dataset.formation));
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
  // El selector tambien se cambia desde el aviso, y tiene que quedar diciendo la verdad.
  const select=document.getElementById('pitch-formation-select');
  if(select) select.value=text;
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
// El modo lo pinta el servidor en la cabecera: leerlo de ahi evita una peticion y evita que el
// aviso prometa algo que este servidor no hace.
const MODE=(document.querySelector('.mode b')||{}).textContent||'manual';

function closeDrawer(){
  if(!drawer) return;
  // Cerrar el comparador es haber terminado de comparar: la barra de abajo se va con el.
  const wasComparing=!!drawer.querySelector('.cmp-view');
  drawer.hidden=true;
  panelWide(false);
  if(wasComparing&&typeof drawTray==='function'){ tray=[]; cmpSave(); drawTray(); }
}

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

// El dia y la hora de una marca ISO, en local y sin año: es como se teclea y como se lee.
function stampText(stamp){
  const when=new Date(stamp);
  if(isNaN(when.getTime())) return '';
  const pad=n=>String(n).padStart(2,'0');
  return `${pad(when.getDate())}/${pad(when.getMonth()+1)} ${pad(when.getHours())}:${pad(when.getMinutes())}`;
}

// Lo que se teclea en el prompt: "21:30" es la proxima vez que sean las 21:30, y "07/09 21:30"
// ese momento del año en curso. Sale en ISO, que es lo que el servidor entiende.
function stampFrom(text,fallback){
  const value=(text||'').trim();
  if(!value) return fallback||'';
  const now=new Date();
  let match=/^(\d{1,2})[\/-](\d{1,2})[ ,]+(\d{1,2}):(\d{2})$/.exec(value);
  if(match){
    const when=new Date(now.getFullYear(),+match[2]-1,+match[1],+match[3],+match[4]);
    if(when<now) when.setFullYear(now.getFullYear()+1);
    return when.toISOString();
  }
  match=/^(\d{1,2}):(\d{2})$/.exec(value);
  if(match){
    const when=new Date(now);
    when.setSeconds(0,0);
    when.setHours(+match[1],+match[2]);
    if(when<=now) when.setDate(when.getDate()+1);
    return when.toISOString();
  }
  const parsed=new Date(value);
  return isNaN(parsed.getTime()) ? '' : parsed.toISOString();
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

// Las cuatro lineas de un equipo, en el orden en que se lee un campo: de atras hacia adelante.
const LINES=[{id:1,label:'POR'},{id:2,label:'DEF'},{id:3,label:'MED'},{id:4,label:'DEL'}];

// Una plantilla se lee por lineas, no como una lista: agrupada asi se ve de un vistazo si a
// alguien le falta un defensa o le sobran delanteros.
function byLine(squad){
  return LINES.map(line=>({
    ...line,
    players:(squad||[]).filter(p=>Number(p.position_id)===line.id)
  })).filter(line=>line.players.length);
}

// La cara del jugador en pequeño, con el escudo detras si no hay foto.
function chipFace(p){
  return p.image
    ? `<img class="chip-face" src="${p.image}" alt="" loading="lazy" onerror="this.remove()">`
    : `<span class="crest crest-${p.team_id}"></span>`;
}

// El campo en pequeño: las mismas cuatro lineas del once, de delantera a porteria, porque una
// plantilla se reconoce por su forma antes que por sus nombres.
function miniPitch(m){
  // El once que alineo, no la plantilla: un campo con dos porteros no es un campo. Cuando la
  // alineacion de esa jornada no esta disponible se dice y se cae a la plantilla agrupada.
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

// Las plazas que dejo vacias: un 4-4-2 con diez no es un 4-4-2, es un 4-4-2 al que le falta un
// medio, y eso son puntos regalados.
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

// El banquillo de esa jornada: lo que tenia y no puso, que es la otra mitad de la decision.
function benchStrip(bench){
  if(!bench || !bench.length) return '';
  return `<div class="mini-bench"><span class="mini-bench-label">banquillo</span>${
    bench.map(p=>`<button class="mini-benched" type="button" data-detail="${p.id}"
      title="${p.name} · ${p.team_short||''}">${chipFace(p)}${p.name}</button>`).join('')}</div>`;
}

// Que tenia cada uno en una jornada. La API no guarda historia: esto sale de deshacer el log de
// traspasos hasta el primer saque, asi que lo que se ve es lo que habia, no lo que hay.
async function openMatchday(week){
  if(!drawer) return;
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
          <button class="p-name" type="button" data-manager="${m.team_id}">${m.manager}</button>
          <span class="md-count">${m.lineup
            ? (m.formation||[]).join('-')+gapNote(m.lineup)
              +(m.week_points!=null?` · ${Math.round(m.week_points)} pts`:'')
            : `${m.playing} de ${m.players} jugaron`}</span>
        </div>
        ${miniPitch(m)}
      </div>`).join('')}
    <p class="drawer-note">Reconstruido del log de traspasos: la API solo dice quien tiene a quien
      ahora. Los jugadores en gris no jugaban esa jornada.</p>`;
  wireDetails(body); wireManagers(body);
}

function wireMatchdays(root=document){
  root.querySelectorAll('button[data-matchday]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',()=>openMatchday(button.dataset.matchday));
  });
}

function wireManagers(root=document){
  root.querySelectorAll('button[data-manager]').forEach(button=>{
    if(button.dataset.wired) return;
    button.dataset.wired='1';
    button.addEventListener('click',(e)=>{ e.stopPropagation(); openManager(button.dataset.manager); });
  });
}

// La plantilla de un rival. Sale del mundo que ya tenemos, asi que no cuesta ninguna peticion a
// LaLiga: solo habia que poder preguntarlo.
async function openManager(teamId){
  if(!drawer) return;
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

// Una fila por jugador: lo que decide si se le puede llegar y por cuanto.
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

async function openDetail(playerId){
  if(!drawer) return;
  drawer.hidden=false;
  panelWide(false);
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
  // El dueño es un enlace: lo que tiene el rival decide si su clausula se paga y si su oferta
  // interesa, y hasta ahora era un nombre y nada mas.
  const owner=p.is_mine ? 'tu'
    : (p.owner && p.owner_team_id
        ? `<button class="p-name" type="button" data-manager="${p.owner_team_id}">${p.owner}</button>`
        : (p.owner||'libre'));
  // La foto identifica antes que el nombre; si no hay, queda el escudo del equipo.
  const face=p.image
    ? `<img class="drawer-face" src="${p.image}" alt="" loading="lazy" onerror="this.remove()">`
    : `<span class="drawer-face crest crest-${p.team_id}"></span>`;
  body.innerHTML=`
    <div class="drawer-head">${face}<h3>${p.name}</h3></div>
    <p class="sub"><span class="pos pos-${(p.position||'').toLowerCase().slice(0,3)}">${p.position}</span>
      ${p.team||''} · ${owner}${p.starred?' · ★':''}
      <button class="cmp-add" type="button" data-cmp="${p.id}" data-cmp-name="${p.name}"
        data-cmp-pos="${p.position||''}">+ comparar</button></p>
    <dl class="drawer-stats">
      <div><dt>Valor de mercado</dt><dd>${exact(p.value)}</dd></div>
      <div><dt>xPts por jornada</dt><dd>${(p.xpts||0).toFixed(2)}</dd></div>
      <div><dt>Puntos por millon</dt><dd>${(p.points_value||0).toFixed(3)}</dd></div>
      <div><dt>Score</dt><dd>${(p.score||0)>=0?'+':''}${(p.score||0).toFixed(2)}
        <span style="color:var(--muted);font-weight:400">· #${p.rank||'?'}</span></dd></div>
      <div><dt>Puntos 25/26</dt><dd>${p.last_season_points||0}</dd></div>
      <div><dt>Puntos temporada</dt><dd>${p.season_points||0}</dd></div>
      <div><dt>Titularidad</dt><dd class="${p.start_probability!=null?titClass(p.start_probability):''}"
        >${p.start_probability!=null?p.start_probability+'%':'—'}${
          p.start_probability_source==='ficha'
            ? `<span style="color:var(--muted);font-weight:400"> · J${p.start_week||''} en su ficha</span>`
            : ''}</dd></div>
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
      ${p.shielded&&p.shielded_until?`<div><dt>Blindaje acaba en</dt>
        <dd><span data-deadline="${p.shielded_until}">${leftUntil(p.shielded_until)}</span>
        <span style="color:var(--muted);font-weight:400"> · ${String(p.shielded_until).slice(11,16)}</span></dd></div>`:''}
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
  wireManagers(body);
  drawTray();
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
  if(a.kind==='note') return `<p class="drawer-note">${a.label}${a.deadline
    ? ` · quedan <span data-deadline="${a.deadline}">${leftUntil(a.deadline)}</span>` : ''}</p>`;
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
  if(a.op==='shield'){
    const suggested=a.suggested?stampText(a.suggested):'';
    const answer=prompt('Blindar a '+player.name+'.\n\n'
      +'Dura 24h y caduca solo, asi que lo unico que se decide es cuando empiezan. Mientras la '
      +'jornada esta en marcha nadie puede pagar clausulas, y un blindaje gastado en esas horas '
      +'no protege de nada.\n\n'
      +(suggested?'Te sugiero '+suggested+', que es cuando reabre la ventana.\n\n'
                 :'La ventana esta abierta, asi que ahora mismo ya protege.\n\n')
      +'Escribe "ahora", o una hora ("21:30" o "07/09 21:30"):', suggested||'ahora');
    if(answer===null) return;
    if(/^\s*(ahora|ya|now)\s*$/i.test(answer)){
      closeDrawer();
      fireOp({op:'shield_player',player_id:player.id},player);
      return;
    }
    const at=stampFrom(answer,a.suggested);
    if(!at){ alert('No he entendido esa hora.'); return; }
    const res=await fetch('/api/shield',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({id:player.id,name:player.name,at})});
    if(!res.ok){ alert('No he podido programarlo.'); return; }
    openDetail(player.id);
    return;
  }
  if(a.op==='cancel_shield'){
    if(!confirm('Cancelar el blindaje programado de '+player.name+'?')) return;
    const res=await fetch('/api/shield/cancel',{method:'POST',
      headers:{'Content-Type':'application/json'},
      body:JSON.stringify({id:player.id})});
    if(!res.ok){ alert('No he podido cancelarlo.'); return; }
    openDetail(player.id);
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
    const raise=a.op==='raise_clause';
    pending={operation:a.op, market_id:a.market_id, player_id:a.player_id||player.id,
             player_team_id:a.player_team_id||player.player_team_id,
             offer_id:a.offer_id, bid_id:a.bid_id, name:player.name, min_bid:a.min||0,
             ideal:player.ideal_bid||0, value:player.value,
             raise, clause:+player.clause||0, safe:+a.safe_margin||0};
    modal.hidden=false;
    modal.querySelector('.bid-action').textContent=a.label+' —';
    modal.querySelector('.bid-who').textContent=player.name;
    // Un cero sugerido se deja en blanco a proposito: el aviso de debajo explica por que.
    modal.querySelector('.bid-amount').value =
      raise && !a.suggested ? '' : group(a.suggested||a.min||0);
    modal.querySelector('#bid-amount-label').textContent=
      raise ? 'Importe a pagar (se descuenta de tu saldo)' : 'Importe de la puja';
    // Las referencias de puja no dicen nada de una clausula, y el techo de futbolfantasy es
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
  }else{
    fireOp(a,player);
  }
}

// La confirmacion de dos pasos vive en los botones de las tablas, asi que una accion del cajon
// se ejecuta prestandole uno: mismo camino, misma ventana, mismo token de un solo uso.
function fireOp(a,player){
  const button=document.createElement('button');
  button.className='op'; button.dataset.op=a.op;
  button.dataset.opMarket=a.market_id||''; button.dataset.opOffer=a.offer_id||'';
  button.dataset.opPlayer=a.player_id||player.id; button.dataset.opName=player.name;
  button.dataset.opAmount=a.amount||'';
  document.body.appendChild(button); wireOps(document.body); button.click();
  button.remove();
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
    +(MODE==='auto' ? 'Se ejecutara solo, sin preguntar, en cuanto se cumpla.'
                    : 'Este servidor esta en modo '+MODE+': lo vera pero no lo ejecutara.'));
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

// ---- tooltip propio: el title nativo tarda casi un segundo -------------------
// Estos son datos que se leen de paso (un candado, un escudo, un "est."), y un segundo de
// espera es justo lo que hace que no se lean. Flotante y pegado al body, no un ::after, que
// dentro de una tabla con scroll se recortaria.
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
window.addEventListener('scroll',hideTip,{passive:true});

// ---- comparador: un fichaje es siempre "en vez de quien" --------------------
// La bandeja vive en localStorage porque el panel se recambia solo en vivo, y perder la
// comparacion a medias por un refresco haria que no se usase.
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
  return true;
}
function cmpDrop(id){
  tray=tray.filter(p=>p.id!==String(id));
  cmpSave(); drawTray();
  // Quitar a uno con la tabla delante tiene que quitarlo de la tabla, no solo de la barra.
  if(comparing()){
    if(tray.length) openCompare();
    else closeDrawer();
  }
}

const comparing=()=> !!(drawer&&!drawer.hidden&&drawer.querySelector('.cmp-view'));

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
    </div>`;
  document.body.appendChild(box);
  box.querySelector('.cmp-go').addEventListener('click',openCompare);
  wireFind(box);
  wireMine(box);
  return box;
}

// El buscador de la bandeja. Sale de la misma peticion que el comparador, asi que no hace
// falta ningun indice nuevo: el mundo entero ya esta en memoria en el servidor.
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
        <span class="cmp-hit-num">${p.team_short||''} · ${p.is_mine?'tuyo':(p.owner||'libre')}
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
    if(event.key==='Escape'){ input.value=''; hide(); input.blur(); }
    // Enter añade el primero: teclear tres letras y pulsar Enter es el camino corto.
    if(event.key==='Enter'&&found.length){
      event.preventDefault();
      const first=found[0];
      cmpAdd(first.id,first.name,first.position);
      input.value=''; hide();
    }
  });
  // Al añadir desde la lista, el hueco se cierra solo: el click lo recoge el documento.
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

// La linea comun de la bandeja: comparar un delantero con tus porteros no dice nada, asi que
// el atajo a la plantilla solo se ofrece por posicion cuando todos coinciden.
function trayLine(){
  const lines=[...new Set(tray.map(p=>p.pos).filter(Boolean))];
  return lines.length===1?lines[0]:'';
}

function drawTray(){
  const box=trayBox();
  const visible=tray.length>0||comparing();
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
  go.disabled=tray.length<2;
  wireDetails(box);
  // El boton de la ficha tiene que decir en que estado esta: "+" invita, "✓" recuerda.
  document.querySelectorAll('button[data-cmp]').forEach(button=>{
    const on=cmpHas(button.dataset.cmp), short=button.classList.contains('small');
    button.classList.toggle('on',on);
    button.textContent=on?(short?'✓':'✓ comparando'):(short?'+':'+ comparar');
    button.title=on?'Quitar del comparador':'Añadir al comparador';
  });
}

// Los botones nacen en contenido que se recambia (fichas, plantillas, la propia bandeja), asi
// que se escuchan en el documento y no hay que recablear nada nunca.
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

// Tu plantilla como menu: metes al que quieras, o los de esa linea de golpe. Mejor que un
// boton que dice "+ mis MED" y decide por ti.
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
      if(tray.length>1) openCompare();
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
    fmt:(v,p)=>v?`${v} ${p.next_home?'🏠':'✈️'}`:'—', text:true},
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

// Una tabla no decide nada por si sola: esta linea dice si el que miras mejora lo que tienes,
// que es la unica razon para estar comparando.
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
}

async function openCompare(){
  if(!drawer) return;
  drawer.hidden=false;
  panelWide(true);
  const body=drawer.querySelector('.drawer-body');
  // Sin nadie dentro sigue siendo el comparador: se abre con el buscador esperando.
  if(!tray.length){
    body.innerHTML=`<div class="cmp-view">
      <div class="drawer-head"><h3>Comparador</h3></div>
      <p class="sub">busca jugadores abajo y ve añadiendolos</p>
      <p class="empty">Puedes meter a cualquiera desde el buscador, desde el boton
        <b>Mi plantilla</b>, o con el <b>+ comparar</b> de cada ficha.</p></div>`;
    drawTray();
    const find=document.querySelector('.cmp-find');
    if(find) find.focus();
    return;
  }
  body.innerHTML='<p class="empty">Comparando…</p>';
  let data;
  try{
    const res=await fetch('/api/compare?ids='+tray.map(p=>p.id).join(','));
    if(!res.ok) throw new Error(res.status);
    data=await res.json();
  }catch(e){
    body.innerHTML='<p class="empty">No he podido comparar.</p>';
    return;
  }
  const list=data.players||[];
  if(!list.length){ body.innerHTML='<p class="empty">No conozco a ninguno de esos.</p>'; return; }
  const head=list.map(p=>`<th>
    <div class="cmp-who">
      ${p.image
        ? `<img src="${p.image}" alt="" loading="lazy" onerror="this.remove()">`
        : `<span class="crest crest-${p.team_id}"></span>`}
      <span class="cmp-name">
        <button class="p-name" type="button" data-detail="${p.id}">${p.name}</button>
        <button class="cmp-x" type="button" data-cmp-drop="${p.id}" aria-label="Quitar">&times;</button>
      </span>
      <span class="cmp-sub"><span class="pos pos-${String(p.position||'').toLowerCase().slice(0,3)}"
        >${p.position}</span> ${p.team_short||p.team||''} ·
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
      // Todos iguales no ensena nada: resaltar ahi solo mancha la tabla.
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
    <div class="drawer-head"><h3>Comparador</h3></div>
    <p class="sub">${list.length} jugadores · lo mejor de cada fila en verde</p>
    ${cmpVerdict(list)}
    <div class="cmp-wrap"><table class="cmp">
      <thead><tr><th></th>${head}</tr></thead>
      <tbody>${rows}
        <tr><td>Estado</td>${list.map(p=>
          `<td><span class="cmp-state">${cmpChips(p)||'<span class="cmp-quiet">sin nada</span>'}</span></td>`
          ).join('')}</tr>
      </tbody>
    </table></div>
    <p class="drawer-note">Valor y clausula en negrita marcan el mas barato, no el mejor.
      Pulsa un nombre para su ficha; al cerrar, la barra de abajo se va con el comparador.</p>
    </div>`;
  wireDetails(body); wireManagers(body); tick(); drawTray();
}

if(drawer){
  drawer.querySelector('.drawer-close').addEventListener('click',closeDrawer);
  drawer.addEventListener('click',(e)=>{ if(e.target===drawer) closeDrawer(); });
  document.addEventListener('keydown',(e)=>{ if(e.key==='Escape') closeDrawer(); });
}

// ---- pestañas: una vista a la vez ------------------------------------------
const TABS=[
  {id:'decidir', label:'Decidir', sections:['plan','acciones']},
  {id:'mercado', label:'Mercado', sections:['fichajes','enventa','misventas','siempre','seguimiento']},
  // Lo que esta en marcha, en su propio sitio: lo que has puesto tu y lo que te han puesto a ti.
  {id:'misofertas', label:'Mis ofertas', sections:['mispujas','ofertas','resueltas']},
  {id:'clausulas', label:'Cláusulas', sections:['programados','calendario','vencimientos','oportunidades','riesgo','clausulas']},
  {id:'plantilla', label:'Plantilla', sections:['once','plantilla','ventas']},
  {id:'partidos', label:'Partidos', sections:['jornada','partidos']},
  // Lo de los demas en su sitio: sus plantillas enteras y lo que pueden pagar por las tuyas.
  {id:'rivales', label:'Rivales', sections:['rivales']},
  {id:'liga', label:'Liga', sections:['movimientos','normas']},
  {id:'ranking', label:'Ranking', sections:['ranking','rentabilidad']},
];

// Un hash puede ser una pestaña (#mercado) o una seccion (#oportunidades): lo
// segundo es lo que hay en los enlaces, asi que hay que resolverlo a su pestaña.
function resolveTarget(hash){
  const id=(hash||'').replace(/^#/,'');
  if(!id) return null;
  if(TABS.some(t=>t.id===id)) return {tab:id, section:null};
  const own=document.getElementById(id);
  if(own&&own.dataset.tab) return {tab:own.dataset.tab, section:id};
  const owner=TABS.find(t=>t.sections.includes(id));
  return owner ? {tab:owner.id, section:id} : null;
}

// Una plantilla rival a la vez: doce apiladas son mucho scroll para una pregunta sobre uno.
const RIVAL_KEY='fantasy:rival';
function applyRivalPick(){
  const sections=[...document.querySelectorAll('section[data-tab="rivales"][id^="rival-"]')];
  if(!sections.length) return;
  // Fuera de su pestaña manda showTab: aqui no se puede volver a ensenar nada.
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

function showTab(id,{section=null,updateHash=true}={}){
  const tab=TABS.find(t=>t.id===id)||TABS[0];
  // Un enlace a un rival concreto manda sobre lo elegido en el desplegable.
  if(section&&section.startsWith('rival-')){
    try{ localStorage.setItem(RIVAL_KEY,section); }catch(e){}
  }
  document.querySelectorAll('section[id]').forEach(s=>{
    // Una seccion puede decir ella misma a que pestaña va: las que nacen en tiempo de
    // ejecucion (una por rival) no pueden estar en una lista escrita aqui.
    s.hidden = s.dataset.tab ? s.dataset.tab!==tab.id : !tab.sections.includes(s.id);
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
  applyRivalPick();
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
  // Una seccion que no existe todavia en esta pestaña no puede aparecer sola: el navegador
  // tiene el HTML de antes (no hay donde meterla) y el JS de antes (no sabe a que pestaña va),
  // asi que hasta ahora se ignoraba en silencio y no se veia hasta recargar a mano.
  const missing=Object.keys(data.sections)
    .filter(id=>!CLIENT_OWNED.has(id)&&!document.getElementById(id));
  if(missing.length){
    // Salvo en mitad de una operacion: recargar con el dialogo abierto se lo llevaria por
    // delante. Se reintenta en el siguiente aviso, y la version no se toca hasta entonces.
    if((drawer&&!drawer.hidden)||(modal&&!modal.hidden)) return;
    location.reload();
    return;
  }
  currentVersion=data.version;
  Object.entries(data.sections).forEach(([id,inner])=>{
    if(CLIENT_OWNED.has(id)) return;
    const node=document.getElementById(id);
    if(node && node.innerHTML!==inner) node.innerHTML=inner;
  });
  wireTables(); wireFilters(); wireStars(); wireBids(); wireOps(); wireDetails(); wireRaids();
  wireManagers(); wireMatchdays(); tick();
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
wireManagers(); wireMatchdays();
wireTabs(); tick(); drawTray();
const headCompare=document.getElementById('open-compare');
if(headCompare) headCompare.addEventListener('click',openCompare);
if(window.EventSource && location.protocol.startsWith('http')) connect();

// ---- legacy (fichero estatico) ----
