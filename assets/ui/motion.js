import {nextViews} from './api.js';

// What the page does when something leaves it: the row or card slides off to the side, then its
// gap closes, so what is below rises instead of jumping.

export const still = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

const frame = () => new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done)));

export async function leave(el) {
  if (!el || still()) return;
  el.style.pointerEvents = 'none';
  await el.animate([{transform: 'none', opacity: 1}, {transform: 'translateX(60%)', opacity: 0}],
    {duration: 260, easing: 'cubic-bezier(.4,0,1,1)', fill: 'forwards'}).finished;
  const gap = el.parentElement ? getComputedStyle(el.parentElement).rowGap : '0px';
  el.style.overflow = 'hidden';
  await el.animate([{height: el.offsetHeight + 'px', marginBottom: '0px'},
    {height: '0px', marginBottom: gap === 'normal' ? '0px' : '-' + gap, paddingTop: '0px',
      paddingBottom: '0px', borderWidth: '0px'}],
  {duration: 200, easing: 'ease-out', fill: 'forwards'}).finished;
}

// Undoes leave. Rows are not keyed, so the element may be handed to the next row once the new view
// is drawn: it is only put back after that, or at once when the change did not happen.
export async function putBack(el, {afterViews = false} = {}) {
  if (!el) return;
  if (afterViews) {
    await Promise.race([nextViews(), new Promise((done) => setTimeout(done, 15000))]);
    await frame();
  }
  el.getAnimations().forEach((animation) => animation.cancel());
  el.style.pointerEvents = el.style.overflow = '';
}
