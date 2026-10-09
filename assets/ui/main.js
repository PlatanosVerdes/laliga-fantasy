import {html, render} from './lib.js';
import {ModalRoot} from './components.js';
import {PlayerPopup} from './player.js';
import {WeekPopup} from './matches.js';
import {ReachPopup} from './rivals.js';
import {ManagerView, MatchdayView, ForecastView} from './drawers.js';
import {CompareTab} from './compare.js';
import {Lineup} from './lineup.js';
import {start, registerUI, registerDrawer} from './shell.js';
import './league.js';
import './decide.js';

// The page: the shell draws the frame and every section; the dialogs and notices sit above it.
registerUI('lineup', Lineup);
registerUI('comparador', CompareTab);
registerDrawer('jugador', ({arg, d}) => html`<${PlayerPopup} id=${arg} from=${d.from}/>`);
registerDrawer('jornada', ({arg}) => html`<${WeekPopup} week=${arg}/>`);
registerDrawer('alcance', ({arg}) => html`<${ReachPopup} team=${arg}/>`);
registerDrawer('manager', ({arg}) => html`<${ManagerView} team=${arg}/>`);
registerDrawer('plantillas', ({arg}) => html`<${MatchdayView} week=${arg}/>`);
registerDrawer('prevision', ({arg}) => html`<${ForecastView} week=${arg}/>`);

start(document.getElementById('app'));

const dialogs = document.createElement('div');
document.body.appendChild(dialogs);
render(html`<${ModalRoot}/>`, dialogs);
