// views/chat.ts — Deneb chat view.
//
// Multi-turn Q&A surface backed by miniapp.chat.send. Conversation history
// lives in a module variable so navigating away and back preserves the
// thread. Pressing the "새 대화" button clears the history and resets the
// session key (the backend then derives a fresh miniapp:<userId> key).
//
// Mount-token guard: `renderChat` bumps `mountToken` on every call (and so
// does the "새 대화" reset). `submit()` captures the token at entry and
// bails out of any post-await DOM/state mutation when the token has moved
// — that closes two races at once. (a) The user navigates away mid-RPC
// and back; `isCurrentHash` would return true again but the captured
// `list` element is detached. (b) The user hits "새 대화" mid-RPC; the
// late response would otherwise re-populate `history` and overwrite
// `activeSessionKey` with the old session's key.

import { sendChat, type ChatResult } from '../chat';
import { RpcError } from '../rpc';
import { renderMarkdown } from '../markdown';

interface Turn {
  role: 'user' | 'assistant' | 'error';
  text: string;
  meta?: string;
  // Cached rendered HTML for assistant turns so re-mounts (navigate-away
  // and back) skip the markdown parse.
  html?: string;
}

const maxHistoryTurns = 200;
let history: Turn[] = [];
let activeSessionKey: string | undefined;
let mountToken = 0;

export function renderChat(root: HTMLElement, initData: string): void {
  mountToken += 1;
  root.innerHTML = '';
  root.appendChild(
    buildHeader(() => {
      // Reset bumps the token so any in-flight RPC's late resolution
      // skips both DOM and state mutation.
      mountToken += 1;
      history = [];
      activeSessionKey = undefined;
      renderChat(root, initData);
    }),
  );

  const list = document.createElement('div');
  list.className = 'chat-list';
  root.appendChild(list);

  if (history.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = 'Deneb 와 대화를 시작하세요';
    list.appendChild(empty);
  } else {
    for (const t of history) {
      list.appendChild(buildBubble(t));
    }
  }

  root.appendChild(buildComposer(list, initData));
  scrollToBottom(list);
}

function buildHeader(onReset: () => void): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'view-header';
  wrap.innerHTML = `
    <span class="view-title">Deneb 채팅</span>
    <button class="link-button" type="button">새 대화</button>
  `;
  wrap.querySelector('button')!.addEventListener('click', onReset);
  return wrap;
}

function buildBubble(turn: Turn): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = `chat-bubble chat-bubble-${turn.role}`;

  if (turn.role === 'assistant') {
    const body = document.createElement('div');
    body.className = 'chat-bubble-body markdown';
    if (turn.html === undefined) {
      turn.html = renderMarkdown(turn.text || '(빈 응답)');
    }
    body.innerHTML = turn.html;
    wrap.appendChild(body);
  } else {
    const body = document.createElement('div');
    body.className = 'chat-bubble-body';
    body.textContent = turn.text;
    wrap.appendChild(body);
  }

  if (turn.meta) {
    const meta = document.createElement('div');
    meta.className = 'chat-bubble-meta';
    meta.textContent = turn.meta;
    wrap.appendChild(meta);
  }
  return wrap;
}

function buildComposer(list: HTMLElement, initData: string): HTMLElement {
  const form = document.createElement('form');
  form.className = 'chat-composer';
  form.innerHTML = `
    <textarea class="chat-input" rows="2" placeholder="메시지를 입력하세요 (Enter 로 전송, Shift+Enter 로 줄바꿈)"></textarea>
    <button type="submit" class="primary chat-send">전송</button>
  `;
  const input = form.querySelector('textarea') as HTMLTextAreaElement;
  const sendBtn = form.querySelector('button') as HTMLButtonElement;

  // Enter to submit, Shift+Enter for newline. `isComposing` is required
  // for Korean IME so Hangul composition keystrokes don't trigger a send.
  input.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter' && !ev.shiftKey && !ev.isComposing) {
      ev.preventDefault();
      form.requestSubmit();
    }
  });

  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    const msg = input.value.trim();
    if (!msg) return;
    input.value = '';
    input.style.height = '';
    void submit(list, initData, msg, sendBtn);
  });

  // Auto-resize textarea up to 6 lines of content.
  input.addEventListener('input', () => {
    input.style.height = 'auto';
    input.style.height = Math.min(input.scrollHeight, 6 * 22) + 'px';
  });

  return form;
}

async function submit(
  list: HTMLElement,
  initData: string,
  message: string,
  sendBtn: HTMLButtonElement,
): Promise<void> {
  const myToken = mountToken;

  // Empty-state placeholder goes away on first turn.
  list.querySelector('.empty-state')?.remove();

  // User bubble.
  const userTurn: Turn = { role: 'user', text: message };
  history.push(userTurn);
  trimHistory();
  list.appendChild(buildBubble(userTurn));

  // Loading bubble (replaced when response arrives or error occurs).
  const loading = document.createElement('div');
  loading.className = 'chat-bubble chat-bubble-assistant chat-bubble-loading';
  loading.innerHTML = `
    <div class="chat-bubble-body">
      <span class="chat-loading-dot"></span>
      <span class="chat-loading-dot"></span>
      <span class="chat-loading-dot"></span>
    </div>
    <div class="chat-bubble-meta chat-elapsed">0s</div>
  `;
  list.appendChild(loading);
  scrollToBottom(list);

  const start = performance.now();
  const elapsedEl = loading.querySelector('.chat-elapsed') as HTMLElement;
  const tick = window.setInterval(() => {
    const sec = Math.round((performance.now() - start) / 1000);
    elapsedEl.textContent = `${sec}s`;
  }, 1000);

  sendBtn.disabled = true;
  try {
    const result: ChatResult = await sendChat(initData, message, {
      sessionKey: activeSessionKey,
    });
    window.clearInterval(tick);
    // mountToken differs when the user reset the conversation or
    // re-entered the view. The captured `list`, `loading`, and `sendBtn`
    // refer to detached / stale DOM in that case; silently drop the
    // response rather than corrupt the live view's state.
    if (myToken !== mountToken) return;
    loading.remove();
    activeSessionKey = result.sessionKey;
    const assistant = pushAssistant(result);
    list.appendChild(buildBubble(assistant));
    scrollToBottom(list);
  } catch (err) {
    window.clearInterval(tick);
    if (myToken !== mountToken) return;
    loading.remove();
    const msgText =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    const errTurn: Turn = { role: 'error', text: `전송 실패: ${msgText}` };
    history.push(errTurn);
    trimHistory();
    list.appendChild(buildBubble(errTurn));
  } finally {
    // sendBtn comes from the same closure as `list`; if the view was
    // reset, this button is detached so the assignment is a no-op.
    sendBtn.disabled = false;
  }
}

function pushAssistant(result: ChatResult): Turn {
  const seconds = Math.round(result.durationMs / 1000);
  const parts: string[] = [];
  if (result.model) parts.push(result.model);
  parts.push(`${seconds}s`);
  if (result.outputTokens) parts.push(`${result.outputTokens.toLocaleString('ko-KR')} tok`);
  const turn: Turn = {
    role: 'assistant',
    text: result.response,
    meta: parts.join(' · '),
  };
  history.push(turn);
  trimHistory();
  return turn;
}

function trimHistory(): void {
  if (history.length > maxHistoryTurns) {
    history = history.slice(history.length - maxHistoryTurns);
  }
}

function scrollToBottom(list: HTMLElement): void {
  // rAF ensures the just-appended child has been laid out so
  // scrollHeight reflects the new content.
  requestAnimationFrame(() => {
    list.scrollTop = list.scrollHeight;
  });
}
