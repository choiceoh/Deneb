// views/crons.ts — list of registered cron jobs.
//
// Read-only surface in the Mini App: shows "what's wired up to fire
// automatically." Each row is one job with its schedule, payload
// preview, and either a "다음 실행" timestamp or an error/disabled
// state badge. Tapping a row currently does nothing — mutation lives
// in the operator tool (cron.add/update/remove); the Mini App is for
// awareness, not editing.

import { listCrons, type CronJobRow } from '../crons';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

export async function renderCrons(
  root: HTMLElement,
  initData: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: '⚡ 자동 작업',
      left: { label: '← 더보기', onClick: () => navigate({ name: 'more' }) },
      right: { label: '새로고침', onClick: () => void renderCrons(root, initData) },
    }),
  );

  const status = buildLoadingNode('자동 작업 불러오는 중…');
  root.appendChild(status);

  try {
    // includeDisabled=true so the user can see auto-disabled jobs
    // (consecutive errors) and toggle them back on via the operator
    // tool. The row badge makes the state visible.
    const { jobs, total } = await listCrons(initData, { includeDisabled: true });
    if (!isCurrentHash(expectedHash)) return;
    status.remove();

    const summary = document.createElement('div');
    summary.className = 'muted cron-summary';
    const activeCount = jobs.filter((j) => j.enabled).length;
    summary.textContent = `총 ${total.toLocaleString('ko-KR')}개 · 활성 ${activeCount.toLocaleString('ko-KR')}개`;
    root.appendChild(summary);

    if (jobs.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'empty-state';
      empty.textContent = '등록된 자동 작업이 없습니다';
      root.appendChild(empty);
      return;
    }
    for (const job of jobs) {
      root.appendChild(buildCronRow(job));
    }
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`자동 작업 로드 실패: ${formatRpcError(err)}`));
  }
}

function buildCronRow(job: CronJobRow): HTMLElement {
  const card = document.createElement('div');
  card.className = 'cron-row';
  if (!job.enabled) card.classList.add('cron-row-disabled');

  const top = document.createElement('div');
  top.className = 'cron-row-top';

  const name = document.createElement('span');
  name.className = 'cron-row-name';
  name.textContent = job.name || job.id;
  top.appendChild(name);

  top.appendChild(buildStateBadge(job));
  card.appendChild(top);

  const schedule = document.createElement('div');
  schedule.className = 'cron-row-schedule';
  schedule.textContent = `⏱ ${job.schedule}`;
  card.appendChild(schedule);

  if (job.payloadPreview) {
    const preview = document.createElement('div');
    preview.className = 'cron-row-preview';
    preview.textContent = job.payloadPreview;
    card.appendChild(preview);
  }

  const foot = document.createElement('div');
  foot.className = 'cron-row-foot';
  const parts: string[] = [];
  if (job.nextRunAtMs && job.nextRunAtMs > 0) {
    parts.push(`다음 ${formatRelative(job.nextRunAtMs)}`);
  }
  parts.push(job.payloadKind);
  foot.textContent = parts.join(' · ');
  card.appendChild(foot);

  if (job.lastError) {
    const err = document.createElement('div');
    err.className = 'cron-row-error';
    err.textContent = `⚠ ${job.lastError}`;
    card.appendChild(err);
  }

  return card;
}

function buildStateBadge(job: CronJobRow): HTMLElement {
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

// formatRelative renders a future timestamp as "N분 후" / "N시간 후"
// (or "HH:mm" today / "M/D HH:mm" otherwise once it's past those
// horizons). format.ts's relativeTime is past-tense, so we want a
// dedicated future-tense helper here. Past timestamps (a missed run
// that hasn't been rescheduled yet) get "지연 N분".
function formatRelative(ms: number): string {
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '';
  const diff = ms - Date.now();
  const abs = Math.abs(diff);
  const min = Math.round(abs / 60000);
  if (diff < 0) {
    if (min < 60) return `지연 ${min}분`;
    const hour = Math.round(abs / 3600000);
    return `지연 ${hour}시간`;
  }
  if (min < 1) return '곧';
  if (min < 60) return `${min}분 후`;
  const hour = Math.round(abs / 3600000);
  if (hour < 24) return `${hour}시간 후`;
  // Beyond a day: show wall-clock.
  const m = String(d.getMonth() + 1);
  const day = String(d.getDate());
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${m}/${day} ${hh}:${mm}`;
}
