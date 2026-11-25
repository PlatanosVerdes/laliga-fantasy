import {html, useState, useEffect} from './lib.js';
import {postJSON} from './api.js';
import {Icon} from './components.js';

// Asking LaLiga now instead of at the next tick: the button, a pull down from the top, and a tab
// that comes back. What changed still arrives over SSE; this only knows when the check is over.
const state = {busy: false, done: ''};
const watchers = new Set();
const moved = () => watchers.forEach((watch) => watch({...state}));

export async function checkNow() {
  if (state.busy) return;
  state.busy = true; state.done = ''; moved();
  let done = 'error';
  try {
    const answer = await postJSON('/api/check', {wait: true});
    done = answer.pending ? 'pending' : answer.rebuilt ? 'moved' : 'same';
  } catch (e) { /* the button says it failed; the page stays as it was */ }
  state.busy = false; state.done = done; moved();
  setTimeout(() => { if (state.done === done) { state.done = ''; moved(); } }, 2500);
}

function useCheck() {
  const [value, setValue] = useState({...state});
  useEffect(() => { watchers.add(setValue); setValue({...state}); return () => watchers.delete(setValue); }, []);
  return value;
}

const TIPS = {same: 'Sin cambios en LaLiga', moved: 'Actualizado', pending: 'Sigue comprobando',
  error: 'No se ha podido comprobar'};

export function CheckButton() {
  const c = useCheck();
  const tip = c.busy ? 'Comprobando LaLiga…' : TIPS[c.done] || 'Comprobar LaLiga ahora';
  return html`<button class=${'check-btn' + (c.busy ? ' busy' : '') + (c.done ? ' ' + c.done : '')} type="button"
    aria-label=${tip} data-tip=${tip} disabled=${c.busy} onClick=${checkNow}>
    <${Icon} name=${c.done === 'same' || c.done === 'moved' ? 'check' : 'refresh'}/></button>`;
}

// ---- pull down from the top, on touch screens ---------------------------------------------
const PULL = 70;
const blocked = (target) => document.querySelector('#drawer:not([hidden]), .modal:not([hidden])') ||
  (target.closest && target.closest('.pitch, .bench, input, textarea, select'));

function scrolledInside(target) {
  for (let el = target; el && el !== document.body; el = el.parentElement) {
    if (el.scrollTop > 0) return true;
  }
  return false;
}

function installPull() {
  const hint = document.createElement('div');
  hint.className = 'pull-hint';
  hint.innerHTML = '<svg class="ic" aria-hidden="true"><use href="#i-refresh"></use></svg>';
  document.body.appendChild(hint);
  let startY = null, pulled = 0;
  const show = (distance) => {
    hint.style.transform = `translate(-50%, ${Math.min(distance, PULL * 1.4) - 44}px) rotate(${distance * 3}deg)`;
    hint.style.opacity = String(Math.min(1, distance / PULL));
    hint.classList.toggle('ready', distance >= PULL);
  };
  const reset = () => { startY = null; pulled = 0; hint.style.transform = ''; hint.style.opacity = ''; hint.classList.remove('ready'); };
  watchers.add((s) => hint.classList.toggle('busy', s.busy));
  document.addEventListener('touchstart', (e) => {
    if (e.touches.length !== 1 || scrollY > 0 || state.busy || blocked(e.target) || scrolledInside(e.target)) return;
    startY = e.touches[0].clientY;
  }, {passive: true});
  document.addEventListener('touchmove', (e) => {
    if (startY === null) return;
    pulled = (e.touches[0].clientY - startY) / 2;
    if (pulled <= 0 || scrollY > 0) { reset(); return; }
    show(pulled);
  }, {passive: true});
  const end = () => {
    if (startY === null) return;
    const go = pulled >= PULL;
    reset();
    if (go) checkNow();
  };
  document.addEventListener('touchend', end);
  document.addEventListener('touchcancel', reset);
}

// ---- a tab that comes back ----------------------------------------------------------------
// Opening the panel after the official app's notification is the moment the data has to be new.
// The server answers a check with the last probe when it is seconds old, so these cost nothing.
const AWAY = 20000;
function installReturn() {
  let hiddenAt = 0;
  const quiet = () => postJSON('/api/check', {wait: false}).catch(() => {});
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) { hiddenAt = Date.now(); return; }
    if (hiddenAt && Date.now() - hiddenAt >= AWAY) quiet();
  });
  quiet();
}

export function installChecks() {
  if (!location.protocol.startsWith('http')) return;
  if ('ontouchstart' in window) installPull();
  installReturn();
}
