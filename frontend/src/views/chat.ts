// views/chat.ts — Deneb chat view.
//
// Two entry shapes:
//
//   #/chat                        — generic chat (default miniapp:<uid> session)
//   #/chat?ctx=mail:<id>:<intent> — context-attached chat: anchored to a
//   #/chat?ctx=wiki:<path>:question  specific mail / wiki page / session.
//   #/chat?ctx=session:<key>:continue  See router.ts ChatContext.
//
// Multi-turn history lives in a per-context map so each attached chat
// keeps its own thread; "새 대화" resets only the active context's entry.
// Plain (no-ctx) chats key into a shared "default" slot.
//
// Reset semantics: clearing thread.sessionKey to `undefined` would let
// the backend re-derive `miniapp:<userId>` and pull the previous
// transcript right back in, defeating the reset. So generic-chat reset
// mints a fresh client-side key (`newSessionKey`). Context-attached
// reset leaves the key undefined because hydrateContext will reassign
// the deterministic per-context key on the next render.
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
import { getMessage } from '../gmail';
import { getPage } from '../memory';
import { RpcError } from '../rpc';
import type { ChatContext } from '../router';
import { renderMarkdown } from '../markdown';

interface Turn {
  role: 'user' | 'assistant' | 'error';
  text: string;
  meta?: string;
  // Cached rendered HTML for assistant turns so re-mounts (navigate-away
  // and back) skip the markdown parse.
  html?: string;
}

interface ThreadState {
  history: Turn[];
  sessionKey?: string;
}

const maxHistoryTurns = 200;
const maxContextRunes = 3000;

// Per-context thread state. Key shape: "default" for generic chat, or
// `<kind>:<id>` for context-attached. Survives navigate-away/back; lost
// on full page reload (Telegram persists the underlying session
// server-side via miniapp.sessions.transcript for now).
const threads = new Map<string, ThreadState>();
let mountToken = 0;

function threadKey(ctx?: ChatContext): string {
  if (!ctx) return 'default';
  return `${ctx.kind}:${ctx.id}`;
}

function getThread(ctx?: ChatContext): ThreadState {
  const key = threadKey(ctx);
  let t = threads.get(key);
  if (!t) {
    t = { history: [] };
    threads.set(key, t);
  }
  return t;
}

export function renderChat(root: HTMLElement, initData: string, ctx?: ChatContext): void {
  mountToken += 1;
  root.innerHTML = '';
  const thread = getThread(ctx);

  root.appendChild(
    buildHeader(ctx, () => {
      // Reset bumps the token so any in-flight RPC's late resolution
      // skips both DOM and state mutation, then clears just this
      // thread's state. Other contexts' threads are untouched.
      //
      // Generic chats need a fresh client-side sessionKey on reset —
      // miniapp.chat.send deterministically derives `miniapp:<userId>`
      // from an undefined key and would replay the prior transcript.
      // Context chats reassign their deterministic key in
      // hydrateContext on the next render, so leaving them undefined
      // is correct.
      mountToken += 1;
      thread.history = [];
      thread.sessionKey = ctx ? undefined : newSessionKey();
      renderChat(root, initData, ctx);
    }),
  );

  // Context summary card — only when entering with ctx and no prior
  // history yet (so we don't pile a card on top of every reset).
  const contextSlot = document.createElement('div');
  contextSlot.dataset.role = 'chat-context-slot';
  root.appendChild(contextSlot);

  const list = document.createElement('div');
  list.className = 'chat-list';
  root.appendChild(list);

  if (thread.history.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.textContent = ctx ? '컨텍스트 불러오는 중…' : 'Deneb 와 대화를 시작하세요';
    list.appendChild(empty);
  } else {
    for (const t of thread.history) {
      list.appendChild(buildBubble(t));
    }
  }

  const composer = buildComposer(list, initData, ctx);
  root.appendChild(composer);

  if (ctx && thread.history.length === 0) {
    // Async: hydrate context card + prefill composer. The empty-state
    // banner says "컨텍스트 불러오는 중…" until the fetch lands.
    void hydrateContext(ctx, initData, composer, contextSlot, list, mountToken);
  }

  scrollToBottom(list);
}

function buildHeader(ctx: ChatContext | undefined, onReset: () => void): HTMLElement {
  const wrap = document.createElement('div');
  wrap.className = 'view-header';
  const title = ctx ? contextTitle(ctx) : 'Deneb 채팅';
  wrap.innerHTML = `
    <span class="view-title"></span>
    <button class="link-button" type="button">새 대화</button>
  `;
  (wrap.querySelector('.view-title') as HTMLElement).textContent = title;
  wrap.querySelector('button')!.addEventListener('click', onReset);
  return wrap;
}

function contextTitle(ctx: ChatContext): string {
  switch (ctx.kind) {
    case 'mail':
      return ctx.intent === 'reply' ? '메일 답장' : ctx.intent === 'analyze' ? '메일 분석' : '메일 질문';
    case 'wiki':
      return '위키 페이지';
    case 'session':
      return '세션 이어가기';
  }
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

function buildComposer(
  list: HTMLElement,
  initData: string,
  ctx: ChatContext | undefined,
): HTMLElement {
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
    void submit(list, initData, msg, sendBtn, ctx);
  });

  // Auto-resize textarea up to 6 lines of content.
  input.addEventListener('input', () => {
    input.style.height = 'auto';
    input.style.height = Math.min(input.scrollHeight, 6 * 22) + 'px';
  });

  return form;
}

// hydrateContext fetches the source content for a context-attached chat,
// renders a summary card at the top, pre-fills the composer with an
// intent-specific template, and assigns the deterministic per-context
// sessionKey so successive sends in this thread stay scoped.
async function hydrateContext(
  ctx: ChatContext,
  initData: string,
  composer: HTMLElement,
  contextSlot: Element,
  list: HTMLElement,
  myToken: number,
): Promise<void> {
  const thread = getThread(ctx);
  const inputEl = composer.querySelector('textarea') as HTMLTextAreaElement;

  if (ctx.kind === 'session' && ctx.intent === 'continue') {
    // Resume mode: no fetch, no prefill, just override sessionKey to
    // the original transcript's key so the agent picks up where it
    // left off. The empty-state banner stays meaningful — "이전 대화 이어서 시작".
    thread.sessionKey = ctx.id;
    if (myToken !== mountToken) return;
    renderEmptyContextCard(contextSlot, '세션 이어가기', `세션 키: ${ctx.id}`);
    const empty = list.querySelector('.empty-state') as HTMLElement | null;
    if (empty) empty.textContent = '이전 대화에 이어서 메시지를 입력하세요';
    return;
  }

  try {
    let prefill = '';
    let summary = '';
    switch (ctx.kind) {
      case 'mail': {
        const msg = await getMessage(initData, ctx.id);
        if (myToken !== mountToken) return;
        const body = capRunes(msg.body, maxContextRunes);
        const header = `From: ${msg.from}\nSubject: ${msg.subject || '(제목 없음)'}`;
        const intentPrefix = mailIntentPrefix(ctx.intent);
        prefill = `${intentPrefix}\n\n${header}\n\n${body}\n\n---\n`;
        summary = `${msg.subject || '(제목 없음)'} · ${msg.from}`;
        thread.sessionKey = `miniapp-mail:${ctx.id}`;
        renderContextCard(contextSlot, '📧 메일', summary);
        break;
      }
      case 'wiki': {
        const page = await getPage(initData, ctx.id);
        if (myToken !== mountToken) return;
        const body = capRunes(page.body, maxContextRunes);
        const title = page.title || ctx.id;
        prefill = `다음 위키 페이지에 대해 질문할 거야:\n\n# ${title}\n\n${body}\n\n---\n`;
        summary = title + (page.category ? ` · #${page.category}` : '');
        thread.sessionKey = `miniapp-wiki:${ctx.id}`;
        renderContextCard(contextSlot, '🧩 위키', summary);
        break;
      }
    }

    inputEl.value = prefill;
    // Resize textarea to fit prefill (capped at 6 rows by the listener).
    inputEl.dispatchEvent(new Event('input'));
    // Focus end of prefill so user types after the "---" separator.
    inputEl.setSelectionRange(prefill.length, prefill.length);

    const empty = list.querySelector('.empty-state') as HTMLElement | null;
    if (empty) empty.textContent = '컨텍스트가 준비됐어요. 질문을 추가하고 전송하세요.';
  } catch (err) {
    if (myToken !== mountToken) return;
    const msgText =
      err instanceof RpcError
        ? `${err.code} — ${err.message}`
        : err instanceof Error
          ? err.message
          : '알 수 없는 오류';
    renderEmptyContextCard(contextSlot, '컨텍스트 로드 실패', msgText);
    const empty = list.querySelector('.empty-state') as HTMLElement | null;
    if (empty) {
      empty.textContent = '컨텍스트 없이 일반 채팅으로 진행합니다.';
    }
  }
}

function mailIntentPrefix(intent: 'reply' | 'analyze' | 'question'): string {
  switch (intent) {
    case 'reply':
      return '다음 메일에 답장 초안을 작성해 줘:';
    case 'analyze':
      return '다음 메일을 분석해 줘 (핵심 요약, 이해관계자, 중요도, 리스크, 다음 단계):';
    case 'question':
      return '다음 메일에 대해 질문할 거야:';
  }
}

function renderContextCard(slot: Element, label: string, summary: string): void {
  const card = document.createElement('div');
  card.className = 'chat-context-card';
  const labelEl = document.createElement('span');
  labelEl.className = 'chat-context-card-label';
  labelEl.textContent = label;
  const sumEl = document.createElement('span');
  sumEl.className = 'chat-context-card-summary';
  sumEl.textContent = summary;
  card.appendChild(labelEl);
  card.appendChild(sumEl);
  slot.replaceWith(card);
}

function renderEmptyContextCard(slot: Element, label: string, body: string): void {
  renderContextCard(slot, label, body);
}

function capRunes(s: string, max: number): string {
  const runes = Array.from(s);
  if (runes.length <= max) return s;
  return runes.slice(0, max).join('') + '\n…(생략)';
}

async function submit(
  list: HTMLElement,
  initData: string,
  message: string,
  sendBtn: HTMLButtonElement,
  ctx: ChatContext | undefined,
): Promise<void> {
  const myToken = mountToken;
  const thread = getThread(ctx);

  // Empty-state placeholder goes away on first turn.
  list.querySelector('.empty-state')?.remove();

  // User bubble.
  const userTurn: Turn = { role: 'user', text: message };
  thread.history.push(userTurn);
  trimHistory(thread);
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
      sessionKey: thread.sessionKey,
    });
    window.clearInterval(tick);
    // mountToken differs when the user reset the conversation or
    // re-entered the view. The captured `list`, `loading`, and `sendBtn`
    // refer to detached / stale DOM in that case; silently drop the
    // response rather than corrupt the live view's state.
    if (myToken !== mountToken) return;
    loading.remove();
    thread.sessionKey = result.sessionKey;
    const assistant = pushAssistant(thread, result);
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
    thread.history.push(errTurn);
    trimHistory(thread);
    list.appendChild(buildBubble(errTurn));
  } finally {
    // sendBtn comes from the same closure as `list`; if the view was
    // reset, this button is detached so the assignment is a no-op.
    sendBtn.disabled = false;
  }
}

function pushAssistant(thread: ThreadState, result: ChatResult): Turn {
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
  thread.history.push(turn);
  trimHistory(thread);
  return turn;
}

function trimHistory(thread: ThreadState): void {
  if (thread.history.length > maxHistoryTurns) {
    thread.history = thread.history.slice(thread.history.length - maxHistoryTurns);
  }
}

function scrollToBottom(list: HTMLElement): void {
  // rAF ensures the just-appended child has been laid out so
  // scrollHeight reflects the new content.
  requestAnimationFrame(() => {
    list.scrollTop = list.scrollHeight;
  });
}

// newSessionKey mints a client-side sessionKey unique enough that the
// backend's deterministic fallback (`miniapp:<userId>`) never collides
// with it. Format mirrors the server's namespacing so logs stay easy
// to grep.
function newSessionKey(): string {
  const ts = Date.now();
  const rand = Math.random().toString(36).slice(2, 10);
  return `miniapp:fresh:${ts}-${rand}`;
}
