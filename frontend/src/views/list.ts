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
// container/wrap .isConnected — "is the node I captured still in the
// document tree?". That single signal is the real staleness authority: on
// mobile every navigation/refresh detaches the old container (the next
// view does root.innerHTML=''), and on the desktop master-detail shell the
// list pane *persists* across detail navigations (the hash changes while
// the pane stays mounted), so an isCurrentHash check would wrongly abort
// load-more/bulk there. Connectivity is correct for both layouts.

import { archive, listRecent, markRead, trash, type GmailMessageRow } from '../gmail';
import {
  cacheRowSummary,
  invalidate,
  isHidden,
  prefetchMessage,
  prefetchSenderContext,
} from '../gmail_prefetch';
import { navigate } from '../router';
import { confirmAction } from '../dialog';
import { errorMessage, formatRpcError, relativeTime, shortFrom } from '../format';
import { setPullToRefreshHandler } from '../pull_to_refresh';
import { buildErrorBanner, buildRowSkeleton, buildViewHeader, showFlash } from './ui';
import { triggerImpactHaptic } from '../app_settings';

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
  selected: Map<string, SelectedMail>;
  selecting: boolean;
  busy: boolean;
  actionBar?: HTMLElement;
}

export async function renderList(root: HTMLElement, initData: string): Promise<void> {
  root.innerHTML = '';
  const header = buildHeader();
  root.appendChild(header);
  setPullToRefreshHandler(() => renderList(root, initData));

  // Container the load-more handler appends into. Keeping it separate
  // from the page-level root lets us swap the "더 보기" button in/out
  // without disturbing the header.
  const rowsContainer = document.createElement('div');
  rowsContainer.className = 'email-list';
  root.appendChild(rowsContainer);
  const selection = createSelectionState(root, rowsContainer, initData);

  // Skeleton placeholders mimic the .email-row geometry so the page
  // doesn't pop blank → populated. buildLoadingNode is still around
  // for non-list views.
  const status = buildRowSkeleton(6);
  rowsContainer.appendChild(status);

  try {
    const result = await listRecent(initData);
    if (!rowsContainer.isConnected) return;
    status.remove();
    if (result.messages.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = 'no mail in the last 7 days';
      rowsContainer.appendChild(empty);
      return;
    }
    for (const row of result.messages) {
      // Skip rows the operator just archived/trashed from the detail view:
      // that action navigates back here before its RPC settles, so
      // listRecent can still return the row for a beat. isHidden keeps it
      // out of view until the mutation lands (or it's un-hidden on failure).
      if (isHidden(row.id)) continue;
      rowsContainer.appendChild(buildRow(row, selection));
    }
    if (result.nextPageToken) {
      mountLoadMore(rowsContainer, selection, result.nextPageToken);
    }
  } catch (err) {
    if (!rowsContainer.isConnected) return;
    status.remove();
    rowsContainer.appendChild(
      buildErrorBanner(`메일 목록 로드 실패: ${formatRpcError(err)}`),
    );
  }
}

function buildHeader(): HTMLElement {
  return buildViewHeader({
    title: 'mail',
  });
}

function createSelectionState(
  root: HTMLElement,
  rowsContainer: HTMLElement,
  initData: string,
): SelectionState {
  return {
    root,
    rowsContainer,
    initData,
    selected: new Map(),
    selecting: false,
    busy: false,
  };
}

function buildRow(row: GmailMessageRow, selection: SelectionState): HTMLElement {
  // Stash this row so the detail view can paint subject/from/when
  // immediately when the operator drills in — no "메일 불러오는 중…"
  // flash while the detail RPC is in flight.
  cacheRowSummary(row);

  const wrap = document.createElement('div');
  wrap.className = 'email-row-wrap';
  // Tag the wrap with its message id so the desktop master-detail shell
  // can mark this row selected while its detail pane is open. Inert on
  // mobile (a data-* attribute changes nothing about touch rendering).
  wrap.dataset.messageId = row.id;
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
      // Soft impact = 'mode changed, you're in selection now'. Soft
      // rather than medium so it reads as a continuous push from the
      // long-press itself, not a fresh tap.
      triggerImpactHaptic('soft');
      enterSelectionMode(selection);
      toggleSelected(selection, row, wrap, true);
    }, longPressMs);
    // Fire the detail + sender-context RPCs the moment the finger
    // touches the row. Wrapped in try/catch defensively: a thrown
    // error here would otherwise interrupt the pointerdown listener
    // and leave the press timer set, which can manifest as "tapping
    // a mail does nothing". Both prefetch helpers are designed to
    // never throw synchronously, but the wrapper makes that contract
    // unbreakable.
    if (!selection.selecting) {
      try {
        prefetchMessage(selection.initData, row.id);
        prefetchSenderContext(selection.initData, row.from);
      } catch (err) {
        console.warn('mail row prefetch failed', err);
      }
    }
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
    // Medium impact on bulk action triggers — destructive (trash) gets
    // upgraded later inside runBulkAction once the confirm clears.
    // Cancel is just a selection clear; light enough to fall through
    // to selectionChanged via the same medium hit.
    triggerImpactHaptic('medium');
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
      if (!item.wrap.isConnected) continue;
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

  if (!selection.rowsContainer.isConnected) return;

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
      showFlash(`${successCount} ${actionLabel}`, 'success');
    }
  } else {
    if (selection.selected.size === 0) {
      exitSelectionMode(selection);
    } else {
      renderSelectionBar(selection);
    }
    showFlash(
      `${successCount} done · ${failed.length} failed: ${failed[0]}`,
      'error',
    );
  }
}

function applyBulkAction(initData: string, action: BulkAction, id: string): Promise<unknown> {
  switch (action) {
    case 'read':
      return markRead(initData, id);
    case 'archive':
      // Drop the cached summary + any in-flight detail prefetch so the
      // detail view can't paint this row back from cache after the
      // bulk action moves it out of the inbox.
      return archive(initData, id).then((res) => {
        invalidate(id);
        return res;
      });
    case 'trash':
      return trash(initData, id).then((res) => {
        invalidate(id);
        return res;
      });
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
// with the new token. Staleness is gated on container.isConnected, so a
// fetch that resolves after the list was torn down (mobile navigation, or
// a family switch in the desktop shell) won't inject into a detached tree.
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
      // Container-attachment check: a refresh or navigation detaches the
      // captured rowsContainer, so appending here would inject rows into an
      // orphaned subtree. We deliberately do NOT also gate on the hash —
      // the desktop master pane persists across detail hash changes.
      if (!container.isConnected) return;
      btn.remove();
      for (const row of result.messages) {
        if (isHidden(row.id)) continue;
        container.appendChild(buildRow(row, selection));
      }
      if (result.nextPageToken) {
        mountLoadMore(container, selection, result.nextPageToken);
      }
    } catch (err) {
      if (!container.isConnected) return;
      btn.disabled = false;
      btn.textContent = originalLabel ?? '더 보기';
      showFlash(`load more failed: ${errorMessage(err)}`, 'error');
    }
  });
  container.appendChild(btn);
}

