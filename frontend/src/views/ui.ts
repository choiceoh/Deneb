// views/ui.ts — DOM builders shared across view modules.
//
// Every view has a near-identical `view-header` row (left | title | right)
// and a near-identical "load failed" banner with optional back button.
// Extracting both keeps individual view files focused on their own data
// flow and means a style tweak (header, banner, primary button) only
// changes one place. `format.ts` covers the matching error-message
// stringification so the two helpers compose: `formatRpcError(err)` →
// `renderErrorView(root, msg, ...)`.
//
// New helpers belong here when ≥2 views need them. One-shot view chrome
// stays inline.

import { triggerImpactHaptic, triggerNotificationHaptic } from '../app_settings';

/** Label + click handler for an inline button slot. */
export interface HeaderButton {
  label: string;
  onClick: () => void;
}

/**
 * buildViewHeader returns the standard page header for any drill-down
 * view. Layout matches the new home idiom: a small actions row (back
 * link on the left, optional action on the right) sits above a large
 * lowercase title.
 *
 *   ← back                                                  refresh
 *   mail
 *
 * Either button may be omitted — the actions row collapses when both
 * slots are empty, so views like memory/categories that only have a
 * title get just the title with no leftover whitespace above. The title
 * itself is also optional: omit it (as the mail detail does) and only
 * the actions row renders.
 */
export function buildViewHeader(opts: {
  title?: string;
  left?: HeaderButton;
  right?: HeaderButton;
}): HTMLElement {
  const wrap = document.createElement('header');
  wrap.className = 'view-header';

  if (opts.left || opts.right) {
    const actions = document.createElement('div');
    actions.className = 'view-actions';
    actions.appendChild(buildSlot(opts.left));
    actions.appendChild(buildSlot(opts.right));
    wrap.appendChild(actions);
  }

  // The title is optional: a drill-down whose body already leads with its
  // own heading (e.g. the mail detail's subject card) passes none, and we
  // skip the <h1> entirely so no empty heading leaves a stray gap below
  // the actions row.
  if (opts.title) {
    const title = document.createElement('h1');
    title.className = 'view-title';
    // Letter-by-letter cascade entry: each grapheme gets its own span
    // with a 25ms-per-char staggered delay via inline --i. The fall-
    // back when split is empty (or for accessibility / RTL languages
    // that don't tokenize well into letters) is to set textContent —
    // the CSS animation just plays on the whole element instead.
    appendLetterCascade(title, opts.title);
    wrap.appendChild(title);
  }
  return wrap;
}

// appendLetterCascade splits text into per-character spans (using the
// Intl.Segmenter where available so CJK + emoji stay coherent) and
// stamps an inline --i index on each so the CSS keyframe can fire
// staggered. Whitespace stays text-node so word wrap still works
// naturally inside lowercase titles.
function appendLetterCascade(host: HTMLElement, text: string): void {
  const segments = segmentGraphemes(text);
  if (!segments.length) {
    host.textContent = text;
    return;
  }
  segments.forEach((seg, i) => {
    if (seg === ' ') {
      host.appendChild(document.createTextNode(' '));
      return;
    }
    const span = document.createElement('span');
    span.className = 'view-title-letter';
    span.style.setProperty('--i', String(i));
    span.textContent = seg;
    host.appendChild(span);
  });
}

function segmentGraphemes(text: string): string[] {
  // Intl.Segmenter is widely available in modern browsers + Telegram
  // WebView. Fall back to Array.from which still respects surrogate
  // pairs (so emoji stay intact even if the segmenter is missing).
  type SegmenterCtor = new (
    locales?: string | string[],
    options?: { granularity?: 'grapheme' | 'word' | 'sentence' },
  ) => { segment(input: string): Iterable<{ segment: string }> };
  const Seg = (Intl as unknown as { Segmenter?: SegmenterCtor }).Segmenter;
  if (Seg) {
    const out: string[] = [];
    for (const s of new Seg(undefined, { granularity: 'grapheme' }).segment(text)) {
      out.push(s.segment);
    }
    return out;
  }
  return Array.from(text);
}

function buildSlot(btn: HeaderButton | undefined): HTMLElement {
  if (!btn) return document.createElement('span');
  const el = document.createElement('button');
  el.type = 'button';
  el.className = 'link-button';
  el.textContent = btn.label;
  el.addEventListener('click', () => {
    // Back / refresh link in every view header. Light impact so the
    // operator gets a confirm that the tap registered even before the
    // page transition starts firing.
    triggerImpactHaptic('light');
    btn.onClick();
  });
  return el;
}

/**
 * buildChipRow returns the horizontal pill-chip strip that sits between a
 * view header and the content below it. Each chip is a tappable action.
 * Today topics + search each pass a single create-action chip ("+ 새 토픽"
 * / "+ 새 페이지"); future filter chips can be appended and the row
 * scrolls horizontally when they overflow. Shared by both views so the
 * chip idiom (CSS: .chip-row / .chip) stays identical across the app.
 */
export function buildChipRow(chips: HeaderButton[]): HTMLElement {
  const row = document.createElement('div');
  row.className = 'chip-row';
  for (const chip of chips) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'chip chip-action';
    btn.textContent = chip.label;
    btn.addEventListener('click', () => {
      // Light impact mirrors the home menu + header links so every
      // tappable chrome element confirms the tap the same way.
      triggerImpactHaptic('light');
      chip.onClick();
    });
    row.appendChild(btn);
  }
  return row;
}

/** Plain `.error` banner element (no parent insertion). */
export function buildErrorBanner(text: string): HTMLElement {
  const banner = document.createElement('div');
  banner.className = 'error';
  banner.textContent = text;
  return banner;
}

/** Plain `.loading` placeholder element. */
export function buildLoadingNode(text: string): HTMLElement {
  const el = document.createElement('div');
  el.className = 'loading';
  el.textContent = text;
  return el;
}

// ----- Toast / flash -----
//
// A single global stack pinned to the bottom-center of the viewport.
// Every call adds another pill which slides up + fades in, sits for a
// duration, then slides out. Tap dismisses immediately. Variants flip
// the color treatment + auto-fire the matching haptic notification.

export type FlashVariant = 'info' | 'success' | 'error';

let flashStackEl: HTMLDivElement | null = null;

function ensureFlashStack(): HTMLDivElement {
  if (flashStackEl?.isConnected) return flashStackEl;
  const el = document.createElement('div');
  el.className = 'flash-stack';
  document.body.appendChild(el);
  flashStackEl = el;
  return el;
}

/**
 * showFlash drops a transient bottom-center pill into the global flash
 * stack. Variant selects color + auto-fires the matching haptic. Tap
 * the pill or wait `duration` ms for it to slide out.
 */
export function showFlash(
  message: string,
  variant: FlashVariant = 'info',
  duration = 2400,
): void {
  const stack = ensureFlashStack();
  const pill = document.createElement('button');
  pill.type = 'button';
  pill.className = `flash flash-${variant}`;
  pill.textContent = message;
  stack.appendChild(pill);

  // Force reflow so the .flash-visible class triggers the transition
  // instead of just landing in the final state.
  void pill.offsetHeight;
  pill.classList.add('flash-visible');

  if (variant === 'error') triggerNotificationHaptic('error');
  else if (variant === 'success') triggerNotificationHaptic('success');

  let dismissed = false;
  const dismiss = (): void => {
    if (dismissed) return;
    dismissed = true;
    pill.classList.remove('flash-visible');
    // After the slide-out transition finishes, remove the node so it
    // doesn't accumulate in the stack. If transitionend never fires
    // (display change, prefers-reduced-motion), the timeout backup
    // catches it.
    let removed = false;
    const remove = (): void => {
      if (removed) return;
      removed = true;
      pill.remove();
    };
    pill.addEventListener('transitionend', remove, { once: true });
    window.setTimeout(remove, 600);
  };

  const timer = window.setTimeout(dismiss, duration);
  pill.addEventListener('click', () => {
    window.clearTimeout(timer);
    dismiss();
  });
}

/**
 * buildRowSkeleton returns N hairline-bracketed empty rows that match
 * the geometry of .email-row / .session-row, used while the real list
 * is fetching. Each row carries a `.skeleton-shimmer` line that pulses
 * in opacity so the placeholder reads as "loading" without spinning.
 * Pass count to control how many rows to render (default 5 — fits
 * roughly one viewport before scroll).
 */
export function buildRowSkeleton(count = 5): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'skeleton-list';
  for (let i = 0; i < count; i++) {
    const row = document.createElement('div');
    row.className = 'skeleton-row';
    row.style.setProperty('--i', String(i));
    row.innerHTML = `
      <span class="skeleton-line skeleton-line-primary"></span>
      <span class="skeleton-line skeleton-line-secondary"></span>
    `;
    wrap.appendChild(row);
  }
  return wrap;
}

/**
 * renderErrorView clears `root` and paints an error banner plus an
 * optional primary back button. Use when the entire view failed to
 * load and the user needs a way out (e.g. mail detail, wiki page).
 * For inline / partial failures (a row, a card slot) use
 * `buildErrorBanner` directly and insert it next to the failing widget.
 */
export function renderErrorView(
  root: HTMLElement,
  text: string,
  back?: HeaderButton,
): void {
  root.innerHTML = '';
  root.appendChild(buildErrorBanner(text));
  if (back) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'primary';
    btn.textContent = back.label;
    btn.addEventListener('click', back.onClick);
    root.appendChild(btn);
  }
}
