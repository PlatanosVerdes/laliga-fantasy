import {html, render, useState, useEffect, useRef, panel} from './lib.js';
import {ApiFace, Empty} from './components.js';
import {Segs} from './view.js';
import {getJSON} from './api.js';
import {fmt} from './format.js';

// The comparator: who is in it (kept in localStorage, so a live refresh never loses it), the
// floating tray that collects them from anywhere, and its own tab.

const CMP_MAX = 8, CMP_KEY = 'fantasy:compare';
let tray = [];
try { tray = (JSON.parse(localStorage.getItem(CMP_KEY)) || []).slice(0, CMP_MAX); } catch (e) { tray = []; }
let message = '';
const watchers = new Set();

function changed() {
  try { localStorage.setItem(CMP_KEY, JSON.stringify(tray)); } catch (e) { /* the tray lives in memory */ }
  watchers.forEach((watch) => watch(tray.slice()));
  dispatchEvent(new Event('panel:tray'));
}

function say(text) {
  message = text;
  changed();
  setTimeout(() => { if (message === text) { message = ''; changed(); } }, 4000);
}

export const compare = {
  list: () => tray.slice(),
  has: (id) => tray.some((p) => p.id === String(id)),
  add(id, name, pos) {
    id = String(id);
    if (compare.has(id)) return true;
    if (tray.length >= CMP_MAX) { say(`El comparador ya lleva ${CMP_MAX}`); return false; }
    tray = [...tray, {id, name: name || id, pos: pos || ''}];
    changed();
    return true;
  },
  drop(id) { tray = tray.filter((p) => p.id !== String(id)); changed(); },
  clear() { tray = []; changed(); },
  // Ids that came in an address: the tray becomes them, keeping the names it already knows.
  adopt(list) {
    const ids = String(list).split(',').map((x) => x.trim()).filter(Boolean).slice(0, CMP_MAX);
    if (ids.join(',') === tray.map((p) => p.id).join(',')) return;
    tray = ids.map((id) => tray.find((p) => p.id === id) || {id, name: id, pos: ''});
    changed();
  },
  name(id, name, pos) {
    const item = tray.find((p) => p.id === String(id));
    if (item && (item.name !== name || !item.pos)) { item.name = name; item.pos = pos || ''; changed(); }
  },
  hash: () => '#comparador' + (tray.length ? '/' + tray.map((p) => p.id).join(',') : ''),
};
window.panelCompare = compare;

export function useTray() {
  const [value, setValue] = useState(tray.slice());
  useEffect(() => { watchers.add(setValue); setValue(tray.slice()); return () => watchers.delete(setValue); }, []);
  return value;
}

// The tab on show, as report.js announces it.
function useTab() {
  const current = () => (window.panelNav || {}).tab;
  const [tab, setTab] = useState(current());
  useEffect(() => {
    const sync = () => setTab(current());
    addEventListener('panel:tab', sync);
    sync();
    return () => removeEventListener('panel:tab', sync);
  }, []);
  return tab;
}

const posTag = (pos) => pos ? html`<span class=${'pos pos-' + pos.toLowerCase().slice(0, 3)}>${pos}</span>` : null;

function Hit({p, first, onPick, extra}) {
  return html`<button class=${'cmp-hit' + (first ? ' first' : '')} type="button" onClick=${() => onPick(p)}>
    <span class="cmp-hit-who">${p.image ? html`<img src=${p.image} alt="" loading="lazy"/>` : html`<span class=${'crest crest-' + p.team_id}></span>`}
      <b>${p.name}</b>${posTag(p.position)}</span>
    <span class="cmp-hit-num">${extra}</span></button>`;
}

// The search: three letters and Enter is the short way through.
function Find({placeholder}) {
  const [query, setQuery] = useState('');
  const [found, setFound] = useState(null);
  const box = useRef(null);
  useEffect(() => {
    if (query.trim().length < 2) { setFound(null); return undefined; }
    const timer = setTimeout(async () => {
      try {
        const data = await getJSON('/api/compare?q=' + encodeURIComponent(query.trim()));
        setFound((data.matches || []).filter((p) => !compare.has(p.id)));
      } catch (e) { setFound(null); }
    }, 180);
    return () => clearTimeout(timer);
  }, [query]);
  useEffect(() => {
    const away = (event) => { if (box.current && !box.current.contains(event.target)) setFound(null); };
    document.addEventListener('click', away);
    return () => document.removeEventListener('click', away);
  }, []);
  const pick = (p) => { compare.add(p.id, p.name, p.position); setQuery(''); setFound(null); };
  return html`<div class="cmp-find-wrap" ref=${box}>
    <input class="cmp-find" type="search" autocomplete="off" spellcheck="false" placeholder=${placeholder}
      aria-label="Buscar jugador para comparar" value=${query} onInput=${(e) => setQuery(e.currentTarget.value)}
      onKeyDown=${(e) => {
        if (e.key === 'Escape') setFound(null);
        if (e.key === 'Enter' && found && found.length) { e.preventDefault(); pick(found[0]); }
      }}/>
    <div class="cmp-results" hidden=${!found}>${found && !found.length ? html`<p class="cmp-none">Nadie con ese nombre</p>`
      : (found || []).map((p, i) => html`<${Hit} p=${p} first=${i === 0} onPick=${pick}
          extra=${html`<span class=${'crest crest-' + p.team_id}></span>${p.team_short || ''} · ${p.is_mine ? 'tuyo' : (p.owner || 'libre')} <b>${fmt(p.value)}</b>`}/>`)}</div>
  </div>`;
}

// My squad as a menu: whoever I want, or that whole line at once when the tray agrees on one.
function Mine({onTab}) {
  const list = useTray();
  const [open, setOpen] = useState(false);
  const [squad, setSquad] = useState(null);
  const box = useRef(null);
  useEffect(() => {
    const away = (event) => { if (box.current && !box.current.contains(event.target)) setOpen(false); };
    document.addEventListener('click', away);
    return () => document.removeEventListener('click', away);
  }, []);
  const lines = [...new Set(list.map((p) => p.pos).filter(Boolean))];
  const line = lines.length === 1 ? lines[0] : '';
  const toggle = async () => {
    if (open) { setOpen(false); return; }
    if (!squad) {
      try { setSquad((await getJSON('/api/compare')).mine || []); } catch (e) { say('No he podido leer tu plantilla'); return; }
    }
    setOpen(true);
  };
  const pick = (p) => { compare.add(p.id, p.name, p.position); setOpen(false); };
  const free = (squad || []).filter((p) => !compare.has(p.id));
  const same = line ? free.filter((p) => p.position === line) : [];
  const rest = free.filter((p) => !same.includes(p));
  const all = () => {
    for (const p of same) { if (!compare.add(p.id, p.name, p.position)) break; }
    setOpen(false);
    if (compare.list().length > 1 && !onTab) panel().openCompare();
  };
  const row = (p) => html`<${Hit} p=${p} onPick=${pick} extra=${html`${(p.xpts || 0).toFixed(2)} xPts · <b>${fmt(p.value)}</b>`}/>`;
  return html`<div class="cmp-mine-wrap" ref=${box}>
    <button type="button" class="cmp-mine" title=${onTab ? undefined : line ? `Meter tus ${line} para verlos al lado` : 'Meter jugadores tuyos'}
      onClick=${toggle}>Mi plantilla</button>
    <div class="cmp-results cmp-mine-list" hidden=${!open}>${!open ? null : !free.length ? html`<p class="cmp-none">Ya estan todos</p>` : html`
      ${same.length > 1 ? html`<button class="cmp-hit cmp-all" type="button" onClick=${all}>Añadir mis ${same.length} ${line}</button>` : null}
      ${same.length ? html`<p class="cmp-group">Tus ${line}</p>${same.map(row)}` : null}
      ${rest.length ? html`<p class="cmp-group">${same.length ? 'El resto' : 'Tu plantilla'}</p>${rest.map(row)}` : null}`}</div>
  </div>`;
}

// The floating tray: who is collected, from any tab but the comparator's own.
function Tray() {
  const list = useTray();
  const tab = useTab();
  const visible = list.length > 0 && tab !== 'comparador';
  useEffect(() => { document.body.classList.toggle('tray-on', visible); }, [visible]);
  const page = panel();
  return html`<div id="cmp-tray" class="cmp-tray" hidden=${!visible}>
    <${Find} placeholder="buscar jugador…"/>
    <div class="cmp-chips">${list.map((p) => html`<span class="cmp-chip">${posTag(p.pos)}
      <button class="p-name" type="button" onClick=${() => page.openDetail(p.id)}>${p.name}</button>
      <button class="cmp-x" type="button" aria-label="Quitar" onClick=${() => compare.drop(p.id)}>×</button></span>`)}</div>
    <span class="cmp-msg">${message}</span>
    <div class="cmp-acts"><${Mine}/>
      <button type="button" class="cmp-go primary" onClick=${() => page.openCompare()}>Comparar (${list.length})</button>
      <button type="button" class="cmp-close" title="Quitar todos" aria-label="Quitar todos" onClick=${() => compare.clear()}>✕</button>
    </div></div>`;
}

// The comparator's tab: the table the server decides for whoever is in the tray.
function CompareTab() {
  const list = useTray();
  const tab = useTab();
  const [state, setState] = useState({data: null, error: false});
  const ids = list.map((p) => p.id).join(',');
  const page = panel();
  useEffect(() => {
    if (tab !== 'comparador') return;
    // Only the bare tab address follows the tray: with a card open on top it is that card's.
    const [base, view] = location.hash.replace(/^#/, '').split('/');
    if (base === 'comparador' && !(view && page.isView && page.isView(view)) && location.hash !== compare.hash()) {
      history.replaceState(history.state, '', compare.hash());
      if (page.routedTo) page.routedTo(location.hash);
    }
  }, [ids, tab]);
  useEffect(() => {
    if (!ids || tab !== 'comparador') return undefined;
    let current = true;
    getJSON('/api/compare?ids=' + ids).then((data) => {
      if (!current) return;
      (data.players || []).forEach((p) => compare.name(p.id, p.name, p.position));
      setState({data, error: false});
    }, () => current && setState({data: null, error: true}));
    return () => { current = false; };
  }, [ids, tab]);
  let body;
  if (!list.length) body = html`<p class="empty">Busca jugadores arriba y ve añadiéndolos, pulsa <b>Mi plantilla</b> para meter a los tuyos, o usa el <b>+ comparar</b> de cada ficha.</p>`;
  else if (state.error) body = html`<p class="empty">No he podido comparar.</p>`;
  else if (!state.data) body = html`<p class="empty">Comparando…</p>`;
  else if (!(state.data.players || []).length) body = html`<p class="empty">No conozco a ninguno de esos.</p>`;
  else body = html`<${CompareView} data=${state.data}/>`;
  return html`<h2>Comparador</h2>
    <div class="cmp-bar"><${Find} placeholder="añadir jugador…"/><${Mine} onTab/>
      <button type="button" class="cmp-clear" onClick=${() => compare.clear()}>Vaciar</button><span class="cmp-msg">${message}</span></div>
    <div class="cmp-body">${body}</div>`;
}

function CompareView({data}) {
  const t = data.table, players = data.players, page = panel();
  return html`<div class="cmp-view">
    <p class="note">${t.note}</p>
    ${t.verdict ? html`<p class="cmp-verdict"><${Segs} list=${t.verdict}/></p>` : null}
    <div class="cmp-wrap"><table class="cmp">
      <thead><tr><th></th>${t.heads.map((h, i) => html`<th><div class="cmp-who">
        <${ApiFace} p=${players[i]} size="md"/>
        <span class="cmp-name"><button class="p-name" type="button" onClick=${() => page.openDetail(h.player.id)}>${h.player.name}</button>
          <button class="cmp-x" type="button" aria-label="Quitar" onClick=${() => compare.drop(h.player.id)}>×</button></span>
        <span class="cmp-sub">${posTag(h.player.position)} ${h.crest ? html`<span class=${'crest crest-' + h.player.team_id}></span>` : null}${h.team} · <${Segs} list=${[h.owner]}/></span>
      </div></th>`)}</tr></thead>
      <tbody>${t.rows.map((row) => html`<tr><td>${row.label}</td>${row.cells.map((c) => html`<td class=${c.c || ''}><${Segs} list=${c.segs}/></td>`)}</tr>`)}
        <tr><td>Estado</td>${t.states.map((chips) => html`<td><span class="cmp-state"><${Segs} list=${chips}/></span></td>`)}</tr></tbody>
    </table></div>
    <p class="drawer-note">Valor y cláusula en verde marcan el más barato, no el mejor. Pulsa un nombre para su ficha.</p>
  </div>`;
}


const trayHost = document.createElement('div');
document.body.appendChild(trayHost);
render(html`<${Tray}/>`, trayHost);

export {CompareTab};
