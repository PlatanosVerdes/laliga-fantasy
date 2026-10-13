import {html, useState, useEffect, panel} from './lib.js';
import {Face, ShieldMark, Countdown, Crest, Empty, ApiFace, Modal} from './components.js';
import {KINDS} from './view.js';
import {getJSON, useStamp, useView} from './api.js';
import {dec, esDay, xClass} from './format.js';

// Partidos' own blocks, and the matchday popup.

KINDS.empty = ({b}) => html`<${Empty}>${b.empty}<//>`;

KINDS.fixtures = ({b}) => html`<ul class="fixtures">${b.data.lines.map((f) => html`<li class="fx">
    <span class="fx-when">${f.when}</span>
    <span class="fx-match"><${Crest} id=${f.local_id} known=${f.local_crest}/><b>${f.local} – ${f.visitor}</b><${Crest} id=${f.visitor_id} known=${f.visitor_crest}/>${
      f.score ? html` <span class="fx-score">${f.score}</span>` : null}</span>
    <span class="chips">${f.players.map((c) => html`<span class=${'tchip ' + c.class} data-pid=${c.player.id}>
      <${Face} p=${c.player} size="xs"/><span class="tname">${c.player.name}<${ShieldMark} p=${c.player}/></span><span class="tx">${c.xpts}</span></span>`)}</span>
  </li>`)}</ul>
  ${b.data.idle && b.data.idle.length ? html`<details class="fold"><summary>${b.data.idle.length} partidos sin tuyos</summary>
    <p class="mk-note">${b.data.idle.join(' · ')}</p></details>` : null}`;

KINDS.matchday = ({b}) => {
  const m = b.data;
  return html`<div class="matchday">
    <div><span class="k">Primer partido · se cierra tu alineación</span><${Countdown} until=${m.deadline} cls="v"/>
      <span class="s">${m.first} · ${m.first_when}</span></div>
    <div><span class="k">Último partido</span><span class="v">${m.last_when}</span>
      <span class="s">${m.last} · ${m.last_mine} tuyos</span></div>
    <div><span class="k">Tu previsión</span><span class="v">${m.total} xPts</span><span class="s">${m.average || ''}</span></div>
  </div>`;
};

// An agenda: each match on one line with its day and hour, the day written once for the matches
// that share it, and how many of mine play in it.
const Fixture = ({m, day}) => html`<li class="ag-row">
  <span class="ag-day">${day ? m.day : ''}</span><span class="ag-hour">${m.hour}</span>
  <span class="ag-match"><${Crest} id=${m.local_id} known=${m.local_crest}/>${m.local} – ${m.visitor}<${Crest} id=${m.visitor_id} known=${m.visitor_crest}/></span>
  <span class="ag-mine">${m.count || ''}</span></li>`;
KINDS.weeks = ({b}) => {
  const [open, setOpen] = useState(false);
  const {weeks, days} = b.data;
  return html`<div class="agenda">${weeks.map((w) => html`<section class="agenda-week">
    <h4><b>${w.label}</b>${w.yours ? html`<span class="meta">${w.yours}</span>` : null}</h4>
    <ul>${w.matches.map((m, i) => html`<${Fixture} key=${m.local_id + '-' + m.visitor_id} m=${m} day=${i === 0 || m.day !== w.matches[i - 1].day}/>`)}</ul>
  </section>`)}
  ${days && days.length ? html`<button type="button" class="mb mb-ghost ag-open" onClick=${() => setOpen(true)}>Ver calendario</button>` : null}
  ${open ? html`<${Modal} label="Calendario de jornadas" onClose=${() => setOpen(false)}><${SeasonCalendar} days=${days} onClose=${() => setOpen(false)}/><//>` : null}
  </div>`;
};

const MONTHS = ['enero', 'febrero', 'marzo', 'abril', 'mayo', 'junio', 'julio', 'agosto', 'septiembre',
  'octubre', 'noviembre', 'diciembre'];
const iso = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;

// A month at a time, each matchday's days marked in its own shade and labelled with its number,
// so a matchday spread over four days and a postponed match in midweek read at a glance.
function SeasonCalendar({days, onClose}) {
  const byDate = new Map();
  for (const d of days) byDate.set(d.date, [...(byDate.get(d.date) || []), d]);
  const today = iso(new Date());
  const [month, setMonth] = useState(() => { const t = new Date(); return new Date(t.getFullYear(), t.getMonth(), 1); });
  const first = days[0].date, last = days[days.length - 1].date;
  const lead = (month.getDay() + 6) % 7;
  const count = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate();
  const cells = [...Array(lead).fill(null), ...Array.from({length: count}, (_, i) => new Date(month.getFullYear(), month.getMonth(), i + 1))];
  const shift = (by) => setMonth(new Date(month.getFullYear(), month.getMonth() + by, 1));
  const canBack = iso(month) > first.slice(0, 8) + '01';
  const canNext = iso(new Date(month.getFullYear(), month.getMonth() + 1, 1)) <= last;
  // A matchday is the first day of its run that the month shows: its label goes there only.
  const labelled = new Set();
  return html`<div class="cal">
    <div class="cal-head">
      <button type="button" class="cal-nav" aria-label="Mes anterior" disabled=${!canBack} onClick=${() => shift(-1)}>‹</button>
      <b>${MONTHS[month.getMonth()]} ${month.getFullYear()}</b>
      <button type="button" class="cal-nav" aria-label="Mes siguiente" disabled=${!canNext} onClick=${() => shift(1)}>›</button>
    </div>
    <div class="cal-grid">${['L', 'M', 'X', 'J', 'V', 'S', 'D'].map((d) => html`<span class="cal-dow">${d}</span>`)}
      ${cells.map((date) => {
        if (!date) return html`<span></span>`;
        const key = iso(date), hits = byDate.get(key) || [];
        const tip = hits.map((h) => `J${h.week} · ${h.matches} ${h.matches === 1 ? 'partido' : 'partidos'}${h.mine ? ` · ${h.mine} ${h.mine === 1 ? 'tuyo' : 'tuyos'}` : ''}`).join('\n');
        const shade = hits.length ? ' w' + (hits[0].week % 4) : '';
        const done = hits.length && hits.every((h) => h.played === h.matches) ? ' done' : '';
        return html`<span class=${'cal-day' + shade + done + (key === today ? ' today' : '')} data-tip=${tip || undefined}>
          <i>${date.getDate()}</i>
          ${hits.map((h) => { const show = !labelled.has(h.week); labelled.add(h.week);
            return show ? html`<em>J${h.week}</em>` : null; })}
          ${hits.some((h) => h.mine) ? html`<s>${hits.reduce((n, h) => n + h.mine, 0)}</s>` : null}
        </span>`;
      })}</div>
    <p class="cal-key"><span class="cal-day w0 key"></span><span class="cal-day w1 key"></span><span class="cal-day w2 key"></span> un color por jornada ·
      <s class="key">2</s> tuyos que juegan</p>
    <div class="cal-foot"><button type="button" class="mb mb-ghost" onClick=${onClose}>Cerrar</button></div>
  </div>`;
}

// One fetch per matchday and world state; an answer with a failed half is shown but not kept.
const seasonCache = new Map();
function season(week, stamp) {
  const key = week + '@' + stamp;
  if (!seasonCache.has(key)) {
    seasonCache.clear();
    const load = Promise.all([
      getJSON('/api/season').catch(() => null),
      getJSON('/api/forecast/' + week).catch(() => null)]).then((got) => {
      if (got.includes(null) && seasonCache.get(key) === load) seasonCache.delete(key);
      return got;
    });
    seasonCache.set(key, load);
  }
  return seasonCache.get(key);
}

// Previsto vs real: my points and place each matchday, and this matchday's forecast, as bars on
// one scale. Each matchday opens its popup.
function HistoryBlock({b}) {
  const {week, planned: plannedHere, hit} = b.data;
  const stamp = useStamp();
  const [data, setData] = useState(null);
  useEffect(() => {
    let current = true;
    season(week, stamp).then((got) => current && setData((old) => (old && !got.some(Boolean) ? old : got)));
    return () => { current = false; };
  }, [week, stamp]);
  let rows = html`<li class="mk-note">Cargando…</li>`, sub = '';
  if (data) {
    const [table, forecast] = data;
    const me = ((table || {}).managers || []).find((m) => m.is_me);
    const weeks = (table || {}).weeks || [];
    const planned = ((forecast || {}).mine || {}).planned || plannedHere;
    const real = weeks.map((w, i) => ({w, pts: me ? me.points[i] : null, rank: me && me.week_rank ? me.week_rank[i] : null}));
    const scale = Math.max(planned, ...real.map((r) => r.pts || 0)) * 1.05 || 1;
    const open = (w) => panel().openWeek(w);
    rows = [...real.map((r) => r.pts == null
      ? html`<li class="hist"><span class="hj">J${r.w}</span><span class="bars"></span><span class="hv">—</span></li>`
      : html`<li class="hist" data-week=${r.w} tabindex="0" onClick=${() => open(r.w)}
          onKeyDown=${(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(r.w); } }}>
          <span class="hj">J${r.w}</span><span class="bars"><span class="bar real" style=${'width:' + Math.max(r.pts, 0) / scale * 100 + '%'}></span></span>
          <span class="hv">${r.pts}<i>${r.rank ? r.rank + 'º' : ''}</i></span></li>`),
    html`<li class="hist now" data-week=${week} tabindex="0" onClick=${() => open(week)}>
      <span class="hj">J${week}</span><span class="bars"><span class="bar fore" style=${'width:' + planned / scale * 100 + '%'}></span></span>
      <span class="hv">${dec(planned)}<i>prev.</i></span></li>`];
    const total = real.reduce((sum, r) => sum + (r.pts || 0), 0), played = real.filter((r) => r.pts != null).length;
    sub = played ? `${total} pts en ${played} jornadas` : '';
  }
  return html`<div class="block"><div class="sec-head"><h2>${b.title}</h2><p><span>${sub}</span></p></div>
    <ul class="history">${rows}</ul>
    <p class="mk-legend"><span class="sw real"></span>tus puntos y puesto · <span class="sw fore"></span>previsto · toca una jornada para verla entera</p>
    ${hit ? html`<p class="mk-note">${hit}</p>` : null}</div>`;
}
HistoryBlock.full = true;
KINDS.history = HistoryBlock;

const XI_LINES = [['striker', 'DEL'], ['midfield', 'MED'], ['defender', 'DEF'], ['goalkeeper', 'POR']];
const POS = {1: 'POR', 2: 'DEF', 3: 'MED', 4: 'DEL'};
// Points as the card's strip colours them; a forecast uses the xPts scale.
const ptsClass = (p) => p == null ? '' : p >= 8 ? 'x-hi' : p >= 4 ? 'x-mid' : p < 0 ? 'x-bad' : 'x-lo';

// A matchday, manager by manager: the eleven each fielded with what every player scored, or for
// a matchday still to come, what each eleven is expected to score.
export function WeekPopup({week}) {
  const view = useView('partidos');
  const stamp = useStamp();
  const [state, setState] = useState(null);
  useEffect(() => {
    let current = true;
    Promise.all([getJSON('/api/matchday/' + week).catch(() => null), getJSON('/api/forecast/' + week).catch(() => null)])
      .then(([md, fc]) => current && setState({md, fc}));
    return () => { current = false; };
  }, [week, stamp]);
  if (!state) return html`<p class="empty">Cargando…</p>`;
  const {md, fc} = state;
  if (!md || !md.managers) return html`<p class="empty">No he podido leer esa jornada.</p>`;
  const history = ((view || {}).aside || []).find((b) => b.kind === 'history');
  const current = history ? history.data.week : 0;
  const future = current && +week >= current;
  const forecasts = {}, plans = {};
  [...((fc || {}).managers || []), ...((fc || {}).mine ? [fc.mine] : [])].forEach((m) => {
    plans[m.team_id] = m.planned;
    (m.players || []).forEach((p) => { forecasts[p.id] = p.forecast; });
  });
  const score = (m) => future ? (plans[m.team_id] || 0) : (m.week_points || 0);
  const managers = md.managers.slice().sort((a, b) => score(b) - score(a));
  const k = md.kickoff ? new Date(md.kickoff) : null;
  const when = k ? esDay(k) : '';
  const page = panel();
  return html`<h3 class="pc-title">J${week}${future ? ' · previsión de cada once' : when ? ' · ' + when : ''}</h3>
    <p class="drawer-note" style="margin:0 0 8px">Toca un manager para ver su once${future ? '' : '; en pequeño, lo previsto si se guardó'}.</p>
    ${managers.map((m, i) => {
      const lineup = m.lineup || {};
      const lines = XI_LINES.map(([key, label]) => {
        let players = (lineup[key] || []).filter(Boolean);
        if (!m.lineup) players = (m.squad || []).filter((p) => POS[p.position_id] === label);
        if (!players.length) return null;
        return html`<div class="line"><span class=${'pos pos-' + label.toLowerCase()}>${label}</span><div class="chips">${players.map((p) => {
          const f = forecasts[p.id];
          const value = future ? (f != null ? dec(f) : '–') : (p.points ?? '–');
          const cls = future ? xClass(f || 0) : ptsClass(p.points);
          return html`<span class=${'tchip ' + cls} data-pid=${p.id}><${ApiFace} p=${p}/><span class="tname">${p.name}</span><span class="tx">${value}</span>${
            !future && f != null ? html` <span class="fcs">${dec(f)}</span>` : null}</span>`;
        })}</div></div>`;
      });
      const total = future ? html`${dec(score(m))} <span class="mf">previsto</span>` : html`${m.week_points ?? '–'} <span class="mf">pts</span>`;
      return html`<details class=${'md-row' + (m.is_me ? ' me' : '')} open=${!!m.is_me}><summary><span class="rk">${i + 1}º</span>
        <span>${m.manager}</span><span class="mp">${total}</span></summary><div class="md-xi">${lines}</div></details>`;
    })}
    <p class="drawer-note"><button class="j-squads" type="button" onClick=${() => page.openMatchday(week)}>plantillas</button>${
      fc && !future ? html` <button class="j-squads" type="button" onClick=${() => page.openForecast(week)}>previsión</button>` : null}</p>`;
}
