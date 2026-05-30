// views/cron_edit.ts — edit an existing cron job.
//
// Reached from the cron detail "✏️ 수정" button. Fetches the job, pre-fills
// a form with its current values, and on save sends the whole field set to
// miniapp.crons.update (which patches + reschedules). The schedule field is
// a smart spec — a cron expression ("0 9 * * *"), an interval ("15m",
// "@daily"), or an ISO timestamp; the backend validates it and rejects a
// malformed value, surfaced here as an inline flash. Enable/disable, run,
// and delete are quick actions on the detail screen, not fields here.

import {
  getCron,
  updateCron,
  type CronJobDetail,
  type CronUpdatePatch,
} from '../crons';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader, showFlash } from './ui';

export async function renderCronEdit(
  root: HTMLElement,
  initData: string,
  id: string,
): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '';

  root.appendChild(
    buildViewHeader({
      title: 'edit cron',
      left: { label: '← 취소', onClick: () => navigate({ name: 'cronDetail', id }) },
    }),
  );

  const status = buildLoadingNode('자동 작업 불러오는 중…');
  root.appendChild(status);

  try {
    const job = await getCron(initData, id);
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    paintForm(root, initData, job);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    status.remove();
    root.appendChild(buildErrorBanner(`자동 작업 로드 실패: ${formatRpcError(err)}`));
  }
}

function paintForm(root: HTMLElement, initData: string, job: CronJobDetail): void {
  const wrap = document.createElement('div');
  wrap.className = 'card wiki-edit';

  const nameInput = buildFieldInput(wrap, '이름', job.name ?? '');

  const scheduleInput = buildFieldInput(wrap, '스케줄', job.scheduleSpec ?? '');
  appendHint(
    wrap,
    '예: "0 9 * * *" (매일 9시), "15m" / "@daily" (반복), ISO 시각 (1회). cron은 시간대를 함께 지정하세요.',
  );
  const tzInput = buildFieldInput(wrap, '시간대 (cron)', job.timezone ?? '');

  const promptArea = buildTextarea(wrap, '지시문', job.prompt ?? '');

  // Execution knobs.
  const modelInput = buildFieldInput(wrap, '모델', job.model ?? '');
  const thinkingInput = buildFieldInput(wrap, '사고 (none/low/medium/high)', job.thinking ?? '');
  const timeoutInput = buildNumberInput(wrap, '타임아웃 (초)', job.timeoutSeconds);
  const retryInput = buildNumberInput(wrap, '재시도 (0–3)', job.retryCount);

  // Delivery target.
  const channelInput = buildFieldInput(wrap, '배달 채널', job.deliveryChannel ?? '');
  const toInput = buildFieldInput(wrap, '수신', job.deliveryTo ?? '');
  const threadInput = buildFieldInput(wrap, '토픽 ID', job.deliveryThreadId ?? '');

  const flash = document.createElement('div');
  flash.className = 'wiki-edit-flash';
  wrap.appendChild(flash);

  const actions = document.createElement('div');
  actions.className = 'wiki-edit-actions';

  const cancelBtn = document.createElement('button');
  cancelBtn.type = 'button';
  cancelBtn.className = 'action-button action-secondary';
  cancelBtn.textContent = '취소';
  cancelBtn.addEventListener('click', () => navigate({ name: 'cronDetail', id: job.id }));
  actions.appendChild(cancelBtn);

  const saveBtn = document.createElement('button');
  saveBtn.type = 'button';
  saveBtn.className = 'action-button action-primary';
  saveBtn.textContent = '저장';
  saveBtn.addEventListener('click', () => {
    const patch: CronUpdatePatch = {
      name: nameInput.value.trim(),
      schedule: scheduleInput.value.trim(),
      tz: tzInput.value.trim(),
      prompt: promptArea.value,
      model: modelInput.value.trim(),
      thinking: thinkingInput.value.trim(),
      timeoutSeconds: parseIntOr(timeoutInput.value, 0),
      retryCount: parseIntOr(retryInput.value, 0),
      delivery: {
        channel: channelInput.value.trim(),
        to: toInput.value.trim(),
        threadId: threadInput.value.trim(),
      },
    };
    void submit(initData, job.id, patch, saveBtn, cancelBtn, flash);
  });
  actions.appendChild(saveBtn);
  wrap.appendChild(actions);

  root.appendChild(wrap);
}

async function submit(
  initData: string,
  id: string,
  patch: CronUpdatePatch,
  saveBtn: HTMLButtonElement,
  cancelBtn: HTMLButtonElement,
  flash: HTMLElement,
): Promise<void> {
  if (!patch.schedule) {
    flash.textContent = '스케줄을 입력하세요.';
    return;
  }

  saveBtn.disabled = true;
  cancelBtn.disabled = true;
  flash.textContent = '';
  saveBtn.textContent = '저장 중…';

  try {
    await updateCron(initData, id, patch);
    showFlash('저장했습니다.', 'success');
    navigate({ name: 'cronDetail', id });
  } catch (err) {
    saveBtn.disabled = false;
    cancelBtn.disabled = false;
    saveBtn.textContent = '저장';
    // Schedule validation errors come back here — show them inline so the
    // user can fix the spec without losing the rest of their edits.
    flash.textContent = `저장 실패: ${formatRpcError(err)}`;
  }
}

// --- field builders (same DOM shape as wiki_new.ts) -----------------------

function buildFieldInput(wrap: HTMLElement, label: string, value: string): HTMLInputElement {
  const labelEl = document.createElement('label');
  labelEl.className = 'wiki-edit-field';
  const textEl = document.createElement('span');
  textEl.className = 'wiki-edit-field-label';
  textEl.textContent = label;
  labelEl.appendChild(textEl);
  const input = document.createElement('input');
  input.className = 'wiki-edit-input';
  input.type = 'text';
  input.value = value;
  labelEl.appendChild(input);
  wrap.appendChild(labelEl);
  return input;
}

function buildNumberInput(wrap: HTMLElement, label: string, value?: number): HTMLInputElement {
  const input = buildFieldInput(wrap, label, value && value > 0 ? String(value) : '');
  input.inputMode = 'numeric';
  return input;
}

function buildTextarea(wrap: HTMLElement, label: string, value: string): HTMLTextAreaElement {
  const labelEl = document.createElement('div');
  labelEl.className = 'wiki-edit-field-label';
  labelEl.textContent = label;
  wrap.appendChild(labelEl);
  const area = document.createElement('textarea');
  area.className = 'wiki-edit-textarea';
  area.rows = 8;
  area.spellcheck = false;
  area.value = value;
  wrap.appendChild(area);
  return area;
}

function appendHint(wrap: HTMLElement, text: string): void {
  const hint = document.createElement('div');
  hint.className = 'cron-edit-hint';
  hint.textContent = text;
  wrap.appendChild(hint);
}

function parseIntOr(raw: string, fallback: number): number {
  const n = parseInt(raw.trim(), 10);
  return Number.isFinite(n) ? n : fallback;
}
