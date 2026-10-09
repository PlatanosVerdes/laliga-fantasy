import {html, useState, useEffect, useRef} from './lib.js';
import {getJSON, postJSON} from './api.js';
import {since, countdownText} from './format.js';
import {registerDrawer, openDrawer, onLiveDot} from './shell.js';

// The server's clocks: how often it probes, rebuilds, follows a match and beats the live
// channel. The live dot opens them as a popup at #<tab>/servicios.

const SOURCE_TIP = {env: (s) => 'de la variable ' + s.env, flag: () => 'de --interval al arrancar',
  default: () => 'el valor por defecto'};

// One native number field per row, in whole minutes: the registry rounds every value to the
// minute, so the number shown is the interval that runs.
function Interval({s, onSaved}) {
  const low = Math.ceil(s.min_seconds / 60), high = Math.floor(s.max_seconds / 60);
  const shown = String(Math.round(s.seconds / 60));
  const [text, setText] = useState(shown);
  const [error, setError] = useState('');
  const timer = useRef(null);
  useEffect(() => setText(shown), [s.seconds]);
  useEffect(() => () => clearTimeout(timer.current), []);
  const save = async (body) => {
    clearTimeout(timer.current);
    setError('');
    try {
      const list = (await postJSON('/api/services', body)).services;
      const next = list.find((x) => x.key === s.key) || s;
      setText(String(Math.round(next.seconds / 60)));
      onSaved(list);
    } catch (e) {
      // The field goes back to what runs; the message says why the new value was refused.
      setText(shown);
      setError(e.message);
    }
  };
  const commit = (value) => {
    clearTimeout(timer.current);
    const typed = value.trim();
    if (typed === shown) return;
    const minutes = parseInt(typed, 10);
    if (isNaN(minutes)) { setText(shown); setError(''); return; }
    save({key: s.key, interval: minutes + 'm'});
  };
  // A run of clicks on the stepper adds up to one write.
  const changed = (e) => {
    const value = e.currentTarget.value;
    setText(value);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => commit(value), 400);
  };
  return html`<span class="svc-dur" data-tip=${`entre ${s.min} y ${s.max} · ${s.env}`}>
      <input class=${'svc-in' + (error ? ' bad' : '')} type="number" inputmode="numeric"
        min=${low} max=${high} step="1" value=${text} aria-label=${s.label + ', en minutos'}
        onInput=${(e) => { setText(e.currentTarget.value); setError(''); }} onChange=${changed}
        onKeyDown=${(e) => { if (e.key === 'Enter') { e.preventDefault(); commit(e.currentTarget.value); } }}/>
      <span class="svc-unit">min</span></span>
    ${s.source === 'ui'
      ? html`<button type="button" class="svc-reset" data-tip=${'vuelve a ' + s.fallback + ', ' + SOURCE_TIP[s.fallback_source](s)}
          onMouseDown=${(e) => e.preventDefault()} onClick=${() => save({key: s.key, reset: true})}>por defecto</button>`
      : html`<span class="svc-src" data-tip=${SOURCE_TIP[s.source](s)}>${s.source === 'default' ? '' : s.source === 'env' ? 'env' : '--interval'}</span>`}
    ${error ? html`<span class="svc-err">${error}</span>` : null}`;
}

function When({s, now}) {
  const next = s.next_run ? new Date(s.next_run).getTime() - now : null;
  const bits = [s.last_run ? since(s.last_run) : null, next != null ? 'en ' + countdownText(next) : null].filter(Boolean);
  return html`<span class="svc-when">${bits.join(' · ')}</span>`;
}

function ServicesView() {
  const [list, setList] = useState(null);
  const [error, setError] = useState('');
  const [now, setNow] = useState(Date.now());
  const reload = useRef(() => {});
  useEffect(() => {
    let live = true;
    const load = () => getJSON('/api/services').then((data) => live && setList(data.services),
      (e) => live && setError('No he podido leer los servicios: ' + e.message));
    reload.current = load;
    load();
    const ask = setInterval(load, 15000);
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => { live = false; clearInterval(ask); clearInterval(tick); };
  }, []);
  // The engine replans just after a change, so the next run is asked for again a moment later.
  const saved = (next) => { setList(next); setTimeout(() => reload.current(), 600); };
  return html`<div class="svc">
    <h3>Servicios</h3>
    <p class="svc-sub">Cada cuánto trabaja el servidor. Se aplica al momento, sin reiniciar.</p>
    ${error ? html`<p class="bid-error">${error}</p>` : null}
    ${list ? html`<ul class="svc-list">${list.filter((s) => s.editable).map((s) => html`<li class="svc-row" key=${s.key}>
        <span class="svc-name">${s.label} <i class="aw-i" data-tip=${s.description}>ⓘ</i></span>
        <${Interval} s=${s} onSaved=${saved}/>
        <${When} s=${s} now=${now}/>
      </li>`)}</ul>` : error ? null : html`<p class="svc-sub">Cargando…</p>`}
  </div>`;
}

registerDrawer('servicios', ServicesView, {popup: true});
onLiveDot(() => openDrawer('servicios'));
