import {html, useState, useEffect, legacy} from './lib.js';
import {Face, Tags, Role, Crest, Countdown, Button, ShieldMark, Switch} from './components.js';
import {dec, mny, fmt, signed, exact, group, digits, whenShort, since, health} from './format.js';
import {getJSON, postJSON} from './api.js';
import {runAction, toggleAlways} from './actions.js';

const weekClass = (w) => {
  const p = w.points;
  return p == null ? (w.forecast != null ? 'fc' : 'na') : p < 0 ? 'rd' : p >= 8 ? 'g' : p >= 4 ? 'bl' : 'br';
};

function Weeks({weeks}) {
  if (!weeks || !weeks.length) return null;
  const legend = weeks.some((w) => w.forecast != null) ? ' · en pequeño, lo previsto' : '';
  return html`<div class="pc-h">Puntos por jornada<span class="pc-h-note">${legend}</span></div>
    <div class="pc-wks">${weeks.map((w) => {
      const p = w.points;
      const shown = p == null ? (w.forecast != null ? dec(w.forecast) : '–') : p;
      const tip = `Jornada ${w.week}${w.rival ? ' · ' + w.rival : ''}${w.ideal ? ' · once ideal' : ''}` +
        (w.forecast != null ? ' · previsto ' + dec(w.forecast) : '');
      return html`<span class=${'pc-wk' + (w.ideal ? ' ideal' : '')} data-tip=${tip}>
        <b class=${weekClass(w)}>${shown}</b>${p != null && w.forecast != null ? html`<small>${dec(w.forecast)}</small>` : null}<i>J${w.week}</i></span>`;
    })}</div>`;
}

// A curve that says, under the cursor, how much and on what day.
function ValueChart({history}) {
  const days = (history || []).filter((h) => h.value != null);
  const [hover, setHover] = useState(null);
  if (days.length < 3) return null;
  const values = days.map((d) => d.value);
  const w = 440, h = 90, lo = Math.min(...values), hi = Math.max(...values), span = (hi - lo) || 1;
  const step = w / (values.length - 1);
  const y = (v) => h - 4 - (v - lo) / span * (h - 12);
  const path = values.map((v, i) => `${(i * step).toFixed(1)},${y(v).toFixed(1)}`).join(' ');
  const tone = values[values.length - 1] >= values[0] ? 'var(--pole-pos)' : 'var(--pole-neg)';
  const move = (event) => {
    const point = event.touches ? event.touches[0] : event;
    if (!point) return;
    const box = event.currentTarget.getBoundingClientRect();
    const ratio = Math.min(1, Math.max(0, (point.clientX - box.left) / box.width));
    setHover({index: Math.round(ratio * (values.length - 1)), ratio, width: box.width});
  };
  const at = hover && days[hover.index];
  const change = at && values[0] ? (at.value - values[0]) / values[0] * 100 : 0;
  return html`<div class="pc-h">Valor · ${days.length} días</div>
    <div class="chart-wrap">
      <svg class="drawer-chart" width="100%" height=${h} viewBox=${`0 0 ${w} ${h}`} preserveAspectRatio="none"
          aria-label="Historico de valor" onMouseMove=${move} onTouchMove=${move} onMouseLeave=${() => setHover(null)}>
        <polyline points=${path} fill="none" stroke=${tone} stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        ${at ? html`<line x1=${hover.index * step} y1="0" x2=${hover.index * step} y2=${h} stroke="var(--muted)" stroke-width="1" stroke-dasharray="3 3" style="opacity:.6"/>
          <circle cx=${hover.index * step} cy=${y(at.value)} r="3.5" fill=${tone}/>` : null}
      </svg>
      ${at ? html`<div class="chart-tip" style=${'left:' + Math.min(hover.width - 120, Math.max(0, hover.ratio * hover.width - 60)) + 'px'}>
        <b>${exact(at.value)}</b><span>${at.date || ''}</span>
        <span class=${change >= 0 ? 'up' : 'down'}>${change >= 0 ? '+' : ''}${change.toFixed(1)}% desde el inicio</span></div>` : null}
    </div>
    <p class="drawer-note">Valor diario, últimos ${days.length} días · mín ${fmt(lo)} · máx ${fmt(hi)} · pasa el cursor para ver cada día</p>`;
}

function lastWeekPct(history) {
  const days = (history || []).filter((h) => h.value != null);
  if (days.length < 8) return null;
  const now = days[days.length - 1].value, then = days[days.length - 8].value;
  return then ? (now / then - 1) * 100 : null;
}

// Tiles by meaning, one row each: performance, money, availability, the season. A tile with
// nothing to say is left out rather than drawn empty.
function Tiles({data}) {
  const p = data.player, l = data.listing || {}, facts = data.facts || {};
  const tile = (k, v, sm, cls, href) => ({k, v, sm, cls, href});
  const past = lastWeekPct(data.history), projected = p.projected_pct;
  const trend = past != null ? past : projected;
  const starts = p.start_probability, xp = p.xpts || 0;
  const xpTile = tile('xPts / jornada', dec(p.xpts), p.rank ? `score #${p.rank}` : '',
    xp >= 6 ? 't-good' : xp >= 3.5 ? 't-info' : xp >= 2 ? 't-warn' : 't-bad');
  const startsTile = starts != null ? tile('Titular', starts + ' %',
    p.role ? html`<${Role} role=${p.role} link/>` : p.hierarchy ? p.hierarchy
      : (p.start_probability_source === 'ficha' ? `J${p.start_week || ''} en su ficha` : ''),
    starts >= 75 ? 't-good' : starts >= 50 ? 't-warn' : 't-bad') : null;
  const nextTile = p.next_rival ? tile('Próximo', html`<${Crest} id=${p.next_rival_id}/>${p.next_rival}`, p.next_home ? 'en casa' : 'fuera') : null;
  const valueTile = tile('Valor', mny(p.value), l.market_id ? `en venta por ${mny(l.min_bid)}` : '');
  const ceilingTile = p.is_mine ? null : tile('Techo rentable', p.ideal_bid ? mny(p.ideal_bid) : 'sin margen',
    p.ff_url ? '↗ futbolfantasy' : (p.ideal_bid ? 'futbolfantasy' : ''), 't-ceiling', p.ff_url);
  const clauseTile = p.clause ? tile('Cláusula', mny(p.clause), p.value ? `${dec(p.clause / p.value, 2)}x su valor` : '') : null;
  let payableTile = null;
  if (p.clause) {
    const shut = facts.window_open === false || (facts.closes && new Date(facts.closes) <= new Date());
    if (p.shielded && p.shielded_until) payableTile = tile('Clausulable', `blindado hasta ${whenShort(p.shielded_until)}`, '', 't-info');
    else if (p.clause_locked && p.clause_locked_until) payableTile = tile('Clausulable',
      html`se libera en <${Countdown} until=${p.clause_locked_until}/>`, whenShort(p.clause_locked_until), 't-warn');
    else if (shut && facts.opens) payableTile = tile('Clausulable', `se abre ${whenShort(facts.opens)}`, 'ventana de cláusulas cerrada', 't-warn');
    else payableTile = tile('Clausulable', 'pagable ya', '', 't-good');
  }
  const bidsTile = (l.kind === 'libre' || l.expires) ? tile('Pujas', l.bids || 'ninguna',
    l.expires ? 'cierra ' + String(l.expires).slice(11, 16) : '') : null;
  let sellTile = null;
  if (p.is_mine) {
    const rule = facts.hold_except ? 'excepción: ' + facts.hold_except : undefined;
    sellTile = p.sale_locked && p.hold_until
      ? tile('Puedes venderlo', html`<span data-tip=${rule}>🔒 en <${Countdown} until=${p.hold_until}/></span>`, 'norma de la liga', 't-warn')
      : tile('Puedes venderlo', html`<span data-tip=${rule}>ya</span>`, '', 't-good');
  }
  const boughtTile = p.bought_at ? tile('Fichado', since(p.bought_at), '') : null;
  const seasonTile = p.season_points != null ? tile('Puntos temporada', p.season_points,
    p.last_season_points ? `25/26: ${p.last_season_points}` : '') : null;
  const trendTile = trend != null ? tile('Valor 7d', `${signed(trend)} %`,
    past != null && projected != null ? `prevé ${signed(projected)} % en 7 días` : (past == null ? 'previsión' : ''),
    trend >= 0 ? 't-good' : 't-bad') : null;
  const rows = [
    [xpTile, startsTile, nextTile],
    [valueTile, ceilingTile, clauseTile],
    [payableTile, bidsTile, sellTile || boughtTile],
    [seasonTile, trendTile, sellTile ? boughtTile : null],
  ].map((row) => row.filter(Boolean)).filter((row) => row.length);
  const draw = (t) => {
    const inner = html`<span>${t.k}</span><b>${t.v}</b>${t.sm ? html`<small>${t.sm}</small>` : null}`;
    return t.href
      ? html`<a class=${(t.cls || '') + ' t-link'} href=${t.href} target="_blank" rel="noopener" data-tip="Su ficha en futbolfantasy">${inner}</a>`
      : html`<div class=${t.cls || ''}>${inner}</div>`;
  };
  return html`<div class="pc-tiles">${rows.map((row) => html`<div class="pc-grid">${row.map(draw)}</div>`)}</div>`;
}

const isDanger = (a) => !!a.danger || a.op === 'decline_offer' || a.op === 'withdraw';

function Note({a, extra}) {
  return html`<p class="pc-info">${a.label}${a.deadline ? html` · quedan <${Countdown} until=${a.deadline}/>` : null}${extra}</p>`;
}

// The standing listing: a switch that arms or drops the rule at once, and under it, while it is
// on, the price it is listed at and whether it sells by itself. Same /api/always payloads as
// always: auto_sell on its own, the amounts on their own.
function AlwaysBlock({a, player, lead}) {
  const floor = a.good_floor || 0;
  const start = {
    min: a.min_price ? group(a.min_price) : '',
    auto: !!a.auto_sell || !!a.accept_above,
    amount: group(a.accept_above || floor || ''),
  };
  const [on, setOn] = useState(!!a.on);
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(start);
  const [form, setForm] = useState(start);
  const [error, setError] = useState('');
  const [open, setOpen] = useState(false);
  const dirty = form.min !== saved.min || form.auto !== saved.auto ||
    (form.auto && form.amount !== saved.amount);
  const flip = async () => {
    setBusy(true);
    setOn(!on);
    try {
      const now = await toggleAlways(player);
      setOn(now);
      setOpen(now);
    } catch (e) { setOn(on); setError('No he podido cambiarlo: ' + e.message); }
    finally { setBusy(false); }
  };
  const typed = (key) => (event) => {
    const n = digits(event.currentTarget.value);
    setForm({...form, [key]: isNaN(n) ? '' : group(n)});
  };
  const save = async () => {
    setBusy(true);
    setError('');
    try {
      if (form.auto !== saved.auto) {
        await postJSON('/api/always', {id: player.id, name: player.name, auto_sell: form.auto});
      }
      const amount = digits(form.amount) || 0;
      // The suggested floor moves with his value; only a number of your own is pinned.
      const accept = form.auto && amount !== floor ? amount : 0;
      const before = saved.auto && digits(saved.amount) !== floor ? digits(saved.amount) || 0 : 0;
      if (form.min !== saved.min || accept !== before) {
        await postJSON('/api/always', {id: player.id, name: player.name,
          min_price: digits(form.min) || 0, accept_above: accept});
      }
      setSaved(form);
    } catch (e) {
      setError('No se ha guardado: ' + e.message);
    } finally { setBusy(false); }
  };
  const floorTip = 'Una oferta buena es la mayor de tres: lo que pides, su valor ×1,02 y el techo ' +
    'rentable de futbolfantasy. Ahora: ' + (floor ? exact(floor) + ' (' + a.good_source + ')' : 'sin dato') + '.';
  const status = form.auto ? 'se vende solo desde ' + mny(digits(form.amount) || floor) : '';
  const price = digits(saved.min) || a.value || 0;
  const summary = on ? [price ? (price / 1e6).toFixed(2).replace('.', ',') + 'M' : '',
    saved.auto ? 'vende solo' : ''].filter(Boolean).join(' · ') : '';
  return html`<div class="aw-line">${lead && lead.length ? html`<div class="drawer-actions pc-acts aw-lead">${lead}</div>` : null}<div class="aw">
    <div class="aw-row aw-head" role="button" tabindex="0" aria-expanded=${open && on}
        onClick=${() => on && setOpen(!open)} onKeyDown=${(e) => { if (e.key === 'Enter' && on) setOpen(!open); }}>
      <span class="aw-label">Siempre en mercado <i class="aw-i" data-tip="Lo vuelve a poner en venta cada vez que caduca su anuncio, al precio que digas.">ⓘ</i></span>
      <span class="aw-sum">${summary}</span>
      <span onClick=${(e) => e.stopPropagation()}><${Switch} on=${on} disabled=${busy} label="Siempre en mercado" onChange=${flip}/></span>
      <span class="aw-chev" aria-hidden="true">${on ? (open ? '▴' : '▾') : ''}</span>
    </div></div></div>
    ${open && on ? html`<div class="aw aw-box"><div class="aw-set">
      <label class="aw-field"><span>Precio en venta</span>
        <input type="text" inputmode="numeric" autocomplete="off" value=${form.min}
          placeholder=${a.value ? group(a.value) : 'valor de mercado'} onInput=${typed('min')}/></label>
      <div class="aw-row">
        <span class="aw-label">Venta automática <i class="aw-i" data-tip=${floorTip}>ⓘ</i></span>
        <${Switch} on=${form.auto} label="Venta automática" onChange=${() => setForm({...form, auto: !form.auto})}/>
      </div>
      ${form.auto ? html`<label class="aw-field aw-inline"><span>si ofrecen</span>
        <input type="text" inputmode="numeric" autocomplete="off" value=${form.amount} onInput=${typed('amount')}/>
        <span>o más</span></label>` : null}
      ${a.room <= 0 ? html`<p class="aw-warn">Es tu último jugador de esa posición: no lo venderé solo.</p>` : null}
      <div class="aw-foot">
        <p class="aw-status">${status}</p>
        <button type="button" class="act aw-save" disabled=${!dirty || busy} onClick=${save}>Guardar</button>
      </div>
    </div></div>` : null}
    ${error ? html`<p class="bid-error">${error}</p>` : null}`;
}

const FICHAR_ORDER = ['bid', 'modify_bid', 'cancel_bid', 'buy_offer', 'cancel_offer', 'direct_offer', 'raid', 'pay_clause'];
const GROUPS = [['oferta', 'Oferta'], ['mercado', 'Mercado'], ['clausula', 'Cláusula'], ['fichar', 'Fichar']];

function offerWho(a) {
  return a.from_market || a.from === 'el mercado' ? 'del mercado' : 'de ' + a.from;
}

// The actions by topic, each with its small label. One button only is filled: the one the
// server marked as the panel's recommendation for him.
function Actions({data, reopen}) {
  const p = data.player;
  const actions = data.actions || [];
  if (!actions.length) return null;
  const run = (a) => runAction(a, p, {fromCard: true, reopen});
  const button = (a, label) => {
    const tone = a.primary ? 'primary' : isDanger(a) && !/^(accept|decline)_offer$/.test(a.op) ? 'danger' : 'outline';
    const tip = a.primary && a.why ? 'recomendado: ' + a.why : a.note;
    return html`<${Button} place="card" tone=${tone} on=${a.op === 'raid' && a.on} disabled=${!!a.blocked}
      tip=${tip} onClick=${() => run(a)}>${label || a.label}${a.blocked ? ' — no te llega' : ''}<//>`;
  };
  const sections = [];
  for (const [key, title] of GROUPS) {
    const mine = actions.filter((a) => a.group === key);
    if (!mine.length) continue;
    if (key === 'oferta') {
      for (const accept of mine.filter((a) => a.op === 'accept_offer')) {
        const decline = mine.find((a) => a.op === 'decline_offer' && a.offer_id === accept.offer_id);
        sections.push(html`<div class="pc-group">
          <div class="pc-gl">${title} ${offerWho(accept)}</div>
          <div class="pc-gb drawer-actions pc-acts">${button(accept, 'Aceptar ' + mny(accept.amount))}${decline ? button(decline, 'Rechazar') : null}</div>
          ${accept.why ? html`<p class="pc-info">${accept.why}</p>` : null}
          ${accept.expires ? html`<p class="pc-info">caduca en <${Countdown} until=${accept.expires}/>${accept.created ? ' · ofrecida ' + since(accept.created) : ''}</p>` : null}
        </div>`);
      }
      continue;
    }
    const order = (a) => FICHAR_ORDER.indexOf(a.op);
    const rest = mine.filter((a) => a.kind !== 'note' && a.op !== 'always' && a.op !== 'cancel_shield')
      .sort((one, two) => key === 'fichar' ? order(one) - order(two) : 0);
    const always = mine.find((a) => a.op === 'always');
    const cancels = mine.filter((a) => a.op === 'cancel_shield');
    const cancelFor = (note) => cancels.find((c) => note.deadline && c.at === note.deadline);
    const cancelLink = (c) => html` <button type="button" class="pc-link" onClick=${() => run(c)}>cancelar</button>`;
    sections.push(html`<div class="pc-group">
      <div class="pc-gl">${title}</div>
      ${rest.length && !always ? html`<div class="pc-gb drawer-actions pc-acts">${rest.map((a) => button(a))}</div>` : null}
      ${always ? html`<${AlwaysBlock} a=${always} player=${p} lead=${rest.map((a) => button(a))}/>` : null}
      ${mine.filter((a) => a.kind === 'note').map((a) => html`<${Note} a=${a} extra=${cancelFor(a) ? cancelLink(cancelFor(a)) : null}/>`)}
      ${cancels.filter((c) => !mine.some((a) => a.kind === 'note' && a.deadline === c.at))
        .map((c) => html`<p class="pc-info">Blindaje programado ${whenShort(c.at)}${cancelLink(c)}</p>`)}
    </div>`);
  }
  return html`<div class="pc-h">Acciones</div><div class="pc-groups">${sections}</div>`;
}

function CompareButton({p}) {
  const page = legacy();
  const [on, setOn] = useState(page.cmpHas ? page.cmpHas(p.id) : false);
  const flip = (event) => {
    event.stopPropagation();
    if (on) { page.cmpDrop(p.id); setOn(false); } else setOn(!!page.cmpAdd(p.id, p.name, p.position || ''));
  };
  return html`<button class=${'cmp-add' + (on ? ' on' : '')} type="button" onClick=${flip}
    title=${on ? 'Quitar del comparador' : 'Añadir al comparador'}>${on ? '✓ comparando' : '+ comparar'}</button>`;
}

export function PlayerPopup({id, from}) {
  const [state, setState] = useState({data: null, error: null});
  useEffect(() => {
    let current = true;
    getJSON('/api/player/' + id).then((data) => current && setState({data, error: null}),
      (error) => current && setState({data: null, error}));
    return () => { current = false; };
  }, [id]);
  if (state.error) return html`<p class="empty">Solo disponible en la version servida (<code>fantasy serve</code>).</p>`;
  const data = state.data;
  if (!data) return html`<p class="empty">Cargando…</p>`;
  const page = legacy();
  const p = data.player, row = data.row || {};
  const owner = p.is_mine ? 'tuyo' : p.owner && p.owner_team_id
    ? html`<button class="p-name" type="button" onClick=${(e) => { e.stopPropagation(); page.openManager(p.owner_team_id); }}>${p.owner}</button>`
    : (p.owner || 'libre');
  const h = health(p), a = p.absence || {};
  const reopen = () => page.openDetail(p.id);
  return html`
    ${from ? html`<button class="drawer-back" type="button"
      onClick=${() => { page.usage.click('ficha', 'volver a la plantilla'); page.openManager(from.id); }}>← ${from.label}</button>` : null}
    <div class="pc-head"><${Face} p=${row} size="xl"/><div class="pc-who"><h3>${p.name}<${ShieldMark} p=${row}/></h3>
      <div class="pc-sub"><span class=${'pos pos-' + (p.position || '').toLowerCase().slice(0, 3)}>${p.position}</span>
        <span class="tags"><${Tags} p=${row} owner=${owner} roleLink star=${p.starred}
          lock=${p.hold_until ? whenShort(p.hold_until) : ''} alerts=${false}/></span>
        <${CompareButton} p=${p}/></div></div></div>
    ${h ? html`<div class=${'pc-status ' + h.ring}>${h.glyph === 'card' ? '' : '✚ '}${[h.label, a.reason, a.since, a.until].filter(Boolean).join(' · ')}</div>` : null}
    <${Tiles} data=${data}/>
    <${Weeks} weeks=${data.weeks}/>
    <${ValueChart} history=${data.history}/>
    <${Actions} data=${data} reopen=${reopen}/>
    ${data.writes_enabled ? null : html`<p class="drawer-note">Servidor en modo solo lectura: las operaciones estan desactivadas.</p>`}`;
}
