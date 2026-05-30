// views/cron_detail.ts — full detail for one cron job.
//
// Reached by tapping a row in the crons list. Surfaces everything the
// list row trims away: the full prompt (the list caps it at 120 runes),
// the delivery target, the parsed schedule pieces, the execution context
// (agent, session target, model/thinking/timeout/retry), and runtime
// state (next run, last session, errors, timestamps). Read-only — there
// is no edit affordance here; cron mutation lives in the operator tool
// RPCs (cron.add/update/remove), deliberately kept off the phone surface.

import { getCron, removeCron, runCron, updateCron, type CronJobDetail } from '../crons';
import { confirmAction } from '../dialog';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { setPullToRefreshHandler } from '../pull_to_refresh';
import { buildErrorBanner, buildLoadingNode, buildViewHeader, showFlash } from './ui';

export async function renderCronDetail(
  root: HTMLElement,
  initData: string,
  id: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'cron',
      left: { label: '← crons', onClick: () => navigate({ name: 'crons' }) },
    }),
  );
  setPullToRefreshHandler(() => renderCronDetail(root, initData, id));

  const status = buildLoadingNode('자동 작업 불러오는 중…');
  root.appendChild(status);

  try {
    const job = await getCron(initData, id);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    paint(root, initData, job);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`자동 작업 로드 실패: ${formatRpcError(err)}`));
  }
}

function paint(root: HTMLElement, initData: string, job: CronJobDetail): void {
  // Name + state badge.
  const head = document.createElement('div');
  head.className = 'cron-detail-head';
  const name = document.createElement('div');
  name.className = 'cron-detail-name';
  name.textContent = job.name || job.id;
  head.appendChild(name);
  head.appendChild(buildStateBadge(job));
  root.appendChild(head);

  // State banner — surface "why isn't this firing" first when relevant.
  const banner = buildStateBanner(job);
  if (banner) root.appendChild(banner);

  // Action bar — edit / run / toggle / delete. All mutations are read by
  // the same miniapp.crons.* RPCs the operator tool uses underneath.
  root.appendChild(buildActionBar(root, initData, job));

  // Schedule.
  const sched = document.createElement('div');
  sched.className = 'card';
  appendRow(sched, '주기', job.schedule);
  if (job.scheduleKind === 'cron') {
    appendRow(sched, '표현식', job.cronExpr ?? '');
    appendRow(sched, '시간대', job.timezone ?? '');
    if (job.staggerMs && job.staggerMs > 0) {
      appendRow(sched, '분산', `${Math.round(job.staggerMs / 1000)}초 이내`);
    }
  }
  appendRow(sched, '다음 실행', formatNextRun(job.nextRunAtMs));
  if (sched.childElementCount > 0) root.appendChild(sched);

  // Execution context.
  const exec = document.createElement('div');
  exec.className = 'card';
  appendRow(exec, '유형', formatPayloadKind(job.payloadKind));
  appendRow(exec, '에이전트', job.agentId ?? '');
  appendRow(exec, '세션', formatSessionTarget(job.sessionTarget));
  appendRow(exec, '모델', job.model ?? '');
  appendRow(exec, '사고', job.thinking ?? '');
  if (job.timeoutSeconds && job.timeoutSeconds > 0) {
    appendRow(exec, '타임아웃', `${job.timeoutSeconds}초`);
  }
  if (job.lightContext) appendRow(exec, '경량 컨텍스트', '사용');
  if (job.retryCount && job.retryCount > 0) {
    appendRow(exec, '재시도', `${job.retryCount}회`);
  }
  if (exec.childElementCount > 0) root.appendChild(exec);

  // Delivery — only when a target is configured.
  if (job.deliveryChannel || job.deliveryTo || job.deliveryThreadId) {
    const delivery = document.createElement('div');
    delivery.className = 'card';
    appendRow(delivery, '배달 채널', formatChannel(job.deliveryChannel));
    appendRow(delivery, '수신', job.deliveryTo ?? '');
    appendRow(delivery, '토픽', job.deliveryThreadId ?? '');
    if (delivery.childElementCount > 0) root.appendChild(delivery);
  }

  // Runtime state.
  const state = document.createElement('div');
  state.className = 'card';
  appendRow(state, '활성', job.enabled ? '예' : '아니오');
  if (job.consecutiveErrors && job.consecutiveErrors > 0) {
    appendRow(state, '연속 오류', `${job.consecutiveErrors}회`);
  }
  if (job.autoDisabledAtMs && job.autoDisabledAtMs > 0) {
    appendRow(state, '자동 비활성', formatWhen(job.autoDisabledAtMs));
  }
  appendRow(state, '마지막 배달', job.lastDeliveryStatus ?? '');
  if (job.failureAlertAfter && job.failureAlertAfter > 0) {
    appendRow(state, '실패 알림', `${job.failureAlertAfter}회 연속 실패 시`);
  }
  if (job.lastSessionKey) {
    appendLinkRow(state, '마지막 세션', '열기', () =>
      navigate({ name: 'sessionTranscript', sessionKey: job.lastSessionKey as string }),
    );
  }
  appendRow(state, '생성', formatWhen(job.createdAtMs));
  appendRow(state, '수정', formatWhen(job.updatedAtMs));
  if (state.childElementCount > 0) root.appendChild(state);

  // Full prompt — the headline reason the detail view exists.
  const label = document.createElement('div');
  label.className = 'cron-detail-label';
  label.textContent = '지시문';
  root.appendChild(label);
  const prompt = document.createElement('div');
  prompt.className = 'cron-detail-prompt';
  prompt.textContent = job.prompt?.trim() || '(지시문 없음)';
  root.appendChild(prompt);

  // Bottom back link — the prompt can be long, so repeat the exit here
  // rather than only in the header (mirrors calendar_event.ts).
  const back = document.createElement('button');
  back.type = 'button';
  back.className = 'link-button';
  back.textContent = '← crons 목록';
  back.addEventListener('click', () => navigate({ name: 'crons' }));
  root.appendChild(back);
}

// buildActionBar is the edit / run / toggle / delete control cluster. The
// edit button routes to the form; the rest fire their mutation RPC inline,
// disable themselves while in flight, and either flash a result or re-render
// the detail (toggle) / leave the screen (delete).
function buildActionBar(root: HTMLElement, initData: string, job: CronJobDetail): HTMLElement {
  const bar = document.createElement('div');
  bar.className = 'cron-action-bar';

  const edit = document.createElement('button');
  edit.type = 'button';
  edit.className = 'action-button action-primary';
  edit.textContent = '✏️ 수정';
  edit.addEventListener('click', () => navigate({ name: 'cronEdit', id: job.id }));
  bar.appendChild(edit);

  const row = document.createElement('div');
  row.className = 'cron-action-row';

  const run = document.createElement('button');
  run.type = 'button';
  run.className = 'action-button action-secondary';
  run.textContent = '▶ 지금 실행';
  run.addEventListener('click', () => {
    void (async () => {
      run.disabled = true;
      try {
        await runCron(initData, job.id);
        showFlash('실행을 예약했습니다. 결과는 배달 채널로 전송됩니다.', 'success');
      } catch (err) {
        showFlash(`실행 실패: ${formatRpcError(err)}`, 'error');
      } finally {
        run.disabled = false;
      }
    })();
  });
  row.appendChild(run);

  const toggle = document.createElement('button');
  toggle.type = 'button';
  toggle.className = 'action-button action-secondary';
  toggle.textContent = job.enabled ? '⏸ 비활성화' : '▶ 활성화';
  toggle.addEventListener('click', () => {
    void (async () => {
      toggle.disabled = true;
      try {
        await updateCron(initData, job.id, { enabled: !job.enabled });
        // Re-render so the badge, banner, and toggle label all reflect the
        // new state without a manual pull-to-refresh.
        void renderCronDetail(root, initData, job.id);
      } catch (err) {
        toggle.disabled = false;
        showFlash(`상태 변경 실패: ${formatRpcError(err)}`, 'error');
      }
    })();
  });
  row.appendChild(toggle);

  const del = document.createElement('button');
  del.type = 'button';
  del.className = 'action-button action-danger';
  del.textContent = '🗑 삭제';
  del.addEventListener('click', () => {
    void (async () => {
      const ok = await confirmAction(`"${job.name || job.id}" 작업을 삭제할까요? 되돌릴 수 없습니다.`);
      if (!ok) return;
      del.disabled = true;
      try {
        await removeCron(initData, job.id);
        showFlash('삭제했습니다.', 'success');
        navigate({ name: 'crons' });
      } catch (err) {
        del.disabled = false;
        showFlash(`삭제 실패: ${formatRpcError(err)}`, 'error');
      }
    })();
  });
  row.appendChild(del);

  bar.appendChild(row);
  return bar;
}

// buildStateBadge mirrors the list-row badge so the detail header reads
// the same as the row the user tapped.
function buildStateBadge(job: CronJobDetail): HTMLElement {
  const badge = document.createElement('span');
  badge.className = 'cron-row-badge';
  if (job.autoDisabledAtMs && job.autoDisabledAtMs > 0) {
    badge.classList.add('cron-row-badge-error');
    badge.textContent = '자동 비활성';
  } else if (!job.enabled) {
    badge.classList.add('cron-row-badge-off');
    badge.textContent = '비활성';
  } else if (job.consecutiveErrors && job.consecutiveErrors > 0) {
    badge.classList.add('cron-row-badge-warn');
    badge.textContent = `에러 ${job.consecutiveErrors}`;
  } else {
    badge.classList.add('cron-row-badge-on');
    badge.textContent = '활성';
  }
  return badge;
}

// buildStateBanner returns a "why isn't this firing as expected" banner,
// or null when the job is healthy and enabled. Auto-disabled and manual-
// disabled are distinct messages; an enabled-but-erroring job gets a
// softer warning that still shows the last error.
function buildStateBanner(job: CronJobDetail): HTMLElement | null {
  const autoDisabled = (job.autoDisabledAtMs ?? 0) > 0;
  const erroring = (job.consecutiveErrors ?? 0) > 0;
  if (job.enabled && !erroring) return null;

  const el = document.createElement('div');
  el.className = 'cron-detail-state';
  const lines: string[] = [];
  if (autoDisabled) {
    el.classList.add('cron-detail-state-error');
    lines.push(`연속 오류로 자동 비활성되었습니다 (${formatWhen(job.autoDisabledAtMs)}).`);
  } else if (!job.enabled) {
    el.classList.add('cron-detail-state-off');
    lines.push('이 작업은 비활성 상태입니다. 예약 시간이 와도 실행되지 않습니다.');
  } else {
    // enabled but erroring
    el.classList.add('cron-detail-state-error');
    lines.push(`최근 ${job.consecutiveErrors}회 연속 오류가 발생했습니다.`);
  }
  if (job.lastError) lines.push(`마지막 오류: ${job.lastError}`);
  el.textContent = lines.join('\n');
  el.style.whiteSpace = 'pre-line';
  return el;
}

// appendRow adds a label/value row to a card, skipping empty values so a
// card only shows the fields a given job actually has.
function appendRow(card: HTMLElement, label: string, value: string): void {
  if (!value) return;
  const row = document.createElement('div');
  row.className = 'row';
  const l = document.createElement('span');
  l.className = 'label';
  l.textContent = label;
  const v = document.createElement('span');
  v.className = 'value';
  v.textContent = value;
  row.appendChild(l);
  row.appendChild(v);
  card.appendChild(row);
}

// appendLinkRow is appendRow whose value is a tappable action (e.g. the
// last-run session opens its transcript).
function appendLinkRow(
  card: HTMLElement,
  label: string,
  action: string,
  onClick: () => void,
): void {
  const row = document.createElement('div');
  row.className = 'row';
  const l = document.createElement('span');
  l.className = 'label';
  l.textContent = label;
  const v = document.createElement('button');
  v.type = 'button';
  v.className = 'link-button value';
  v.textContent = action;
  v.addEventListener('click', onClick);
  row.appendChild(l);
  row.appendChild(v);
  card.appendChild(row);
}

function formatPayloadKind(kind: string): string {
  switch (kind) {
    case 'agentTurn':
      return '에이전트 턴';
    case 'systemEvent':
      return '시스템 이벤트';
    default:
      return kind;
  }
}

function formatSessionTarget(target?: string): string {
  switch (target) {
    case 'main':
      return '메인 세션';
    case 'isolated':
      return '격리 세션';
    case 'current':
      return '현재 세션';
    case 'subagent':
      return '서브에이전트 (메인 복제)';
    default:
      return target ?? '';
  }
}

function formatChannel(channel?: string): string {
  switch (channel) {
    case 'telegram':
      return '텔레그램';
    case 'last':
      return '마지막 채널';
    case '':
    case undefined:
      return '';
    default:
      return channel;
  }
}

// formatWhen renders a past/absolute timestamp as a local wall-clock
// string. Returns '' for missing/zero so appendRow drops the row.
function formatWhen(ms?: number): string {
  if (!ms || ms <= 0) return '';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString('ko-KR', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });
}

// formatNextRun shows the next fire time as wall-clock plus a relative
// hint ("3시간 후"). A missing/zero next-run reads as "예정 없음" so the
// row is still informative (e.g. a one-shot that already fired).
function formatNextRun(ms?: number): string {
  if (!ms || ms <= 0) return '예정 없음';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '예정 없음';
  const abs = formatWhen(ms);
  const diff = ms - Date.now();
  const rel = formatRelative(diff);
  return rel ? `${abs} (${rel})` : abs;
}

function formatRelative(diffMs: number): string {
  const past = diffMs < 0;
  const min = Math.round(Math.abs(diffMs) / 60000);
  if (min < 1) return past ? '방금' : '곧';
  let phrase: string;
  if (min < 60) {
    phrase = `${min}분`;
  } else if (min < 60 * 24) {
    phrase = `${Math.round(min / 60)}시간`;
  } else {
    phrase = `${Math.round(min / (60 * 24))}일`;
  }
  return past ? `${phrase} 지연` : `${phrase} 후`;
}
