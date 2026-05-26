// views/session_transcript.ts — show recent messages of a single session.
//
// Reached by tapping a row in the "최근 세션" list. Lays messages out
// as a vertical timeline; each bubble is colored by role (user / assistant /
// tool). No interaction yet — open-and-continue lives in chat, not Mini App.

import { getTranscript, type TranscriptMessage } from '../sessions';
import { RpcError } from '../rpc';
import { isCurrentHash, navigate } from '../router';

export async function renderSessionTranscript(
  root: HTMLElement,
  initData: string,
  sessionKey: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  const header = document.createElement('div');
  header.className = 'view-header';
  header.innerHTML = `
    <button class="link-button">← 세션 목록</button>
    <span class="view-title">대화 기록</span>
    <span></span>
  `;
  header.querySelector('button')!.addEventListener('click', () =>
    navigate({ name: 'sessions' }),
  );
  root.appendChild(header);

  const status = document.createElement('div');
  status.className = 'loading';
  status.textContent = '대화 불러오는 중…';
  root.appendChild(status);

  try {
    const result = await getTranscript(initData, sessionKey, 50);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    paintMessages(root, sessionKey, result.messages, result.total);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    const msgText =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    const banner = document.createElement('div');
    banner.className = 'error';
    banner.textContent = `대화 로드 실패: ${msgText}`;
    root.appendChild(banner);
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
    empty.textContent = '메시지가 없습니다';
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

  // Chat action: resume this session in chat. SessionKey is the
  // original (no `miniapp-*` prefix) so the same thread continues.
  // No prefill — user just keeps talking.
  const chatActions = document.createElement('div');
  chatActions.className = 'action-bar chat-action-bar';
  const chatBtn = document.createElement('button');
  chatBtn.className = 'action-button action-secondary';
  chatBtn.type = 'button';
  chatBtn.textContent = '💬 대화';
  chatBtn.addEventListener('click', () =>
    navigate({ name: 'chat', ctx: { kind: 'session', id: sessionKey, intent: 'continue' } }),
  );
  chatActions.appendChild(chatBtn);
  root.appendChild(chatActions);
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
