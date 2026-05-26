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
// its rows. Token is tracked in module-scoped closure state for the
// current render only — re-rendering (refresh) clobbers it, as intended.

import { listRecent, trash, type GmailMessageRow } from '../gmail';
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
      rowsContainer.appendChild(buildRow(row, initData));
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

function buildRow(row: GmailMessageRow, initData: string): HTMLElement {
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

  const openDetail = () => navigate({ name: 'detail', messageId: row.id });
  main.addEventListener('click', openDetail);
  main.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openDetail();
    }
  });
  wrap.appendChild(main);

  const del = document.createElement('button');
  del.className = 'email-row-delete';
  del.setAttribute('aria-label', '삭제');
  del.textContent = '🗑';
  del.addEventListener('click', (e) => {
    e.stopPropagation();
    void handleRowDelete(wrap, del, row, initData);
  });
  wrap.appendChild(del);

  return wrap;
}

async function handleRowDelete(
  wrap: HTMLElement,
  button: HTMLButtonElement,
  row: GmailMessageRow,
  initData: string,
): Promise<void> {
  const subjectPreview = (row.subject || '(제목 없음)').slice(0, 40);
  const ok = await confirmAction(`"${subjectPreview}" 메일을 휴지통으로 옮길까요?`);
  if (!ok) return;

  button.disabled = true;
  try {
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
  btn.textContent = '더 보기';
  btn.addEventListener('click', async () => {
    btn.disabled = true;
    const originalLabel = btn.textContent;
    btn.textContent = '불러오는 중…';
    try {
      const result = await listRecent(initData, { pageToken });
      if (!isCurrentHash(expectedHash)) return;
      btn.remove();
      for (const row of result.messages) {
        container.appendChild(buildRow(row, initData));
      }
      if (result.nextPageToken) {
        mountLoadMore(container, initData, result.nextPageToken, expectedHash);
      }
    } catch (err) {
      if (!isCurrentHash(expectedHash)) return;
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
  const existing = anchor.parentElement?.querySelector(':scope > .flash');
  if (existing) existing.remove();
  const f = document.createElement('div');
  f.className = 'flash';
  f.textContent = message;
  anchor.after(f);
  setTimeout(() => f.remove(), 2500);
}
