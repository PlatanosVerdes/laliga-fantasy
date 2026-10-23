import {html, useState, useEffect} from './lib.js';
import {Modal, Icon} from './components.js';
import {getJSON, postJSON, closeDialog, openDialog} from './api.js';

// The account's devices and the QR that brings a new one in, over the panel rather than on
// pages of their own. /vincular stays a page: the device typing the code is not signed in yet.

const LAPTOP = html`<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="5" width="16" height="11" rx="1.5"/><path d="M2 19h20"/></svg>`;
const PHONE = html`<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="7" y="2.5" width="10" height="19" rx="2"/><path d="M11 18.5h2"/></svg>`;

const countries = (() => { try { return new Intl.DisplayNames(['es'], {type: 'region'}); } catch (e) { return null; } })();
const country = (code) => { try { return code && countries ? countries.of(code) : code; } catch (e) { return code; } };
const placeText = (p = {}) => p.network === 'tailnet' ? 'Tailnet' : p.network === 'lan' ? 'Red local'
  : [p.city || p.region, country(p.country)].filter(Boolean).join(', ');
// The stamp moves at most every ten minutes, so anything fresher than that is now.
const ago = (stamp) => {
  const m = Math.floor((Date.now() - new Date(stamp)) / 60000);
  return m < 10 ? 'activo ahora' : m < 60 ? `hace ${m} min` : m < 1440 ? `hace ${Math.floor(m / 60)} h` : `hace ${Math.floor(m / 1440)} d`;
};

function DevicesDialog() {
  const [devices, setDevices] = useState(null);
  const [error, setError] = useState('');
  const load = () => getJSON('/api/devices').then((d) => setDevices(d.devices || []), () => setError('No he podido leer las sesiones.'));
  useEffect(() => { load(); }, []);
  const closeOne = (id) => postJSON('/api/devices', {close: id}).then(load, (e) => setError(e.message));
  const closeAll = () => postJSON('/api/logout', {everywhere: true}).then(() => { location.href = '/'; }, (e) => setError(e.message));
  return html`<${Modal} label="Dispositivos" onClose=${closeDialog}>
    <button type="button" class="modal-x" aria-label="Cerrar" onClick=${closeDialog}><${Icon} name="x"/></button>
    <h3>Dispositivos</h3>
    <p class="modal-lead">Dónde tienes abierto el panel. Cerrar uno lo saca al momento.</p>
    <div class="device-list">${devices == null ? html`<p class="empty">Cargando…</p>` : devices.map((d) => html`<div class="device-row">
      ${/iPhone|iPad|Android/.test(d.device || '') ? PHONE : LAPTOP}
      <span class="device-who"><b>${d.device || 'Dispositivo desconocido'}</b>
        <small>${[placeText(d.place), d.current ? 'activo ahora' : ago(d.seen)].filter(Boolean).join(' · ')}</small></span>
      ${d.current ? html`<span class="device-here">Este dispositivo</span>`
        : html`<button type="button" class="device-out" onClick=${() => closeOne(d.id)}>Cerrar</button>`}</div>`)}</div>
    ${error ? html`<p class="bid-error">${error}</p>` : null}
    <div class="modal-actions device-actions">
      <button type="button" onClick=${closeDialog}>Volver</button>
      <button type="button" class="device-all" onClick=${closeAll}>Cerrar todas las sesiones</button>
    </div>
  <//>`;
}

function LinkDialog() {
  const [link, setLink] = useState(null);
  const [state, setState] = useState({text: 'Creando el código…', tone: ''});
  const [round, setRound] = useState(0);
  useEffect(() => {
    let poll = null, tick = null, live = true;
    setLink(null); setState({text: 'Creando el código…', tone: ''});
    postJSON('/api/link', {}).then((data) => {
      if (!live) return;
      setLink(data);
      const until = Date.now() + data.expires_in * 1000;
      const left = () => {
        const s = Math.max(0, Math.round((until - Date.now()) / 1000));
        setState({text: `Un solo uso · caduca en ${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`, tone: ''});
      };
      left(); tick = setInterval(left, 1000);
      poll = setInterval(async () => {
        const answer = await postJSON('/api/link/status', {code: data.code}).catch(() => null);
        if (!answer || answer.status === 'waiting') return;
        clearInterval(poll); clearInterval(tick);
        setLink(null);
        setState(answer.status === 'linked' ? {text: '✓ Dispositivo vinculado.', tone: 'good'} : {text: 'El código ha caducado.', tone: 'bad', again: true});
      }, 2000);
    }, (e) => live && setState({text: e.message, tone: 'bad', again: true}));
    return () => { live = false; clearInterval(poll); clearInterval(tick); };
  }, [round]);
  return html`<${Modal} label="Vincular un dispositivo" onClose=${closeDialog}>
    <button type="button" class="modal-x" aria-label="Cerrar" onClick=${closeDialog}><${Icon} name="x"/></button>
    <h3>Vincular un dispositivo</h3>
    <p class="modal-lead">Escanéalo con la cámara del otro dispositivo, o abre <b>${link ? link.url.split('#')[0] : '/vincular'}</b> y escribe el código.</p>
    ${link ? html`<div class="link-qr" dangerouslySetInnerHTML=${{__html: link.qr}}></div><div class="link-code">${link.code}</div>` : null}
    <p class=${'link-state ' + state.tone}>${state.text}</p>
    <div class="modal-actions">
      ${state.again ? html`<button type="button" onClick=${() => setRound(round + 1)}>Otro código</button>` : null}
      <button type="button" onClick=${closeDialog}>Volver</button>
    </div>
  <//>`;
}

export const DEVICE_DIALOGS = {devices: DevicesDialog, link: LinkDialog};
let opened = 0;
export const openDevices = () => openDialog({kind: 'devices', key: ++opened});
export const openLink = () => openDialog({kind: 'link', key: ++opened});
