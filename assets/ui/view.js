import {html, useState, useEffect, panel} from './lib.js';
import {Face, Tags, ShieldMark, PosTag, Countdown, Empty} from './components.js';
import {useView, postJSON, changed} from './api.js';
import {runAct, toggleAlways} from './actions.js';
import {compare, useTray} from './compare.js';

// The renderer of render/viewmodel.go: a tab's blocks, rows, segments, chips and buttons, in the
// same markup the Go renderer wrote, so report.css draws them as before.

const MAKES_CHIP = /^(role|mk-chip|xi-mark)/;

export function Seg({s}) {
  if (!s) return null;
  const tip = s.tip || undefined;
  if (s.icon) return html`<span class=${s.c} data-tip=${tip}><svg class="ic" aria-hidden="true"><use href=${'#i-' + s.icon}></use></svg></span>`;
  if (s.until) return html`<${Countdown} kind="pill" until=${s.until}/>`;
  const inner = s.kids ? html`<${Segs} list=${s.kids}/>`
    : s.c && s.c.startsWith('role ') ? html`<i class="rdot"></i>${s.t}` : s.t;
  if (s.href && s.href.startsWith('#')) return html`<a class=${s.c || undefined} href=${s.href} data-tip=${tip}>${inner}</a>`;
  if (s.href) return html`<a class=${s.c || undefined} href=${s.href} target="_blank" rel="noopener" data-tip=${tip}>${inner}</a>`;
  if (s.pid && s.c === 'p-name') return html`<button class="p-name" type="button"
    onClick=${(e) => { e.stopPropagation(); panel().openDetail(s.pid); }}>${inner}</button>`;
  if (s.team && s.c === 'p-name') return html`<button class="p-name" type="button"
    onClick=${(e) => { e.stopPropagation(); panel().openManager(s.team); }}>${inner}</button>`;
  if (s.pid || s.team) return html`<span class=${s.c || undefined} data-pid=${s.pid || undefined} data-team=${s.team || undefined} data-tip=${tip}>${inner}</span>`;
  const El = s.el || 'span';
  if (!s.c && !tip && !s.el && !s.style && !s.kids) return s.t;
  return html`<${El} class=${s.c || undefined} style=${s.style || undefined} data-tip=${tip}>${inner}</${El}>`;
}

export const Segs = ({list}) => (list || []).map((s) => html`<${Seg} s=${s}/>`);

// A row's second line: the first item, then its chips (never cut), then the rest, which is the
// only part that shortens.
function Meta({list}) {
  if (!list || !list.length) return null;
  const chips = list.filter((s) => MAKES_CHIP.test(s.c || ''));
  const plain = list.filter((s) => !MAKES_CHIP.test(s.c || ''));
  if (!plain.length) return html`<span class="meta"><${Segs} list=${chips}/></span>`;
  const tail = plain.slice(1);
  return html`<span class="meta"><span class="mhead"><${Seg} s=${plain[0]}/></span><${Segs} list=${chips}/>${
    tail.length ? html`<span class="mtail">${tail.map((s, i) => html`${i ? ' · ' : ''}<${Seg} s=${s}/>`)}</span>` : null}</span>`;
}

export function ChipView({c}) {
  if (c.icon) return html`<${Seg} s=${c}/>`;
  if (c.do) return html`<button type="button" class=${('mk-chip ' + (c.c || '')).trim()} title=${c.tip || undefined}
    onClick=${(e) => { e.stopPropagation(); runAct(c); }}>${c.t}</button>`;
  if (c.until) return html`<${Countdown} kind="chip" until=${c.until} label=${c.label || ''}/>`;
  return html`<span class=${('mk-chip ' + (c.c || '')).trim()} data-tip=${c.tip || undefined}>${c.t}</span>`;
}

function AlwaysAct({a}) {
  const [on, setOn] = useState(!!a.args.on);
  const [busy, setBusy] = useState(false);
  useEffect(() => setOn(!!a.args.on), [a.args.on]);
  const flip = async () => {
    setBusy(true);
    setOn(!on);
    try { setOn(await toggleAlways({id: a.args.player_id, name: a.args.name})); }
    catch (e) { setOn(on); alert('No he podido cambiarlo: ' + e.message); }
    finally { setBusy(false); }
  };
  const cls = a.class.replace(/\bon\b/, '').trim() + (on ? ' on' : '');
  return html`<button type="button" class=${cls} disabled=${busy} data-tip=${a.tip || undefined}
    onClick=${flip}>${on ? '● ' : ''}Siempre en mercado</button>`;
}

// The comparator's "+" follows the tray.
function CmpAct({a}) {
  const list = useTray();
  const on = list.some((item) => item.id === String(a.args.id));
  const short = a.class.includes('small');
  return html`<button type="button" class=${a.class + (on ? ' on' : '')}
    title=${on ? 'Quitar del comparador' : 'Añadir al comparador'}
    onClick=${(e) => { e.stopPropagation(); if (on) compare.drop(a.args.id); else compare.add(a.args.id, a.args.name, a.args.pos); }}>${
    on ? (short ? '✓' : '✓ comparando') : (short ? '+' : '+ comparar')}</button>`;
}

// A followed player's star, painted at once and confirmed by the answer.
export function StarAct({a}) {
  const [on, setOn] = useState(!!a.args.on);
  useEffect(() => setOn(!!a.args.on), [a.args.on]);
  const flip = async (e) => {
    e.stopPropagation();
    const before = on;
    setOn(!before);
    try { setOn(!!(await postJSON('/api/favourite', {id: a.args.id, name: a.args.name})).starred); changed(); }
    catch (err) { setOn(before); }
  };
  return html`<button class=${'star' + (on ? ' on' : '')} type="button" aria-pressed=${on ? 'true' : 'false'}
    title=${on ? 'Quitar de favoritos' : 'Marcar como favorito'} onClick=${flip}>${on ? '★' : '☆'}</button>`;
}

export function ActView({a}) {
  if (a.text) return html`<span class=${a.class || undefined}>${a.label}</span>`;
  if (a.do === 'star') return html`<${StarAct} a=${a}/>`;
  if (a.toggle) return html`<button type="button" class=${a.class} aria-pressed=${a.pressed ? 'true' : 'false'}
    onClick=${(e) => { e.stopPropagation(); a.toggle(a.args.id); }}>${a.label}</button>`;
  if (a.do === 'cmp') return html`<${CmpAct} a=${a}/>`;
  if (a.do === 'always') return html`<${AlwaysAct} a=${a}/>`;
  const button = html`<button type="button" class=${a.class === '' ? undefined : a.class || 'mb mb-ghost'}
    disabled=${!!a.off} data-tip=${a.tip || undefined}
    onClick=${(event) => { event.stopPropagation(); runAct(a); }}>${a.label}</button>`;
  return a.wrap ? html`<span data-tip=${a.wrap}>${button}</span>` : button;
}

export function RowView({r}) {
  if (r.head || r.head_segs) return html`<li class=${('line-head ' + (r.head_c || '')).trim()}>${r.head_segs ? html`<${Segs} list=${r.head_segs}/>` : r.head}</li>`;
  const p = r.player;
  const lead = p ? html`<${Face} p=${p}/>`
    : html`<span class=${('rank-dot ' + (r.lead_c || '')).trim()}><${Segs} list=${r.lead}/></span>`;
  const name = p ? html`<b>${p.name}<${ShieldMark} p=${p}/></b><${PosTag} p=${p}/>${
    p.starred ? html`<span class="fav" title="Favorito">★</span>` : null}` : html`<b>${r.name}</b>`;
  const tags = p || (r.tags && r.tags.length)
    ? html`<span class="tags">${p ? html`<${Tags} p=${p}/>` : null}<${Segs} list=${r.tags}/></span>` : null;
  const note = r.note && r.note.length;
  return html`<li class=${('r ' + (r.tone || '')).trim()} data-pid=${(p && p.id) || r.pid || undefined}
      data-team=${r.team || undefined}>
    ${lead}
    <span class="rwho"><span class="rname">${name}</span><${Meta} list=${r.meta}/><${Meta} list=${r.sub}/></span>
    ${tags}
    ${r.value || note ? html`<span class="rval" data-tip=${r.why || undefined}>${r.value ? html`<b>${r.value}</b>` : null}${
      note ? html`<span class="rnote"><${Segs} list=${r.note}/></span>` : null}</span>` : null}
    <span class="rtail"><span class="rchip">${(r.chips || []).map((c) => html`<${ChipView} c=${c}/>`)}</span>${
      r.acts && r.acts.length ? html`<span class="ract">${r.acts.map((a) => html`<${ActView} a=${a}/>`)}</span>` : null}</span>
  </li>`;
}

// The filter bar: one state for every tab that has it, as the page always kept it.
const filters = {pos: 'all', price: '', text: ''};
const filterWatchers = new Set();
function setFilters(next) { Object.assign(filters, next); filterWatchers.forEach((watch) => watch({...filters})); }
function useFilters() {
  const [value, setValue] = useState({...filters});
  useEffect(() => { filterWatchers.add(setValue); setValue({...filters}); return () => filterWatchers.delete(setValue); }, []);
  return value;
}

// What people type as a price: "20", "20M", "20,5", "20.000.000". Small numbers are millions;
// empty is no limit.
function parsePrice(raw) {
  let text = String(raw || '').trim().toLowerCase().replace(/\s|€/g, '');
  if (!text) return Infinity;
  const millions = /m$/.test(text);
  text = text.replace(/m$/, '');
  if (/^\d{1,3}(\.\d{3})+$/.test(text)) text = text.replace(/\./g, '');
  const n = parseFloat(text.replace(',', '.'));
  if (!isFinite(n)) return Infinity;
  return millions || n < 1000 ? n * 1e6 : n;
}
const plain = (t) => String(t || '').normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase();

function passes(row, state) {
  if (!row.find) return true;
  const max = parsePrice(state.price), needle = plain(state.text.trim());
  return (state.pos === 'all' || row.find.pos === state.pos) && row.find.price <= max &&
    (!needle || plain(row.find.text || row.find.name).includes(needle));
}
const filtering = (state) => state.pos !== 'all' || parsePrice(state.price) !== Infinity || !!state.text.trim();

function FilterBar({view}) {
  const state = useFilters();
  const all = [...view.main, ...(view.aside || []), ...(view.row2 || [])].flatMap((b) => (b.rows || []).filter((r) => r.find));
  const shown = all.filter((r) => passes(r, state)).length;
  return html`<div class="mk-filters"><div class="filters">
    <label>Posición
      <select class="f-pos" value=${state.pos} onInput=${(e) => setFilters({pos: e.currentTarget.value})}>
        <option value="all">todas</option><option value="POR">portero</option><option value="DEF">defensa</option>
        <option value="MED">medio</option><option value="DEL">delantero</option>
      </select></label>
    <label>Precio máximo
      <input class="f-price" type="text" inputmode="decimal" autocomplete="off" placeholder="sin límite"
        title="20, 20M, 20,5 o 20.000.000: en millones si es pequeño" value=${state.price}
        onInput=${(e) => setFilters({price: e.currentTarget.value})}/></label>
    <label>Buscar
      <input class="f-text" type="search" placeholder="nombre" title="nombre, equipo o dueño" value=${state.text}
        onInput=${(e) => setFilters({text: e.currentTarget.value})}/></label>
    <button class="f-reset" type="button" onClick=${() => setFilters({pos: 'all', price: '', text: ''})}>Limpiar</button>
    <span class="f-count kpi-label">${shown} de ${all.length} filas</span>
  </div></div>`;
}

export const KINDS = {};

export function list(rows, scroll, extra) {
  const ul = html`<ul class=${'rows' + (extra ? ' ' + extra : '')}>${rows.map((r) => html`<${RowView} r=${r}/>`)}</ul>`;
  return scroll ? html`<div class="scrollbox" style=${'max-height:' + scroll + 'px'}>${ul}</div>` : ul;
}

export function BlockView({b}) {
  const state = useFilters();
  if (!b || (!b.title && !b.kind)) return null;
  const Kind = b.kind && KINDS[b.kind];
  if (Kind && Kind.full) return html`<${Kind} b=${b}/>`;
  const active = filtering(state);
  const rows = (b.rows || []).filter((r) => !active || passes(r, state));
  const filtered = active && (b.rows || []).some((r) => r.find);
  const counted = b.count == null ? null : filtered ? rows.filter((r) => !r.head).length : b.count;
  let body;
  if (Kind) body = html`<${Kind} b=${b}/>`;
  else if (!rows.length) body = filtered && b.rows.length ? html`<p class="mk-empty f-none">Ninguno con este filtro.</p>`
    : b.empty ? html`<${Empty}>${b.empty}<//>` : null;
  else body = list(rows, b.scroll, b.list_c);
  return html`<div class="block" id=${b.id || undefined}>
    ${b.title ? html`<div class="sec-head"><h2>${b.title}${counted != null ? html`<span class="count">${counted}</span>` : null}</h2>${
      b.sub ? html`<p>${b.sub}</p>` : null}</div>` : null}
    ${b.lead ? html`<p class="lead">${b.lead}</p>` : null}${body}
    ${(b.folds || []).map((f) => html`<details class="fold"><summary>${f.summary}</summary>${list(f.rows, f.scroll)}</details>`)}
    ${b.links && b.links.length ? html`<p class="mk-note">${b.links.map((a) => html`<${ActView} a=${a}/>`)}</p>` : null}
    ${b.note ? html`<p class="mk-note">${b.note}</p>` : null}
  </div>`;
}

const Blocks = ({list}) => (list || []).map((b) => html`<${BlockView} b=${b}/>`);

// A tab's screen: the main column alone, with an aside, or with a second row under both.
export function ViewScreen({name}) {
  const view = useView(name);
  if (!view) return null;
  const main = html`${view.filters ? html`<${FilterBar} view=${view}/>` : null}<${Blocks} list=${view.main}/>`;
  if (view.plain) return html`<div class="main">${main}</div>`;
  if (view.row2 && view.row2.length) return html`<div class="layout with-row"><div class="main">${main}</div>
    <aside class="side"><${Blocks} list=${view.aside}/></aside>
    <div class="row2"><div class="duo"><${Blocks} list=${view.row2}/></div></div></div>`;
  if (view.aside && view.aside.length) return html`<div class="layout"><div class="main">${main}</div>
    <aside class="side"><${Blocks} list=${view.aside}/></aside></div>`;
  return html`<div class="main solo">${main}</div>`;
}

KINDS.stars = ({b}) => html`<ul class="stars">${b.data.map((item) => html`<li class="mk-star" data-pid=${item.player.id}>
  <${Face} p=${item.player} size="xs"/><span>${item.player.name}<${ShieldMark} p=${item.player}/></span>
  <span class="meta">${item.owner}</span><span class=${'tx ' + item.class}>${item.xpts}</span></li>`)}</ul>`;

KINDS.calendar = ({b}) => !b.data.length ? html`<${Empty}>${b.empty}<//>` : html`<ul class="calendar">${b.data.map((day) => html`
  <li class="mk-cal"><span class="mk-cal-day">${day.label}</span><span class="mk-cal-body">${
    day.mine.map((p) => html`<span class="mk-cal-mine" data-pid=${p.id}>🛡 ${p.name}</span>`)}${
    day.theirs.map((p) => html`<span class="mk-cal-them" data-pid=${p.id}>${p.name} <i>${p.gain}</i></span>`)}<span class="meta">${day.reach} a tu alcance</span></span></li>`)}</ul>
  <p class="mk-note">🛡 tuyas que se abren · en gris, las de rivales que más suman a tu once (xPts)</p>`;

KINDS.kv = ({b}) => b.data.map((kv) => html`<div class="kv"><span>${kv.label}</span><b class=${kv.c || undefined}>${kv.value}</b></div>`);
