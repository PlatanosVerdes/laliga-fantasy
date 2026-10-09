import {html, useState, useEffect, panel} from './lib.js';
import {ApiFace, Countdown} from './components.js';
import {getJSON} from './api.js';
import {fmt, exact} from './format.js';
import {compare, useTray} from './compare.js';

// The side drawer's views: a rival's squad, the squads of a past matchday, and a matchday's
// forecast against what it made.

function useAnswer(url) {
  const [state, setState] = useState({data: null, error: false});
  useEffect(() => {
    let current = true;
    setState({data: null, error: false});
    getJSON(url).then((data) => current && setState({data, error: false}), () => current && setState({data: null, error: true}));
    return () => { current = false; };
  }, [url]);
  return state;
}

const LINES = [{id: 1, label: 'POR'}, {id: 2, label: 'DEF'}, {id: 3, label: 'MED'}, {id: 4, label: 'DEL'}];
// A squad reads by lines: grouped this way it shows at a glance who is short of a defender.
const byLine = (squad) => LINES.map((line) => ({...line, players: (squad || []).filter((p) => Number(p.position_id) === line.id)}))
  .filter((line) => line.players.length);
const posClass = (p) => 'pos pos-' + String(p.position || '').toLowerCase().slice(0, 3);
const open = (id) => (e) => { e.stopPropagation(); panel().openDetail(id); };

function SmallCmp({p}) {
  const list = useTray();
  const on = list.some((item) => item.id === String(p.id));
  return html`<button class=${'cmp-add small' + (on ? ' on' : '')} type="button" title=${on ? 'Quitar del comparador' : 'Añadir al comparador'}
    onClick=${(e) => { e.stopPropagation(); if (on) compare.drop(p.id); else compare.add(p.id, p.name, p.position || ''); }}>${on ? '✓' : '+'}</button>`;
}

// One row per player: what decides whether he can be reached, and for how much.
function ManagerRow({p}) {
  const listing = p.market || {};
  const chips = [];
  if (listing.market_id) chips.push(html`<span class="chip">en venta ${fmt(listing.min_bid)}</span>`);
  if (p.shielded) chips.push(p.shielded_until
    ? html`<span class="chip chip-warn">blindado <${Countdown} kind="pill" until=${p.shielded_until}/></span>`
    : html`<span class="chip chip-warn">blindado</span>`);
  else if (p.clause_locked && p.clause_locked_until) chips.push(html`<span class="chip chip-warn">clausula en <${Countdown} kind="pill" until=${p.clause_locked_until}/></span>`);
  else if (p.clause) chips.push(html`<span class="chip chip-good">clausula pagable</span>`);
  if (p.sale_locked) chips.push(html`<span class="chip chip-warn">🔒 recien fichado</span>`);
  if (!p.available) chips.push(html`<span class="chip chip-bad">no puntua</span>`);
  return html`<div class="squad-row">
    <span class="squad-who"><${ApiFace} p=${p}/>
      <button class="p-name" type="button" onClick=${open(p.id)}>${p.name}</button>
      <span class=${posClass(p)}>${p.position}</span><${SmallCmp} p=${p}/></span>
    <span class="squad-nums"><b>${fmt(p.value)}</b><span title="Clausula">${p.clause ? fmt(p.clause) : '—'}</span>
      <span title="xPts por jornada">${(p.xpts || 0).toFixed(1)}</span></span>
    <span class="squad-chips">${chips}</span>
  </div>`;
}

// A rival's squad. It comes off the world already in memory, so it costs no request to LaLiga.
export function ManagerView({team}) {
  const {data: d, error} = useAnswer('/api/manager/' + team);
  useEffect(() => { if (d) panel().labelDrawer(d.manager || 'la plantilla'); }, [d]);
  if (error) return html`<p class="empty">No he podido leer esa plantilla.</p>`;
  if (!d) return html`<p class="empty">Cargando…</p>`;
  const pos = d.position ? `${d.position}º` : '—';
  return html`<div class="drawer-head"><h3>${d.manager}</h3></div>
    <p class="sub">${d.team_name || ''} · ${pos} con ${Math.round(d.points)} puntos</p>
    <dl class="drawer-stats">
      <div><dt>Caja estimada</dt><dd>${exact(Math.round(d.estimated_cash))}</dd></div>
      <div><dt>Valor de plantilla</dt><dd>${exact(Math.round(d.squad_value))}</dd></div>
      <div><dt>Suma de clausulas</dt><dd>${exact(Math.round(d.clause_total))}</dd></div>
      <div><dt>xPts de la plantilla</dt><dd>${(d.xpts_total || 0).toFixed(1)}</dd></div>
      <div><dt>Jugadores</dt><dd>${d.players}${d.listed ? ` · ${d.listed} en venta` : ''}</dd></div>
      <div><dt>Clausulas bloqueadas</dt><dd>${d.clauses_locked} de ${d.players}</dd></div>
    </dl>
    ${byLine(d.squad).map((line) => html`<div class="squad-line">
      <span class=${'squad-line-label pos pos-' + line.label.toLowerCase()}>${line.label}</span>
      <div class="squad-list">${line.players.map((p) => html`<${ManagerRow} p=${p}/>`)}</div></div>`)}
    <p class="drawer-note">La caja es una estimacion reconstruida del log de traspasos, no un dato que publique el juego. Pulsa un jugador para su ficha.</p>`;
}

// The slots he left empty: a 4-4-2 with ten is a 4-4-2 missing a player.
function gapNote(fielded) {
  let put = 0, slots = 0;
  Object.keys(fielded || {}).forEach((line) => (fielded[line] || []).forEach((p) => { slots++; if (p) put++; }));
  return put < slots ? html` · <span class="md-gap">puso ${put} de ${slots}</span>` : null;
}

function SlotChip({p, line, fielded}) {
  const out = !fielded && !p.played;
  return html`<button class=${'mini-slot' + (out ? ' mini-out' : '')} type="button" onClick=${open(p.id)}
    title=${`${p.name} · ${p.team_short || ''} · ${line}${out ? ' · no jugaba esa jornada' : ''}`}>
    <${ApiFace} p=${p}/><span class="mini-name">${p.name}</span>${fielded && p.points != null ? html`<span class="mini-points">${p.points}</span>` : null}</button>`;
}

// The pitch in miniature, attack to keeper: a squad is recognised by its shape before its names.
function MiniPitch({m}) {
  const fielded = m.lineup;
  if (!fielded) {
    const lines = byLine(m.squad).slice().reverse();
    if (!lines.length) return html`<p class="empty">Sin jugadores.</p>`;
    return html`<p class="drawer-note">No tengo la alineacion de esa jornada; esto es la plantilla que tenia.</p>
      <div class="mini-pitch is-squad">${lines.map((line) => html`<div class="mini-line">${line.players.map((p) => html`<${SlotChip} p=${p} line=${line.label}/>`)}</div>`)}</div>`;
  }
  const order = ['striker', 'midfield', 'defender', 'goalkeeper'];
  const label = {goalkeeper: 'POR', defender: 'DEF', midfield: 'MED', striker: 'DEL'};
  return html`<div class="mini-pitch">${order.map((line) => {
    const players = fielded[line] || [];
    if (!players.length) return null;
    return html`<div class="mini-line">${players.map((p) => p ? html`<${SlotChip} p=${p} line=${label[line]} fielded/>`
      : html`<span class="mini-hole" title=${label[line] + ' sin cubrir'}>⚠</span>`)}</div>`;
  })}</div>${m.bench && m.bench.length ? html`<div class="mini-bench"><span class="mini-bench-label">banquillo</span>${
    m.bench.map((p) => html`<button class="mini-benched" type="button" onClick=${open(p.id)} title=${`${p.name} · ${p.team_short || ''}`}><${ApiFace} p=${p}/>${p.name}</button>`)}</div>` : null}`;
}

// Every manager's squad on a past matchday, rebuilt from the transfer log.
export function MatchdayView({week}) {
  const {data: d, error} = useAnswer('/api/matchday/' + week);
  if (error) return html`<p class="empty">No he podido reconstruir esa jornada.</p>`;
  if (!d) return html`<p class="empty">Reconstruyendo…</p>`;
  return html`<div class="drawer-head"><h3>Jornada ${d.week}</h3></div>
    <p class="sub">plantillas a ${String(d.kickoff).slice(0, 10)} · ${String(d.kickoff).slice(11, 16)}</p>
    ${(d.managers || []).map((m) => html`<div class=${'md-manager' + (m.is_me ? ' md-mine' : '')}>
      <div class="md-head">${m.week_rank != null ? html`<span class=${'md-rank' + (m.week_rank <= 3 ? ' md-podium' : '')}>${m.week_rank}º</span>` : null}
        <button class="p-name" type="button" onClick=${() => panel().openManager(m.team_id)}>${m.manager}</button>
        <span class="md-count">${m.lineup ? html`${m.week_points != null ? html`<b>${Math.round(m.week_points)} pts</b> · ` : null}${(m.formation || []).join('-')}${gapNote(m.lineup)}`
          : `${m.playing} de ${m.players} jugaron`}</span></div>
      <${MiniPitch} m=${m}/></div>`)}
    <p class="drawer-note">Reconstruido del log de traspasos: la API solo dice quien tiene a quien ahora. Los jugadores en gris no jugaban esa jornada.</p>`;
}

const one = (v) => v == null ? '—' : (Math.round(v * 10) / 10).toString();

// What was expected of each eleven on a matchday and what it made, recorded player by player
// before each kick-off.
export function ForecastView({week}) {
  const {data: d, error} = useAnswer('/api/forecast/' + week);
  if (error) return html`<p class="empty">No guarde la prevision de esa jornada.</p>`;
  if (!d) return html`<p class="empty">Cargando…</p>`;
  const diff = (real, planned) => real == null ? html`<span class="fc-diff">—</span>`
    : html`<span class=${'fc-diff ' + (real >= planned ? 'fc-up' : 'fc-down')}>${real >= planned ? '+' : ''}${one(real - planned)}</span>`;
  const managers = [...(d.managers || [])].sort((a, b) => b.actual - a.actual || b.planned - a.planned);
  // Level on points is level on the place, the way the game ranks a matchday.
  const place = (m) => 1 + managers.filter((o) => o.actual > m.actual).length;
  const chip = (real, scale) => {
    if (real == null) return html`<span>—</span>`;
    const p = real / scale;
    const cls = p < 0 ? 'wk-neg' : p >= 8 ? 'wk-hi' : p >= 4 ? 'wk-mid' : 'wk-lo';
    return html`<span><span class=${'wk ' + cls}>${one(real)}</span></span>`;
  };
  // The fill says how far from the forecast, the chip how much.
  const tint = (real, forecast) => {
    if (real == null) return undefined;
    const gap = real - forecast, strength = Math.min(Math.abs(gap) / 10, 1) * 22 + 4;
    return `--fc-tint:color-mix(in srgb,var(${gap >= 0 ? '--good' : '--critical'}) ${Math.round(strength)}%,transparent)`;
  };
  const row = (name, planned, real, forecast, scale, at) => html`<span class="fc-place">${at ? at + 'º' : ''}</span><span class="fc-name">${name}</span>
    <span>${one(planned)}</span>${chip(real, scale)}${diff(real, forecast)}`;
  return html`<div class="drawer-head"><h3>Jornada ${d.week} · prevision</h3></div>
    <p class="sub">${d.complete ? 'terminada' : 'en juego: el real solo cuenta a quien ya ha jugado'}</p>
    <div class="fc-row fc-head"><span class="fc-place">#</span><span class="fc-name">Manager</span><span>Previsto</span><span>Real</span><span>Dif.</span></div>
    <div class="fc-rows">${managers.map((m) => html`<details class=${'fc-team' + (m.is_me ? ' fc-me' : '')}>
      <summary class="fc-row fc-tinted" style=${tint(m.counted ? m.actual : null, m.forecast)}>${
        row(m.short ? html`${m.manager} <span class="fc-short" title="Sin 11 alineados: la jornada cuenta 0">sin 11</span>` : m.manager,
          m.planned, m.counted ? m.actual : null, m.forecast, Math.max(m.counted, 1), m.counted ? place(m) : null)}</summary>
      ${(m.players || []).map((p) => html`<button class="fc-row fc-player fc-tinted" type="button" onClick=${open(p.id)}
        style=${tint(p.points, p.forecast)}>${row(p.name, p.forecast, p.points, p.forecast, 1)}</button>`)}
    </details>`)}</div>
    <p class="drawer-note">Pulsa un manager para ver a sus jugadores. La prevision de cada uno es la que tenia justo antes de su partido.${
      d.counted ? ` De media se fallo por ${one(d.mean_abs_error)} puntos por jugador.` : ''}</p>`;
}
