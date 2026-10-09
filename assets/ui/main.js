import {html, render, legacy} from './lib.js';
import {getJSON} from './api.js';
import {ModalRoot} from './components.js';
import {PlayerPopup} from './player.js';
import {Vender} from './vender.js';

const dialogs = document.createElement('div');
document.body.appendChild(dialogs);
render(html`<${ModalRoot}/>`, dialogs);

// The card is drawn into a host of its own inside the drawer: every other drawer view still
// writes the drawer's HTML whole, and that has to take the card's tree down with it.
let host = null;
const unmount = () => { if (host) { render(null, host); host = null; } };

export function mountPlayer(body, id, from) {
  unmount();
  body.textContent = '';
  host = document.createElement('div');
  body.appendChild(host);
  const mine = host;
  new MutationObserver((changes, observer) => {
    if (!mine.isConnected) { observer.disconnect(); if (host === mine) unmount(); }
  }).observe(body, {childList: true});
  render(html`<${PlayerPopup} key=${id} id=${id} from=${from}/>`, host);
}

// Vender is the browser's from the first answer on; until then the server's HTML stays.
async function mountVender() {
  const section = document.getElementById('v-vender');
  if (!section) return;
  legacy().own('v-vender');
  let initial;
  try { initial = await getJSON('/api/view/vender'); } catch (e) { legacy().disown('v-vender'); return; }
  section.textContent = '';
  render(html`<${Vender} initial=${initial}/>`, section);
}

mountVender();
