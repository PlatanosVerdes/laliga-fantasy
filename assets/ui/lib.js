import {h, render, Fragment} from '../vendor/preact.js';
import {useState, useEffect, useRef, useMemo, useCallback} from '../vendor/hooks.js';
import htm from '../vendor/htm.js';

export const html = htm.bind(h);
export {h, render, Fragment, useState, useEffect, useRef, useMemo, useCallback};

// The shell's navigation (open a card, a squad, a tab), registered by shell.js: the components
// reach it through here rather than importing the shell, which imports them.
export const panel = () => window.panel || {};
