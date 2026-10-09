import {html, useState, useEffect, legacy} from './lib.js';
import {Face, ShieldMark, Countdown, Crest, Empty} from './components.js';
import {KINDS} from './view.js';
import {getJSON, useStamp, useView} from './api.js';
import {dec, health} from './format.js';

// Partidos' own blocks, and the matchday popup.

// A face from the API's own player shape, with the crest behind it when there is no photo:
// the matchday and forecast answers carry no status, so no ring.
export function ApiFace({p, size = 'xs'}) {
  const h = health(p);
  return html`<span class=${'face face-' + size + (h ? ' ring-' + h.ring : '')}>${
    p.image ? html`<img src=${p.image} alt="" loading="lazy"/>` : html`<span class=${'crest crest-' + p.team_id}></span>`}</span>`;
}

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

KINDS.weeks = ({b}) => html`<ul class="calendar">${b.data.map((w) => html`<li class="mk-cal">
  <span class="mk-cal-day">${w.label}<span class="meta">${w.day}</span></span>
  <span class="mk-cal-body">${w.matches.map((m) => html`<span class="mk-cal-them"><${Crest} id=${m.local_id} known=${m.local_crest}/>${m.local}–${m.visitor}<${Crest} id=${m.visitor_id} known=${m.visitor_crest}/> <i>${m.count}</i></span>`)}<span class="meta">${w.yours}</span></span>
</li>`)}</ul>`;

let seasonCache = null;
async function season(week) {
  if (!seasonCache) {
    seasonCache = Promise.all([
      getJSON('/api/season').catch(() => null),
      getJSON('/api/forecast/' + week).catch(() => null)]);
  }
  return seasonCache;
}

// Previsto vs real: my points and place each matchday, and this matchday's forecast, as bars on
// one scale. Each matchday opens its popup.
function HistoryBlock({b}) {
  const {week, planned: plannedHere, hit} = b.data;
  const [data, setData] = useState(null);
  useEffect(() => { season(week).then(setData); }, [week]);
  let rows = html`<li class="mk-note">Cargando…</li>`, sub = '';
  if (data) {
    const [table, forecast] = data;
    const me = ((table || {}).managers || []).find((m) => m.is_me);
    const weeks = (table || {}).weeks || [];
    const planned = ((forecast || {}).mine || {}).planned || plannedHere;
    const real = weeks.map((w, i) => ({w, pts: me ? me.points[i] : null, rank: me && me.week_rank ? me.week_rank[i] : null}));
    const scale = Math.max(planned, ...real.map((r) => r.pts || 0)) * 1.05 || 1;
    const open = (w) => legacy().openWeek(w);
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
// Same scale as render.xptsClass for a forecast, and points as the card's strip reads them.
const ptsClass = (p) => p == null ? '' : p >= 8 ? 'x-hi' : p >= 4 ? 'x-mid' : p < 0 ? 'x-bad' : 'x-lo';
const fcClass = (v) => v >= 6 ? 'x-hi' : v >= 3.5 ? 'x-mid' : v >= 2 ? 'x-lo' : 'x-bad';
const DAYS = ['dom', 'lun', 'mar', 'mié', 'jue', 'vie', 'sáb'];
const MONTHS = ['ene', 'feb', 'mar', 'abr', 'may', 'jun', 'jul', 'ago', 'sep', 'oct', 'nov', 'dic'];

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
  const when = k ? DAYS[k.getDay()] + ' ' + k.getDate() + ' ' + MONTHS[k.getMonth()] : '';
  const page = legacy();
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
          const cls = future ? fcClass(f || 0) : ptsClass(p.points);
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
