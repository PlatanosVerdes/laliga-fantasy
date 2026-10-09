import {html, useState, useEffect, useRef, legacy} from './lib.js';
import {KINDS, Segs} from './view.js';
import {getJSON, useStamp} from './api.js';

// Liga's own blocks: the season chart, the league log and the pact.

KINDS.rules = ({b}) => html`<div class="mk-box"><ul class="rules">${b.data.map((rule) => html`
  <li class=${rule.live ? 'rule-live' : undefined}><span class=${rule.class}>${rule.tag}</span><${Segs} list=${rule.line}/></li>`)}</ul></div>`;

const FEED_SORT_KEY = 'fantasy:feed-sort';
const stored = (key, fallback) => { try { return localStorage.getItem(key) || fallback; } catch (e) { return fallback; } };
const store = (key, value) => { try { localStorage.setItem(key, value); } catch (e) { /* the order is only a preference */ } };

// The whole log, newest first or biggest first at a click, inside a rail that scrolls.
function FeedBlock({b}) {
  const [order, setOrder] = useState(() => stored(FEED_SORT_KEY, 'recent'));
  const rail = useRef(null);
  const feed = b.data;
  const lines = order === 'amount' && feed.sortable
    ? feed.lines.map((line, i) => [line, i]).sort((a, c) => (c[0].size - a[0].size) || (a[1] - c[1])).map((x) => x[0])
    : feed.lines;
  const pick = (next) => { store(FEED_SORT_KEY, next); setOrder(next); if (rail.current) rail.current.scrollTop = 0; };
  return html`${feed.sortable ? html`<div class="feed-sort" role="group" aria-label="Ordenar">
      <button type="button" class=${order !== 'amount' ? 'on' : undefined} onClick=${() => pick('recent')}>Lo último</button>
      <button type="button" class=${order === 'amount' ? 'on' : undefined} onClick=${() => pick('amount')}>Más grandes</button></div>
    <span class="feed-legend"><i class="feed-mine"></i>tuyo<i class="feed-bid"></i>pujaste<i class="feed-fav"></i>favorito</span>` : null}
  <div class="feed feed-rail" ref=${rail}>${lines.map((line) => line.quiet
    ? html`<div class="feed-row feed-quiet"><span class="feed-date">${line.date}</span><span class="feed-kind">${line.kind}</span>
        <span class="feed-body"><${Segs} list=${line.body}/></span><span class="feed-amount"></span></div>`
    : html`<div class=${'feed-row' + (line.mark ? ' feed-' + line.mark : '')}><span class="feed-date">${line.date}</span>
        <span class="feed-kind">${line.kind}</span><span class="feed-body"><${Segs} list=${line.body}/></span>
        <span class="feed-amount">${line.amount}</span>${line.then ? html`<span class="feed-then">valia <b>${line.then}</b></span><span class=${line.pill}>${line.ratio}</span>` : null}</div>`)}
  </div>`;
}
KINDS.feed = FeedBlock;

// ---- the season chart -----------------------------------------------------------------
// One line per manager over the finished matchdays, every manager in a colour of his own and me
// in the accent. Picking managers on the chips leaves only those (and me) coloured, the rest thin
// and grey. Rank or points.
const EVO_KEY = 'fantasy:evo', EVO_HUES = 11;
function evoState() {
  try {
    const saved = JSON.parse(localStorage.getItem(EVO_KEY) || '{}');
    return {mode: saved.mode === 'points' ? 'points' : 'place', picked: saved.picked || []};
  } catch (e) { return {mode: 'place', picked: []}; }
}

let seasonAnswer = null;

function SeasonChart() {
  const stamp = useStamp();
  const box = useRef(null);
  const [data, setData] = useState(seasonAnswer);
  const [failed, setFailed] = useState(false);
  const [state, setState] = useState(evoState);
  const [width, setWidth] = useState(0);
  const [hl, setHl] = useState(null);
  useEffect(() => {
    getJSON('/api/season').then((answer) => { seasonAnswer = answer; setData(answer); setFailed(false); },
      () => setFailed(!seasonAnswer));
  }, [stamp]);
  useEffect(() => {
    const measure = () => box.current && setWidth(box.current.clientWidth);
    measure();
    let timer;
    const later = () => { clearTimeout(timer); timer = setTimeout(measure, 200); };
    addEventListener('resize', later);
    return () => removeEventListener('resize', later);
  }, [data]);
  const change = (next, what) => {
    store(EVO_KEY, JSON.stringify(next));
    setState(next);
    (legacy().usage || {click() {}}).click('liga', 'evolucion', what);
  };
  let inner;
  if (failed) inner = html`<p class="empty">No he podido reconstruir la clasificacion.</p>`;
  else if (!data) inner = html`<p class="empty">Reconstruyendo la clasificacion…</p>`;
  else if (!width) inner = null;
  else inner = chart(data, width, state, change, hl, setHl);
  return html`<div class="evo" data-season="1" ref=${box}>${inner}</div>`;
}

function chart(d, width, state, change, hl, setHl) {
  const weeks = d.weeks || [];
  const managers = (d.managers || []).filter((m) => (m.place || []).some((p) => p != null));
  if (weeks.length < 1 || !managers.length) return html`<p class="empty">Aun no hay jornadas terminadas.</p>`;
  const picked = state.picked.filter((id) => managers.some((m) => m.team_id === id));
  // Each manager's colour is fixed, by his place in a stable order, so it never moves.
  const hue = new Map([...managers].filter((m) => !m.is_me).sort((a, b) => String(a.team_id).localeCompare(b.team_id))
    .map((m, i) => [m.team_id, i % EVO_HUES + 1]));
  const shown = (id) => !picked.length || picked.includes(id);
  const byPoints = state.mode === 'points';
  const w = Math.max(300, width || 760), narrow = w < 560;
  const padL = byPoints ? (narrow ? 50 : 58) : (narrow ? 34 : 44), padR = narrow ? 78 : 130, padT = 14, padB = 24;
  const rows = managers.length, h = narrow ? Math.round(w * 0.75) : Math.max(260, padT + padB + (rows - 1) * 22);
  const step = weeks.length > 1 ? (w - padL - padR) / (weeks.length - 1) : 0;
  const x = (i) => padL + step * i;
  const top = Math.max(1, ...managers.flatMap((m) => (m.total || []).filter((v) => v != null)));
  const y = byPoints ? (v) => padT + (h - padT - padB) * (1 - v / top)
    : (p) => padT + (h - padT - padB) * (rows > 1 ? (p - 1) / (rows - 1) : 0);
  const valueOf = (m, i) => byPoints ? m.total[i] : m.place[i];
  const ticks = byPoints ? [0, Math.round(top / 2), Math.round(top)].map((v) => [v, v + ' pts']) : [[1, '1º'], [rows, rows + 'º']];
  const kind = (m) => !shown(m.team_id) ? 'rest' : m.is_me ? 'me' : 'pick';
  const order = {rest: 0, pick: 1, me: 2};
  const painted = [...managers].sort((a, b) => order[kind(a)] - order[kind(b)]);
  const labels = [];
  const lines = painted.map((m) => {
    const points = [];
    (m.place || []).forEach((place, i) => {
      const v = valueOf(m, i);
      if (place != null && v != null) points.push([x(i), y(v), i, place]);
    });
    if (!points.length) return null;
    const k = kind(m);
    const cls = k === 'me' ? 'evo-me' : k === 'pick' ? `evo-pick evo-h${hue.get(m.team_id)}` : 'evo-rest';
    if (k !== 'rest') {
      const last = points[points.length - 1];
      labels.push({x: last[0] + 9, y: last[1] + 3.5, name: m.manager, cls});
    }
    const path = points.map((p) => p[0] + ',' + p[1]).join(' ');
    return html`<g class=${'evo-row ' + cls + (hl === m.team_id ? ' hl' : '')} data-evo-team=${m.team_id}>
      <polyline class="evo-line" points=${path}></polyline><polyline class="evo-hit" points=${path}></polyline>
      ${points.map(([px, py, i, place]) => html`<circle class="evo-dot" cx=${px} cy=${py} r=${k === 'rest' ? 3 : 4} tabindex="0"
        data-tip=${`${m.manager} · J${weeks[i]} · ${place}º · ${Math.round(m.points[i] || 0)} pts · ${Math.round(m.total[i] || 0)} acumulados`}></circle>`)}</g>`;
  });
  // The labels at the right end, pushed apart so no two overlap.
  labels.sort((a, b) => a.y - b.y);
  labels.forEach((label, i) => { if (i && label.y < labels[i - 1].y + 12) label.y = labels[i - 1].y + 12; });
  const cut = (name) => narrow && name.length > 9 ? name.slice(0, 9) + '…' : name;
  const toggle = (id) => change({...state, picked: picked.includes(id) ? picked.filter((p) => p !== id) : [...picked, id]}, id);
  const chips = [...managers].sort((a, b) => (b.is_me ? 1 : 0) - (a.is_me ? 1 : 0) || String(a.manager).localeCompare(b.manager, 'es'))
    .map((m) => html`<button type="button" class=${'evo-chip ' + (m.is_me ? 'evo-me' : `evo-h${hue.get(m.team_id)}`) + (picked.includes(m.team_id) ? ' on' : '')}
      aria-pressed=${kind(m) !== 'rest' ? 'true' : 'false'} onClick=${() => toggle(m.team_id)}
      onMouseOver=${() => setHl(m.team_id)} onMouseOut=${() => setHl(null)}><i class="evo-sw"></i>${m.manager}</button>`);
  return html`<div class="evo-controls"><div class="evo-mode" role="group">
      <button type="button" class=${byPoints ? '' : 'on'} onClick=${() => change({...state, mode: 'place'}, 'place')}>Puesto</button>
      <button type="button" class=${byPoints ? 'on' : ''} onClick=${() => change({...state, mode: 'points'}, 'points')}>Puntos</button></div>
    <div class="evo-chips">${chips}${picked.length ? html`<button type="button" class="evo-clear" onClick=${() => change({...state, picked: []}, 'limpiar')}>Todos</button>` : null}</div>
    <p class="evo-hint">${picked.length ? 'Toca más managers para añadirlos o quitarlos.' : 'Toca un manager para ver solo su línea.'}</p></div>
  <svg class="evo-svg" width=${w} height=${h} viewBox=${`0 0 ${w} ${h}`} role="img"
      aria-label=${(byPoints ? 'Puntos acumulados' : 'Puesto') + ' de cada manager jornada a jornada'}>
    ${weeks.map((week, i) => html`<line class="evo-grid" x1=${x(i)} y1=${padT - 6} x2=${x(i)} y2=${h - padB + 4}></line>
      <text class="evo-axis" x=${x(i)} y=${h - padB + 16} text-anchor="middle">J${week}</text>`)}
    ${ticks.map(([v, label]) => html`<text class="evo-axis" x=${padL - 8} y=${y(v) + 3} text-anchor="end">${label}</text>`)}
    ${lines}
    ${labels.map((l) => html`<text class=${'evo-name ' + l.cls} x=${l.x} y=${l.y}>${cut(l.name)}</text>`)}
  </svg>`;
}

KINDS.season = SeasonChart;
