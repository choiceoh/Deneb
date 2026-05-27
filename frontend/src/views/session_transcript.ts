// views/session_transcript.ts — show recent messages of a single session.
//
// Reached by tapping a row in the topics list. Lays messages out as a
// vertical timeline; each bubble is colored by role (user / assistant /
// tool). No interaction yet — open-and-continue lives in chat, not Mini App.

import { getTranscript, type TranscriptMessage } from '../sessions';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

export async function renderSessionTranscript(
  root: HTMLElement,
  initData: string,
  sessionKey: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'transcript',
      left: { label: '← topics', onClick: () => navigate({ name: 'sessions' }) },
    }),
  );

  const status = buildLoadingNode('대화 불러오는 중…');
  root.appendChild(status);

  try {
    const result = await getTranscript(initData, sessionKey, 50);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    paintMessages(root, sessionKey, result.messages, result.total);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`대화 로드 실패: ${formatRpcError(err)}`));
  }
}

function paintMessages(
  root: HTMLElement,
  sessionKey: string,
  messages: TranscriptMessage[],
  total: number,
): void {
  const keyLabel = document.createElement('div');
  keyLabel.className = 'muted transcript-key';
  keyLabel.textContent = sessionKey;
  root.appendChild(keyLabel);

  if (messages.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = 'no messages';
    root.appendChild(empty);
    return;
  }

  const list = document.createElement('div');
  list.className = 'transcript';
  for (const m of messages) {
    list.appendChild(buildBubble(m));
  }
  root.appendChild(list);

  if (total > messages.length) {
    const note = document.createElement('div');
    note.className = 'muted';
    note.textContent = `최근 ${messages.length}건 표시 · 전체 ${total.toLocaleString('ko-KR')}건`;
    root.appendChild(note);
  }
}

function buildBubble(m: TranscriptMessage): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = `transcript-message role-${m.role}`;

  const meta = document.createElement('div');
  meta.className = 'transcript-meta';
  meta.textContent = roleLabel(m.role);
  if (m.timestampMs) {
    const time = document.createElement('span');
    time.className = 'transcript-time';
    time.textContent = new Date(m.timestampMs).toLocaleString('ko-KR');
    meta.appendChild(time);
  }
  wrap.appendChild(meta);

  const body = document.createElement('pre');
  body.className = 'transcript-body';
  body.textContent = m.content || '(빈 메시지)';
  wrap.appendChild(body);

  return wrap;
}

function roleLabel(role: string): string {
  switch (role) {
    case 'user':
      return '👤 사용자';
    case 'assistant':
      return '🤖 어시스턴트';
    case 'tool_result':
      return '🛠 도구 결과';
    case 'system':
      return '⚙️ 시스템';
    default:
      return role;
  }
}
