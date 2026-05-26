// views/list.ts — Gmail inbox triage queue.
//
// Calls miniapp.gmail.list_recent and renders one row per message. Tap on
// the row → navigate to detail. Each row also exposes an inline 🗑 button
// for quick triage without entering the detail view; the row itself stops
// being a <button> (so we can nest a sibling delete button) and uses a
// div with role="button" for keyboard / screen-reader parity.
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
import { RpcError } from '../rpc';
import { isCurrentHash, navigate } from '../router';
import { confirmAction } from '../dialog';
import { relativeTime, shortFrom } from '../format';

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

  const status = document.createElement('div');
  status.className = 'loading';
  status.textContent = '메일 불러오는 중…';
  rowsContainer.appendChild(status);

  try {
    const result = await listRecent(initData);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    if (result.messages.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = '최근 7일간 받은 메일이 없습니다 ✨';
      rowsContainer.appendChild(empty);
      return;
    }
    for (const row of result.messages) {
      rowsContainer.appendChild(buildRow(row, initData, expectedHash));
    }
    if (result.nextPageToken) {
      mountLoadMore(rowsContainer, initData, result.nextPageToken, expectedHash);
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    const msg =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    const banner = document.createElement('div');
    banner.className = 'error';
    banner.textContent = `메일 목록 로드 실패: ${msg}`;
    rowsContainer.appendChild(banner);
  }
}

function buildHeader(onRefresh: () => void): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'view-header';
  wrap.innerHTML = `
    <span class="view-title">받은 편지함</span>
    <button class="link-button" aria-label="새로고침">새로고침</button>
  `;
  wrap.querySelector('button')!.addEventListener('click', onRefresh);
  return wrap;
}

function buildRow(row: GmailMessageRow, initData: string, expectedHash: string): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'email-row-wrap';
  if (row.isUnread) wrap.classList.add('email-row-unread');

  const main = document.createElement('div');
  main.className = 'email-row';
  main.setAttribute('role', 'button');
  main.tabIndex = 0;

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

  // Set the inert flag the moment we initiate a navigation so a quick
  // second tap on the 🗑 button can't fire a delete against the row
  // the user has already left.
  const openDetail = () => {
    wrap.dataset.navigating = '1';
    navigate({ name: 'detail', messageId: row.id });
  };
  main.addEventListener('click', openDetail);
  main.addEventListener('keydown', (e) => {
    // e.repeat guards against held Space/Enter spamming navigate(); a
    // native <button> handles repeat at the OS level, but we lost that
    // when switching to a div role=button.
    if (e.repeat) return;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openDetail();
    }
  });
  wrap.appendChild(main);

  // Inline action stack. Order matches the detail-view action bar so the
  // gesture is consistent: read first (lightest), archive (removes from
  // inbox), delete (destructive).
  const actions = document.createElement('div');
  actions.className = 'email-row-actions';

  // 📌 읽음: only meaningful if the row is currently unread. We still
  // render the button when read (greyed out via disabled) so the layout
  // doesn't reflow between rows.
  const readBtn = document.createElement('button');
  readBtn.className = 'email-row-action';
  readBtn.type = 'button';
  readBtn.setAttribute('aria-label', '읽음');
  readBtn.textContent = '📌';
  if (!row.isUnread) {
    readBtn.disabled = true;
    readBtn.title = '이미 읽음';
  }
  readBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    void handleRowMarkRead(wrap, readBtn, row, initData, expectedHash);
  });
  actions.appendChild(readBtn);

  const archBtn = document.createElement('button');
  archBtn.className = 'email-row-action';
  archBtn.type = 'button';
  archBtn.setAttribute('aria-label', '보관');
  archBtn.textContent = '📁';
  archBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    void handleRowArchive(wrap, archBtn, row, initData, expectedHash);
  });
  actions.appendChild(archBtn);

  const del = document.createElement('button');
  del.className = 'email-row-action email-row-action-danger';
  del.type = 'button';
  del.setAttribute('aria-label', '삭제');
  del.textContent = '🗑';
  del.addEventListener('click', (e) => {
    e.stopPropagation();
    void handleRowDelete(wrap, del, row, initData, expectedHash);
  });
  actions.appendChild(del);

  wrap.appendChild(actions);

  return wrap;
}

// handleRowMarkRead: in-place toggle. No confirm dialog — mark-read is
// non-destructive (the operator can always re-mark unread from Gmail
// directly), and a confirm on every click would defeat the "one-tap
// triage" point of inline actions. On success the row stays in place but
// loses its unread border + the button greys out.
async function handleRowMarkRead(
  wrap: HTMLElement,
  button: HTMLButtonElement,
  row: GmailMessageRow,
  initData: string,
  expectedHash: string,
): Promise<void> {
  if (wrap.dataset.navigating === '1' || !isCurrentHash(expectedHash) || !wrap.isConnected) {
    return;
  }
  if (button.disabled) return;
  button.disabled = true;
  try {
    await markRead(initData, row.id);
    if (!wrap.isConnected) return;
    wrap.classList.remove('email-row-unread');
    button.title = '이미 읽음';
    row.isUnread = false;
  } catch (err) {
    button.disabled = false;
    const msg = err instanceof RpcError ? err.message : err instanceof Error ? err.message : err;
    flashNear(wrap, `읽음 처리 실패: ${msg}`);
  }
}

// handleRowArchive: also no confirm. Archiving in Gmail is reversible
// (the message returns to the inbox if anyone replies, and the operator
// can search `in:all from:...` to find it), so the speed of one-tap
// archive beats the safety of a confirm. trash gets the confirm because
// it moves to a 30-day-then-gone bucket.
async function handleRowArchive(
  wrap: HTMLElement,
  button: HTMLButtonElement,
  row: GmailMessageRow,
  initData: string,
  expectedHash: string,
): Promise<void> {
  if (wrap.dataset.navigating === '1' || !isCurrentHash(expectedHash) || !wrap.isConnected) {
    return;
  }
  if (button.disabled) return;
  button.disabled = true;
  try {
    await archive(initData, row.id);
    if (!wrap.isConnected) return;
    wrap.remove();
  } catch (err) {
    button.disabled = false;
    const msg = err instanceof RpcError ? err.message : err instanceof Error ? err.message : err;
    flashNear(wrap, `보관 실패: ${msg}`);
  }
}

async function handleRowDelete(
  wrap: HTMLElement,
  button: HTMLButtonElement,
  row: GmailMessageRow,
  initData: string,
  expectedHash: string,
): Promise<void> {
  // Bail if we got here from a tap that landed on 🗑 AFTER a tap on
  // the main row started navigating away — the user shouldn't have a
  // background delete fire against the email they just opened.
  if (wrap.dataset.navigating === '1' || !isCurrentHash(expectedHash) || !wrap.isConnected) {
    return;
  }
  // Disable BEFORE awaiting confirm: a double-tap on 🗑 would otherwise
  // queue two confirm dialogs (or one dialog + one already-resolved
  // path) and fire two trash RPCs — the second hits 404 and the error
  // path flashes on a detached node.
  if (button.disabled) return;
  button.disabled = true;
  try {
    const subjectPreview = (row.subject || '(제목 없음)').slice(0, 40);
    const ok = await confirmAction(`"${subjectPreview}" 메일을 휴지통으로 옮길까요?`);
    if (!ok) {
      button.disabled = false;
      return;
    }
    // Re-check after the modal: the user may have navigated away
    // while the confirm dialog was up.
    if (!isCurrentHash(expectedHash) || !wrap.isConnected) {
      button.disabled = false;
      return;
    }
    await trash(initData, row.id);
    wrap.remove();
  } catch (err) {
    button.disabled = false;
    const msg = err instanceof RpcError ? err.message : err instanceof Error ? err.message : err;
    flashNear(wrap, `삭제 실패: ${msg}`);
  }
}

// mountLoadMore appends a "더 보기" button below the rendered rows; the
// button replaces itself with a loading state on click, fetches the next
// page, appends its rows, and (if more pages remain) re-mounts itself
// with the new token. expectedHash threads the route token through so a
// stale fetch from a stale page doesn't inject into the wrong view.
function mountLoadMore(
  container: HTMLElement,
  initData: string,
  pageToken: string,
  expectedHash: string,
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
      const result = await listRecent(initData, { pageToken });
      // Hash AND container-attachment check: an inbox → detail →
      // inbox round-trip leaves the hash identical but replaces
      // rowsContainer, so isCurrentHash alone would still pass and
      // we'd silently append rows into an orphaned subtree.
      if (!isCurrentHash(expectedHash) || !container.isConnected) return;
      btn.remove();
      for (const row of result.messages) {
        container.appendChild(buildRow(row, initData, expectedHash));
      }
      if (result.nextPageToken) {
        mountLoadMore(container, initData, result.nextPageToken, expectedHash);
      }
    } catch (err) {
      if (!isCurrentHash(expectedHash) || !container.isConnected) return;
      btn.disabled = false;
      btn.textContent = originalLabel ?? '더 보기';
      const msg =
        err instanceof RpcError
          ? err.message
          : err instanceof Error
            ? err.message
            : '알 수 없는 오류';
      flashNear(btn, `더 불러오기 실패: ${msg}`);
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
