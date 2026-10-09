import {useState, useEffect, useRef} from './lib.js';

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

// The world's version, as the live refresh in report.js learns it, plus a local bump after a
// write of ours so what changed is asked for at once.
let stamp = 0;
const watchers = new Set();
const moved = () => { stamp += 1; watchers.forEach((watch) => watch(stamp)); };
addEventListener('panel:version', moved);
export const changed = moved;

export function useStamp() {
  const [value, setValue] = useState(stamp);
  useEffect(() => { watchers.add(setValue); return () => watchers.delete(setValue); }, []);
  return value;
}

// A JSON resource that is asked for again whenever the world moves. The last good answer stays
// on screen while the next one is on its way.
export function useResource(url, {live = true, initial = null} = {}) {
  const tick = useStamp();
  const [state, setState] = useState({data: initial, error: null});
  const fresh = useRef(initial != null);
  useEffect(() => {
    if (fresh.current) { fresh.current = false; return undefined; }
    let current = true;
    getJSON(url).then((data) => current && setState({data, error: null}),
      (error) => current && setState((before) => ({data: before.data, error})));
    return () => { current = false; };
  }, [url, live ? tick : 0]);
  return state;
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
  useEffect(() => { dialogWatchers.add(setValue); return () => dialogWatchers.delete(setValue); }, []);
  return value;
}
