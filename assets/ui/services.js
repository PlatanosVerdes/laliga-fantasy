import {html, useState, useEffect, useRef} from './lib.js';
import {getJSON, postJSON} from './api.js';
import {since, countdownText} from './format.js';
import {registerDrawer, openDrawer, onLiveDot} from './shell.js';

// The server's clocks: how often it probes, rebuilds, follows a match and beats the live
// channel. The live dot opens them as a popup at #<tab>/servicios.

const SOURCE_TIP = {env: (s) => 'de la variable ' + s.env, flag: () => 'de --interval al arrancar',
  default: () => 'el valor por defecto'};

// One native time field per row read as a duration, the way the shield dialog asks for an hour:
// 00:02 is two minutes, 01:00 an hour.
const pad = (n) => String(n).padStart(2, '0');
const clockOf = (minutes) => `${pad(Math.floor(minutes / 60))}:${pad(minutes % 60)}`;
const minutesOf = (clock) => {
  const [h, m] = clock.split(':').map(Number);
  return isNaN(h) || isNaN(m) ? null : h * 60 + m;
};

function Interval({s, onSaved}) {
  const shown = clockOf(Math.round(s.seconds / 60));
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
      setText(clockOf(Math.round(next.seconds / 60)));
      onSaved(list);
    } catch (e) { setError(e.message); }
  };
  const commit = (value) => {
    clearTimeout(timer.current);
    if (value === shown) return;
    const minutes = value ? minutesOf(value) : null;
    if (minutes == null) { setText(shown); return; }
    save({key: s.key, interval: minutes + 'm'});
  };
  // The field changes on every segment typed or stepped: one write once it settles.
  const changed = (e) => {
    const value = e.currentTarget.value;
    setText(value);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => commit(value), 400);
  };
  return html`<span class="svc-dur" data-tip=${`entre ${s.min} y ${s.max} · ${s.env}`}>
      <input class=${'svc-in' + (error ? ' bad' : '')} type="time" step="60" value=${text}
        min=${clockOf(Math.ceil(s.min_seconds / 60))} max=${clockOf(Math.floor(s.max_seconds / 60))}
        aria-label=${s.label + ', horas y minutos'}
        onInput=${(e) => setText(e.currentTarget.value)} onChange=${changed}
        onKeyDown=${(e) => { if (e.key === 'Enter') { e.preventDefault(); commit(e.currentTarget.value); } }}/>
      <span class="svc-exact">${s.seconds % 60 ? s.interval : ''}</span></span>
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
  useEffect(() => {
    let live = true;
    const load = () => getJSON('/api/services').then((data) => live && setList(data.services),
      (e) => live && setError('No he podido leer los servicios: ' + e.message));
    load();
    const ask = setInterval(load, 15000);
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => { live = false; clearInterval(ask); clearInterval(tick); };
  }, []);
  return html`<div class="svc">
    <h3>Servicios</h3>
    <p class="svc-sub">Cada cuánto trabaja el servidor. Se aplica al momento, sin reiniciar.</p>
    ${error ? html`<p class="bid-error">${error}</p>` : null}
    ${list ? html`<ul class="svc-list">${list.filter((s) => s.editable).map((s) => html`<li class="svc-row" key=${s.key}>
        <span class="svc-name">${s.label} <i class="aw-i" data-tip=${s.description}>ⓘ</i></span>
        <${Interval} s=${s} onSaved=${setList}/>
        <${When} s=${s} now=${now}/>
      </li>`)}</ul>` : error ? null : html`<p class="svc-sub">Cargando…</p>`}
  </div>`;
}

registerDrawer('servicios', ServicesView, {popup: true});
onLiveDot(() => openDrawer('servicios'));
