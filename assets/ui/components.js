import {html, useState, useEffect, useRef, panel} from './lib.js';
import {esWhen, exact, chipText, countdownText, health} from './format.js';
import {prepare, confirm, changed, closeDialog, useDialog} from './api.js';
import {DIALOGS, Notices, flash} from './dialogs.js';
import {leave, putBack} from './motion.js';

// The sprite render.IconSprite puts once at the top of the page.
export const Icon = ({name}) =>
  html`<svg class="ic" aria-hidden="true"><use href=${'#i-' + name}></use></svg>`;

export const Crest = ({id, known = true}) =>
  id && known ? html`<span class=${'crest crest-' + id}></span>` : null;

function useNow(every = 1000) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => { const timer = setInterval(() => setNow(Date.now()), every); return () => clearInterval(timer); }, [every]);
  return now;
}

// A live countdown. "chip" is a row's chip (red in its last six hours, "cerrado" once past);
// "plain" is the figure inside a sentence, coloured as its last hours run out.
export function Countdown({until, kind = 'plain', label = '', cls}) {
  const now = useNow();
  const left = new Date(until).getTime() - now;
  if (isNaN(left)) return null;
  if (kind === 'chip') {
    const cls = 'mk-chip' + (left > 0 && left < 6 * 3600000 ? ' soon' : '') + (left <= 0 ? ' done' : '');
    return html`<span class=${cls} title=${(label + ' ' + esWhen(until)).trim()}><span class="left">${chipText(left)}</span></span>`;
  }
  const hours = left / 3600000;
  // A table's countdown, coloured as a pill: red in its last hour, amber in its last day.
  if (kind === 'pill') {
    const pill = left <= 0 ? 'pill-critical' : hours < 1 ? 'pill-critical' : hours < 24 ? 'pill-warning' : 'pill-neutral';
    return html`<span class=${pill}>${countdownText(left)}</span>`;
  }
  const color = left <= 0 ? '' : hours < 1 ? 'var(--critical)' : hours < 6 ? 'var(--warning)' : '';
  return html`<span class=${cls} style=${color ? 'color:' + color : ''}>${countdownText(left)}</span>`;
}

const CROSS = 'M6.4 2.5h3.2v3.9h3.9v3.2H9.6v3.9H6.4V9.6H2.5V6.4h3.9z';

// The round photo, ringed by his status, with the badge drawn rather than typed.
export function Face({p, size = 'sm'}) {
  const [broken, setBroken] = useState(false);
  const health = p.health || {};
  const badge = health.glyph === 'cross'
    ? html`<span class="hb hb-cross"><svg viewBox="0 0 16 16" aria-hidden="true"><path d=${CROSS}/></svg></span>`
    : health.glyph === 'card'
      ? html`<span class="hb hb-card"><svg viewBox="0 0 16 16" aria-hidden="true"><rect x="4.5" y="2.5" width="7" height="11" rx="1.3"/></svg></span>`
      : null;
  return html`<span class=${'face face-' + size + (health.ring ? ' ring-' + health.ring : '')}
      data-pid=${p.id} data-tip=${health.reason || undefined}>
    <span class="ini">${p.initials}</span>
    ${p.image && !broken ? html`<img src=${p.image} alt="" loading="lazy" onError=${() => setBroken(true)}/>` : null}
    ${badge}</span>`;
}

export const PosTag = ({p}) => html`<span class=${'pos pos-' + p.pos}>${p.position}</span>`;

export const ShieldMark = ({p}) => p.shielded
  ? html` <span class="shield-mark" data-tip=${'blindado' + (p.shielded_when ? ' hasta ' + p.shielded_when : '')}>🛡</span>`
  : null;

// futbolfantasy's category in his club: a dot in their colour and the word; with the club's
// page known it is a link, and the editors' note is the instant tooltip.
export function Role({role, link = false}) {
  if (!role || !role.key) return null;
  const inner = html`<i class="rdot"></i>${role.label}`;
  return link && role.team_url
    ? html`<a class=${'role role-' + role.key} href=${role.team_url} target="_blank" rel="noopener" data-tip=${role.note || undefined}>${inner}</a>`
    : html`<span class=${'role role-' + role.key} data-tip=${role.note || undefined}>${inner}</span>`;
}

// A player's second line: club, owner, role and starting odds as muted text, then the alerts
// that apply, boxed. The card passes its own owner (a link) and lock wording.
export function Tags({p, owner, roleLink = false, star = false, lock, alerts = true}) {
  const health = p.health || {};
  const locked = p.locked_until;
  return html`<span class="pl">
      ${p.team_short ? html`<span class="pl-team"><${Crest} id=${p.team_id} known=${p.crest}/>${p.team_short}</span>` : null}
      <span class="pl-owner">${owner || p.owner}</span>
      <${Role} role=${p.role} link=${roleLink}/>
      ${p.start_probability != null ? html`<span data-tip="probabilidad de ser titular">${p.start_probability} %</span>` : null}
      ${star ? html`<span>★</span>` : null}
    </span>
    ${locked ? html`<span class="tg tg-warn" data-tip=${'no se puede vender hasta el ' + p.locked_when}>🔒 hasta ${lock || p.locked_day}</span>` : null}
    ${alerts && p.shielded ? html`<span class="tg tg-info">🛡</span>` : null}
    ${p.role && p.role.change === 'down' ? html`<span class="tg tg-warn" data-tip=${p.role.note || undefined}>bajó a ${p.role.label}</span>` : null}
    ${alerts && health.ring ? html`<span class=${'tg ' + (health.ring === 'out' ? 'tg-bad' : 'tg-warn')}>${health.reason}</span>` : null}`;
}

export const Empty = ({children}) => html`<p class="mk-empty">${children}</p>`;

// Buttons come in three tones. In a list row they wear the rows' classes (mb-*), in the card the
// card's (act-*), so both keep the look they already had.
export function Button({tone = 'outline', place = 'row', on = false, tip, className = '', children, ...rest}) {
  const cls = place === 'row'
    ? `mb mb-${tone === 'primary' ? 'primary' : 'ghost'}${tone === 'danger' ? ' mb-danger' : ''}`
    : `act${tone === 'primary' ? ' act-primary' : tone === 'danger' ? ' act-danger' : ''}`;
  return html`<button type="button" class=${(cls + (on ? ' on' : '') + ' ' + className).trim()}
    data-tip=${tip || undefined} ...${rest}>${children}</button>`;
}

// A dialog over the page: Escape and a click on the backdrop close it.
export function Modal({label, onClose, children}) {
  useEffect(() => {
    const onKey = (event) => { if (event.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);
  return html`<div class="modal" role="dialog" aria-modal="true" aria-label=${label}
      onClick=${(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <div class="modal-card">${children}</div></div>`;
}

const OP_LABELS = {accept_offer: 'Aceptar oferta por', decline_offer: 'Rechazar oferta por',
  withdraw: 'Retirar del mercado a', sell_to_market: 'Poner en venta a',
  cancel_offer: 'Retirar tu oferta por', cancel_bid: 'Cancelar tu puja por',
  pay_clause: 'Pagar la cláusula de', shield_player: 'Blindar 24h a'};
const DONE_LABEL = {sell_to_market: 'Puesto en venta', accept_offer: 'Oferta aceptada',
  decline_offer: 'Oferta rechazada', withdraw: 'Retirado del mercado', cancel_bid: 'Puja retirada',
  cancel_offer: 'Oferta retirada', shield_player: 'Blindado 24h', pay_clause: 'Clausula pagada'};

// The confirm button says what it does: "Aceptar" on a dialog that rejects an offer read as
// taking it.
const GO_LABEL = {accept_offer: 'Aceptar oferta', decline_offer: 'Rechazar', withdraw: 'Retirar',
  sell_to_market: 'Poner en venta', cancel_offer: 'Retirar oferta', cancel_bid: 'Cancelar puja',
  pay_clause: 'Pagar cláusula', shield_player: 'Blindar'};
const AMOUNT_NAME = {accept_offer: 'Oferta', decline_offer: 'Oferta', pay_clause: 'Cláusula',
  sell_to_market: 'Precio'};
// Only these move the balance, so only these show it: in with an offer taken, out with a clause.
const CASH_SIGN = {accept_offer: 1, pay_clause: -1};

// A dialog's own trace: opened, and cancelled when it closes without the operation going through.
// Confirmed is logged where it is sent, so the three read together as how often one is abandoned.
export function useDialogTrace(name) {
  const done = useRef(false);
  useEffect(() => {
    const trace = panel().usage || {op() {}};
    trace.op('abrir ' + name);
    return () => { if (!done.current) trace.op('cancelar ' + name); };
  }, []);
  return () => { done.current = true; };
}

// The round comparator button's mark, drawn rather than typed: a text "+" sits on the font's
// baseline and looked low in its circle.
export const CmpIcon = ({on}) => html`<svg viewBox="0 0 16 16" aria-hidden="true"><path d=${on ? 'M3.5 8.5l3 3 6-7' : 'M8 3.5v9M3.5 8h9'}/></svg>`;

// The operations after which the row they were started from is no longer there.
const GONE_AFTER = new Set(['accept_offer', 'decline_offer', 'withdraw', 'cancel_offer', 'cancel_bid']);

// The operation confirmed twice, for the ones without an amount to type: what the server
// answers to prepare is shown, and only Aceptar spends the token.
export function ConfirmOp({op}) {
  const [step, setStep] = useState({summary: null, error: '', sending: false, done: ''});
  const token = useRef(null);
  const traced = useDialogTrace(op.op);
  useEffect(() => {
    prepare({operation: op.op, market_id: op.market_id, offer_id: op.offer_id, bid_id: op.bid_id,
      player_id: op.player_id, amount: op.amount || undefined})
      .then((data) => { token.current = data.token; setStep((s) => ({...s, summary: data})); },
        (error) => setStep((s) => ({...s, error: error.message})));
  }, []);
  const send = async () => {
    setStep((s) => ({...s, sending: true, error: ''}));
    (panel().usage || {op() {}}).op('confirmar ' + op.op);
    try {
      const data = await confirm(token.current);
      const done = DONE_LABEL[op.op] || 'Hecho';
      traced();
      if (data.dry_run) { setStep((s) => ({...s, sending: false, done: done + ' (simulacro)'})); return; }
      closeDialog();
      // What the operation took away leaves the list now, and the notice comes once it is gone:
      // the page itself takes seconds to be drawn again.
      const origin = GONE_AFTER.has(op.op) ? op.origin : null;
      await leave(origin);
      flash(done, op.name);
      changed();
      putBack(origin, {afterViews: true});
    } catch (error) {
      setStep((s) => ({...s, sending: false, error: error.message}));
    }
  };
  const s = step.summary;
  const sign = CASH_SIGN[op.op];
  const undoing = op.op === 'cancel_bid' || op.op === 'cancel_offer';
  return html`<${Modal} label="Confirmar operación" onClose=${closeDialog}>
    <h3><span class="bid-action">${OP_LABELS[op.op] || 'Confirmar'}</span> <span class="bid-who">${op.name}</span></h3>
    <div class="bid-summary">
      ${step.done ? html`<p class="bid-ok">${step.done}.</p>`
        : s ? html`<dl class="bid-dl">
            ${s.amount ? html`<dt>${AMOUNT_NAME[op.op] || 'Importe'}</dt><dd><strong>${exact(s.amount)}</strong></dd>` : null}
            ${sign && s.amount ? html`<dt>Saldo</dt><dd>${exact(s.cash_before)} → ${exact(s.cash_before + sign * s.amount)}</dd>` : null}
          </dl>${(s.warnings || []).map((w) => html`<p class="bid-warn-line">⚠ ${w}</p>`)}`
        : step.error ? null : html`<p>Comprobando…</p>`}
    </div>
    <p class="bid-error">${step.error}</p>
    <div class="modal-actions">
      <button class="bid-cancel" type="button" onClick=${closeDialog}>${step.done ? 'Cerrar' : undoing ? 'Volver' : 'Cancelar'}</button>
      ${s && !step.done ? html`<button class="bid-confirm" type="button" disabled=${step.sending} onClick=${send}>${step.sending ? 'Enviando…' : GO_LABEL[op.op] || 'Confirmar'}</button>` : null}
    </div>
    ${sign < 0 ? html`<p class="modal-note">Se comprueba contra tu saldo antes de ejecutar.</p>` : null}
  <//>`;
}

export function ModalRoot() {
  const dialog = useDialog();
  if (!dialog) return html`<${Notices}/>`;
  const Kind = dialog.kind === 'confirm' ? ConfirmOp : DIALOGS[dialog.kind];
  return html`<${Notices}/>${Kind ? html`<${Kind} key=${dialog.key} ...${dialog}/>` : null}`;
}

// A real switch: track and knob, the accent when on.
export const Switch = ({on, disabled, label, onChange}) => html`<button type="button" role="switch"
  class=${'tgl' + (on ? ' on' : '')} aria-checked=${on ? 'true' : 'false'} aria-label=${label}
  disabled=${disabled} onClick=${onChange}><span class="tgl-knob"></span></button>`;

// A face from the API's own player shape (matchday, comparator, squads): the crest behind it
// when there is no photo, the ring and badge of his status.
export function ApiFace({p, size = 'xs'}) {
  const h = health(p);
  const [broken, setBroken] = useState(false);
  const a = p.absence || {};
  const tip = h ? [h.label, a.reason || 'sin detalle en futbolfantasy', a.since, a.until].filter(Boolean).join(' · ') : undefined;
  const badge = !h ? null : h.glyph === 'card'
    ? html`<span class="hb hb-card"><svg viewBox="0 0 16 16" aria-hidden="true"><rect x="4.5" y="2.5" width="7" height="11" rx="1.3"/></svg></span>`
    : h.glyph === 'cross' ? html`<span class="hb hb-cross"><svg viewBox="0 0 16 16" aria-hidden="true"><path d=${CROSS}/></svg></span>` : null;
  return html`<span class=${'face face-' + size + (h ? ' ring-' + h.ring : '')} data-tip=${tip}>${
    p.image && !broken ? html`<img src=${p.image} alt="" loading="lazy" onError=${() => setBroken(true)}/>` : html`<span class=${'crest crest-' + p.team_id}></span>`}${badge}</span>`;
}
