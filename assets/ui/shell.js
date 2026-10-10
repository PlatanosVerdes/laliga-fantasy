import {html, render, useState, useEffect, useRef} from './lib.js';
import {ViewScreen} from './view.js';
import {ApiFace, Icon, Countdown} from './components.js';
import {getJSON, useView, useCash, changed, getViews} from './api.js';
import {fmt, esMoney, exact, countdownText} from './format.js';
import {flash} from './dialogs.js';
import {compare} from './compare.js';

// The page around the views: the tab bar with the search, the live dot and the version, the
// four cards, the sections of the tab on show, the foot; the addresses (#tab/view/arg) and Back;
// the drawer the card and the other views open in; the live connection; the instant tooltip;
// and the usage the server logs.

// ---- usage: what is looked at, what is touched and for how long ---------------------------
// What no server log can hold because it never becomes a request: the tab, the click inside it
// and the order of a table. The why is in internal/usage.
const seed = (() => { try { return JSON.parse(document.getElementById('views-data').textContent || '{}'); } catch (e) { return {}; } })();
export const usage = (() => {
  const OFF = {tab() {}, click() {}, sort() {}, op() {}};
  const mode = (seed.meta || {}).mode || '';
  // The static report has nowhere to send it.
  if (!mode || mode === 'informe' || !location.protocol.startsWith('http')) return OFF;
  const phone = matchMedia('(max-width: 700px)').matches;
  let queue = [], timer = null, tab = null, since = 0, seen = 0;
  const send = (beacon) => {
    if (!queue.length) return;
    const batch = queue.slice(0, 200); queue = queue.slice(200);
    const body = JSON.stringify(batch);
    try {
      if (beacon && navigator.sendBeacon) navigator.sendBeacon('/api/usage', new Blob([body], {type: 'application/json'}));
      else fetch('/api/usage', {method: 'POST', headers: {'Content-Type': 'application/json'}, body, keepalive: true}).catch(() => {});
    } catch (e) { /* a lost batch is a lost measurement, nothing more */ }
  };
  const push = (event) => {
    queue.push({at: new Date().toISOString(), phone, ...event});
    if (queue.length >= 200) { send(false); return; }
    if (!timer) timer = setTimeout(() => { timer = null; send(false); }, 15000);
  };
  // The clock runs only while the page is visible: the panel sits open all afternoon.
  const stop = () => { if (tab && since) seen += Date.now() - since; since = 0; };
  const start = () => { if (tab && !since) since = Date.now(); };
  const close = () => {
    stop();
    if (tab && seen >= 1000) push({kind: 'tab', what: tab, seconds: Math.round(seen / 1000)});
    tab = null; seen = 0;
  };
  document.addEventListener('visibilitychange', () => { if (document.hidden) stop(); else start(); });
  addEventListener('pagehide', () => { close(); send(true); });
  return {
    tab(id) { if (id === tab) return; close(); tab = id; seen = 0; start(); },
    click(where, label, what) { push({kind: 'click', where, label, what}); },
    sort(where, column) { push({kind: 'sort', where, what: column}); },
    op(name, where) { push({kind: 'op', what: name, where: where || tab || ''}); },
  };
})();

// ---- the instant tooltip: the native title takes nearly a second -------------------------
let tipBox = null;
function showTip(target) {
  const message = target.dataset.tip;
  if (!message) return;
  if (!tipBox) { tipBox = document.createElement('div'); tipBox.className = 'tip-float'; document.body.appendChild(tipBox); }
  tipBox.textContent = message;
  tipBox.hidden = false;
  const anchor = target.getBoundingClientRect(), own = tipBox.getBoundingClientRect();
  const left = Math.max(8, Math.min(anchor.left + anchor.width / 2 - own.width / 2, innerWidth - own.width - 8));
  let top = anchor.top - own.height - 8;
  if (top < 8) top = anchor.bottom + 8;
  tipBox.style.left = left + 'px';
  tipBox.style.top = top + 'px';
}
const hideTip = () => { if (tipBox) tipBox.hidden = true; };
document.addEventListener('mouseover', (e) => { const t = e.target.closest && e.target.closest('[data-tip]'); if (t) showTip(t); else hideTip(); });
document.addEventListener('mouseout', (e) => { if (e.target.closest && e.target.closest('[data-tip]')) hideTip(); });
// Without a mouse there is no hovering, so a tap opens it and the next one closes it.
document.addEventListener('click', (e) => {
  if (!matchMedia('(hover:none)').matches) return;
  const t = e.target.closest && e.target.closest('[data-tip]');
  if (t) showTip(t); else hideTip();
});
addEventListener('scroll', hideTip, {passive: true});
document.addEventListener('focusin', (e) => { const t = e.target.closest && e.target.closest('[data-tip]'); if (t) showTip(t); });
document.addEventListener('focusout', hideTip);

// ---- the address: #tab, #section, or #tab/view/arg for what the drawer shows --------------
// The tabs before they were split by direction, and the sections of the old tables, so old
// links and bookmarks still land on the tab that took their place.
const TAB_ALIASES = {mercado: 'comprar', misofertas: 'vender', plan: 'decidir', acciones: 'decidir', caja: 'decidir',
  chollos: 'comprar', fichajes: 'comprar', enventa: 'comprar', mispujas: 'comprar', seguimiento: 'comprar',
  resueltas: 'comprar', misventas: 'vender', ofertas: 'vender', siempre: 'vender', ventas: 'plantilla',
  subir: 'clausulas', programados: 'clausulas', calendario: 'clausulas', vencimientos: 'clausulas',
  oportunidades: 'clausulas', jornada: 'partidos', pinta: 'rivales', rentabilidad: 'ranking'};
const RIVAL_KEY = 'fantasy:rival';
const DRAWER_VIEWS = new Set(['jugador', 'manager', 'plantillas', 'prevision', 'jornada', 'alcance', 'comparar']);
const POPUPS = new Set(['jugador', 'jornada', 'alcance']);

const store = (key, value) => { try { localStorage.setItem(key, value); } catch (e) { /* a preference only */ } };
const stored = (key) => { try { return localStorage.getItem(key); } catch (e) { return null; } };

function hashParts(hash = location.hash) {
  const [base, view, arg] = (hash || '').replace(/^#/, '').split('/');
  return {base: base || '', view: view || '', arg: arg ? decodeURIComponent(arg) : ''};
}

const nav = {tab: null, section: null, drawer: null, rival: stored(RIVAL_KEY), version: 0};
const navWatchers = new Set();
const moved = () => { nav.version += 1; navWatchers.forEach((watch) => watch(nav.version)); };
function useNav() {
  const [, setVersion] = useState(nav.version);
  // Subscribed after the first paint, so whatever moved before it is caught up here.
  useEffect(() => { navWatchers.add(setVersion); setVersion(nav.version); return () => navWatchers.delete(setVersion); }, []);
  return nav;
}

const pageData = () => getViews().page || {};
const tabIds = () => (pageData().tabs || []).map((t) => t.id);

// A hash can be a tab (#comprar) or a section (#v-clausulas), and the second is what the links
// carry, so it is resolved to the tab that owns it.
function resolveTarget(hash) {
  let id = (hash || '').replace(/^#/, '');
  id = TAB_ALIASES[id] || id;
  if (!id) return null;
  if (tabIds().includes(id)) return {tab: id, section: null};
  const section = (pageData().sections || []).find((s) => s.id === id);
  return section ? {tab: section.tab, section: id} : null;
}

function showTab(id, {section = null, updateHash = true} = {}) {
  const ids = tabIds();
  const tab = ids.includes(id) ? id : ids[0] || 'decidir';
  // A link to one rival overrides the one picked in the dropdown.
  if (section && section.startsWith('rival-')) { nav.rival = section; store(RIVAL_KEY, section); }
  nav.tab = tab; nav.section = section;
  store('fantasy-tab', tab);
  usage.tab(tab);
  // replaceState, not assignment: neither a history entry per click nor a hashchange on itself.
  if (updateHash) history.replaceState(null, '', '#' + (section || tab));
  moved();
  dispatchEvent(new Event('panel:tab'));
  if (section) setTimeout(() => { const node = document.getElementById(section); if (node) node.scrollIntoView({behavior: 'smooth', block: 'start'}); }, 50);
}

let routing = false, routed = null, routedOnce = false;

// Every drawer opened is a history entry, so Back closes it instead of leaving the page.
function markView(view, arg = '') {
  if (routing) return;
  const base = hashParts().base || nav.tab || 'decidir';
  const target = '#' + base + '/' + view + (arg !== '' ? '/' + arg : '');
  if (location.hash === target) return;
  const depth = (history.state && history.state.depth) || 0;
  history.pushState({depth: depth + 1}, '', target);
  routed = target;
}

function openView(name, arg, extra = {}) {
  markView(name, arg);
  if (name === 'comparar') { openCompare({replace: true}); return; }
  nav.drawer = {name, arg: String(arg), ...extra};
  moved();
}

export function openDetail(id) {
  usage.click('ficha', 'abrir ficha', String(id));
  // Opened from a rival's squad, the card offers the way back to it.
  const from = nav.drawer && nav.drawer.name === 'manager' && nav.drawer.label
    ? {id: nav.drawer.arg, label: nav.drawer.label} : null;
  openView('jugador', id, {from});
}
export const openManager = (team) => openView('manager', team);
export const openWeek = (week) => openView('jornada', week);
export const openReach = (team) => openView('alcance', team);
export const openMatchday = (week) => openView('plantillas', week);
export const openForecast = (week) => openView('prevision', week);

// What the drawer is showing, named once it is known: the card's way back says whose squad.
export function labelDrawer(label) { if (nav.drawer) nav.drawer.label = label; }

export function shutDrawer() { if (nav.drawer) { nav.drawer = null; moved(); } }

export function closeDrawer() {
  const depth = (history.state && history.state.depth) || 0;
  // Opened from this page: step back to where it was. From a pasted link there is no page
  // behind it, so the address is rewritten instead.
  if (depth > 0) { history.go(-depth); return; }
  if (hashParts().view) history.replaceState(null, '', '#' + (hashParts().base || 'decidir'));
  routed = location.hash;
  shutDrawer();
}

export function openCompare({replace = false} = {}) {
  shutDrawer();
  if (replace) history.replaceState(null, '', compare.hash()); else history.pushState(null, '', compare.hash());
  routed = location.hash;
  showTab('comparador', {updateHash: false});
}

// Back, forward, a pasted link: whatever the address says is what the page shows.
function route() {
  // Back fires both popstate and hashchange: the second must not reload the drawer.
  if (routed === location.hash) return;
  routed = location.hash;
  const {base, view, arg} = hashParts();
  // Ids in the address only count when it is how the page was opened: on Back the tray, which
  // has moved on since that entry was written, is the truth.
  if (base === 'comparador' && view && !DRAWER_VIEWS.has(view) && !routedOnce) compare.adopt(view);
  routedOnce = true;
  const target = resolveTarget('#' + base);
  if (target && (nav.tab !== target.tab || target.section)) showTab(target.tab, {section: target.section, updateHash: false});
  routing = true;
  try {
    if (view && DRAWER_VIEWS.has(view)) {
      if (view === 'comparar') openCompare({replace: true});
      else { nav.drawer = {name: view, arg}; moved(); }
    } else shutDrawer();
  } finally { routing = false; }
}

export function goto(where) {
  const target = resolveTarget(where);
  if (target) showTab(target.tab, {section: target.section}); else showTab(where);
}

// ---- the tab bar, the search, the four cards ------------------------------------------------

function Find() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [found, setFound] = useState(null);
  const [cur, setCur] = useState(0);
  const box = useRef(null), input = useRef(null);
  const collapse = () => { setOpen(false); setQuery(''); setFound(null); setCur(0); };
  const expand = () => { setOpen(true); setTimeout(() => input.current && input.current.focus(), 0); };
  useEffect(() => {
    const away = (e) => { if (box.current && !box.current.contains(e.target)) collapse(); };
    // The slash opens it from anywhere but a field being typed in.
    const slash = (e) => {
      if (e.key !== '/' || e.metaKey || e.ctrlKey) return;
      const at = document.activeElement;
      if (at && (at.tagName === 'INPUT' || at.tagName === 'TEXTAREA' || at.isContentEditable)) return;
      e.preventDefault();
      expand();
    };
    document.addEventListener('click', away);
    document.addEventListener('keydown', slash);
    return () => { document.removeEventListener('click', away); document.removeEventListener('keydown', slash); };
  }, []);
  useEffect(() => {
    if (query.trim().length < 2) { setFound(null); return undefined; }
    const timer = setTimeout(async () => {
      try {
        const matches = (await getJSON('/api/compare?q=' + encodeURIComponent(query.trim()))).matches || [];
        // Your own first: the card you look up most is one of yours.
        setFound([...matches.filter((p) => p.is_mine), ...matches.filter((p) => !p.is_mine)]);
        setCur(0);
      } catch (e) { setFound(null); }
    }, 180);
    return () => clearTimeout(timer);
  }, [query]);
  const pick = (id) => { collapse(); openDetail(id); };
  const keys = (e) => {
    if (e.key === 'Escape') collapse();
    if (!found || !found.length) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      setCur(Math.max(0, Math.min(found.length - 1, cur + (e.key === 'ArrowDown' ? 1 : -1))));
    }
    if (e.key === 'Enter') { e.preventDefault(); pick(found[cur].id); }
  };
  return html`<div class=${'head-find' + (open ? ' open' : '')} ref=${box}>
    <button class="find-btn" type="button" aria-label="Buscar jugador" data-tip="Buscar jugador (tecla /)"
      onClick=${() => (open ? collapse() : expand())}><svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="7" cy="7" r="4.6"/><path d="M10.4 10.4l3.6 3.6"/></svg></button>
    <div class="find-pop" hidden=${!open}><input id="find" class="head-input" type="search" autocomplete="off" spellcheck="false"
      placeholder="buscar jugador…" aria-label="Buscar un jugador y abrir su ficha" ref=${input} value=${query}
      onInput=${(e) => setQuery(e.currentTarget.value)} onKeyDown=${keys}/>
      <div class="cmp-results find-results" hidden=${!found}>${found && !found.length ? html`<p class="cmp-none">Nadie con ese nombre</p>`
        : (found || []).map((p, i) => html`<button class=${'cmp-hit' + (i === cur ? ' first' : '')} type="button" onClick=${() => pick(p.id)}>
          <span class="cmp-hit-who"><${ApiFace} p=${p}/><b>${p.name}</b><span class=${'pos pos-' + String(p.position || '').toLowerCase().slice(0, 3)}>${p.position}</span></span>
          <span class="cmp-hit-num"><span class=${'crest crest-' + p.team_id}></span>${p.team_short || ''} · ${p.is_mine ? 'tuyo' : (p.owner || 'libre')} <b>${fmt(p.value)}</b></span>
        </button>`)}</div></div>
  </div>`;
}

// The live connection's state, the build and when the page last changed, in the dot's tooltip.
const live = {state: 'Sin conexión en vivo', on: false, at: new Date(), stamp: 'estatico'};
const liveWatchers = new Set();
const liveMoved = () => liveWatchers.forEach((watch) => watch({...live}));
function useLive() {
  const [value, setValue] = useState({...live});
  useEffect(() => { liveWatchers.add(setValue); setValue({...live}); return () => liveWatchers.delete(setValue); }, []);
  return value;
}

// What a click on the live dot does, when something registers one (it does nothing by itself).
let liveDotAction = null, liveDotHint = '';
const liveDotWatchers = new Set();
// The hint is added to the dot's tooltip, so the click it now has is not a secret.
export function onLiveDot(action, hint = 'pulsa para ver los servicios') {
  liveDotAction = action;
  liveDotHint = action ? hint : '';
  liveDotWatchers.forEach((watch) => watch(action));
}

function LiveDot({build}) {
  const l = useLive();
  const [action, setAction] = useState(() => liveDotAction);
  useEffect(() => { liveDotWatchers.add(setAction); setAction(() => liveDotAction); return () => liveDotWatchers.delete(setAction); }, []);
  const hm = String(l.at.getHours()).padStart(2, '0') + ':' + String(l.at.getMinutes()).padStart(2, '0');
  return html`<span id="live-dot" class=${l.on ? 'live-on' : 'live-off'} data-build=${build || undefined}
    data-tip=${[l.state, build, 'actualizado ' + hm, action ? liveDotHint : ''].filter(Boolean).join(' · ')}
    role=${action ? 'button' : undefined} tabindex=${action ? '0' : undefined} style=${action ? 'cursor:pointer' : undefined}
    onClick=${action ? () => action() : undefined}
    onKeyDown=${action ? (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); action(); } } : undefined}></span>`;
}

function Stat({s}) {
  const cash = useCash();
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!s.deadline) return undefined;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [s.deadline]);
  let value = s.value, note = s.note, hot = false;
  if (s.deadline) {
    const left = new Date(s.deadline).getTime() - now;
    if (!isNaN(left)) { value = countdownText(left); hot = left > 0 && left < 6 * 3600000; }
  }
  // The balance moves between rebuilds, and every button is judged against it.
  if (s.value_id === 'kpi-cash' && typeof cash === 'number') { value = esMoney(cash); note = exact(cash); }
  const inner = html`<span class="k">${s.icon ? s.icon + ' ' : ''}${s.label}</span><span class="v" id=${s.value_id || undefined}
    title=${s.value_id === 'kpi-cash' && typeof cash === 'number' ? 'Tu saldo ahora mismo: ' + exact(cash) : undefined}>${value}${
    s.small ? html` <small>${s.small}</small>` : null}</span><span class="s">${note || ''}</span>`;
  return s.tab ? html`<button class=${'stat' + (hot ? ' hot' : '')} type="button" onClick=${() => goto(s.tab)}>${inner}</button>`
    : html`<div class=${'stat' + (hot ? ' hot' : '')}>${inner}</div>`;
}

function Header({page, meta}) {
  const n = useNav();
  const bar = useRef(null);
  useEffect(() => {
    const on = bar.current && bar.current.querySelector('.tab.on');
    // The strip scrolls, so the tab that just lit up can be outside it.
    if (on) on.scrollIntoView({block: 'nearest', inline: 'center'});
  }, [n.tab]);
  const click = (id) => {
    shutDrawer();
    history.pushState(null, '', '#' + id);
    routed = location.hash;
    showTab(id, {updateHash: false});
  };
  return html`<div class="topbar">${page.tabs && page.tabs.length ? html`<div class="tabs" id="tabs" role="tablist" ref=${bar}>${page.tabs.map((t) => html`
      <button class=${'tab' + (n.tab === t.id ? ' on' : '')} role="tab" data-tab=${t.id} aria-selected=${n.tab === t.id ? 'true' : 'false'}
        type="button" onClick=${() => click(t.id)}>${t.label}</button>`)}</div>` : null}
    <div class="topright"><${LiveDot} build=${meta.build}/>${meta.build ? html`<span class="build-tag">${meta.build}</span>` : null}<${Find}/></div>
  </div>
  ${page.stats && page.stats.length ? html`<div class="strip">${page.stats.map((s) => html`<${Stat} s=${s}/>`)}</div>` : null}`;
}

const MODE_CLASS = {auto: 'mode-auto', 'solo lectura': 'mode-read', informe: 'mode-read'};

function Foot({foot}) {
  const l = useLive();
  return html`<header class="topline"><h1>LaLiga Fantasy</h1>
    <p>${foot.generated}${foot.league ? html` · liga <strong>${foot.league}</strong>` : null} · jornada ${foot.week}</p>
    <span class="live"><span id="live-stamp">${l.stamp}</span></span>
    ${foot.mode ? html`<span class=${'mode ' + (MODE_CLASS[foot.mode] || 'mode-manual')} data-mode=${foot.mode}
      title="Que puede hacer este servidor: auto ejecuta las instrucciones permanentes, manual solo lo que pulses, solo lectura nada">Mode: <b>${foot.mode}</b></span>` : null}
  </header>
  <footer>Datos: API oficial de LaLiga Fantasy y futbolfantasy.com. <code>xPts</code> es una estimacion propia: puntos por jornada de la temporada pasada y de la actual (peso actual ${foot.weight}), ajustados por probabilidad de ser titular, dificultad del proximo rival y confianza del dato. <code>est.</code> marca a quien no tiene historico y se estima por precio. El barrido de valor a 7 dias es una proyeccion amortiguada, no una promesa. Herramienta de consulta: no ejecuta ninguna operacion.</footer>`;
}

// ---- the page --------------------------------------------------------------------------------

const UI = {};
export function registerUI(name, component) { UI[name] = component; }

function Section({s, n}) {
  let hidden = s.tab !== n.tab;
  // One rival squad at a time: twelve stacked is a lot of scrolling for a question about one.
  if (!hidden && s.id.startsWith('rival-')) {
    const rivals = (pageData().sections || []).filter((x) => x.id.startsWith('rival-')).map((x) => x.id);
    const choice = n.rival === 'all' || rivals.includes(n.rival) ? n.rival : rivals[0];
    hidden = choice !== 'all' && s.id !== choice;
  }
  const Own = s.ui && UI[s.ui];
  return html`<section id=${s.id} data-tab=${s.tab} class=${s.class || undefined} data-view=${s.view || undefined}
    data-ui=${s.ui || undefined} hidden=${hidden}>${Own ? html`<${Own}/>` : html`<${ViewScreen} name=${s.view}/>`}</section>`;
}

export function pickRival(id) { nav.rival = id; store(RIVAL_KEY, id); moved(); }
export const currentRival = () => {
  const rivals = (pageData().sections || []).filter((x) => x.id.startsWith('rival-')).map((x) => x.id);
  return nav.rival === 'all' || rivals.includes(nav.rival) ? nav.rival : rivals[0];
};

function App() {
  const page = useView('page') || {};
  const meta = useView('meta') || {};
  const n = useNav();
  return html`<${Header} page=${page} meta=${meta}/>
    ${(page.sections || []).map((s) => html`<${Section} key=${s.id} s=${s} n=${n}/>`)}
    ${page.foot ? html`<${Foot} foot=${page.foot}/>` : null}`;
}

// ---- the drawer: the card and the other views a name opens --------------------------------

const DRAWER = {};
// A view the drawer can show, addressable as #<tab>/<name>[/<arg>]; popup centres it like the card.
export function registerDrawer(name, component, {popup = false} = {}) {
  DRAWER[name] = component;
  DRAWER_VIEWS.add(name);
  if (popup) POPUPS.add(name);
}

// Opens a registered drawer view, with its own history entry so Back closes it.
export const openDrawer = (name, arg = '') => openView(name, arg);

function Drawer() {
  const n = useNav();
  const d = n.drawer;
  useEffect(() => {
    if (!d) return undefined;
    // A dialog opened from the card closes first; the card waits for its own Escape.
    const escape = (e) => { if (e.key === 'Escape' && !document.querySelector('.modal')) closeDrawer(); };
    document.addEventListener('keydown', escape);
    return () => document.removeEventListener('keydown', escape);
  }, [!!d]);
  const View = d && DRAWER[d.name];
  return html`<div class=${'drawer' + (d && POPUPS.has(d.name) ? ' as-pop' : '')} id="drawer" hidden=${!d} role="dialog" aria-modal="true"
      onClick=${(e) => { if (e.target === e.currentTarget) closeDrawer(); }}>
    <div class="drawer-panel">
      <button class="drawer-close" type="button" aria-label="Cerrar" onClick=${closeDrawer}>×</button>
      <div class="drawer-body" data-view=${d ? d.name : undefined}>${View ? html`<${View} key=${d.name + '/' + d.arg} arg=${d.arg} d=${d}/>` : null}</div>
    </div>
  </div>`;
}

// ---- the live connection ----------------------------------------------------------------

let currentVersion = null;
const EFFECT_LABELS = {cash: 'Saldo', squad: 'Jugadores', squad_value: 'Valor de la plantilla', listed: 'En el mercado',
  offers: 'Ofertas recibidas', points: 'Puntos de la plantilla', absences: 'Bajas'};
const OPERATION_LABELS = {sell_to_market: 'Puesto en venta', accept_offer: 'Oferta aceptada', decline_offer: 'Oferta rechazada',
  withdraw: 'Retirado del mercado', bid: 'Puja enviada', modify_bid: 'Puja modificada', cancel_bid: 'Puja cancelada',
  direct_offer: 'Oferta directa', pay_clause: 'Clausulazo pagado', raise_clause: 'Clausula subida', save_lineup: 'Alineacion guardada',
  policy: 'Instruccion ejecutada', shield_player: 'Jugador blindado', shield: 'Blindaje programado', traspaso: 'Se ha movido la liga',
  mercado: 'Cambios en el mercado', partido: 'Partido en juego', vencimiento: 'Ha vencido algo', refresco: 'Actualizado'};

// What an operation actually moves: the before and the after, not a "done".
function showEffect(message) {
  const rows = Object.entries(message.changed || {}).map(([key, change]) => {
    const money = key === 'cash' || key === 'squad_value';
    const show = (n) => money ? esMoney(Math.abs(n || 0)) : String(Math.abs(n ?? 0));
    const worse = key === 'absences';
    const sign = change.delta > 0 ? (worse ? 'down' : 'up') : (change.delta < 0 ? (worse ? 'up' : 'down') : '');
    return {label: EFFECT_LABELS[key] || key, before: show(change.before), after: show(change.after),
      delta: (change.delta > 0 ? '+' : change.delta < 0 ? '−' : '') + show(change.delta), sign};
  });
  if (rows.length) flash(OPERATION_LABELS[message.operation] || message.operation, null, rows);
}

function moveTo(version) {
  if (version === currentVersion) return;
  currentVersion = version;
  live.at = new Date();
  live.stamp = 'actualizado ' + new Date().toLocaleTimeString('es-ES');
  liveMoved();
  changed();
}

function connect() {
  const source = new EventSource('/api/events');
  source.onopen = () => { live.on = true; live.state = 'En vivo'; liveMoved(); };
  source.onmessage = (event) => {
    const message = JSON.parse(event.data);
    if (message.type === 'effect') showEffect(message);
    if (['effect', 'state', 'hello'].includes(message.type)) moveTo(message.version);
  };
  source.onerror = () => {
    live.on = false; live.state = 'Sin conexión: reintentando'; liveMoved();
    source.close();
    setTimeout(connect, 5000);
  };
}

// ---- start -------------------------------------------------------------------------------

// Faces, names and rows in the views open the player's card; a button inside them does its own.
document.addEventListener('click', (event) => {
  const target = event.target;
  if (!target.closest || target.closest('button, a, input, select, label')) return;
  const team = target.closest('.mk [data-team]');
  if (team && !target.closest('[data-pid]')) { openManager(team.dataset.team); return; }
  const host = target.closest('.mk [data-pid], .md-xi [data-pid]');
  if (host && host.dataset.pid) { event.preventDefault(); openDetail(host.dataset.pid); }
});

window.panelNav = nav;
window.panel = {openDetail, openManager, openWeek, openReach, openMatchday, openForecast, closeDrawer,
  shutDrawer, goto, usage, openCompare, labelDrawer, pickRival, currentRival, openDrawer,
  isView: (name) => DRAWER_VIEWS.has(name), routedTo: (hash) => { routed = hash; }};

export function start(appRoot) {
  render(html`<${App}/>`, appRoot);
  const drawerHost = document.createElement('div');
  document.body.appendChild(drawerHost);
  render(html`<${Drawer}/>`, drawerHost);
  addEventListener('popstate', route);
  // A plain <a href="#..."> link moves the hash without popstate in some browsers.
  addEventListener('hashchange', route);
  if (resolveTarget('#' + hashParts().base)) route();
  else {
    const saved = stored('fantasy-tab');
    showTab(TAB_ALIASES[saved] || saved || 'decidir');
  }
  if (window.EventSource && location.protocol.startsWith('http')) connect();
}
