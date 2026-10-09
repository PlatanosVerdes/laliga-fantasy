import {panel} from './lib.js';
import {getJSON, postJSON, openDialog, changed} from './api.js';
import {stampText} from './format.js';
import {openBid, openAmount, shieldDialog, raidDialog} from './dialogs.js';

let opened = 0;
export function confirmOp(op) { opened += 1; openDialog({kind: 'confirm', key: opened, op}); }

// One of the player's actions, run exactly as his card always has: the amount dialogs and the
// shield and raid forms are report.js's; the plain two-step confirmation is ConfirmOp.
export async function runAction(a, player, {fromCard = false, reopen} = {}) {
  const page = panel();
  if (a.op === 'note') return;
  if (a.op === 'raid') {
    raidDialog({id: player.id, name: player.name, suggested: a.suggested, clause: player.clause,
      opens: player.clause_locked_until}, reopen);
    return;
  }
  if (a.op === 'shield') { shieldDialog(a, player); return; }
  if (a.op === 'cancel_shield') {
    if (!confirm('Cancelar el blindaje de ' + player.name + ' del ' + stampText(a.at) + '?')) return;
    try { await postJSON('/api/shield/cancel', {id: player.id, at: a.at}); } catch (e) { alert('No he podido cancelarlo.'); return; }
    if (reopen) reopen();
    return;
  }
  if (fromCard) page.closeDrawer();
  if (a.kind === 'amount') {
    openAmount(a, player);
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

// A view's button, as render/viewmodel.go described it.
export async function runAct(a) {
  const x = a.args || {};
  const page = panel();
  switch (a.do) {
    case 'op':
      confirmOp({op: x.op, name: x.name, player_id: x.player_id, market_id: x.market_id,
        offer_id: x.offer_id, amount: x.amount || null});
      return;
    case 'card': await runCardAction(x.player_id, x.op); return;
    case 'bid': openBid(x); return;
    case 'raid':
      raidDialog({id: x.id, name: x.name, suggested: x.max, clause: x.clause});
      return;
    case 'raise':
      openAmount({op: 'raise_clause', kind: 'amount', label: 'Subir cláusula', player_id: x.id,
        player_team_id: x.slot, suggested: x.pay || 0},
      {id: x.id, name: x.name, clause: x.clause || 0, value: x.clause || 0, ideal_bid: 0});
      return;
    case 'cancel_raid':
      // An instruction of ours, not an operation against LaLiga: no two-step confirmation.
      if (!confirm('Cancelar el clausulazo programado de ' + x.name + '?')) return;
      try { await postJSON('/api/raid/cancel', {id: x.player_id, name: x.name}); changed(); }
      catch (e) { alert('No he podido cancelarlo: ' + e.message); }
      return;
    case 'drop_always':
      if (!confirm('Quitar ' + x.name + ' de siempre-en-mercado?')) return;
      try {
        const data = await postJSON('/api/always', {id: x.player_id, name: x.name});
        // The toggle would have armed it: put it back as it was and say so.
        if (data.always_listed) {
          await postJSON('/api/always', {id: x.player_id, name: x.name});
          throw new Error('no estaba armado');
        }
        changed();
      } catch (e) { alert('No he podido quitarlo: ' + e.message); }
      return;
    case 'goto': page.goto(x.target); return;
    case 'reach': (page.usage || {click() {}}).click('rivales', 'alcance', x.team_id); page.openReach(x.team_id); return;
    case 'detail': page.openDetail(x.player_id); return;
    case 'manager': page.openManager(x.team_id); return;
    default:
  }
}
