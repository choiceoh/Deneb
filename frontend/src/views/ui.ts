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
 * buildViewHeader returns a `.view-header` row with the standard 3-slot
 * layout: left button | title | right button. Either button may be
 * omitted — an empty span fills the slot so the flex spacing stays
 * predictable (justify-content: space-between with 2 children pushes
 * them to the edges, with 3 children centers the middle one).
 */
export function buildViewHeader(opts: {
  title: string;
  left?: HeaderButton;
  right?: HeaderButton;
}): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'view-header';
  wrap.appendChild(buildSlot(opts.left));
  const title = document.createElement('span');
  title.className = 'view-title';
  title.textContent = opts.title;
  wrap.appendChild(title);
  wrap.appendChild(buildSlot(opts.right));
  return wrap;
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
