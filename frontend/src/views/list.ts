// views/list.ts — Gmail inbox triage queue.
//
// Calls miniapp.gmail.list_recent and renders one row per message. Tap on
// the row → navigate to detail. The list is read-only here; mark_read /
// archive happen from the detail view.

import { listRecent, type GmailMessageRow } from '../gmail';
import { RpcError } from '../rpc';
import { isCurrentHash, navigate } from '../router';
import { relativeTime, shortFrom } from '../format';

export async function renderList(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';
  const header = buildHeader(() => renderList(root, initData));
  root.appendChild(header);

  const status = document.createElement('div');
  status.className = 'loading';
  status.textContent = '메일 불러오는 중…';
  root.appendChild(status);

  try {
    const result = await listRecent(initData);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    if (result.messages.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = '최근 7일간 받은 메일이 없습니다 ✨';
      root.appendChild(empty);
      return;
    }
    for (const row of result.messages) {
      root.appendChild(buildRow(row));
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
    root.appendChild(banner);
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

function buildRow(row: GmailMessageRow): HTMLElement {
  const el = document.createElement('button');
  el.className = 'email-row';
  if (row.isUnread) el.classList.add('email-row-unread');

  const fromLine = document.createElement('div');
  fromLine.className = 'email-row-meta';
  fromLine.innerHTML = `
    <span class="email-row-from"></span>
    <span class="email-row-time"></span>
  `;
  (fromLine.querySelector('.email-row-from') as HTMLElement).textContent = shortFrom(row.from);
  (fromLine.querySelector('.email-row-time') as HTMLElement).textContent = relativeTime(row.date);
  el.appendChild(fromLine);

  const subject = document.createElement('div');
  subject.className = 'email-row-subject';
  subject.textContent = row.subject || '(제목 없음)';
  el.appendChild(subject);

  const snippet = document.createElement('div');
  snippet.className = 'email-row-snippet';
  snippet.textContent = row.snippet || '';
  el.appendChild(snippet);

  el.addEventListener('click', () => navigate({ name: 'detail', messageId: row.id }));
  return el;
}
