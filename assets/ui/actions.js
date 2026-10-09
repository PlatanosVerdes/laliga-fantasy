import {legacy} from './lib.js';
import {getJSON, postJSON, openDialog, changed} from './api.js';
import {stampText} from './format.js';

let opened = 0;
export function confirmOp(op) { opened += 1; openDialog({kind: 'confirm', key: opened, op}); }

// One of the player's actions, run exactly as his card always has: the amount dialogs and the
// shield and raid forms are report.js's; the plain two-step confirmation is ConfirmOp.
export async function runAction(a, player, {fromCard = false, reopen} = {}) {
  const page = legacy();
  if (a.op === 'note') return;
  if (a.op === 'raid') {
    page.raidDialog({id: player.id, name: player.name, suggested: a.suggested, clause: player.clause,
      opens: player.clause_locked_until}, reopen);
    return;
  }
  if (a.op === 'shield') { page.shieldDialog(a, player); return; }
  if (a.op === 'cancel_shield') {
    if (!confirm('Cancelar el blindaje de ' + player.name + ' del ' + stampText(a.at) + '?')) return;
    try { await postJSON('/api/shield/cancel', {id: player.id, at: a.at}); } catch (e) { alert('No he podido cancelarlo.'); return; }
    if (reopen) reopen();
    return;
  }
  if (fromCard) page.closeDrawer();
  if (a.kind === 'amount') {
    page.openAmount(a, player);
  } else {
    confirmOp({op: a.op, name: player.name, player_id: a.player_id || player.id,
      market_id: a.market_id, offer_id: a.offer_id, amount: a.amount || null});
  }
}

// A row's button for one of his card's actions: the card's own list says whether it can be done
// right now and with what, so it is asked for rather than guessed from the row.
export async function runCardAction(playerId, op) {
  let data;
  try { data = await getJSON('/api/player/' + playerId); } catch (e) { alert('Solo disponible en la versión servida.'); return; }
  const action = (data.actions || []).find((a) => a.op === op);
  if (!action) { alert('Ahora mismo no se puede: abre su ficha para ver por qué.'); return; }
  await runAction(action, data.player);
}

// The standing listing, switched on or off. It moves no money, so it goes in one step.
export async function toggleAlways(player) {
  const data = await postJSON('/api/always', {id: player.id, name: player.name});
  changed();
  return !!data.always_listed;
}
