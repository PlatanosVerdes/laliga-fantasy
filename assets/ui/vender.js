import {html, useState, useEffect} from './lib.js';
import {Row, Block, Empty, Button, Countdown} from './components.js';
import {esMoney, esNum, esRatio} from './format.js';
import {useResource} from './api.js';
import {confirmOp, runCardAction, toggleAlways} from './actions.js';

const ratioClass = (ratio) => ratio >= 1 ? 'up' : 'down';

function OfferRow({offer}) {
  const p = offer.player;
  const op = (name) => () => confirmOp({op: name, name: p.name, player_id: p.id,
    market_id: offer.market_id, offer_id: offer.offer_id, amount: offer.amount || null});
  // Both buttons, always Aceptar then Rechazar; only the recommended one is filled.
  const tip = 'recomendado: ' + offer.why;
  const actions = html`
    <${Button} tone=${offer.take ? 'primary' : 'outline'} tip=${offer.take ? tip : undefined} onClick=${op('accept_offer')}>Aceptar<//>
    <${Button} tone=${offer.take ? 'outline' : 'primary'} tip=${offer.take ? undefined : tip} onClick=${op('decline_offer')}>Rechazar<//>`;
  const note = html`<span class=${ratioClass(offer.ratio)}>${offer.ratio < 1 ? '▼' : '▲'} ${esRatio(offer.ratio)}</span> vale ${esMoney(offer.value)}${offer.drop > 0 ? ' · once −' + esNum(offer.drop, 1) : ''}`;
  return html`<${Row} p=${p} value=${esMoney(offer.amount)} note=${note} why=${offer.facts}
    chip=${offer.expires ? html`<${Countdown} kind="chip" until=${offer.expires} label="caduca"/>` : null}
    actions=${actions} tone=${offer.tone}/>`;
}

function ListedRow({item}) {
  const note = html`<span class=${ratioClass(item.ratio)} title="lo que pides frente a su valor">${esRatio(item.ratio)}</span>${item.best > 0 ? ' · mejor ' + esMoney(item.best) : ''}`;
  return html`<${Row} p=${item.player} value=${esMoney(item.asking)} note=${note}
    chip=${item.expires ? html`<${Countdown} kind="chip" until=${item.expires} label="cierra"/>` : null}
    actions=${html`<${Button} onClick=${() => runCardAction(item.player.id, 'withdraw')}>Quitar<//>`}/>`;
}

function RestRow({item}) {
  const p = item.player;
  const [always, setAlways] = useState(item.always);
  const [busy, setBusy] = useState(false);
  useEffect(() => setAlways(item.always), [item.always]);
  const flip = async () => {
    setBusy(true);
    setAlways(!always);
    try { setAlways(await toggleAlways(p)); } catch (e) { setAlways(always); alert('No he podido cambiarlo: ' + e.message); }
    finally { setBusy(false); }
  };
  const trend = item.trend;
  const note = html`${esMoney(item.value)} · <span class=${trend < 0 ? 'down' : 'up'}>${trend < 0 ? '' : '+'}${esNum(trend, 1)} %</span> 7d`;
  const sell = item.locked_why
    ? html`<span data-tip=${item.locked_why}><${Button} disabled>Poner en venta<//></span>`
    : html`<${Button} onClick=${() => runCardAction(p.id, 'sell_to_market')}>Poner en venta<//>`;
  const toggle = html`<${Button} className="act" on=${always} disabled=${busy} onClick=${flip}
    tip="Lo mantiene en venta; importes y venta automática, en su ficha">${always ? '● ' : ''}Siempre en mercado<//>`;
  return html`<${Row} p=${p} value=${esNum(item.xpts, 1) + ' xPts'} note=${note} actions=${html`${sell}${toggle}`}/>`;
}

function Rest({rest}) {
  if (!rest.length) return null;
  const bench = rest.filter((item) => !item.starter), eleven = rest.filter((item) => item.starter);
  return html`<${Block} title="El resto de tu plantilla" count=${rest.length} sub="lo que no tienes en venta">
    <ul class="rows">
      ${bench.length ? html`<li class="line-head">Fuera de tu once · ${bench.length}</li>` : null}
      ${bench.map((item) => html`<${RestRow} key=${item.player.id} item=${item}/>`)}
      ${eleven.length ? html`<li class="line-head xi-head">En tu once · ${eleven.length} · venderlos baja tus xPts</li>` : null}
      ${eleven.map((item) => html`<${RestRow} key=${item.player.id} item=${item}/>`)}
    </ul><//>`;
}

function Always({rules}) {
  return html`<${Block} title="Siempre en mercado" count=${rules.length}>
    ${rules.length ? html`<ul class="rows">${rules.map((rule) => html`<${Row} key=${rule.player.id} p=${rule.player}
        value=${rule.amount > 0 ? esMoney(rule.amount) : ''} note=${rule.terms} why=${rule.why}/>`)}</ul>`
      : html`<${Empty}>Ninguna regla activa: se arma con «Siempre en mercado».<//>`}
    <p class="mk-note">Solo lo mantiene en venta. Para que se venda solo, fija «aceptar desde» o marca la venta automática en su ficha; si no, una buena oferta solo avisa.</p><//>`;
}

// Vender, drawn from /api/view/vender and asked for again whenever the world moves.
export function Vender({initial}) {
  const {data} = useResource('/api/view/vender', {initial});
  const view = (data || {}).view;
  if (!view) return null;
  return html`<div class="layout"><div class="main">
      <${Block} title="Ofertas recibidas" count=${view.offers.length}>
        ${view.offers.length ? html`<ul class="rows">${view.offers.map((offer) => html`<${OfferRow} key=${offer.offer_id} offer=${offer}/>`)}</ul>`
          : html`<${Empty}>Ninguna oferta ahora mismo.<//>`}<//>
      <${Rest} rest=${view.rest}/>
    </div><aside class="side">
      <${Block} title="En venta ahora" count=${view.listed.length}>
        ${view.listed.length ? html`<ul class="rows">${view.listed.map((item) => html`<${ListedRow} key=${item.player.id} item=${item}/>`)}</ul>`
          : html`<${Empty}>No tienes a nadie en venta.<//>`}<//>
      <${Always} rules=${view.always}/>
    </aside></div>`;
}
