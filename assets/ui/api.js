import {useState, useEffect} from './lib.js';

export async function getJSON(url) {
  const res = await fetch(url, {cache: 'no-store'});
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || String(res.status));
  return data;
}

export async function postJSON(url, body) {
  const res = await fetch(url, {method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(body)});
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || String(res.status));
  return data;
}

// The world's version, as the live refresh learns it, plus a local bump after a
// write of ours so what changed is asked for at once.
let stamp = 0;
const watchers = new Set();
const moved = () => { stamp += 1; watchers.forEach((watch) => watch(stamp)); refreshViews(); };
addEventListener('panel:version', moved);
export const changed = moved;

// Every tab the browser draws, as the page carried them and then as /api/views answers after
// each move of the world.
const seed = document.getElementById('views-data');
let views = seed ? JSON.parse(seed.textContent || '{}') : {};
// The balance every amount is judged against: the build's, then the live one.
let cash = views.meta && typeof views.meta.cash === 'number' ? views.meta.cash : null;
const viewWatchers = new Set();
let asking = null;
async function refreshViews() {
  const mine = asking = getJSON('/api/views');
  try {
    const data = await mine;
    if (asking !== mine) return;
    views = data.views || {};
    if (typeof data.cash === 'number') cash = data.cash;
    viewWatchers.forEach((watch) => watch(views));
  } catch (e) { /* the last good views stay on screen */ }
}

export function useCash() {
  const [, setAll] = useState(views);
  useEffect(() => { viewWatchers.add(setAll); setAll(views); return () => viewWatchers.delete(setAll); }, []);
  return cash;
}
export const getViews = () => views;

export function useMeta() {
  const [all, setAll] = useState(views);
  useEffect(() => { viewWatchers.add(setAll); setAll(views); return () => viewWatchers.delete(setAll); }, []);
  return all.meta || {};
}

export function useView(name) {
  const [all, setAll] = useState(views);
  useEffect(() => { viewWatchers.add(setAll); setAll(views); return () => viewWatchers.delete(setAll); }, []);
  return all[name];
}

export function useStamp() {
  const [value, setValue] = useState(stamp);
  useEffect(() => { watchers.add(setValue); setValue(stamp); return () => watchers.delete(setValue); }, []);
  return value;
}

// The two-step write: the server checks and hands back a summary and a single-use token, and
// only the token can confirm. Same calls as the bid modal.
export const prepare = (body) => postJSON('/api/bid/prepare', body);
export const confirm = (token) => postJSON('/api/bid/confirm', {token});

// The one dialog open at a time, drawn by ModalRoot.
let dialog = null;
const dialogWatchers = new Set();
export function openDialog(next) { dialog = next; dialogWatchers.forEach((watch) => watch(dialog)); }
export const closeDialog = () => openDialog(null);
export function useDialog() {
  const [value, setValue] = useState(dialog);
  useEffect(() => { dialogWatchers.add(setValue); setValue(dialog); return () => dialogWatchers.delete(setValue); }, []);
  return value;
}
