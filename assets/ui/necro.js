import {html, useState, useEffect} from './lib.js';
import {KINDS, RowView, Segs} from './view.js';
import {postJSON} from './api.js';

// The side game's list: every team a toggle, exactly two picked (a third drops the one picked
// first), and the two sent to the ballot through /api/necroporra/vote.
function NecroBlock({b}) {
  const n = b.data;
  const initial = n.teams.filter((t) => t.on).map((t) => t.id);
  const [order, setOrder] = useState(initial);
  const [msg, setMsg] = useState({text: '', cls: ''});
  const [busy, setBusy] = useState(false);
  useEffect(() => setOrder(n.teams.filter((t) => t.on).map((t) => t.id)), [n.teams.map((t) => t.id + t.on).join()]);
  const toggle = (id) => {
    if (order.includes(id)) setOrder(order.filter((x) => x !== id));
    else setOrder([...(order.length >= 2 ? order.slice(1) : order), id]);
  };
  const send = async () => {
    if (order.length !== 2) return;
    setBusy(true);
    setMsg({text: 'Enviando…', cls: ''});
    try {
      await postJSON('/api/necroporra/vote', {gw: n.gameweek, a: order[0], b: order[1]});
      setMsg({text: 'Voto enviado ✓', cls: 'ok'});
    } catch (e) { setMsg({text: String(e.message || e), cls: 'err'}); }
    finally { setBusy(false); }
  };
  return html`<div class="mk-box">
    ${n.facts.map((line) => html`<p class="lead"><${Segs} list=${line}/></p>`)}
    <ul class="rows">${n.teams.map((t) => {
      const on = order.includes(t.id);
      const acts = t.id ? [{text: false, label: on ? 'voto ✓' : 'votar', class: 'mb mb-ghost necro-pick' + (on ? ' on' : ''),
        do: 'necro', args: {id: t.id}, pressed: on, toggle}] : [];
      return html`<${RowView} r=${{...t.row, tone: on ? 'pick' : '', acts}}/>`;
    })}</ul>
    ${!n.open ? null : n.can_vote ? html`<div class="necro-actions"><span class=${('necro-msg ' + msg.cls).trim()}>${msg.text}</span>
      <button type="button" class="mb mb-primary necro-vote" disabled=${order.length !== 2 || busy} onClick=${send}>Enviar</button></div>`
      : html`<p class="mk-empty">Sin sesión de la necroporra configurada: solo predicción.</p>`}
  </div>`;
}
KINDS.necro = NecroBlock;
