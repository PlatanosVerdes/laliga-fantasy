import {html, legacy} from './lib.js';
import {KINDS, ActView, ChipView, Segs, Seg} from './view.js';
import {Face, ShieldMark, PosTag, Empty} from './components.js';

// Decidir's own pieces: the decision cards, the eleven they are about, and the way out to the
// other tabs.

function Card({card, rank}) {
  const p = card.player;
  const faces = card.in
    ? html`<span class="swap"><${Face} p=${p} size="md"/><span class="arrow">→</span><${Face} p=${card.in} size="md"/></span>`
    : html`<${Face} p=${p} size="lg"/>`;
  return html`<article class=${'card tone-' + card.tone} data-kind=${card.kind}><div class="rank">${rank}</div>
    <div class="card-top">${faces}<div class="who" data-pid=${p.id}><b>${p.name}<${ShieldMark} p=${p}/></b>
      <span class="meta"><${PosTag} p=${p}/>${card.meta}</span></div>${(card.chips || []).map((c) => html`<${ChipView} c=${c}/>`)}</div>
    <div class="verb">${card.verb}</div><div class="big">${card.big}${card.big_note ? html`<span class=${'big-note ' + (card.note_class || '')}>${card.big_note}</span>` : null}</div>
    <ul class="why">${card.why.map((line) => html`<li>${line}</li>`)}</ul>
    <div class="card-foot">${card.buttons.map((a) => html`<${ActView} a=${a}/>`)}<span class="impact">${card.impact}</span></div>
  </article>`;
}

const Cards = ({b}) => html`<div class="sec-head"><h2>Qué hacer ahora</h2></div>${b.data.cards.length
  ? html`<div class="cards">${b.data.cards.map((card, i) => html`<${Card} card=${card} rank=${i + 1}/>`)}</div><p class="rest">${b.data.rest}</p>`
  : html`<${Empty}>${b.data.empty}<//>`}`;
Cards.full = true;
KINDS.cards = Cards;

KINDS.eleven = ({b}) => {
  const e = b.data;
  return html`<div class="pitchlist">
      <div class="total">${e.from ? html`<span class="from">${e.from}</span>` : null}<span class="to">${e.to}</span>${e.gain ? html`<span class="gain">${e.gain}</span>` : null}</div>
      ${e.lines.map((line) => html`<div class="line"><span class=${'pos pos-' + line.pos}>${line.pos.toUpperCase()}</span><div class="chips">${
        line.players.map((c) => html`<span class=${'tchip ' + c.class} data-pid=${c.player.id}><${Face} p=${c.player} size="xs"/>
          <span class="tname">${c.player.name}<${ShieldMark} p=${c.player}/></span><span class="tx">${c.xpts}</span></span>`)}</div></div>`)}
      <div class="mk-legend">${e.fresh ? '● fichaje nuevo · ' : ''}xPts por jornada: <span class="x-hi-t">≥6</span> · <span class="x-lo-t">2–3,5</span> · <span class="x-bad-t">${'<2'}</span></div>
    </div>
    ${(e.warnings || []).map((w) => html`<p class="mk-note plan-warn" data-pid=${w.pid || undefined}>${w.t}</p>`)}
    ${e.place ? html`<p class="mk-note finish">Tu puesto previsto en la J${e.week}: <button class="linkish" type="button" data-wired="1"
      onClick=${() => legacy().goto('partidos')}><b>${e.place}º</b></button></p>` : null}`;
};

const More = ({b}) => html`<nav class="more"><span>Ver todo en</span>${b.data.map((a) => html`<${ActView} a=${{...a, class: ''}}/>`)}</nav>`;
More.full = true;
KINDS.more = More;
