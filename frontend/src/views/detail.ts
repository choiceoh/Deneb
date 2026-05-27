// views/detail.ts — single-message view with [읽음] / [보관] / [삭제] / [닫기] actions.
//
// Auto-fires miniapp.gmail.mark_read on entry (fire-and-forget) so the
// message stops cluttering the inbox after the user opens it. Explicit
// [읽음] button is kept anyway — it's a no-op cost-wise and makes the
// model of "I've actioned this" visible.

import {
  analyzeMessage,
  archive,
  getMessage,
  markRead,
  senderContext,
  trash,
  type GmailMessageDetail,
  type SenderContext,
} from '../gmail';
import { isCurrentHash, navigate } from '../router';
import { errorMessage, formatRpcError, humanSize, relativeTime } from '../format';
import { renderMarkdown } from '../markdown';
import { confirmAction } from '../dialog';
import { buildErrorBanner, buildViewHeader, renderErrorView } from './ui';

export async function renderDetail(
  root: HTMLElement,
  initData: string,
  messageId: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">메일 불러오는 중…</div>';

  try {
    const msg = await getMessage(initData, messageId);
    if (!isCurrentHash(expectedHash)) return;
    paint(root, initData, msg);
    // Auto mark-read in the background. Ignore the result; if it fails
    // the row keeps its UNREAD style on next list refresh and the user
    // can hit [읽음] explicitly.
    void markRead(initData, messageId).catch(() => undefined);
    // Fetch sender context in parallel and inject the card once it lands.
    // Errors are non-fatal — the rest of the page is already useful.
    // `expectedHash` threads the route token through so a stale fetch
    // never injects the wrong sender's context.
    void hydrateSenderContext(root, initData, msg.from, expectedHash);
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
      title: '메일',
      left: { label: '← 받은 편지함', onClick: () => navigate({ name: 'inbox' }) },
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

  // Placeholder for the sender context card. Filled asynchronously by
  // hydrateSenderContext; left out of the static layout above so the
  // initial paint is fast.
  const senderSlot = document.createElement('div');
  senderSlot.dataset.role = 'sender-context-slot';
  root.appendChild(senderSlot);

  if (msg.attachments && msg.attachments.length > 0) {
    const attBox = document.createElement('div');
    attBox.className = 'card attachments';
    const label = document.createElement('div');
    label.className = 'label';
    label.textContent = '첨부';
    attBox.appendChild(label);
    for (const att of msg.attachments) {
      const chip = document.createElement('div');
      chip.className = 'attachment-chip';
      chip.innerHTML = `
        <span class="attachment-icon">📎</span>
        <span class="attachment-name"></span>
        <span class="attachment-size"></span>
      `;
      (chip.querySelector('.attachment-name') as HTMLElement).textContent =
        att.filename || '(이름없음)';
      (chip.querySelector('.attachment-size') as HTMLElement).textContent = humanSize(att.size);
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

  const analyzeBtn = makeAction('🔍 분석', 'secondary', async () => {
    void runAnalysis(root, initData, msg, analyzeBtn, false);
  });
  actions.appendChild(analyzeBtn);

  const readBtn = makeAction('📌 읽음', 'secondary', async () => {
    readBtn.disabled = true;
    try {
      await markRead(initData, msg.id);
      flash(actions, '✓ 읽음 처리');
    } catch (err) {
      flash(actions, `읽음 실패: ${errorMessage(err)}`);
      readBtn.disabled = false;
    }
  });
  actions.appendChild(readBtn);

  const archBtn = makeAction('📁 보관', 'secondary', async () => {
    archBtn.disabled = true;
    try {
      await archive(initData, msg.id);
      navigate({ name: 'inbox' });
    } catch (err) {
      flash(actions, `보관 실패: ${errorMessage(err)}`);
      archBtn.disabled = false;
    }
  });
  actions.appendChild(archBtn);

  const trashBtn = makeAction('🗑 삭제', 'danger', async () => {
    // Disable BEFORE awaiting confirm: prevents a double-tap from
    // queueing a second confirm dialog + second trash RPC.
    if (trashBtn.disabled) return;
    trashBtn.disabled = true;
    let navigated = false;
    try {
      const ok = await confirmAction('이 메일을 휴지통으로 옮길까요?');
      if (!ok) return;
      await trash(initData, msg.id);
      navigate({ name: 'inbox' });
      navigated = true;
    } catch (err) {
      flash(actions, `삭제 실패: ${errorMessage(err)}`);
    } finally {
      // Re-enable on every exit except a successful navigate (where
      // the view is about to be torn down anyway); otherwise a
      // user-cancelled confirm or a navigate that fails silently
      // would leave the button permanently disabled with no feedback.
      if (!navigated) trashBtn.disabled = false;
    }
  });
  actions.appendChild(trashBtn);

  const closeBtn = makeAction('← 닫기', 'primary', () => navigate({ name: 'inbox' }));
  actions.appendChild(closeBtn);
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
  const originalLabel = button.textContent;
  button.textContent = '⏳ 분석 중…';
  slot.innerHTML = '';

  const loading = document.createElement('div');
  loading.className = 'card analysis-loading';
  loading.innerHTML = `
    <div class="analysis-loading-spinner">⏳</div>
    <div class="analysis-loading-text">메일 분석 중… (최대 4분 소요)</div>
    <div class="analysis-loading-elapsed">0s</div>
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
    button.disabled = false;
    button.textContent = originalLabel ?? '🔍 분석';
  } catch (err) {
    window.clearInterval(tick);
    if (!isCurrentHash(expectedHash)) return;
    slot.innerHTML = '';
    slot.appendChild(buildErrorBanner(`분석 실패: ${formatRpcError(err)}`));
    button.disabled = false;
    button.textContent = originalLabel ?? '🔍 분석';
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
    <span class="analysis-card-title">🔍 분석</span>
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
  refreshBtn.textContent = '🔄 다시 분석';
  refreshBtn.addEventListener('click', onRefresh);
  card.appendChild(refreshBtn);

  return card;
}

async function hydrateSenderContext(
  root: HTMLElement,
  initData: string,
  fromHeader: string,
  expectedHash: string,
): Promise<void> {
  if (!fromHeader) return;
  let ctx: SenderContext;
  try {
    ctx = await senderContext(initData, fromHeader);
  } catch {
    // Best-effort enrichment. A failure here should not interrupt the
    // user reading the email — leave the slot empty.
    return;
  }
  // Route-token check: if the user navigated to a different message (or
  // away from detail entirely) while we were fetching, do nothing. The
  // querySelector below would otherwise find the NEW message's slot and
  // inject the OLD sender's context into it.
  if (!isCurrentHash(expectedHash)) return;
  const slot = root.querySelector('[data-role="sender-context-slot"]');
  if (!slot) return; // Detail view navigated away while we were fetching.
  if (!hasUsefulContext(ctx)) return;

  const card = document.createElement('div');
  card.className = 'card sender-context';

  const header = document.createElement('div');
  header.className = 'sender-context-header';
  header.textContent = '보낸이 컨텍스트';
  card.appendChild(header);

  if (ctx.recent) {
    const recent = document.createElement('div');
    recent.className = 'sender-context-row';
    const parts: string[] = [];
    parts.push(`최근 ${ctx.recent.windowDays}일간 ${ctx.recent.count}건`);
    if (ctx.recent.lastReceivedAt) {
      parts.push(`마지막 ${relativeTime(ctx.recent.lastReceivedAt)}`);
    }
    recent.textContent = parts.join(' · ');
    card.appendChild(recent);
  }

  if (ctx.wikiHits.length > 0) {
    const wiki = document.createElement('div');
    wiki.className = 'sender-context-wiki';
    const label = document.createElement('div');
    label.className = 'sender-context-wiki-label';
    label.textContent = '메모리';
    wiki.appendChild(label);
    for (const hit of ctx.wikiHits) {
      // Wiki chip is a button so tapping opens the wiki page detail.
      // Without this the user could see "메모리 / Alice (#사람)" right
      // there in the mail view but had to navigate the long way
      // (more → memory → search "Alice" → tap) to actually read it.
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'sender-context-chip';
      chip.addEventListener('click', () =>
        navigate({ name: 'wikiPage', path: hit.path }),
      );
      const title = document.createElement('span');
      title.className = 'sender-context-chip-title';
      title.textContent = hit.title || hit.path;
      chip.appendChild(title);
      if (hit.category) {
        const cat = document.createElement('span');
        cat.className = 'sender-context-chip-cat';
        cat.textContent = `#${hit.category}`;
        chip.appendChild(cat);
      }
      if (hit.summary) {
        const sub = document.createElement('div');
        sub.className = 'sender-context-chip-sub';
        sub.textContent = hit.summary;
        chip.appendChild(sub);
      }
      wiki.appendChild(chip);
    }
    card.appendChild(wiki);
  }

  // wikiFacts is the graphify-CLI snapshot — free-form Korean text
  // about the sender's network (relationships, deals, decisions). The
  // backend already truncated; the UI renders as a quote block.
  if (ctx.wikiFacts) {
    const facts = document.createElement('div');
    facts.className = 'sender-context-facts';
    const label = document.createElement('div');
    label.className = 'sender-context-wiki-label';
    label.textContent = '메모리 그래프';
    facts.appendChild(label);
    const body = document.createElement('pre');
    body.className = 'sender-context-facts-body';
    body.textContent = ctx.wikiFacts;
    facts.appendChild(body);
    card.appendChild(facts);
  }

  if (ctx.notices && ctx.notices.length > 0) {
    const notice = document.createElement('div');
    notice.className = 'sender-context-notice';
    notice.textContent = ctx.notices.join(' · ');
    card.appendChild(notice);
  }

  slot.replaceWith(card);
}

function hasUsefulContext(ctx: SenderContext): boolean {
  if (ctx.recent && ctx.recent.count > 0) return true;
  if (ctx.wikiHits.length > 0) return true;
  if (ctx.wikiFacts && ctx.wikiFacts.trim() !== '') return true;
  // If only notices came back (both sources unavailable), don't render a
  // banner-on-every-mail — the user can't act on it from this surface.
  return false;
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

function flash(host: HTMLElement, message: string): void {
  const existing = host.parentElement?.querySelector('.flash');
  if (existing) existing.remove();
  const f = document.createElement('div');
  f.className = 'flash';
  f.textContent = message;
  host.after(f);
  setTimeout(() => f.remove(), 2500);
}
