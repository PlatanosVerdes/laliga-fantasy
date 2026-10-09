import {html, render, legacy} from './lib.js';
import {ModalRoot} from './components.js';
import {PlayerPopup} from './player.js';
import {ViewScreen} from './view.js';

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

// Every tab the server hands over as a view is the browser's: drawn from the views the page
// carries, and again whenever the world moves.
for (const section of document.querySelectorAll('section[data-view]')) {
  render(html`<${ViewScreen} name=${section.dataset.view}/>`, section);
}
