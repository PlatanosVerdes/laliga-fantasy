import {h, render, Fragment} from '../vendor/preact.js';
import {useState, useEffect, useRef, useMemo, useCallback} from '../vendor/hooks.js';
import htm from '../vendor/htm.js';

export const html = htm.bind(h);
export {h, render, Fragment, useState, useEffect, useRef, useMemo, useCallback};

// The page's own code: dialogs, the drawer, the comparator tray. Exposed by report.js.
export const legacy = () => window.panel || {};
