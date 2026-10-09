// Figures the Spanish way. Go writes most of them before they get here (render/cards.go:
// esMoney, esNum, esWhen, xptsClass); what is formatted on both sides has to read the same.

const DAYS = ['dom', 'lun', 'mar', 'mié', 'jue', 'vie', 'sáb'];
const MONTHS = ['ene', 'feb', 'mar', 'abr', 'may', 'jun', 'jul', 'ago', 'sep', 'oct', 'nov', 'dic'];

export const dots = (whole) => String(whole).replace(/\B(?=(\d{3})+(?!\d))/g, '.');

export function esMoney(amount) {
  let sign = '';
  if (amount < 0) { sign = '−'; amount = -amount; }
  if (amount >= 999500) return sign + (amount / 1e6).toFixed(1).replace('.', ',') + 'M';
  if (amount >= 1e3) return sign + dots(Math.round(amount / 1e3)) + 'K';
  return sign + Math.round(amount) + ' €';
}

const parse = (stamp) => { const when = new Date(stamp); return isNaN(when) ? null : when; };
const pad = (n) => String(n).padStart(2, '0');

export function esWhen(stamp) {
  const when = parse(stamp);
  if (!when) return stamp || '';
  return `${DAYS[when.getDay()]} ${when.getDate()} ${MONTHS[when.getMonth()]} ` +
    `${pad(when.getHours())}:${pad(when.getMinutes())}`;
}

export function esDay(stamp) {
  const when = parse(stamp);
  return when ? `${DAYS[when.getDay()]} ${when.getDate()} ${MONTHS[when.getMonth()]}` : '';
}

// The xPts colour scale, render.xptsClass's thresholds.
export const xClass = (v) => v >= 6 ? 'x-hi' : v >= 3.5 ? 'x-mid' : v >= 2 ? 'x-lo' : 'x-bad';

// The card's own helpers.
export const dec = (v, d = 1) => v == null || isNaN(v) ? '—'
  : Number(v).toFixed(d).replace('.', ',').replace('-', '−');
export const mny = (v) => v == null || isNaN(v) ? '—'
  : Math.abs(v) >= 1e6 ? dec(v / 1e6) + 'M' : Math.abs(v) >= 1e3 ? Math.round(v / 1e3) + 'K' : String(v);
export const signed = (v, d = 1) => (v >= 0 ? '+' : '−') + dec(Math.abs(v), d);
export const exact = (n) => n == null ? '—' : Number(n).toLocaleString('es-ES') + ' €';
export const group = (n) => (n == null || isNaN(n)) ? '' : Number(n).toLocaleString('es-ES');
export const digits = (s) => parseInt(String(s).replace(/[^0-9]/g, ''), 10);
export const fmt = (n) => n == null ? '—'
  : (Math.abs(n) >= 1e6 ? (n / 1e6).toFixed(2) + 'M' : Math.abs(n) >= 1e3 ? (n / 1e3).toFixed(0) + 'K' : String(n));

export function whenShort(stamp) {
  const t = parse(stamp);
  if (!t) return '';
  const hm = pad(t.getHours()) + ':' + pad(t.getMinutes());
  return t - Date.now() < 6 * 86400000 ? `${DAYS[t.getDay()]} ${hm}`
    : `${DAYS[t.getDay()]} ${t.getDate()} ${MONTHS[t.getMonth()]} ${hm}`;
}

export function stampText(stamp) {
  const when = parse(stamp);
  return when ? `${pad(when.getDate())}/${pad(when.getMonth() + 1)} ${pad(when.getHours())}:${pad(when.getMinutes())}` : '';
}

export function since(stamp) {
  const gone = Date.now() - new Date(stamp).getTime();
  if (isNaN(gone) || gone < 0) return '—';
  const h = Math.floor(gone / 3600000), m = Math.floor(gone % 3600000 / 60000);
  return h >= 24 ? 'hace ' + Math.floor(h / 24) + 'd ' + (h % 24) + 'h'
    : h > 0 ? 'hace ' + h + 'h ' + pad(m) + 'm' : 'hace ' + m + 'm';
}

// A countdown in the units the page uses everywhere: days and hours far off, seconds at the end.
export function countdownText(left) {
  if (left <= 0) return 'ya';
  const h = Math.floor(left / 3600000), m = Math.floor(left % 3600000 / 60000), s = Math.floor(left % 60000 / 1000);
  return h >= 24 ? Math.floor(h / 24) + 'd ' + (h % 24) + 'h'
    : h > 0 ? h + 'h ' + pad(m) + 'm' : m + 'm ' + pad(s) + 's';
}

// A row's chip: "4 h 04 min", "cerrado" once past.
export function chipText(left) {
  const minutes = Math.round(left / 60000), d = Math.floor(minutes / 1440),
    h = Math.floor(minutes % 1440 / 60), m = minutes % 60;
  return left <= 0 ? 'cerrado' : d ? `${d} d ${h} h` : h ? `${h} h ${pad(m)} min` : `${m} min`;
}

// The card's status line, in its own words: an injury that is only a doubt is "Tocado".
export function health(player) {
  const st = player.status || 'ok', a = player.absence || {};
  if (st === 'suspended' || st === 'sanctioned' || a.kind === 'sancionado')
    return {ring: 'out', glyph: 'card', label: 'Sancionado'};
  if (st === 'injured' || (a.kind === 'lesionado' && Number(a.severity) === 0 && a.severity != null))
    return {ring: 'out', glyph: 'cross', label: 'Lesionado'};
  if (st === 'doubtful' || a.kind === 'lesionado' || a.kind === 'duda')
    return {ring: 'doubt', glyph: a.kind === 'lesionado' ? 'cross' : '', label: a.kind === 'lesionado' ? 'Tocado' : 'Duda'};
  return null;
}
