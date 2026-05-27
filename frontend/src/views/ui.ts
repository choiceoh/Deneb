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
 * title get just the title with no leftover whitespace above.
 */
export function buildViewHeader(opts: {
  title: string;
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

  const title = document.createElement('h1');
  title.className = 'view-title';
  // Letter-by-letter cascade entry: each grapheme gets its own span
  // with a 25ms-per-char staggered delay via inline --i. The fall-
  // back when split is empty (or for accessibility / RTL languages
  // that don't tokenize well into letters) is to set textContent —
  // the CSS animation just plays on the whole element instead.
  appendLetterCascade(title, opts.title);
  wrap.appendChild(title);
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
  el.addEventListener('click', btn.onClick);
  return el;
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
