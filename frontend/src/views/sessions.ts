// views/sessions.ts — Recent topics list.
//
// Backed by miniapp.sessions.recent — the underlying RPC still returns one
// row per agent run, but in forum-supergroup mode each row's key carries a
// "telegram:CHAT:thread:N" suffix that identifies the owning topic. The
// row display surfaces that suffix as a "topic #N" tag so the user can
// see which Telegram topic each session belongs to. Sessions without a
// thread suffix render as plain rows (1:1 chats, General topic, btw/cron).
//
// Read-only for now: tapping a row opens the transcript. A "resume here"
// or "switch active topic" flow is left as future work.

import { recentSessions, type SessionRow } from '../sessions';
import { formatRpcError, relativeTime } from '../format';
import { isCurrentHash, navigate } from '../router';
import { setPullToRefreshHandler } from '../pull_to_refresh';
import { buildChipRow, buildErrorBanner, buildRowSkeleton, buildViewHeader } from './ui';

export async function renderSessions(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'topics',
    }),
  );
  // Horizontal chip strip below the header. Today it carries a single
  // action chip ("+ 새 토픽"); the same row is the natural home for
  // real topic-filter chips once a deneb-side topic store lands (the
  // forum-topic-created event listener that M4b needs). Keeping the
  // structure here from day one means that future change is a chip
  // append, not a header re-layout.
  root.appendChild(
    buildChipRow([{ label: '+ 새 토픽', onClick: () => navigate({ name: 'topicNew' }) }]),
  );
  setPullToRefreshHandler(() => renderSessions(root, initData));

  const status = buildRowSkeleton(6);
  root.appendChild(status);

  try {
    const { sessions } = await recentSessions(initData, { limit: 20 });
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    if (sessions.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = 'no topics yet';
      root.appendChild(empty);
      return;
    }
    for (const s of sessions) {
      root.appendChild(buildRow(s));
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`토픽 목록 로드 실패: ${formatRpcError(err)}`));
  }
}

// extractThreadID pulls "N" out of a "telegram:CHAT:thread:N" key. Returns
// null for any key without that exact suffix (1:1 chats, General messages,
// non-telegram channels). Kept as a small helper because both the row
// display and any future grouping logic will want the same parse rule.
function extractThreadID(key: string): string | null {
  const m = key.match(/:thread:(\d+)$/);
  return m ? m[1] : null;
}

function buildRow(s: SessionRow): HTMLElement {
  const el = document.createElement('button');
  el.className = 'session-row';
  // Tag the row with its session key so the desktop master-detail shell
  // can mark it selected while its transcript pane is open (inert on mobile).
  el.dataset.sessionKey = s.key;

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

  // Tag row: prepend "topic #N" for forum-topic sessions so the user can
  // tell at a glance which rows belong to a thread vs the chat's General /
  // 1:1 / non-telegram path. Other tags (channel, kind, status, model)
  // follow unchanged.
  const middle = document.createElement('div');
  middle.className = 'session-row-tags';
  const threadID = extractThreadID(s.key);
  const tags = [
    threadID !== null ? `topic #${threadID}` : null,
    s.channel,
    s.kind,
    s.status,
    s.model,
  ].filter(Boolean) as string[];
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
