import {html, useState, useEffect, panel} from './lib.js';
import {KINDS, list, Segs, ActView, RowView} from './view.js';
import {Empty} from './components.js';
import {useView} from './api.js';

// Rivales' own pieces: the picker of rival squads, a squad with its full table, the table, and
// the popup of whom a rival's cash reaches.

KINDS.rivalpick = ({b}) => html`<div class="pick-bar"><label>Equipo<select id="rival-pick" value=${panel().currentRival()}
    onChange=${(e) => panel().pickRival(e.currentTarget.value)}>${
  b.data.map((o) => html`<option value=${o.value}>${o.label}</option>`)}</select></label></div>`;

KINDS.squad = ({b}) => html`${list(b.rows, b.scroll)}<details class="fold"><summary>tabla completa</summary><${TableView} t=${b.data}/></details>`;

// The kinds that sort as numbers; any other column sorts as text.
const NUMERIC = new Set(['money', 'pct', 'num', 'num1', 'int', 'pct_plain', 'spark', 'verdict', 'mag',
  'ideal', 'hours', 'ratio', 'live_points', 'waiting', 'projection']);

export function TableView({t}) {
  const [order, setOrder] = useState(null);
  if (!t.rows.length) return html`<p class="empty">Sin jugadores</p>`;
  let rows = t.rows;
  if (order) {
    const numeric = NUMERIC.has(t.cols[order.col].kind);
    rows = rows.slice().sort((a, b) => {
      const x = a.cells[order.col].sort, y = b.cells[order.col].sort;
      const cmp = numeric ? parseFloat(x || 0) - parseFloat(y || 0) : String(x).localeCompare(String(y), 'es');
      return order.desc ? -cmp : cmp;
    });
  }
  const sortBy = (col, event) => {
    const desc = !(order && order.col === col && order.desc);
    setOrder({col, desc});
    const section = event.currentTarget.closest('section[id]');
    (panel().usage || {sort() {}}).sort(section ? section.id : '', t.cols[col].label || t.cols[col].kind);
  };
  return html`<div class=${'table-wrap' + (t.sticky ? ' sticky-first' : '')}><table class="sortable">
    <thead><tr>${t.cols.map((c, i) => html`<th data-kind=${c.kind} onClick=${(e) => sortBy(i, e)}
      class=${[c.num ? 'right' : '', c.wide ? 'wide-only' : '', order && order.col === i ? (order.desc ? 'sorted-desc' : 'sorted-asc') : ''].filter(Boolean).join(' ') || undefined}>${c.label}</th>`)}</tr></thead>
    <tbody>${rows.map((r) => html`<tr class=${r.me ? 'row-me' : undefined}>${r.cells.map((c) => html`<td class=${c.c || undefined} data-sort=${c.sort}>
      <${Segs} list=${c.segs}/>${(c.acts || []).map((a) => html`<${ActView} a=${a}/>`)}</td>`)}</tr>`)}</tbody>
  </table></div>`;
}

export function ReachPopup({team}) {
  const view = useView('rivales');
  const reach = view && view.main[0] && (view.main[0].data || {})[team];
  if (!reach) return html`<p class="empty">Cargando…</p>`;
  return html`<h3 class="pc-title">${reach.title}</h3><div class="mk reach">
    <p class="mk-note"><${Segs} list=${reach.note}/></p>
    ${reach.rows.length ? html`<ul class="rows">${reach.rows.map((r) => html`<${RowView} r=${r}/>`)}</ul>`
      : html`<${Empty}>${reach.empty}<//>`}</div>`;
}
