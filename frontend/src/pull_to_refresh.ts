// pull_to_refresh.ts — gesture-driven "pull from top to refresh" for
// drill-down views. Replaces the per-view header "refresh" link with a
// gesture that feels native on Telegram WebView.
//
// Design choices:
//   - Single shared installation. Each view's render() registers its own
//     refresh callback via setPullToRefreshHandler(cb); navigation clears
//     it via clearPullToRefreshHandler() from dispatch(). This keeps
//     the touch listeners attached exactly once instead of churning on
//     every view rerender (and avoids leaking handlers across hashes).
//   - Window-level scrollY gate. We only arm the gesture when the page
//     is already at the top; mid-scroll pulldowns are ignored so a
//     long page scrolls naturally.
//   - Resistance. Visible offset is half the finger delta (capped) so
//     the indicator feels weighted instead of glued to the finger.
//   - preventDefault during pull. Without it the body would rubber-band
//     on iOS Telegram even though overscroll-behavior is set; with it
//     the gesture stays inside the page even on older WebViews.

type RefreshCallback = () => void | Promise<void>;

let activeCallback: RefreshCallback | null = null;
let installed = false;
let indicatorEl: HTMLElement | null = null;
let arrowEl: HTMLElement | null = null;

let startY = 0;
let lastY = 0;
let armed = false;
let pulling = false;
let refreshing = false;

const TRIGGER_PX = 64;
const MAX_PX = 110;
const RESISTANCE = 0.5;

export function setPullToRefreshHandler(cb: RefreshCallback): void {
  activeCallback = cb;
  ensureInstalled();
}

export function clearPullToRefreshHandler(): void {
  activeCallback = null;
  // Cancel any in-flight gesture state so a navigation mid-pull doesn't
  // leave the indicator stuck on screen.
  if (pulling || armed) {
    endPull(false);
  }
}

function ensureInstalled(): void {
  if (installed) return;
  installed = true;
  buildIndicator();
  // touchmove is non-passive because we call preventDefault when the
  // user is actively pulling — otherwise iOS WebView rubber-bands the
  // whole body even with overscroll-behavior set.
  window.addEventListener('touchstart', onTouchStart, { passive: true });
  window.addEventListener('touchmove', onTouchMove, { passive: false });
  window.addEventListener('touchend', onTouchEnd, { passive: true });
  window.addEventListener('touchcancel', onTouchCancel, { passive: true });
}

function buildIndicator(): void {
  const wrap = document.createElement('div');
  wrap.className = 'ptr-indicator';
  wrap.setAttribute('aria-hidden', 'true');
  const arrow = document.createElement('span');
  arrow.className = 'ptr-arrow';
  // SVG arrow — rotates to "release to refresh" when past threshold,
  // becomes a spinner on the refreshing state via CSS.
  arrow.innerHTML =
    '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">' +
    '<path d="M8 2v10M4 8l4 4 4-4" fill="none" stroke="currentColor" ' +
    'stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>' +
    '</svg>';
  wrap.appendChild(arrow);
  document.body.appendChild(wrap);
  indicatorEl = wrap;
  arrowEl = arrow;
}

function onTouchStart(e: TouchEvent): void {
  if (refreshing || !activeCallback) return;
  if (window.scrollY > 0) return;
  if (e.touches.length !== 1) return;
  startY = e.touches[0].clientY;
  lastY = startY;
  armed = true;
  pulling = false;
}

function onTouchMove(e: TouchEvent): void {
  if (!armed) return;
  // If the page scrolled (shouldn't happen since we arm at scrollY=0,
  // but defensive against momentum), give up.
  if (window.scrollY > 0) {
    endPull(false);
    return;
  }
  const y = e.touches[0].clientY;
  lastY = y;
  const dy = y - startY;
  if (dy <= 0) {
    // Upward swipe — let the page scroll normally.
    if (pulling) endPull(false);
    return;
  }
  // Past first downward pixel: take over the gesture.
  if (!pulling) {
    pulling = true;
    document.body.classList.add('ptr-pulling');
  }
  if (e.cancelable) e.preventDefault();
  const visible = Math.min(MAX_PX, dy * RESISTANCE);
  setIndicatorOffset(visible);
  setArmedState(visible >= TRIGGER_PX);
}

function onTouchEnd(): void {
  if (!armed) return;
  const wasPulling = pulling;
  armed = false;
  if (!wasPulling) {
    pulling = false;
    return;
  }
  const dy = lastY - startY;
  const visible = Math.min(MAX_PX, dy * RESISTANCE);
  if (visible >= TRIGGER_PX && activeCallback) {
    triggerRefresh();
  } else {
    endPull(false);
  }
}

function onTouchCancel(): void {
  if (!armed && !pulling) return;
  endPull(false);
}

function setIndicatorOffset(px: number): void {
  if (!indicatorEl) return;
  indicatorEl.style.setProperty('--ptr-offset', `${px}px`);
}

function setArmedState(ready: boolean): void {
  if (!indicatorEl) return;
  indicatorEl.classList.toggle('ptr-armed', ready);
}

function triggerRefresh(): void {
  if (!activeCallback) {
    endPull(false);
    return;
  }
  refreshing = true;
  pulling = false;
  if (indicatorEl) {
    indicatorEl.classList.add('ptr-refreshing');
    indicatorEl.classList.remove('ptr-armed');
    // Park the indicator at the trigger position while the refresh
    // callback is running.
    indicatorEl.style.setProperty('--ptr-offset', `${TRIGGER_PX}px`);
  }
  // Lock body so the pull animation doesn't run concurrently.
  document.body.classList.add('ptr-refreshing');

  let result: void | Promise<void>;
  try {
    result = activeCallback();
  } catch {
    finishRefresh();
    return;
  }
  if (result && typeof (result as Promise<void>).then === 'function') {
    (result as Promise<void>).then(finishRefresh, finishRefresh);
  } else {
    // Synchronous handler — give the indicator a beat so the spinner
    // doesn't flash imperceptibly.
    window.setTimeout(finishRefresh, 350);
  }
}

function finishRefresh(): void {
  refreshing = false;
  endPull(true);
}

function endPull(wasRefresh: boolean): void {
  armed = false;
  pulling = false;
  document.body.classList.remove('ptr-pulling');
  document.body.classList.remove('ptr-refreshing');
  if (indicatorEl) {
    indicatorEl.classList.remove('ptr-armed');
    indicatorEl.classList.remove('ptr-refreshing');
    indicatorEl.style.setProperty('--ptr-offset', '0px');
  }
  // Suppress unused-arg lint without changing the call-site shape: a
  // future "did refresh" haptic could key off wasRefresh.
  void wasRefresh;
  void arrowEl;
}
