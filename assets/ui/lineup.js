import {html, render, useState, useEffect, useRef, legacy} from './lib.js';
import {ApiFace, ShieldMark} from './components.js';
import {getJSON, postJSON, useStamp} from './api.js';
import {dec, mny, whenShort, health} from './format.js';

// The lineup editor: the saved eleven from /api/lineup on a pitch, the bench beside it, drag
// and drop (or lift and tap on a touchscreen), formations, the best eleven the server worked out,
// and Guardar through the same POST /api/lineup.

const LINE_ORDER = ['striker', 'midfield', 'defender', 'goalkeeper'];   // top to bottom
const LINE_LABEL = {goalkeeper: 'POR', defender: 'DEF', midfield: 'MED', striker: 'DEL'};
const LINE_POS = {goalkeeper: 1, defender: 2, midfield: 3, striker: 4};
const LINE_WORD = {goalkeeper: ['portero', 'porteros'], defender: ['defensa', 'defensas'],
  midfield: ['medio', 'medios'], striker: ['delantero', 'delanteros']};
const POS_WORD = {1: 'portero', 2: 'defensa', 3: 'medio', 4: 'delantero'};
// Same scale as render.xptsClass.
const xClass = (v) => v >= 6 ? 'x-hi' : v >= 3.5 ? 'x-mid' : v >= 2 ? 'x-lo' : 'x-bad';
// Only a confirmed absence keeps a starter from scoring; a doubt or a knock still plays.
const cannotPlay = (p) => !!p && (p.available === false || (health(p) || {}).ring === 'out');

// How a reserve stands for selling: the hold rule first, then what is already on the table.
function sellState(p) {
  if (p.sale_locked && p.hold_until) return `🔒 hasta ${whenShort(p.hold_until)}`;
  if (p.best_offer) return `oferta de ${mny(p.best_offer)}`;
  if (p.listed_for) return `en venta por ${mny(p.listed_for)}`;
  return 'se puede vender';
}

const asShield = (p) => ({shielded: p.shielded, shielded_when: p.shielded_until ? whenShort(p.shielded_until) : ''});

function Lineup() {
  const stamp = useStamp();
  const [, setTick] = useState(0);
  const redraw = () => setTick((n) => n + 1);
  const st = useRef({state: null, dirty: false, failed: false, lifted: null, dragged: null,
    justDragged: false, status: '', warn: false, saving: false}).current;

  const load = async () => {
    try { st.state = await getJSON('/api/lineup'); st.failed = false; } catch (e) { st.failed = true; }
    st.dirty = false; st.status = ''; st.lifted = null;
    redraw();
  };
  // A rebuild must not throw away unsaved changes: the pitch is reloaded only when clean.
  useEffect(() => { if (!st.state || !st.dirty) load(); }, [stamp]);

  if (st.failed) return html`<${Frame} st=${st}><div class="pitch" id="pitch"><p class="slot empty" style="width:auto">Solo disponible en la version servida</p></div><//>`;
  const s = st.state;
  if (!s) return html`<${Frame} st=${st}><div class="pitch" id="pitch"></div><//>`;

  const flash = (message) => {
    st.status = message; st.warn = true; redraw();
    setTimeout(() => { st.warn = false; st.status = ''; redraw(); }, 2600);
  };
  const takeFrom = (source) => {
    if (source.from === 'bench') {
      const i = s.bench.findIndex((p) => p && String(p.id) === String(source.id));
      return i < 0 ? null : s.bench.splice(i, 1)[0];
    }
    const arr = s.lines[source.line];
    const player = arr[source.index]; arr[source.index] = null;
    return player;
  };
  const dropOnSlot = (moving, line, index) => {
    if (moving.from === 'pitch' && moving.line === line && moving.index === index) return;
    const target = s.lines[line][index] || null;
    const player = takeFrom(moving);
    if (!player) return;
    // A line takes only its own position.
    if (player.position_id !== LINE_POS[line]) {
      if (moving.from === 'bench') s.bench.push(player); else s.lines[moving.line][moving.index] = player;
      flash(`${player.name} es ${POS_WORD[player.position_id]}, no puede jugar de ${LINE_WORD[line][0]}.`);
      return;
    }
    s.lines[line][index] = player;
    if (target) {
      if (moving.from === 'bench') s.bench.push(target); else s.lines[moving.line][moving.index] = target;
    }
    st.dirty = true; redraw();
  };
  const dropOnBench = (moving) => {
    if (moving.from === 'bench') return;
    const player = takeFrom(moving);
    if (player) s.bench.push(player);
    st.dirty = true; redraw();
  };
  const place = (target) => {
    if (!st.lifted) return false;
    const moving = st.lifted;
    st.lifted = null; st.status = '';
    if (target.bench) dropOnBench(moving); else dropOnSlot(moving, target.line, target.index);
    redraw();
    return true;
  };
  const applyFormation = (text) => {
    const [d, m, k] = text.split(',').map(Number);
    const want = {goalkeeper: 1, defender: d, midfield: m, striker: k};
    const spare = [];
    LINE_ORDER.forEach((line) => {
      const arr = s.lines[line] || [];
      while (arr.length > want[line]) { const p = arr.pop(); if (p) spare.push(p); }
      while (arr.length < want[line]) arr.push(null);
      s.lines[line] = arr;
    });
    // Fill the holes with reserves of that position, the rest to the bench.
    LINE_ORDER.forEach((line) => {
      s.lines[line] = s.lines[line].map((slot) => {
        if (slot) return slot;
        const pool = spare.concat(s.bench);
        const i = pool.findIndex((p) => p && p.position_id === LINE_POS[line]);
        if (i < 0) return null;
        const chosen = pool[i];
        const inSpare = spare.indexOf(chosen);
        if (inSpare >= 0) spare.splice(inSpare, 1); else s.bench.splice(s.bench.indexOf(chosen), 1);
        return chosen;
      });
    });
    s.bench = s.bench.concat(spare);
    s.formation = [d, m, k];
    st.dirty = true; redraw();
  };
  const applyBest = () => {
    const best = s.best;
    if (!best) return;
    const everyone = [...LINE_ORDER.flatMap((l) => (s.lines[l] || []).filter(Boolean)), ...(s.bench || [])];
    const byId = Object.fromEntries(everyone.map((p) => [String(p.id), p]));
    const used = new Set();
    const lines = {};
    LINE_ORDER.forEach((l) => {
      lines[l] = (best.lines[l] || []).map((id) => { used.add(String(id)); return byId[String(id)] || null; });
    });
    s.lines = lines;
    s.bench = everyone.filter((p) => !used.has(String(p.id)));
    s.formation = best.formation;
    st.dirty = true;
    (legacy().usage || {click() {}}).click('alineacion', 'poner el mejor once');
    redraw();
  };
  const save = async () => {
    if (LINE_ORDER.some((l) => (s.lines[l] || []).some((p) => !p))) {
      flash('Hay huecos sin cubrir: completa el once antes de guardar.');
      return;
    }
    const ids = (l) => s.lines[l].map((p) => p.player_team_id);
    st.saving = true; redraw();
    try {
      const data = await postJSON('/api/lineup', {goalkeeper: ids('goalkeeper')[0], defender: ids('defender'),
        midfield: ids('midfield'), striker: ids('striker'), formation: s.formation});
      st.dirty = false;
      s.formation = data.formation || s.formation;
      st.status = 'guardada ' + new Date().toLocaleTimeString('es-ES');
    } catch (err) { flash('No se ha guardado: ' + err.message); }
    finally { st.saving = false; redraw(); }
  };

  // What drags: who and from where.
  const source = (p, from, line, index) => ({id: String(p.id), from, line, index});
  const draggable = (p, from, line, index) => ({
    draggable: true,
    onDragStart: (e) => {
      st.dragged = source(p, from, line, index);
      e.currentTarget.classList.add('dragging');
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', String(p.id));
    },
    onDragEnd: (e) => {
      e.currentTarget.classList.remove('dragging'); st.dragged = null;
      st.justDragged = true; setTimeout(() => { st.justDragged = false; }, 250);
      document.querySelectorAll('.drop-target').forEach((n) => n.classList.remove('drop-target'));
    },
    // Dragging and clicking start the same way, so a drop must not open the card.
    onClick: (e) => {
      if (e.target.closest('.slot-grip')) return;
      if (place(from === 'bench' ? {bench: true} : {line, index})) return;
      if (st.justDragged) return;
      legacy().openDetail(p.id);
    },
  });
  const target = (where) => ({
    onDragOver: (e) => { e.preventDefault(); e.currentTarget.classList.add('drop-target'); },
    onDragLeave: (e) => e.currentTarget.classList.remove('drop-target'),
    onDrop: (e) => {
      e.preventDefault(); e.currentTarget.classList.remove('drop-target');
      if (!st.dragged) return;
      if (where.bench) dropOnBench(st.dragged); else dropOnSlot(st.dragged, where.line, where.index);
    },
  });
  // A touchscreen emits no dragstart, so the grip lifts a player and the next tap drops him.
  const grip = (p, from, line, index) => html`<button class="slot-grip" type="button" title="Mover"
    onClick=${(e) => {
      e.stopPropagation();
      if (st.lifted && st.lifted.id === String(p.id)) { st.lifted = null; st.status = ''; }
      else { st.lifted = source(p, from, line, index); st.status = 'Toca el hueco donde va'; }
      redraw();
    }}>⇅</button>`;
  const liftedHere = (p) => st.lifted && st.lifted.id === String(p.id) ? ' lifted' : '';

  const shirt = (p, line, index) => {
    if (!p) return html`<div class="slot empty gap" data-line=${line} data-index=${index} title="No tienes con quien cubrir esta plaza"
      ...${target({line, index})} onClick=${() => place({line, index})}>⚠<br/>${LINE_LABEL[line]}<br/>sin cubrir</div>`;
    return html`<div class=${'slot tokslot' + liftedHere(p)} data-line=${line} data-index=${index} data-player=${p.id}
      data-pt=${p.player_team_id} title=${p.name + (p.next_rival ? ' · vs ' + p.next_rival + (p.next_home ? ' (en casa)' : ' (fuera)') : '')}
      ...${draggable(p, 'pitch', line, index)} ...${target({line, index})}>
      ${grip(p, 'pitch', line, index)}${p.listed_for ? html`<span class="tok-flag" title=${'en venta por ' + mny(p.listed_for)}><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M2.2 8.6V3.2a1 1 0 0 1 1-1h5.4l5.2 5.2a1 1 0 0 1 0 1.4l-4.4 4.4a1 1 0 0 1-1.4 0z"/><circle cx="5.4" cy="5.4" r="1.2"/></svg></span>` : null}
      <${ApiFace} p=${p} size="md"/>
      <span class="tok-name">${p.name}<${ShieldMark} p=${asShield(p)}/></span>
      <span class=${'tok-x ' + xClass(p.xpts || 0)}>${dec(p.xpts || 0)}</span>
    </div>`;
  };
  const benchItem = (p) => {
    const pos = {1: 'POR', 2: 'DEF', 3: 'MED', 4: 'DEL'}[p.position_id] || 'ENT';
    return html`<div class=${'bench-item' + liftedHere(p)} data-player=${p.id} data-pt=${p.player_team_id} data-from="bench" title=${p.name}
      ...${draggable(p, 'bench')}>
      ${grip(p, 'bench')}<${ApiFace} p=${p} size="sm"/>
      <span class="bench-who"><span class="bench-name">${p.name}<${ShieldMark} p=${asShield(p)}/></span>
        <span class="bench-sell">${sellState(p)}</span></span>
      <span class=${'pos pos-' + pos.toLowerCase()}>${pos}</span>
      <span class=${'tx ' + xClass(p.xpts || 0)}>${dec(p.xpts || 0)}</span>
    </div>`;
  };

  const all = [...(s.formations.free || []), ...(s.formations.premium || [])];
  const current = (s.formation || []).join(',');
  const savedXPts = LINE_ORDER.reduce((sum, l) => sum + (s.lines[l] || []).reduce((t, p) => t + (p ? (p.xpts || 0) : 0), 0), 0);
  const status = st.status || (st.dirty ? 'cambios sin guardar' : (s.writes_enabled ? '' : 'servidor en solo lectura'));
  return html`<${Frame} st=${st} formation=${(s.formation || []).join('-')}
      best=${s.best && s.best.xpts - savedXPts >= 0.05 ? html`Tu mejor once suma <b>${dec(s.best.xpts)}</b>; el que tienes, <b>${dec(savedXPts)}</b>. <button type="button" class="pitch-apply" onClick=${applyBest}>Poner el mejor</button>` : null}
      select=${html`<select id="pitch-formation-select" value=${current} onChange=${(e) => applyFormation(e.currentTarget.value)}>${all.map((f) => {
        const premium = (s.formations.premium || []).includes(f);
        return html`<option value=${f}>${f.replace(/,/g, '-')}${premium ? ' (premium)' : ''}</option>`;
      })}</select>`}
      status=${status} onReset=${load} onSave=${save} canSave=${st.dirty && s.writes_enabled && !st.saving} saving=${st.saving}
      alert=${alert(s, applyFormation)}>
    <div class="pitch" id="pitch">${LINE_ORDER.map((line) => html`<div class="pitch-line" data-line=${line}>${(s.lines[line] || []).map((p, i) => shirt(p, line, i))}</div>`)}</div>
    <aside class="bench" id="bench" ...${target({bench: true})} onClick=${() => place({bench: true})}>
      <h3>Banquillo</h3>
      <div class="bench-list" id="bench-list">${(s.bench || []).length ? s.bench.map(benchItem) : html`<p class="bench-empty">Sin reservas</p>`}</div>
    </aside>
  <//>`;
}

// A hole in the eleven is points not played, so it is said at the top with the way out beside
// it: the formation that fits the players who can play, if any. Eleven shirts with a suspended
// player among them are ten players and one named hole.
function alert(s, applyFormation) {
  const holes = []; let missing = 0; const idle = [], doubts = [];
  LINE_ORDER.forEach((line) => {
    const slots = s.lines[line] || [];
    slots.forEach((p) => { if (cannotPlay(p)) idle.push(p); else if (p && health(p)) doubts.push(p); });
    const empty = slots.filter((p) => !p).length;
    if (!empty) return;
    missing += empty;
    holes.push(`${empty} ${LINE_WORD[line][empty > 1 ? 1 : 0]}`);
  });
  const doubtLine = doubts.length ? html`<span class="pitch-doubt">${doubts.map((p, i) => html`${i ? ' · ' : ''}<b>${p.name}</b>: ${health(p).label.toLowerCase()}${p.start_probability != null ? ` (${p.start_probability} %)` : ''}`)}</span>` : null;
  if (!missing && !idle.length) return doubtLine ? {soft: true, body: doubtLine} : null;
  const have = {1: 0, 2: 0, 3: 0, 4: 0}, can = {1: 0, 2: 0, 3: 0, 4: 0};
  const tally = (p) => { if (!p) return; have[p.position_id]++; if (!cannotPlay(p)) can[p.position_id]++; };
  LINE_ORDER.forEach((line) => (s.lines[line] || []).forEach(tally));
  (s.bench || []).forEach(tally);
  const squad = have[1] + have[2] + have[3] + have[4];
  const playable = can[1] + can[2] + can[3] + can[4];
  const fits = (f) => { const [d, m, k] = f.split(',').map(Number); return can[1] >= 1 && can[2] >= d && can[3] >= m && can[4] >= k; };
  const free = (s.formations.free || []).find(fits);
  const premium = free ? null : (s.formations.premium || []).find(fits);
  const option = free || premium;
  const shape = (s.formation || []).join('-');
  const parts = [];
  if (missing) parts.push(html`<span>⚠ <b>Once incompleto</b>: el ${shape} pide 11 y sales con ${11 - missing}. Falta${missing > 1 ? 'n' : ''} ${holes.join(' y ')}.</span>`);
  if (idle.length) {
    parts.push(html`<span>⚠ ${idle.map((p, i) => html`${i ? ', ' : ''}<b>${p.name}</b>${health(p) ? ` (${health(p).label.toLowerCase()})` : ''}`)} en el campo sin poder jugar: esa plaza no puntua.</span>`);
  }
  if (option) {
    parts.push(html`<span>Con <b>${option.replace(/,/g, '-')}</b>${premium ? ' (premium)' : ''} cuadras el once sin contar a quien no puede jugar.</span>`,
      html`<button type="button" onClick=${() => applyFormation(option)}>Cambiar a ${option.replace(/,/g, '-')}</button>`);
  } else if (playable < 11) {
    parts.push(html`<span>Hoy solo pueden jugar ${playable} de tus ${squad}: ninguna formacion cuadra el once, y cambiarla no lo arregla. Toca fichar.</span>`);
  } else {
    parts.push(html`<span>Ninguna formacion cuadra con ${squad} jugadores: toca fichar.</span>`);
  }
  return {soft: false, body: html`${parts}${doubtLine}`};
}

// The section's frame, the same markup the page used to carry as a static piece.
function Frame({st, formation = '', best, select, status, onReset, onSave, canSave, saving, alert, children}) {
  return html`<div class="block"><div class="sec-head"><h2>Alineación<span class="count" id="pitch-formation">${formation}</span></h2>
      <p><span class="on-mouse">arrastra entre el campo y el banquillo</span><span class="on-touch">levanta con ⇅ y toca el hueco</span></p></div>
    <p class="pitch-best" id="pitch-best" hidden=${!best}>${best}</p>
    <div class="pitch-bar">
      <label>Formación ${select || html`<select id="pitch-formation-select"></select>`}</label>
      <span id="pitch-status" class="kpi-label" style=${st.warn ? 'color:var(--warning)' : undefined}>${status || ''}</span>
      <button id="pitch-reset" type="button" onClick=${onReset}>Descartar cambios</button>
      <button id="pitch-save" class="primary" type="button" disabled=${!canSave} onClick=${onSave}>${saving ? 'Guardando…' : 'Guardar alineación'}</button>
    </div>
    <p class=${'pitch-alert' + (alert && alert.soft ? ' soft' : '')} id="pitch-alert" hidden=${!alert}>${alert ? alert.body : null}</p>
    <div class="pitch-wrap">${children}</div></div>`;
}

export function mountLineup(section) {
  section.textContent = '';
  render(html`<${Lineup}/>`, section);
}
