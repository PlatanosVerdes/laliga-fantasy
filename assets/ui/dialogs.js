import {html, useState, useEffect, useRef, panel} from './lib.js';
import {Modal} from './components.js';
import {prepare, confirm, postJSON, changed, closeDialog, openDialog, useCash, useMeta} from './api.js';
import {exact, group, digits, stampText} from './format.js';

// The dialogs that ask for something before an operation: an amount (bid, offer, sale, clause
// payment or raise), the shield's day and hour, the clausulazo's ceiling. Money goes through the
// same two steps as always: /api/bid/prepare checks and hands back a token, /api/bid/confirm
// spends it.

let opened = 0;
const show = (kind, props) => { opened += 1; openDialog({kind, key: opened, ...props}); };

// What doubles is the rise, not what you pay: pay 8.555 and the clause gains 17.110.
const CLAUSE_FACTOR = 2;
// When the money comes in rather than goes out, and what has not happened yet: a bid takes
// nothing until it is won, an offer nothing until it is accepted.
const CASH_IN = new Set(['sell_to_market', 'accept_offer']);
const CASH_WHEN = {bid: 'si la ganas', modify_bid: 'si la ganas', buy_offer: 'si te la aceptan',
  direct_offer: 'si te la aceptan', sell_to_market: 'si te lo compran', accept_offer: 'al aceptarla'};
const DONE_LABEL = {bid: 'Puja enviada', sell_to_market: 'Puesto en venta', accept_offer: 'Oferta aceptada',
  decline_offer: 'Oferta rechazada', withdraw: 'Retirado del mercado', direct_offer: 'Oferta enviada',
  pay_clause: 'Clausula pagada', raise_clause: 'Clausula subida', cancel_bid: 'Puja retirada',
  modify_bid: 'Puja cambiada', buy_offer: 'Oferta enviada', cancel_offer: 'Oferta retirada',
  shield_player: 'Blindado 24h'};
const AMOUNT_LABEL = {bid: 'Pujas', modify_bid: 'Nueva puja', buy_offer: 'Ofreces', sell_to_market: 'Precio de venta',
  accept_offer: 'Cobras', direct_offer: 'Ofreces', pay_clause: 'Pagas', raise_clause: 'Pagas'};
const MOVES_CASH = new Set(['bid', 'modify_bid', 'buy_offer', 'direct_offer', 'pay_clause', 'accept_offer',
  'raise_clause', 'sell_to_market']);

const usage = () => panel().usage || {op() {}, click() {}};

// A bid button of a list: the market takes a bid, a rival's sale an offer, and a bid already
// placed is changed rather than doubled.
export function openBid(data) {
  const existing = data.bid || null;
  const operation = existing ? 'modify_bid' : (data.operation || 'bid');
  const min = +data.min || 0, ideal = +data.ideal || 0;
  show('amount', {p: {operation, market_id: data.market, player_id: data.player, name: data.name,
    min_bid: min, ideal, value: +data.value, bid_id: existing, bids: +data.bids || 0, expires: data.expires,
    title: existing ? 'Cambiar tu puja por' : (operation === 'buy_offer' ? 'Ofertar por' : 'Pujar por'),
    label: 'Importe de la puja', suggested: ideal && ideal >= min ? ideal : min, focus: true}});
}

// One of the card's amount actions, with the player already resolved.
export function openAmount(a, player) {
  const raise = a.op === 'raise_clause';
  show('amount', {p: {operation: a.op, market_id: a.market_id, player_id: a.player_id || player.id,
    player_team_id: a.player_team_id || player.player_team_id, offer_id: a.offer_id, bid_id: a.bid_id,
    name: player.name, min_bid: a.min || 0, ideal: player.ideal_bid || 0, value: player.value,
    raise, clause: +player.clause || 0, safe: +a.safe_margin || 0, bids: +a.bids || 0, expires: a.expires,
    title: a.label + ' —',
    label: raise ? 'Importe a pagar (se descuenta de tu saldo)' : a.op === 'pay_clause'
      ? 'Importe de la clausula (se descuenta de tu saldo)' : a.op === 'sell_to_market' ? 'Precio de venta' : 'Importe de la puja',
    suggested: raise && !a.suggested ? '' : (a.suggested || a.min || 0)}});
}

function Rivals({p}) {
  const isBid = ['bid', 'modify_bid', 'buy_offer'].includes(p.operation) || !p.operation;
  if (!isBid) return null;
  // One of those bids can be mine, and counting it as a rival's is counting wrong.
  const mine = p.bid_id ? 1 : 0, others = Math.max(0, p.bids - mine);
  const text = !p.bids ? 'ninguna' : mine ? (others ? `${p.bids} · ${others} de rivales y la tuya` : 'solo la tuya') : String(p.bids);
  return html`<span class="bid-rivals-wrap">Pujas vigentes <b class=${'bid-rivals' + (others ? ' rivals-on' : '')}>${text}</b></span>`;
}

function ClauseSums({p, amount}) {
  const rise = amount * CLAUSE_FACTOR, next = (p.clause || 0) + rise;
  const times = p.value ? next / p.value : 0, safe = p.safe || 0;
  return html`<dl class="bid-dl bid-clause" id="bid-clause">
    <dt>Multiplicador</dt><dd>${CLAUSE_FACTOR}x</dd>
    <dt>Sube la clausula</dt><dd>${exact(rise)}</dd>
    <dt>Clausula ahora</dt><dd>${exact(p.clause || 0)}</dd>
    <dt>Clausula nueva</dt><dd class="clause-new">${exact(next)}${times ? ` · ${times.toFixed(2)}x su valor` : ''}</dd>
    ${safe && times ? html`<dt></dt><dd class=${times >= safe ? 'clause-safe' : 'clause-open'}>${times >= safe
      ? `por encima de ${safe.toFixed(2)}x: a nadie le renta pagarla` : `por debajo de ${safe.toFixed(2)}x: sigue siendo negocio para quien pueda pagarla`}</dd>` : null}
  </dl>`;
}

// The amount dialog: typed with thousands separators, checked against the minimum, the
// profitable ceiling and the balance while it is typed, then confirmed twice.
function AmountDialog({p}) {
  const cash = useCash();
  const [text, setText] = useState(p.suggested === '' ? '' : group(p.suggested));
  const [step, setStep] = useState({n: 1, summary: null, token: null, error: '', sending: false, done: '', drop: false});
  const input = useRef(null);
  useEffect(() => { if (p.focus && input.current) input.current.focus(); }, []);
  const amount = digits(text) || 0;
  let warn = '';
  if (p.raise) {
    if (!amount) {
      const now = p.value ? (p.clause || 0) / p.value : 0;
      warn = p.safe && now >= p.safe
        ? `Ya esta a ${now.toFixed(2)}x su valor, por encima de ${p.safe.toFixed(2)}x: no hace falta subirla. Si aun asi quieres, escribe un importe.`
        : 'Escribe lo que quieres pagar.';
    }
  } else {
    if (!amount) warn = 'Escribe un importe.';
    else if (amount < p.min_bid) warn = 'Por debajo de la puja minima (' + exact(p.min_bid) + ').';
    if (p.ideal && amount > p.ideal) warn = 'Por encima del techo rentable de futbolfantasy.';
    else if (!p.ideal) warn = 'futbolfantasy no le ve rentabilidad a este precio.';
  }
  const op = p.operation || 'bid';
  const typed = (event) => {
    const el = event.currentTarget, caret = el.selectionStart, before = el.value.length;
    const next = group(digits(el.value));
    setText(next);
    requestAnimationFrame(() => { const shift = next.length - before; el.setSelectionRange(Math.max(0, caret + shift), Math.max(0, caret + shift)); });
  };
  const next = async () => {
    setStep({...step, error: ''});
    usage().op('empezar ' + op);
    try {
      const data = await prepare({operation: op, amount, market_id: p.market_id, player_id: p.player_id,
        player_team_id: p.player_team_id, offer_id: p.offer_id, bid_id: p.bid_id});
      setStep({...step, n: 2, summary: data, token: data.token, error: ''});
    } catch (e) { setStep({...step, error: e.message}); }
  };
  const drop = async () => {
    try {
      const data = await prepare({operation: 'cancel_bid', market_id: p.market_id, bid_id: p.bid_id, player_id: p.player_id});
      setStep({...step, n: 2, summary: null, token: data.token, drop: true, error: ''});
    } catch (e) { setStep({...step, error: e.message}); }
  };
  const send = async () => {
    const operation = step.drop ? 'cancel_bid' : op;
    setStep({...step, sending: true, error: ''});
    usage().op('confirmar ' + operation);
    try {
      const data = await confirm(step.token);
      const done = DONE_LABEL[operation] || 'Hecho';
      if (data.dry_run) { setStep({...step, sending: false, done: done + ' (simulacro)'}); return; }
      changed();
      flash(done, p.name);
      closeDialog();
    } catch (e) { setStep({...step, sending: false, error: e.message}); }
  };
  const after = cash == null ? null : cash + (CASH_IN.has(op) ? amount : -amount);
  const s = step.summary;
  return html`<${Modal} label="Confirmar operacion" onClose=${closeDialog}>
    <h3><span class="bid-action">${p.title}</span> <span class="bid-who">${p.name}</span></h3>
    ${step.n === 1 ? html`<div id="bid-amount-step">
      <div class="bid-field"><label for="bid-amount" id="bid-amount-label">${p.label}</label>
        <input class="bid-amount" id="bid-amount" type="text" inputmode="numeric" autocomplete="off" spellcheck="false"
          ref=${input} value=${text} onInput=${typed} onKeyDown=${(e) => { if (e.key === 'Enter') next(); }}/></div>
      ${p.raise ? html`<${ClauseSums} p=${p} amount=${amount}/>` : html`<p class="bid-refs">
        <span>Puja minima <b class="bid-min">${p.min_bid ? exact(p.min_bid) : 'sin minimo'}</b></span>
        <span>Techo rentable <b class="bid-ideal">${p.ideal ? exact(p.ideal) : 'sin margen'}</b></span>
        <span>Valor <b class="bid-value">${exact(p.value)}</b></span>
        <${Rivals} p=${p}/></p>`}
      ${cash != null && amount ? html`<p class="bid-balance">Saldo <b>${exact(cash)}</b> → <b class=${after < 0 ? 'balance-bad' : 'balance-after'}>${exact(after)}</b>${
        CASH_WHEN[op] ? html` <span class="muted">${CASH_WHEN[op]}</span>` : null}${after < 0 ? html` <span class="balance-bad">no te llega</span>` : null}</p>` : null}
      ${warn ? html`<p class="bid-warn">${warn}</p>` : null}
    </div>` : html`<div id="bid-summary-step"><div class="bid-summary">
      ${step.done ? html`<p class="bid-ok">${step.done}.</p>`
        : step.drop ? html`<p>Vas a <strong>retirar tu puja</strong> por ${p.name}.</p>`
        : html`<dl class="bid-dl">
          <dt>Jugador</dt><dd>${s.player_name || p.name}</dd>
          <dt>${AMOUNT_LABEL[op] || 'Importe'}</dt><dd><strong>${exact(s.amount)}</strong></dd>
          ${s.new_clause ? html`<dt>Clausula</dt><dd>${exact(s.clause)} → <strong>${exact(s.new_clause)}</strong></dd>` : null}
          <dt>Saldo ahora</dt><dd>${exact(s.cash_before)}</dd>
          ${MOVES_CASH.has(op) ? html`<dt>Saldo ${CASH_WHEN[op] || 'despues'}</dt><dd><strong>${exact(s.cash_after)}</strong></dd>` : null}
        </dl>${(s.warnings || []).map((w) => html`<p class="bid-warn-line">⚠ ${w}</p>`)}`}
    </div></div>`}
    <p class="bid-error">${step.error}</p>
    <div class="modal-actions">
      ${step.n === 1 && p.bid_id ? html`<button class="bid-drop" type="button" onClick=${drop}>Retirar mi puja</button>` : null}
      <button class="bid-cancel" type="button" onClick=${closeDialog}>${step.done ? 'Cerrar' : 'Cancelar'}</button>
      ${step.n === 1 ? html`<button class="bid-next primary" type="button" onClick=${next}>Continuar</button>` : null}
      ${step.n === 2 && !step.done ? html`<button class="bid-confirm" type="button" disabled=${step.sending} onClick=${send}>${step.sending ? 'Enviando…' : 'Aceptar'}</button>` : null}
    </div>
    <p class="modal-note">Se comprueba contra tu saldo antes de ejecutar.</p>
  <//>`;
}

// The shield: one moment, prefilled with when the cover is worth starting, and the
// matchday's two shields counted. "Ahora" goes through the two-step confirmation.
export function shieldDialog(a, player) { show('shield', {a, player}); }

function ShieldDialog({a, player}) {
  const pad = (n) => String(n).padStart(2, '0');
  const local = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  const [moment, setMoment] = useState(() => local(a.suggested ? new Date(a.suggested) : new Date()));
  const [error, setError] = useState('');
  const help = a.because === 'round' ? 'Los dos blindajes de esta jornada ya están usados: te propongo ' + stampText(a.suggested) + ', cuando empieza la siguiente.'
    : a.because === 'shield' ? 'Su blindaje acaba el ' + stampText(a.suggested) + ': te propongo esa hora para encadenar el siguiente.'
    : a.suggested ? 'Las clausulas estan cerradas hasta ' + stampText(a.suggested) + ': te propongo esa hora, antes no protege de nada.'
    : 'Las clausulas se pueden pagar ahora mismo: blindarlo ya protege.';
  const budget = a.budget || {};
  let quota = null;
  if (budget.known) {
    const left = budget.limit - (budget.used || []).length - (budget.booked || []).length;
    const who = [...(budget.used || []).map((u) => u.player + ' ' + stampText(u.at)),
      ...(budget.booked || []).map((u) => u.player + ' ' + stampText(u.at) + ' programado')];
    quota = html`<p class=${'shield-quota' + (left <= 0 ? ' full' : '')}>Jornada ${budget.round.week}: te quedan ${Math.max(left, 0)} de ${budget.limit}${who.length ? ' (' + who.join(', ') + ')' : ''}</p>`;
  }
  const now = () => {
    closeDialog();
    panel().closeDrawer();
    openDialog({kind: 'confirm', key: ++opened, op: {op: 'shield_player', name: player.name, player_id: player.id}});
  };
  const save = async () => {
    const when = new Date(moment);
    if (!moment || isNaN(when.getTime())) { setError('Elige dia y hora.'); return; }
    if (when <= new Date()) { setError('Esa hora ya ha pasado: usa "Ahora".'); return; }
    try { await postJSON('/api/shield', {id: player.id, name: player.name, at: when.toISOString()}); }
    catch (e) { setError(e.message || 'No he podido programarlo.'); return; }
    closeDialog();
    changed();
    panel().openDetail(player.id);
  };
  return html`<${Modal} label="Blindar jugador" onClose=${closeDialog}>
    <h3>Blindar a <span class="shield-who">${player.name}</span></h3>
    <p class="shield-help">${help}</p>${quota || html`<p class="shield-quota"></p>`}
    <div class="bid-field shield-when"><label for="shield-at">Cuándo</label>
      <input id="shield-at" type="datetime-local" step="60" min=${local(new Date())} value=${moment}
        onInput=${(e) => setMoment(e.currentTarget.value)}/></div>
    <p class="bid-error shield-error">${error}</p>
    <div class="modal-actions">
      <button class="shield-cancel" type="button" onClick=${closeDialog}>Cancelar</button>
      ${a.now_allowed ? html`<button class="shield-now" type="button" onClick=${now}>Ahora</button>` : null}
      <button class="shield-save bid-next" type="button" onClick=${save}>Programar</button>
    </div>
    <p class="modal-note">Dura 24h desde la hora que elijas. Puedes programar varios: se hacen en orden.</p>
  <//>`;
}

// The scheduled clausulazo asks for one number, the most you would pay, grouped as it is typed so
// a million and ten millions cannot be told apart by counting zeros.
export function raidDialog(p, done) { show('raid', {p, done}); }

function RaidDialog({p, done}) {
  const cash = useCash();
  const meta = useMeta();
  const mode = meta.mode || 'manual';
  const [text, setText] = useState(group(p.suggested || p.clause || 0));
  const [error, setError] = useState('');
  const input = useRef(null);
  useEffect(() => { if (input.current) { input.current.focus(); input.current.select(); } }, []);
  const save = async () => {
    const maxPay = digits(text);
    if (!maxPay) { setError('Escribe un importe.'); return; }
    if (p.clause && maxPay < p.clause) { setError('Por debajo de la cláusula actual: no se pagaría.'); return; }
    try { await postJSON('/api/raid', {id: p.id, name: p.name, max_pay: maxPay}); }
    catch (e) { setError('No se ha podido programar.'); return; }
    closeDialog();
    changed();
    if (done) done();
  };
  return html`<${Modal} label="Programar clausulazo" onClose=${closeDialog}>
    <h3>Clausulazo a <span class="raid-who">${p.name}</span></h3>
    <dl class="bid-dl raid-facts">
      ${p.clause ? html`<dt>Cláusula ahora</dt><dd><strong>${exact(p.clause)}</strong></dd>` : null}
      ${p.opens && new Date(p.opens) > new Date() ? html`<dt>Se libera</dt><dd>${stampText(p.opens)}</dd>` : null}
      ${cash != null ? html`<dt>Tu saldo</dt><dd>${exact(cash)}</dd>` : null}
      ${mode !== 'auto' ? html`<dt></dt><dd class="clause-open">este servidor está en modo ${mode}: lo guardará pero no lo pagará solo</dd>` : null}
    </dl>
    <div class="bid-field"><label for="raid-max">Pago máximo</label>
      <input id="raid-max" type="text" inputmode="numeric" autocomplete="off" spellcheck="false" ref=${input} value=${text}
        onInput=${(e) => setText(group(digits(e.currentTarget.value)))} onKeyDown=${(e) => { if (e.key === 'Enter') save(); }}/></div>
    <p class="bid-error raid-error">${error}</p>
    <div class="modal-actions">
      <button class="raid-cancel" type="button" onClick=${closeDialog}>Cancelar</button>
      <button class="raid-save bid-next" type="button" onClick=${save}>Programar</button>
    </div>
    <p class="modal-note">Se paga en cuanto se libere la cláusula, solo si sigue por debajo de este importe. Si la suben por encima o lo blindan, se cancela sola.</p>
  <//>`;
}

export const DIALOGS = {amount: AmountDialog, shield: ShieldDialog, raid: RaidDialog};

// The notice that an operation landed, said at once without waiting for the rebuild.
let notices = [];
const noticeWatchers = new Set();
export function flash(title, detail, rows) {
  const id = Date.now() + Math.random();
  notices = [...notices, {id, title, detail, rows}];
  noticeWatchers.forEach((watch) => watch(notices));
  setTimeout(() => { notices = notices.filter((n) => n.id !== id); noticeWatchers.forEach((watch) => watch(notices)); },
    rows ? 12000 : 7000);
}
export function Notices() {
  const [list, setList] = useState(notices);
  useEffect(() => { noticeWatchers.add(setList); return () => noticeWatchers.delete(setList); }, []);
  const drop = (id) => { notices = notices.filter((n) => n.id !== id); noticeWatchers.forEach((watch) => watch(notices)); };
  return list.map((n) => html`<div class="effect in" key=${n.id}>
    <button class="effect-close" aria-label="Cerrar" onClick=${() => drop(n.id)}>×</button>
    <h4>${n.title}</h4>${n.detail ? html`<p class="effect-line">${n.detail}</p>` : null}
    ${n.rows ? html`<table>${n.rows.map((r) => html`<tr><th>${r.label}</th><td>${r.before}</td><td class="arrow">→</td><td>${r.after}</td><td class=${'delta ' + r.sign}>${r.delta}</td></tr>`)}</table>` : null}
  </div>`);
}
