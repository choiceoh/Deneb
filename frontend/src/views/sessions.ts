// views/sessions.ts — Recent sessions list.
//
// Read-only for now: tapping a row just returns to home. A "open session"
// flow (load transcript, /resume, etc.) is left as future work.

import { recentSessions, type SessionRow } from '../sessions';
import { RpcError } from '../rpc';
import { relativeTime } from '../format';
import { navigate } from '../router';

export async function renderSessions(root: HTMLElement, initData: string): Promise<void> {
  root.innerHTML = '';

  const header = document.createElement('div');
  header.className = 'view-header';
  header.innerHTML = `
    <span class="view-title">최근 세션</span>
    <button class="link-button">새로고침</button>
  `;
  header.querySelector('button')!.addEventListener('click', () => {
    void renderSessions(root, initData);
  });
  root.appendChild(header);

  const status = document.createElement('div');
  status.className = 'loading';
  status.textContent = '세션 불러오는 중…';
  root.appendChild(status);

  try {
    const { sessions } = await recentSessions(initData, { limit: 20 });
    status.remove();
    if (sessions.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = '최근 세션이 없습니다';
      root.appendChild(empty);
      return;
    }
    for (const s of sessions) {
      root.appendChild(buildRow(s));
    }
  } catch (err) {
    status.remove();
    const msg =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    const banner = document.createElement('div');
    banner.className = 'error';
    banner.textContent = `세션 목록 로드 실패: ${msg}`;
    root.appendChild(banner);
  }
}

function buildRow(s: SessionRow): HTMLElement {
  const el = document.createElement('button');
  el.className = 'session-row';

  const top = document.createElement('div');
  top.className = 'session-row-meta';
  top.innerHTML = `
    <span class="session-row-key"></span>
    <span class="session-row-time"></span>
  `;
  (top.querySelector('.session-row-key') as HTMLElement).textContent = s.label || s.key;
  (top.querySelector('.session-row-time') as HTMLElement).textContent = s.updatedAtMs
    ? relativeTime(new Date(s.updatedAtMs).toISOString())
    : '';
  el.appendChild(top);

  const middle = document.createElement('div');
  middle.className = 'session-row-tags';
  const tags = [s.channel, s.kind, s.status, s.model].filter(Boolean) as string[];
  middle.textContent = tags.join(' · ');
  el.appendChild(middle);

  if (s.totalTokens) {
    const stats = document.createElement('div');
    stats.className = 'session-row-stats';
    const runtimeSec = s.runtimeMs ? Math.round(s.runtimeMs / 1000) : null;
    const parts = [
      `tokens ${s.totalTokens.toLocaleString('ko-KR')}`,
      runtimeSec !== null ? `런타임 ${runtimeSec}s` : null,
    ].filter(Boolean);
    stats.textContent = parts.join(' · ');
    el.appendChild(stats);
  }

  el.addEventListener('click', () => navigate({ name: 'home' }));
  return el;
}
