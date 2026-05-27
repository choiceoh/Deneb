// views/list.ts — Gmail inbox triage queue.
//
// Calls miniapp.gmail.list_recent and renders one row per message. Tap on
// the row → navigate to detail. Long-press a row → enter multi-select mode;
// normal taps then toggle rows and a fixed bottom action bar exposes bulk
// read/archive/delete actions.
//
// Pagination: the backend returns nextPageToken; when non-empty we show
// a "더 보기" button at the bottom that fetches the next page and appends
// its rows. The token is passed through each mountLoadMore call (no
// module-scoped state) so a refresh or navigation that replaces
// rowsContainer cleanly orphans any in-flight pagination work.
//
// Stale-render guards: every async handler that mutates the DOM checks
// both isCurrentHash (route didn't change) AND container.isConnected
// (the specific container it captured is still in the document tree).
// The hash check alone is insufficient because an inbox refresh leaves
// the hash identical but replaces rowsContainer.

import { archive, listRecent, markRead, trash, type GmailMessageRow } from '../gmail';
import { isCurrentHash, navigate } from '../router';
import { confirmAction } from '../dialog';
import { errorMessage, formatRpcError, relativeTime, shortFrom } from '../format';
import { buildErrorBanner, buildRowSkeleton, buildViewHeader } from './ui';

const longPressMs = 450;
const longPressMoveTolerancePx = 10;

type BulkAction = 'read' | 'archive' | 'trash';

interface SelectedMail {
  row: GmailMessageRow;
  wrap: HTMLElement;
}

interface SelectionState {
  root: HTMLElement;
  rowsContainer: HTMLElement;
  initData: string;
  expectedHash: string;
  selected: Map<string, SelectedMail>;
  selecting: boolean;
  busy: boolean;
  actionBar?: HTMLElement;
}

export async function renderList(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';
  const header = buildHeader(() => renderList(root, initData));
  root.appendChild(header);

  // Container the load-more handler appends into. Keeping it separate
  // from the page-level root lets us swap the "더 보기" button in/out
  // without disturbing the header.
  const rowsContainer = document.createElement('div');
  rowsContainer.className = 'email-list';
  root.appendChild(rowsContainer);
  const selection = createSelectionState(root, rowsContainer, initData, expectedHash);

  // Skeleton placeholders mimic the .email-row geometry so the page
  // doesn't pop blank → populated. buildLoadingNode is still around
  // for non-list views.
  const status = buildRowSkeleton(6);
  rowsContainer.appendChild(status);

  try {
    const result = await listRecent(initData);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    if (result.messages.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = 'no mail in the last 7 days';
      rowsContainer.appendChild(empty);
      return;
    }
    for (const row of result.messages) {
      rowsContainer.appendChild(buildRow(row, selection));
    }
    if (result.nextPageToken) {
      mountLoadMore(rowsContainer, selection, result.nextPageToken);
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    rowsContainer.appendChild(
      buildErrorBanner(`메일 목록 로드 실패: ${formatRpcError(err)}`),
    );
  }
}

function buildHeader(onRefresh: () => void): HTMLElement {
  return buildViewHeader({
    title: 'mail',
    right: { label: 'refresh', onClick: onRefresh },
  });
}

function createSelectionState(
  root: HTMLElement,
  rowsContainer: HTMLElement,
  initData: string,
  expectedHash: string,
): SelectionState {
  return {
    root,
    rowsContainer,
    initData,
    expectedHash,
    selected: new Map(),
    selecting: false,
    busy: false,
  };
}

function buildRow(row: GmailMessageRow, selection: SelectionState): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'email-row-wrap';
  if (row.isUnread) wrap.classList.add('email-row-unread');

  const main = document.createElement('div');
  main.className = 'email-row';
  main.setAttribute('role', 'button');
  main.setAttribute('aria-pressed', 'false');
  main.tabIndex = 0;

  const indicator = document.createElement('span');
  indicator.className = 'email-row-select-indicator';
  indicator.setAttribute('aria-hidden', 'true');
  main.appendChild(indicator);

  const fromLine = document.createElement('div');
  fromLine.className = 'email-row-meta';
  fromLine.innerHTML = `
    <span class="email-row-from"></span>
    <span class="email-row-time"></span>
  `;
  (fromLine.querySelector('.email-row-from') as HTMLElement).textContent = shortFrom(row.from);
  (fromLine.querySelector('.email-row-time') as HTMLElement).textContent = relativeTime(row.date);
  main.appendChild(fromLine);

  const subject = document.createElement('div');
  subject.className = 'email-row-subject';
  subject.textContent = row.subject || '(제목 없음)';
  main.appendChild(subject);

  const snippet = document.createElement('div');
  snippet.className = 'email-row-snippet';
  snippet.textContent = row.snippet || '';
  main.appendChild(snippet);

  let pressTimer: number | undefined;
  let longPressTriggered = false;
  let pointerStartX = 0;
  let pointerStartY = 0;

  const clearPressTimer = () => {
    if (pressTimer !== undefined) {
      window.clearTimeout(pressTimer);
      pressTimer = undefined;
    }
  };

  const openDetail = () => {
    if (selection.selecting) return;
    wrap.dataset.navigating = '1';
    navigate({ name: 'detail', messageId: row.id });
  };

  main.addEventListener('pointerdown', (e) => {
    if (e.button !== 0 || selection.busy) return;
    longPressTriggered = false;
    pointerStartX = e.clientX;
    pointerStartY = e.clientY;
    clearPressTimer();
    pressTimer = window.setTimeout(() => {
      longPressTriggered = true;
      enterSelectionMode(selection);
      toggleSelected(selection, row, wrap, true);
    }, longPressMs);
  });

  main.addEventListener('pointermove', (e) => {
    const dx = Math.abs(e.clientX - pointerStartX);
    const dy = Math.abs(e.clientY - pointerStartY);
    if (dx > longPressMoveTolerancePx || dy > longPressMoveTolerancePx) clearPressTimer();
  });

  main.addEventListener('pointerup', clearPressTimer);
  main.addEventListener('pointercancel', clearPressTimer);
  main.addEventListener('pointerleave', clearPressTimer);
  main.addEventListener('contextmenu', (e) => e.preventDefault());

  main.addEventListener('click', (e) => {
    if (longPressTriggered) {
      e.preventDefault();
      longPressTriggered = false;
      return;
    }
    if (selection.selecting) {
      e.preventDefault();
      toggleSelected(selection, row, wrap);
      return;
    }
    openDetail();
  });

  main.addEventListener('keydown', (e) => {
    // e.repeat guards against held Space/Enter spamming navigate(); a
    // native <button> handles repeat at the OS level, but we lost that
    // when switching to a div role=button.
    if (e.repeat) return;
    if (e.key === 'Escape' && selection.selecting) {
      e.preventDefault();
      exitSelectionMode(selection);
      return;
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      if (selection.selecting) {
        toggleSelected(selection, row, wrap);
      } else {
        openDetail();
      }
    }
  });
  wrap.appendChild(main);

  return wrap;
}

function enterSelectionMode(selection: SelectionState): void {
  if (selection.selecting) return;
  selection.selecting = true;
  selection.rowsContainer.classList.add('email-list-selecting');
  renderSelectionBar(selection);
}

function exitSelectionMode(selection: SelectionState): void {
  selection.selecting = false;
  selection.busy = false;
  selection.rowsContainer.classList.remove('email-list-selecting');
  for (const { wrap } of selection.selected.values()) {
    updateRowSelection(selection, wrap, false);
  }
  selection.selected.clear();
  selection.actionBar?.remove();
  selection.actionBar = undefined;
}

function toggleSelected(
  selection: SelectionState,
  row: GmailMessageRow,
  wrap: HTMLElement,
  forceSelected?: boolean,
): void {
  if (selection.busy || !wrap.isConnected) return;
  if (!selection.selecting) enterSelectionMode(selection);
  const selected = forceSelected ?? !selection.selected.has(row.id);
  if (selected) {
    selection.selected.set(row.id, { row, wrap });
  } else {
    selection.selected.delete(row.id);
  }
  updateRowSelection(selection, wrap, selected);
  if (selection.selected.size === 0) {
    exitSelectionMode(selection);
  } else {
    renderSelectionBar(selection);
  }
}

function updateRowSelection(
  selection: SelectionState,
  wrap: HTMLElement,
  selected: boolean,
): void {
  wrap.classList.toggle('email-row-selected', selected);
  const main = wrap.querySelector('.email-row');
  main?.setAttribute('aria-pressed', selection.selecting ? String(selected) : 'false');
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

  const count = document.createElement('div');
  count.className = 'email-bulk-count';
  count.textContent = `${selection.selected.size.toLocaleString('ko-KR')}개 선택`;
  bar.appendChild(count);

  const actions = document.createElement('div');
  actions.className = 'email-bulk-actions';
  actions.appendChild(
    buildBulkButton('read', selection.busy, () => runBulkAction(selection, 'read')),
  );
  actions.appendChild(
    buildBulkButton('archive', selection.busy, () => runBulkAction(selection, 'archive')),
  );
  actions.appendChild(
    buildBulkButton('trash', selection.busy, () => runBulkAction(selection, 'trash')),
  );
  actions.appendChild(
    buildBulkButton('cancel', selection.busy, () => exitSelectionMode(selection)),
  );
  bar.appendChild(actions);
}

function buildBulkButton(
  label: string,
  disabled: boolean,
  onClick: () => void | Promise<void>,
): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'email-bulk-button';
  btn.textContent = label;
  btn.disabled = disabled;
  btn.addEventListener('click', () => {
    void onClick();
  });
  return btn;
}

async function runBulkAction(
  selection: SelectionState,
  action: BulkAction,
): Promise<void> {
  if (selection.busy || selection.selected.size === 0) return;
  const items = Array.from(selection.selected.values());
  if (action === 'trash') {
    const ok = await confirmAction(
      `${items.length.toLocaleString('ko-KR')}개 메일을 휴지통으로 옮길까요?`,
    );
    if (!ok) return;
  }

  selection.busy = true;
  renderSelectionBar(selection);

  const succeeded: SelectedMail[] = [];
  const failed: string[] = [];
  try {
    for (const item of items) {
      if (!isCurrentHash(selection.expectedHash) || !item.wrap.isConnected) continue;
      try {
        await applyBulkAction(selection.initData, action, item.row.id);
        succeeded.push(item);
      } catch (err) {
        failed.push(errorMessage(err));
      }
    }
  } finally {
    selection.busy = false;
  }

  if (!isCurrentHash(selection.expectedHash) || !selection.rowsContainer.isConnected) return;

  for (const item of succeeded) {
    if (action === 'read') {
      item.row.isUnread = false;
      item.wrap.classList.remove('email-row-unread');
      selection.selected.delete(item.row.id);
      updateRowSelection(selection, item.wrap, false);
    } else {
      selection.selected.delete(item.row.id);
      item.wrap.remove();
    }
  }

  const successCount = succeeded.length;
  const actionLabel = bulkActionLabel(action);
  if (failed.length === 0) {
    exitSelectionMode(selection);
    if (successCount > 0) {
      flashNear(
        selection.rowsContainer,
        `${successCount.toLocaleString('ko-KR')}개 메일 ${actionLabel} 완료`,
      );
    }
  } else {
    if (selection.selected.size === 0) {
      exitSelectionMode(selection);
    } else {
      renderSelectionBar(selection);
    }
    flashNear(
      selection.rowsContainer,
      `${successCount.toLocaleString('ko-KR')}개 완료 · ${failed.length.toLocaleString(
        'ko-KR',
      )}개 실패: ${failed[0]}`,
    );
  }
}

function applyBulkAction(initData: string, action: BulkAction, id: string): Promise<unknown> {
  switch (action) {
    case 'read':
      return markRead(initData, id);
    case 'archive':
      return archive(initData, id);
    case 'trash':
      return trash(initData, id);
  }
}

function bulkActionLabel(action: BulkAction): string {
  switch (action) {
    case 'read':
      return '읽음 처리';
    case 'archive':
      return '보관';
    case 'trash':
      return '삭제';
  }
}

// mountLoadMore appends a "더 보기" button below the rendered rows; the
// button replaces itself with a loading state on click, fetches the next
// page, appends its rows, and (if more pages remain) re-mounts itself
// with the new token. expectedHash threads the route token through so a
// stale fetch from a stale page doesn't inject into the wrong view.
function mountLoadMore(
  container: HTMLElement,
  selection: SelectionState,
  pageToken: string,
): void {
  const btn = document.createElement('button');
  btn.className = 'load-more-button';
  btn.type = 'button';
  btn.textContent = '더 보기';
  btn.addEventListener('click', async () => {
    btn.disabled = true;
    const originalLabel = btn.textContent;
    btn.textContent = '불러오는 중…';
    try {
      const result = await listRecent(selection.initData, { pageToken });
      // Hash AND container-attachment check: an inbox → detail →
      // inbox round-trip leaves the hash identical but replaces
      // rowsContainer, so isCurrentHash alone would still pass and
      // we'd silently append rows into an orphaned subtree.
      if (!isCurrentHash(selection.expectedHash) || !container.isConnected) return;
      btn.remove();
      for (const row of result.messages) {
        container.appendChild(buildRow(row, selection));
      }
      if (result.nextPageToken) {
        mountLoadMore(container, selection, result.nextPageToken);
      }
    } catch (err) {
      if (!isCurrentHash(selection.expectedHash) || !container.isConnected) return;
      btn.disabled = false;
      btn.textContent = originalLabel ?? '더 보기';
      flashNear(btn, `더 불러오기 실패: ${errorMessage(err)}`);
    }
  });
  container.appendChild(btn);
}

function flashNear(anchor: HTMLElement, message: string): void {
  // Scope dedup to "the flash that belongs to THIS anchor" (its
  // immediate next sibling) — using a parent-wide selector like
  // `:scope > .flash` would let a row-delete flash wipe an unrelated
  // load-more flash and vice-versa, since both anchors share
  // rowsContainer as parent.
  const next = anchor.nextElementSibling;
  if (next?.classList.contains('flash')) {
    next.remove();
  }
  const f = document.createElement('div');
  f.className = 'flash';
  f.textContent = message;
  anchor.after(f);
  // Guard against removing a node that was moved/re-parented by a
  // later refresh — element.remove() on an already-detached node is a
  // no-op so this is safe.
  setTimeout(() => f.remove(), 2500);
}
