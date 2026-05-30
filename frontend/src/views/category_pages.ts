// views/category_pages.ts — Pages within a single wiki category.
//
// Drilled into from the categories explorer. Each row shows title +
// summary + updated date; tap to open the full wiki page view.
//
// Long-press a row → enter multi-select mode (same gesture as the mail
// inbox). With exactly two pages selected, a bottom action bar exposes
// "병합" — fold one project page into the other (see category_merge).

import { listPagesInCategory, type MemoryPageRow } from '../memory';
import { formatRpcError, relativeTime } from '../format';
import { isCurrentHash, navigate } from '../router';
import { triggerImpactHaptic } from '../app_settings';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';
import { runMerge } from './category_merge';

const longPressMs = 450;
const longPressMoveTolerancePx = 10;

interface SelectionState {
  root: HTMLElement;
  rowsContainer: HTMLElement;
  initData: string;
  category: string;
  expectedHash: string;
  selected: Map<string, MemoryPageRow>;
  selecting: boolean;
  busy: boolean;
  actionBar?: HTMLElement;
}

export async function renderCategoryPages(
  root: HTMLElement,
  initData: string,
  category: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  const displayName = category === '(root)' ? 'root' : category;
  // Seed wiki-new with the current category so the user doesn't have
  // to retype it. (root) is filesystem-fake (Stats() reports it for
  // pages without a category) so we don't seed for that bucket.
  const newPageCategory = category === '(root)' ? undefined : category;
  root.appendChild(
    buildViewHeader({
      title: displayName,
      left: { label: '← categories', onClick: () => navigate({ name: 'categories' }) },
      right: {
        label: '+ new',
        onClick: () => navigate({ name: 'wikiNew', category: newPageCategory }),
      },
    }),
  );

  const rowsContainer = document.createElement('div');
  rowsContainer.className = 'memory-list';
  root.appendChild(rowsContainer);
  const selection: SelectionState = {
    root,
    rowsContainer,
    initData,
    category,
    expectedHash,
    selected: new Map(),
    selecting: false,
    busy: false,
  };

  const status = buildLoadingNode('페이지 목록 불러오는 중…');
  rowsContainer.appendChild(status);

  try {
    const { pages, total } = await listPagesInCategory(initData, category);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();

    if (pages.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = 'no pages in this category';
      rowsContainer.appendChild(empty);
      return;
    }
    for (const page of pages) {
      rowsContainer.appendChild(buildPageRow(page, selection));
    }
    if (total > pages.length) {
      const note = document.createElement('div');
      note.className = 'muted';
      note.textContent = `최근 ${pages.length}건 표시 · 전체 ${total.toLocaleString('ko-KR')}건`;
      rowsContainer.appendChild(note);
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    rowsContainer.appendChild(buildErrorBanner(`페이지 목록 로드 실패: ${formatRpcError(err)}`));
  }
}

function buildPageRow(page: MemoryPageRow, selection: SelectionState): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'memory-row-wrap';

  const card = document.createElement('button');
  card.type = 'button';
  card.className = 'memory-card';
  card.setAttribute('aria-pressed', 'false');

  const indicator = document.createElement('span');
  indicator.className = 'memory-row-select-indicator';
  indicator.setAttribute('aria-hidden', 'true');
  card.appendChild(indicator);

  const title = document.createElement('div');
  title.className = 'memory-title';
  title.textContent = page.title || page.path;
  card.appendChild(title);

  if (page.summary) {
    const summary = document.createElement('div');
    summary.className = 'memory-summary';
    summary.textContent = page.summary;
    card.appendChild(summary);
  }

  const meta = document.createElement('div');
  meta.className = 'memory-meta';
  const parts: string[] = [page.path];
  if (page.updated) parts.push(`갱신 ${relativeTime(page.updated)}`);
  meta.textContent = parts.join(' · ');
  card.appendChild(meta);

  let pressTimer: number | undefined;
  let longPressTriggered = false;
  let pointerStartX = 0;
  let pointerStartY = 0;

  const clearPressTimer = (): void => {
    if (pressTimer !== undefined) {
      window.clearTimeout(pressTimer);
      pressTimer = undefined;
    }
  };

  card.addEventListener('pointerdown', (e) => {
    if (e.button !== 0 || selection.busy) return;
    longPressTriggered = false;
    pointerStartX = e.clientX;
    pointerStartY = e.clientY;
    clearPressTimer();
    pressTimer = window.setTimeout(() => {
      longPressTriggered = true;
      triggerImpactHaptic('soft');
      enterSelectionMode(selection);
      toggleSelected(selection, page, wrap, true);
    }, longPressMs);
  });

  card.addEventListener('pointermove', (e) => {
    const dx = Math.abs(e.clientX - pointerStartX);
    const dy = Math.abs(e.clientY - pointerStartY);
    if (dx > longPressMoveTolerancePx || dy > longPressMoveTolerancePx) clearPressTimer();
  });

  card.addEventListener('pointerup', clearPressTimer);
  card.addEventListener('pointercancel', clearPressTimer);
  card.addEventListener('pointerleave', clearPressTimer);
  card.addEventListener('contextmenu', (e) => e.preventDefault());

  card.addEventListener('click', (e) => {
    // Swallow the click synthesized at the end of a long-press so it
    // doesn't immediately navigate into the row we just selected.
    if (longPressTriggered) {
      e.preventDefault();
      longPressTriggered = false;
      return;
    }
    if (selection.busy) return;
    if (selection.selecting) {
      e.preventDefault();
      toggleSelected(selection, page, wrap);
      return;
    }
    navigate({ name: 'wikiPage', path: page.path });
  });

  card.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && selection.selecting) {
      e.preventDefault();
      exitSelectionMode(selection);
    }
  });

  wrap.appendChild(card);
  return wrap;
}

function enterSelectionMode(selection: SelectionState): void {
  if (selection.selecting) return;
  selection.selecting = true;
  selection.rowsContainer.classList.add('memory-list-selecting');
  renderSelectionBar(selection);
}

function exitSelectionMode(selection: SelectionState): void {
  selection.selecting = false;
  selection.busy = false;
  selection.rowsContainer.classList.remove('memory-list-selecting');
  for (const wrap of selectedWraps(selection)) {
    updateRowSelection(wrap, false);
  }
  selection.selected.clear();
  selection.actionBar?.remove();
  selection.actionBar = undefined;
}

// We key the selection map by path but need the wrap node to clear the
// highlight; stash it on the row's dataset rather than thread a second map.
function selectedWraps(selection: SelectionState): HTMLElement[] {
  return Array.from(
    selection.rowsContainer.querySelectorAll<HTMLElement>('.memory-row-wrap.memory-row-selected'),
  );
}

function toggleSelected(
  selection: SelectionState,
  page: MemoryPageRow,
  wrap: HTMLElement,
  forceSelected?: boolean,
): void {
  if (selection.busy || !wrap.isConnected) return;
  if (!selection.selecting) enterSelectionMode(selection);
  const selected = forceSelected ?? !selection.selected.has(page.path);
  if (selected) {
    selection.selected.set(page.path, page);
  } else {
    selection.selected.delete(page.path);
  }
  updateRowSelection(wrap, selected);
  if (selection.selected.size === 0) {
    exitSelectionMode(selection);
  } else {
    renderSelectionBar(selection);
  }
}

function updateRowSelection(wrap: HTMLElement, selected: boolean): void {
  wrap.classList.toggle('memory-row-selected', selected);
  wrap.querySelector('.memory-card')?.setAttribute('aria-pressed', String(selected));
}

function renderSelectionBar(selection: SelectionState): void {
  if (!selection.selecting || selection.selected.size === 0) return;
  const bar = selection.actionBar ?? document.createElement('div');
  if (!selection.actionBar) {
    bar.className = 'email-bulk-bar';
    selection.root.appendChild(bar);
    selection.actionBar = bar;
  }
  bar.innerHTML = '';

  const n = selection.selected.size;
  const count = document.createElement('div');
  count.className = 'email-bulk-count';
  count.textContent = n === 2 ? '2개 선택' : `${n.toLocaleString('ko-KR')}개 선택 · 병합하려면 2개`;
  bar.appendChild(count);

  const actions = document.createElement('div');
  actions.className = 'email-bulk-actions';

  const merge = document.createElement('button');
  merge.type = 'button';
  merge.className = 'email-bulk-button';
  merge.textContent = '병합';
  merge.disabled = selection.busy || n !== 2;
  merge.addEventListener('click', () => {
    triggerImpactHaptic('medium');
    void doMerge(selection);
  });
  actions.appendChild(merge);

  const cancel = document.createElement('button');
  cancel.type = 'button';
  cancel.className = 'email-bulk-button';
  cancel.textContent = '취소';
  cancel.disabled = selection.busy;
  cancel.addEventListener('click', () => exitSelectionMode(selection));
  actions.appendChild(cancel);

  bar.appendChild(actions);
}

async function doMerge(selection: SelectionState): Promise<void> {
  if (selection.busy || selection.selected.size !== 2) return;
  const [a, b] = Array.from(selection.selected.values());

  selection.busy = true;
  renderSelectionBar(selection);

  const merged = await runMerge(selection.initData, a, b);

  if (!merged) {
    // Cancelled or failed — drop the busy lock and let the user retry.
    selection.busy = false;
    if (selection.selecting) renderSelectionBar(selection);
    return;
  }

  // Merged: tear down selection and reload the list so the surviving page
  // shows its combined body and the source row is gone.
  exitSelectionMode(selection);
  if (isCurrentHash(selection.expectedHash)) {
    void renderCategoryPages(selection.root, selection.initData, selection.category);
  }
}
