// views/sessions.ts — Recent sessions list.
//
// Read-only for now: tapping a row just returns to home. A "open session"
// flow (load transcript, /resume, etc.) is left as future work.

import { recentSessions, type SessionRow } from '../sessions';
import { formatRpcError, relativeTime } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

export async function renderSessions(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: '최근 세션',
      right: { label: '새로고침', onClick: () => void renderSessions(root, initData) },
    }),
  );

  const status = buildLoadingNode('세션 불러오는 중…');
  root.appendChild(status);

  try {
    const { sessions } = await recentSessions(initData, { limit: 20 });
    if (!isCurrentHash(expectedHash)) return;
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
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`세션 목록 로드 실패: ${formatRpcError(err)}`));
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

  el.addEventListener('click', () => navigate({ name: 'sessionTranscript', sessionKey: s.key }));
  return el;
}
