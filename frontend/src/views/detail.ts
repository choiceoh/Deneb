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
  markRead,
  trash,
  type CachedAnalysis,
  type GmailMessageDetail,
  type ProjectRef,
  type SenderContext,
} from '../gmail';
import {
  fetchMessage,
  fetchSenderContext,
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
    // Fetch sender context in parallel and inject the card once it lands.
    // Errors are non-fatal — the rest of the page is already useful.
    // `expectedHash` threads the route token through so a stale fetch
    // never injects the wrong sender's context.
    void hydrateSenderContext(root, initData, msg.from, expectedHash);
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
      title: 'message',
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

  // Placeholder for the sender context card. Filled asynchronously by
  // hydrateSenderContext; left out of the static layout above so the
  // initial paint is fast.
  const senderSlot = document.createElement('div');
  senderSlot.dataset.role = 'sender-context-slot';
  root.appendChild(senderSlot);

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
      const chip = document.createElement('div');
      chip.className = 'attachment-chip';
      chip.innerHTML = `
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

async function hydrateSenderContext(
  root: HTMLElement,
  initData: string,
  fromHeader: string,
  expectedHash: string,
): Promise<void> {
  if (!fromHeader) return;
  let ctx: SenderContext;
  try {
    // fetchSenderContext returns the prefetched in-flight promise
    // (kicked at pointerdown in the list view) when available, so the
    // RPC has typically been running in parallel with the message
    // fetch since before the operator finished tapping. Sender
    // context is also the slow source — graphify can take 300-1000ms
    // — so the prefetch + server-side parallelization combine to
    // bring this card up significantly sooner than a cold start
    // would.
    ctx = await fetchSenderContext(initData, fromHeader);
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

  // Memory ("메모리") and memory-graph ("메모리 그래프") sections were
  // removed here: sender-name wiki search mostly surfaced the person's own
  // page, not what the mail is about. Related *projects* now come from the
  // email analysis and render in the project slot via hydrateCachedAnalysis.

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
  // wikiHits/wikiFacts are no longer rendered here (related projects moved to
  // the analysis-driven project slot), so only the recent-activity row makes
  // this card worth showing.
  return false;
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

