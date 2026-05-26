// views/chat.ts — Deneb chat view.
//
// Multi-turn Q&A surface backed by miniapp.chat.send. Conversation history
// lives in a module variable so navigating away and back preserves the
// thread. Pressing the "새 대화" button clears the history and resets the
// session key (the backend then derives a fresh miniapp:<userId> key).

import { sendChat, type ChatResult } from '../chat';
import { RpcError } from '../rpc';
import { isCurrentHash } from '../router';
import { renderMarkdown } from '../markdown';

interface Turn {
  role: 'user' | 'assistant' | 'error';
  text: string;
  meta?: string;
}

let history: Turn[] = [];
let activeSessionKey: string | undefined;

export function renderChat(root: HTMLElement, initData: string): void {
  root.innerHTML = '';
  root.appendChild(buildHeader(() => {
    history = [];
    activeSessionKey = undefined;
    renderChat(root, initData);
  }));

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

  const composer = buildComposer(root, list, initData);
  root.appendChild(composer);

  // Auto-scroll on initial paint so the latest turn is visible.
  requestAnimationFrame(() => list.scrollTop = list.scrollHeight);
}

function buildHeader(onReset: () => void): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'view-header';
  wrap.innerHTML = `
    <span class="view-title">Deneb 채팅</span>
    <button class="link-button">새 대화</button>
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
    body.innerHTML = renderMarkdown(turn.text || '(빈 응답)');
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

function buildComposer(
  root: HTMLElement,
  list: HTMLElement,
  initData: string,
): HTMLElement {
  const form = document.createElement('form');
  form.className = 'chat-composer';
  form.innerHTML = `
    <textarea class="chat-input" rows="2" placeholder="메시지를 입력하세요 (Enter 로 전송, Shift+Enter 로 줄바꿈)"></textarea>
    <button type="submit" class="primary chat-send">전송</button>
  `;
  const input = form.querySelector('textarea') as HTMLTextAreaElement;
  const sendBtn = form.querySelector('button') as HTMLButtonElement;

  // Enter to submit, Shift+Enter for newline — standard chat UX.
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
    void submit(root, list, initData, msg, sendBtn);
  });

  // Auto-resize textarea up to 6 rows.
  input.addEventListener('input', () => {
    input.style.height = 'auto';
    input.style.height = Math.min(input.scrollHeight, 6 * 22) + 'px';
  });

  return form;
}

async function submit(
  _root: HTMLElement,
  list: HTMLElement,
  initData: string,
  message: string,
  sendBtn: HTMLButtonElement,
): Promise<void> {
  const expectedHash = location.hash;

  // Empty-state placeholder goes away on first turn.
  list.querySelector('.empty-state')?.remove();

  // User bubble.
  const userTurn: Turn = { role: 'user', text: message };
  history.push(userTurn);
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
  list.scrollTop = list.scrollHeight;

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
    if (!isCurrentHash(expectedHash)) {
      // User navigated away; keep history updated but skip DOM mutation.
      activeSessionKey = result.sessionKey;
      pushAssistant(result);
      return;
    }
    loading.remove();
    activeSessionKey = result.sessionKey;
    const assistant = pushAssistant(result);
    list.appendChild(buildBubble(assistant));
    list.scrollTop = list.scrollHeight;
  } catch (err) {
    window.clearInterval(tick);
    if (!isCurrentHash(expectedHash)) return;
    loading.remove();
    const msgText =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    const errTurn: Turn = { role: 'error', text: `전송 실패: ${msgText}` };
    history.push(errTurn);
    list.appendChild(buildBubble(errTurn));
  } finally {
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
  return turn;
}
