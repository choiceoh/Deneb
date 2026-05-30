// views/detail.ts — single-message view with [읽음] / [보관] / [삭제] / [닫기] actions.
//
// Auto-fires miniapp.gmail.mark_read on entry (fire-and-forget) so the
// message stops cluttering the inbox after the user opens it. Explicit
// [읽음] button is kept anyway — it's a no-op cost-wise and makes the
// model of "I've actioned this" visible.

import {
  analysisCached,
  analyzeMessage,
  archive,
  askMail,
  markRead,
  trash,
  type CachedAnalysis,
  type GmailMessageDetail,
  type ProjectRef,
  type QATurn,
} from '../gmail';
import {
  fetchMessage,
  hideMessage,
  unhideMessage,
} from '../gmail_prefetch';
import { isCurrentHash, navigate } from '../router';
import { errorMessage, formatRpcError, humanSize, relativeTime } from '../format';
import { renderMarkdown } from '../markdown';
import { confirmAction } from '../dialog';
import { buildErrorBanner, buildViewHeader, renderErrorView, showFlash } from './ui';
import { triggerImpactHaptic } from '../app_settings';

export async function renderDetail(
  root: HTMLElement,
  initData: string,
  messageId: string,
): Promise<void> {
  const expectedHash = location.hash;

  // Show a clear "loading mail" indicator so the operator sees
  // explicit feedback that the tap landed and a new view is
  // mounting. The previous optimistic-shell paint reused the list
  // row's subject/from/snippet here, but the result was visually
  // close enough to an inbox row that some taps read as "the click
  // did nothing" rather than "the detail view is loading". We still
  // get the perceived-latency win from prefetchMessage — the RPC
  // fired on pointerdown — so the loading text below is usually
  // visible for only one frame before paint() replaces it.
  root.innerHTML = '<div class="loading">메일 불러오는 중…</div>';

  try {
    const msg = await fetchMessage(initData, messageId);
    if (!isCurrentHash(expectedHash)) return;
    paint(root, initData, msg);
    // Auto mark-read in the background. Ignore the result; if it fails
    // the row keeps its UNREAD style on next list refresh and the user
    // can hit [읽음] explicitly.
    void markRead(initData, messageId).catch(() => undefined);
    // Fetch any pre-computed analysis (autonomous poller or a prior run) and,
    // if present, show it in the analyze slot + cite its related projects —
    // no manual tap needed. A miss leaves the manual analyze button as-is.
    void hydrateCachedAnalysis(root, initData, msg, expectedHash);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    renderErrorView(root, `메일 로드 실패: ${formatRpcError(err)}`, {
      label: '← 받은 편지함으로',
      onClick: () => navigate({ name: 'inbox' }),
    });
  }
}

function paint(root: HTMLElement, initData: string, msg: GmailMessageDetail): void {
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      // No title: the subject card directly below is this page's real
      // heading, so a generic "message" h1 would just be redundant chrome.
      left: { label: '← mail', onClick: () => navigate({ name: 'inbox' }) },
    }),
  );

  const meta = document.createElement('div');
  meta.className = 'card email-meta';
  meta.innerHTML = `
    <div class="email-subject"></div>
    <div class="row"><span class="label">From</span><span class="value"></span></div>
    <div class="row"><span class="label">To</span><span class="value"></span></div>
    <div class="row"><span class="label">When</span><span class="value"></span></div>
  `;
  const subjectEl = meta.querySelector('.email-subject') as HTMLElement;
  subjectEl.textContent = msg.subject || '(제목 없음)';
  const valueCells = meta.querySelectorAll('.value');
  (valueCells[0] as HTMLElement).textContent = msg.from;
  (valueCells[1] as HTMLElement).textContent = msg.to || '—';
  (valueCells[2] as HTMLElement).textContent = relativeTime(msg.date);
  root.appendChild(meta);

  // Slot for the related-projects card, filled asynchronously by
  // hydrateCachedAnalysis from a cached analysis's project links.
  const projectSlot = document.createElement('div');
  projectSlot.dataset.role = 'project-slot';
  root.appendChild(projectSlot);

  if (msg.attachments && msg.attachments.length > 0) {
    const attBox = document.createElement('div');
    attBox.className = 'card attachments';
    const label = document.createElement('div');
    label.className = 'label';
    label.textContent = '첨부';
    attBox.appendChild(label);
    for (const att of msg.attachments) {
      const chip = document.createElement('button');
      chip.className = 'attachment-chip';
      chip.type = 'button';
      chip.innerHTML = `
        <span class="attachment-name"></span>
        <span class="attachment-size"></span>
      `;
      (chip.querySelector('.attachment-name') as HTMLElement).textContent =
        att.filename || '(이름없음)';
      (chip.querySelector('.attachment-size') as HTMLElement).textContent = humanSize(att.size);
      chip.addEventListener('click', () => {
        triggerImpactHaptic('light');
        openAttachment(buildAttachmentURL(initData, msg.id, att));
      });
      attBox.appendChild(chip);
    }
    root.appendChild(attBox);
  }

  const body = document.createElement('pre');
  body.className = 'email-body';
  body.textContent = msg.body || '(본문 없음)';
  root.appendChild(body);

  if (msg.bodyTotal > msg.body.length) {
    // Server truncated. Show the original length so the user knows.
    const note = document.createElement('div');
    note.className = 'muted';
    note.textContent = `(${msg.bodyTotal.toLocaleString('ko-KR')}자 중 일부만 표시)`;
    root.appendChild(note);
  }

  const actions = document.createElement('div');
  actions.className = 'action-bar';
  root.appendChild(actions);

  const analyzeBtn = makeAction('analyze', 'secondary', () => {
    triggerImpactHaptic('medium');
    void runAnalysis(root, initData, msg, analyzeBtn, false);
  });
  // Tagged so hydrateCachedAnalysis can wire the card's "rerun" action back
  // to this button without threading a reference through the call chain.
  analyzeBtn.dataset.role = 'analyze-btn';
  actions.appendChild(analyzeBtn);

  // Read = optimistic. Disabling the button immediately is the "this is
  // handled" cue; the RPC fires in the background. The success toast (and
  // its success haptic) waits for the RPC to *fulfill* — firing it up front
  // raced the failure path, so a rejected mark_read could stack a green
  // "read" pill and a red "read failed" pill at once. Now exactly one of
  // the two shows. On failure we also roll the button back.
  const readBtn = makeAction('read', 'secondary', () => {
    if (readBtn.disabled) return;
    triggerImpactHaptic('medium');
    readBtn.disabled = true;
    void markRead(initData, msg.id).then(
      () => showFlash('read', 'success'),
      (err) => {
        readBtn.disabled = false;
        showFlash(`read failed: ${errorMessage(err)}`, 'error');
      },
    );
  });
  actions.appendChild(readBtn);

  // Archive = optimistic + immediate navigate. hideMessage() both drops
  // the cached summary and marks the id hidden, so the inbox re-render
  // skips this row even though listRecent() can still return it until the
  // archive RPC lands server-side. The RPC continues in the background; on
  // failure we un-hide the id (the row returns on the next refresh) and
  // toast the reason. showFlash also fires notify-err itself.
  const archBtn = makeAction('archive', 'secondary', () => {
    if (archBtn.disabled) return;
    triggerImpactHaptic('medium');
    archBtn.disabled = true;
    hideMessage(msg.id);
    void archive(initData, msg.id).catch((err) => {
      unhideMessage(msg.id);
      showFlash(`archive failed: ${errorMessage(err)}`, 'error');
    });
    navigate({ name: 'inbox' });
  });
  actions.appendChild(archBtn);

  // Trash = optimistic, but only after the confirm dialog. We can't
  // skip the dialog (a stray tap deleting mail is a real category of
  // mistake), so the only win is to navigate right after the confirm
  // resolves instead of after the RPC. Heavy impact on confirm so
  // 'this is destructive' reads in the wrist before the bus stop.
  const trashBtn = makeAction('trash', 'danger', async () => {
    if (trashBtn.disabled) return;
    triggerImpactHaptic('medium');
    trashBtn.disabled = true;
    try {
      const ok = await confirmAction('이 메일을 휴지통으로 옮길까요?');
      if (!ok) {
        trashBtn.disabled = false;
        return;
      }
      triggerImpactHaptic('heavy');
      hideMessage(msg.id);
      void trash(initData, msg.id).catch((err) => {
        unhideMessage(msg.id);
        showFlash(`trash failed: ${errorMessage(err)}`, 'error');
      });
      navigate({ name: 'inbox' });
    } catch (err) {
      showFlash(`trash failed: ${errorMessage(err)}`, 'error');
      trashBtn.disabled = false;
    }
  });
  actions.appendChild(trashBtn);

  const closeBtn = makeAction('close', 'primary', () => {
    triggerImpactHaptic('light');
    navigate({ name: 'inbox' });
  });
  actions.appendChild(closeBtn);
}

function buildAttachmentURL(
  initData: string,
  messageID: string,
  att: GmailMessageDetail['attachments'][number],
): string {
  const url = new URL('/api/v1/miniapp/gmail/attachment', location.origin);
  url.searchParams.set('initData', initData);
  url.searchParams.set('messageId', messageID);
  url.searchParams.set('attachmentId', att.id);
  if (att.filename) url.searchParams.set('filename', att.filename);
  if (att.mimeType) url.searchParams.set('mimeType', att.mimeType);
  return url.toString();
}

function openAttachment(url: string): void {
  const tg = window.Telegram?.WebApp;
  if (tg?.openLink) {
    tg.openLink(url);
    return;
  }

  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.target = '_blank';
  anchor.rel = 'noopener';
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

async function runAnalysis(
  root: HTMLElement,
  initData: string,
  msg: GmailMessageDetail,
  button: HTMLButtonElement,
  force: boolean,
): Promise<void> {
  const expectedHash = location.hash;
  // Find or create the analysis card slot. Re-running the analysis just
  // replaces the previous result in place rather than stacking cards.
  let slot = root.querySelector('[data-role="analysis-slot"]') as HTMLElement | null;
  if (!slot) {
    slot = document.createElement('div');
    slot.dataset.role = 'analysis-slot';
    // Insert just above the action bar so the result sits between the
    // email body and the action buttons.
    const actionBar = root.querySelector('.action-bar');
    if (actionBar) {
      actionBar.before(slot);
    } else {
      root.appendChild(slot);
    }
  }

  const start = performance.now();
  button.disabled = true;
  // Action buttons are pure text now (no inner spans), so we mutate
  // textContent directly. originalLabel snapshots whatever it was so
  // both the success and failure paths can restore it.
  const originalLabel = button.textContent ?? '';
  button.textContent = 'analyzing…';
  slot.innerHTML = '';

  // Loading state is text-only — a quiet typography line that pulses.
  // No spinner SVG. The elapsed counter sits to the right and ticks up
  // every second so a long LLM call still feels alive.
  const loading = document.createElement('div');
  loading.className = 'analysis-loading';
  loading.innerHTML = `
    <span class="analysis-loading-text">메일 분석 중… (최대 4분)</span>
    <span class="analysis-loading-elapsed">0s</span>
  `;
  slot.appendChild(loading);

  // Tick the elapsed counter so the operator knows the request is still
  // alive on slow LLM responses. Cached results return in ~10ms so the
  // counter usually never gets to "1s" before being cleared.
  const elapsedEl = loading.querySelector('.analysis-loading-elapsed') as HTMLElement;
  const tick = window.setInterval(() => {
    const sec = Math.round((performance.now() - start) / 1000);
    elapsedEl.textContent = `${sec}s`;
  }, 1000);

  try {
    const result = await analyzeMessage(initData, msg.id, force);
    window.clearInterval(tick);
    // Analysis can take 30s-4min on the main LLM; the user may well
    // have navigated away. Don't repaint into a stale detail view.
    if (!isCurrentHash(expectedHash)) return;
    slot.innerHTML = '';
    // The card owns the "다시 분석" affordance now, so the action-bar
    // button just snaps back to its original label. Disabling it would
    // be redundant — the card refresh button covers the re-run case.
    slot.appendChild(
      buildAnalysisCard(result, () => {
        void runAnalysis(root, initData, msg, button, true);
      }),
    );
    mountMailQA(root, initData, msg.id);
    button.disabled = false;
    button.textContent = originalLabel || 'analyze';
  } catch (err) {
    window.clearInterval(tick);
    if (!isCurrentHash(expectedHash)) return;
    slot.innerHTML = '';
    slot.appendChild(buildErrorBanner(`분석 실패: ${formatRpcError(err)}`));
    button.disabled = false;
    button.textContent = originalLabel || 'analyze';
  }
}

function buildAnalysisCard(
  result: {
    analysis: string;
    durationMs: number;
    cached?: boolean;
    createdAt?: string;
  },
  onRefresh: () => void,
): HTMLElement {
  const card = document.createElement('div');
  card.className = 'card analysis-card';

  const header = document.createElement('div');
  header.className = 'analysis-card-header';
  // Cached results show "저장된 분석 · <relative time>"; fresh runs
  // show the wall-clock duration the LLM took. Two distinct labels so
  // the operator can tell at a glance whether they're spending tokens
  // every time they reopen the email.
  let metaText: string;
  if (result.cached && result.createdAt) {
    metaText = `저장된 분석 · ${relativeTime(result.createdAt)}`;
  } else {
    const seconds = Math.round(result.durationMs / 1000);
    metaText = `${seconds}s`;
  }
  header.innerHTML = `
    <span class="analysis-card-title">analysis</span>
    <span class="analysis-card-meta"></span>
  `;
  (header.querySelector('.analysis-card-meta') as HTMLElement).textContent = metaText;
  card.appendChild(header);

  const body = document.createElement('div');
  body.className = 'analysis-card-body';
  // Markdown comes from a trusted LLM call but escapeHTML inside
  // renderMarkdown guards against any HTML it might emit by accident.
  body.innerHTML = renderMarkdown(result.analysis);
  card.appendChild(body);

  // Per-card refresh keeps the affordance next to the analysis itself
  // (visually closer to "what the user wants to redo" than the
  // bottom action bar). Single tap → force=true LLM re-run.
  const refreshBtn = document.createElement('button');
  refreshBtn.className = 'analysis-card-refresh';
  refreshBtn.type = 'button';
  refreshBtn.textContent = 'rerun';
  refreshBtn.addEventListener('click', onRefresh);
  card.appendChild(refreshBtn);

  return card;
}

// hydrateCachedAnalysis loads a pre-computed analysis for the open email and,
// on a hit, fills the analyze slot with it and renders its related projects —
// so a polled/previously-analyzed mail shows up complete without a tap. On a
// miss it does nothing; the manual analyze button stays the only path.
async function hydrateCachedAnalysis(
  root: HTMLElement,
  initData: string,
  msg: GmailMessageDetail,
  expectedHash: string,
): Promise<void> {
  let cached: CachedAnalysis;
  try {
    cached = await analysisCached(initData, msg.id);
  } catch {
    return; // best-effort; analyze button remains available
  }
  if (!isCurrentHash(expectedHash)) return;
  if (!cached.cached) return;

  showCachedAnalysis(root, initData, msg, cached);
  renderRelatedProjects(root, cached.relatedProjects);
}

// showCachedAnalysis renders a stored analysis into the analyze slot exactly
// like a fresh run would, so the operator sees it on open. The card's "rerun"
// action is wired back to the analyze button (a forced re-run).
function showCachedAnalysis(
  root: HTMLElement,
  initData: string,
  msg: GmailMessageDetail,
  cached: CachedAnalysis,
): void {
  let slot = root.querySelector('[data-role="analysis-slot"]') as HTMLElement | null;
  if (!slot) {
    slot = document.createElement('div');
    slot.dataset.role = 'analysis-slot';
    const actionBar = root.querySelector('.action-bar');
    if (actionBar) actionBar.before(slot);
    else root.appendChild(slot);
  }
  // Don't clobber an in-flight or already-painted manual run.
  if (slot.querySelector('.analysis-card') || slot.querySelector('.analysis-loading')) {
    return;
  }
  slot.innerHTML = '';
  slot.appendChild(
    buildAnalysisCard(
      { analysis: cached.analysis, durationMs: 0, cached: true, createdAt: cached.createdAt },
      () => {
        const btn = root.querySelector('[data-role="analyze-btn"]') as HTMLButtonElement | null;
        if (btn) void runAnalysis(root, initData, msg, btn, true);
      },
    ),
  );
  mountMailQA(root, initData, msg.id);
}

// renderRelatedProjects fills the project slot with chips linking to the
// project wiki pages the analysis cited, reusing the sender-context chip
// styling. No projects → leaves the slot empty (no card).
function renderRelatedProjects(root: HTMLElement, projects?: ProjectRef[]): void {
  const slot = root.querySelector('[data-role="project-slot"]');
  if (!slot || !projects || projects.length === 0) return;

  const card = document.createElement('div');
  card.className = 'card sender-context';

  const header = document.createElement('div');
  header.className = 'sender-context-header';
  header.textContent = '관련 프로젝트';
  card.appendChild(header);

  const wrap = document.createElement('div');
  wrap.className = 'sender-context-wiki';
  for (const p of projects) {
    const chip = document.createElement('button');
    chip.type = 'button';
    chip.className = 'sender-context-chip';
    chip.addEventListener('click', () => navigate({ name: 'wikiPage', path: p.path }));
    const title = document.createElement('span');
    title.className = 'sender-context-chip-title';
    title.textContent = p.title || p.path;
    chip.appendChild(title);
    if (p.summary) {
      const sub = document.createElement('div');
      sub.className = 'sender-context-chip-sub';
      sub.textContent = p.summary;
      chip.appendChild(sub);
    }
    wrap.appendChild(chip);
  }
  card.appendChild(wrap);
  slot.replaceWith(card);
}

// mountMailQA installs the inline Q&A section just below the analysis card: a
// composer (textarea + send) plus a running log of question/answer bubbles.
// The Q&A is grounded in the email + its analysis (assembled server-side) and
// kept ephemeral — `history` lives only in this closure and rides along on
// each askMail call so the backend stays stateless. Re-mount is a no-op.
function mountMailQA(root: HTMLElement, initData: string, messageId: string): void {
  const analysisSlot = root.querySelector('[data-role="analysis-slot"]');
  if (!analysisSlot) return;
  if (root.querySelector('[data-role="qa-slot"]')) return; // already mounted

  const slot = document.createElement('div');
  slot.dataset.role = 'qa-slot';
  slot.className = 'card mail-qa';
  analysisSlot.after(slot);

  const header = document.createElement('div');
  header.className = 'mail-qa-header';
  header.textContent = '이 메일에 대해 질문';
  slot.appendChild(header);

  const log = document.createElement('div');
  log.className = 'mail-qa-log';
  slot.appendChild(log);

  // Q&A history is closure-local: it never leaves this mounted view, and is
  // re-sent with each call so the backend holds no conversation state.
  const history: QATurn[] = [];

  const form = document.createElement('form');
  form.className = 'mail-qa-composer';
  const input = document.createElement('textarea');
  input.className = 'mail-qa-input';
  input.rows = 1;
  input.placeholder = '예: 가장 급한 건 뭐야? 답장 초안 잡아줘';
  input.setAttribute('enterkeyhint', 'send');
  const sendBtn = document.createElement('button');
  sendBtn.type = 'submit';
  sendBtn.className = 'mail-qa-send';
  sendBtn.textContent = '질문';
  form.appendChild(input);
  form.appendChild(sendBtn);
  slot.appendChild(form);

  // Auto-grow the textarea up to ~5 lines.
  input.addEventListener('input', () => {
    input.style.height = 'auto';
    input.style.height = `${Math.min(input.scrollHeight, 120)}px`;
  });

  // Enter sends; Shift+Enter newlines. isComposing guards Korean IME so a
  // composition-commit Enter doesn't fire a premature send.
  input.addEventListener('keydown', (ev) => {
    if (ev.key !== 'Enter' || ev.isComposing || ev.shiftKey) return;
    ev.preventDefault();
    form.requestSubmit();
  });

  form.addEventListener('submit', (ev) => {
    ev.preventDefault();
    if (sendBtn.disabled) return;
    const question = input.value.trim();
    if (!question) return;
    // Route token: if the operator navigates away before the answer lands,
    // don't paint into a stale view.
    const expectedHash = location.hash;

    appendQABubble(log, 'q', question);
    input.value = '';
    input.style.height = 'auto';
    sendBtn.disabled = true;
    input.disabled = true;

    const pending = appendQABubble(log, 'a', '답변 생성 중…');
    pending.classList.add('mail-qa-pending');

    void askMail(initData, messageId, question, history).then(
      (res) => {
        if (!isCurrentHash(expectedHash)) return;
        pending.classList.remove('mail-qa-pending');
        pending.innerHTML = renderMarkdown(res.answer);
        history.push({ q: question, a: res.answer });
        sendBtn.disabled = false;
        input.disabled = false;
        input.focus();
      },
      (err) => {
        if (!isCurrentHash(expectedHash)) return;
        pending.classList.remove('mail-qa-pending');
        pending.classList.add('mail-qa-error');
        pending.textContent = `질문 실패: ${formatRpcError(err)}`;
        sendBtn.disabled = false;
        input.disabled = false;
      },
    );
  });
}

// appendQABubble adds a question (right) or answer (left) bubble to the log
// and returns it. Answers render markdown; questions stay plain text.
function appendQABubble(log: HTMLElement, kind: 'q' | 'a', text: string): HTMLElement {
  const bubble = document.createElement('div');
  bubble.className =
    kind === 'q' ? 'mail-qa-bubble mail-qa-q' : 'mail-qa-bubble mail-qa-a';
  if (kind === 'a') bubble.innerHTML = renderMarkdown(text);
  else bubble.textContent = text;
  log.appendChild(bubble);
  bubble.scrollIntoView({ block: 'nearest' });
  return bubble;
}

function makeAction(
  label: string,
  variant: 'primary' | 'secondary' | 'danger',
  onClick: () => void | Promise<void>,
): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.className = `action-button action-${variant}`;
  btn.textContent = label;
  btn.addEventListener('click', () => {
    void onClick();
  });
  return btn;
}
