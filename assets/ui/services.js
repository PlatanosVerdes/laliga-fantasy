import {html, useState, useEffect} from './lib.js';
import {getJSON, postJSON} from './api.js';
import {since, countdownText} from './format.js';
import {registerDrawer, openDrawer, onLiveDot} from './shell.js';

// The server's clocks: how often it probes, rebuilds, follows a match and beats the live
// channel. The live dot opens them as a popup at #<tab>/servicios.

const SOURCE_TIP = {env: (s) => 'de la variable ' + s.env, flag: () => 'de --interval al arrancar',
  default: () => 'el valor por defecto'};

function Interval({s, onSaved}) {
  const [text, setText] = useState(s.interval);
  const [error, setError] = useState('');
  useEffect(() => setText(s.interval), [s.interval]);
  const save = async (body) => {
    setError('');
    try {
      const list = (await postJSON('/api/services', body)).services;
      setText((list.find((x) => x.key === s.key) || s).interval);
      onSaved(list);
    } catch (e) { setError(e.message); }
  };
  const commit = () => {
    const typed = text.trim();
    if (!typed || typed === s.interval) { setText(s.interval); return; }
    save({key: s.key, interval: typed});
  };
  const bounds = `entre ${s.min} y ${s.max} · ${s.env}`;
  return html`<input class=${'svc-in' + (error ? ' bad' : '')} type="text" autocomplete="off" spellcheck="false"
      value=${text} aria-label=${s.label} data-tip=${bounds} placeholder=${s.fallback}
      onInput=${(e) => setText(e.currentTarget.value)} onBlur=${commit}
      onKeyDown=${(e) => { if (e.key === 'Enter') { e.preventDefault(); e.currentTarget.blur(); } }}/>
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
    ${list ? html`<ul class="svc-list">${list.map((s) => html`<li class="svc-row" key=${s.key}>
        <span class="svc-name">${s.label} <i class="aw-i" data-tip=${s.description}>ⓘ</i></span>
        <${Interval} s=${s} onSaved=${setList}/>
        <${When} s=${s} now=${now}/>
      </li>`)}</ul>` : error ? null : html`<p class="svc-sub">Cargando…</p>`}
    <p class="modal-note">Admite 90s, 5m o 1h. Lo que pongas aquí manda sobre la variable de entorno, y esta sobre el valor por defecto.</p>
  </div>`;
}

registerDrawer('servicios', ServicesView, {popup: true});
onLiveDot(() => openDrawer('servicios'));
