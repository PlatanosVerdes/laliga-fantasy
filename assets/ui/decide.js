import {html, panel, useEffect, useRef} from './lib.js';
import {postJSON, changed} from './api.js';
import {leave, putBack, still} from './motion.js';
import {KINDS, ActView, ChipView, Segs, Seg} from './view.js';
import {Face, ShieldMark, PosTag, Empty, Countdown} from './components.js';

// Decidir's own pieces: the decision cards, the eleven they are about, and the way out to the
// other tabs.

// Saying no to a card is an instruction to the panel, not to LaLiga: one step, no confirmation.
async function dismiss(body, refresh = true) {
  try { await postJSON('/api/dismiss', body); if (refresh) changed(); }
  catch (e) { alert('No he podido descartarla: ' + e.message); throw e; }
}

// The card leaves first and the page is asked again only after, so the next card is not drawn
// into an element still sliding out.
async function dismissCard(event, key) {
  const card = event.currentTarget.closest('.card');
  const saved = dismiss({key}, false);
  await leave(card);
  try { await saved; changed(); } catch (e) { putBack(card); }
}

function Card({card, rank}) {
  const p = card.player;
  const faces = card.in
    ? html`<span class="swap"><${Face} p=${p} size="md"/><span class="arrow">→</span><${Face} p=${card.in} size="md"/></span>`
    : html`<${Face} p=${p} size="lg"/>`;
  return html`<article class=${'card tone-' + card.tone} data-kind=${card.kind}><div class="rank">${rank}</div>
    <div class="card-top">${faces}<div class="who" data-pid=${p.id}><b>${p.name}<${ShieldMark} p=${p}/></b>
      <span class="meta"><${PosTag} p=${p}/>${card.meta}</span></div>${(card.chips || []).map((c) => html`<${ChipView} c=${c}/>`)}${
      card.dismiss ? html`<button type="button" class="dismiss" aria-label="Descartar" data-tip="Descartar"
        onClick=${(event) => dismissCard(event, card.dismiss)}><svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4.5 4.5l7 7M11.5 4.5l-7 7"/></svg></button>` : null}</div>
    <div class="verb">${card.verb}</div><div class="big">${card.big}${card.big_note ? html`<span class=${'big-note ' + (card.note_class || '')}>${card.big_note}</span>` : null}</div>
    <ul class="why">${card.why.map((line) => html`<li>${line}</li>`)}</ul>
    <div class="card-foot">${card.buttons.map((a) => html`<${ActView} a=${a}/>`)}<span class="impact">${card.impact}</span></div>
  </article>`;
}

// The side game's nudge, while its round is open and you have not voted.
const Remind = ({r}) => html`<div class="necro-remind warn"><b>Necroporra J${r.week}</b>: aún no has votado${
  r.closes_at ? html` cierra en <b><${Countdown} kind="pill" until=${r.closes_at}/></b>` : null}. <${Segs} list=${r.tail}/></div>`;

// How many cards the restore brings back, until they are drawn: the click only asks, the cards
// arrive with the next refresh of the view.
let cascadeTo = 0;
const MAX_CARDS = 5;

function bringBack(data) {
  cascadeTo = Math.min(data.cards.length + data.hidden, MAX_CARDS);
  // A dismissed card may have expired meanwhile, and then fewer come back than were counted.
  setTimeout(() => { cascadeTo = 0; }, 5000);
  dismiss({restore: true}).catch(() => { cascadeTo = 0; });
}

// The brought-back cards fall into place one after another, top to bottom.
function useCascade(list, count) {
  useEffect(() => {
    if (!cascadeTo || count < cascadeTo || !list.current) return;
    cascadeTo = 0;
    if (still()) return;
    list.current.querySelectorAll('.card').forEach((card, i) => card.animate(
      [{opacity: 0, transform: 'translateY(-16px)'}, {opacity: 1, transform: 'none'}],
      {duration: 340, delay: i * 90, easing: 'cubic-bezier(.2,.7,.3,1)', fill: 'backwards'}));
  });
}

function Cards({b}) {
  const list = useRef(null);
  useCascade(list, b.data.cards.length);
  return html`${b.data.remind ? html`<${Remind} r=${b.data.remind}/>` : null}<div class="sec-head"><h2>Qué hacer ahora</h2></div>${b.data.cards.length
  ? html`<div class="cards" ref=${list}>${b.data.cards.map((card, i) => html`<${Card} key=${card.dismiss} card=${card} rank=${i + 1}/>`)}</div><p class="rest">${b.data.rest}</p>`
  : html`<${Empty}>${b.data.empty}<//>`}${b.data.hidden ? html`<p class="rest">${b.data.hidden === 1 ? 'Una descartada' : b.data.hidden + ' descartadas'} · <button type="button" class="linkish"
    onClick=${() => bringBack(b.data)}>recuperarlas</button></p>` : null}`;
}
Cards.full = true;
KINDS.cards = Cards;

KINDS.eleven = ({b}) => {
  const e = b.data;
  return html`<div class="pitchlist">
      <div class="total">${e.from ? html`<span class="from">${e.from}</span>` : null}<span class="to">${e.to}</span>${e.gain ? html`<span class="gain">${e.gain}</span>` : null}</div>
      ${e.lines.map((line) => html`<div class="line"><span class=${'pos pos-' + line.pos}>${line.pos.toUpperCase()}</span><div class="chips">${
        line.players.map((c) => html`<span class=${'tchip ' + c.class} data-pid=${c.player.id}><${Face} p=${c.player} size="xs"/>
          <span class="tname">${c.player.name}<${ShieldMark} p=${c.player}/></span><span class="tx">${c.xpts}</span>${
            c.pending ? html`<span class="fcs">${c.pending}</span>` : null}</span>`)}</div></div>`)}
      ${e.live ? html`<div class="mk-legend">puntos de la jornada: <span class="leg-pts pts-g">≥8</span> <span class="leg-pts pts-bl">4–7</span> <span class="leg-pts pts-br">0–3</span> <span class="leg-pts pts-rd">${'<0'}</span> · en gris, por jugar (sus xPts)</div>` : html`<div class="mk-legend">${e.fresh ? html`<span class="leg-new"></span>fichaje nuevo · ` : ''}xPts por jornada: <span class="x-hi-t">≥6</span> · <span class="x-lo-t">2–3,5</span> · <span class="x-bad-t">${'<2'}</span></div>`}
    </div>
    ${(e.warnings || []).map((w) => html`<p class="mk-note plan-warn" data-pid=${w.pid || undefined}>${w.t}</p>`)}
    ${e.place ? html`<p class="mk-note finish">Tu puesto previsto en la J${e.week}: <button class="linkish" type="button"
      onClick=${() => panel().goto('partidos')}><b>${e.place}º</b></button></p>` : null}`;
};

const More = ({b}) => html`<nav class="more"><span>Ver todo en</span>${b.data.map((a) => html`<${ActView} a=${{...a, class: ''}}/>`)}</nav>`;
More.full = true;
KINDS.more = More;
