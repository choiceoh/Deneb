// views/model_select.ts — choose the default model from a dedicated screen.

import { triggerSelectionHaptic } from '../app_settings';
import { errorMessage, formatRpcError } from '../format';
import {
  addMiniappModel,
  listMiniappModels,
  setMiniappModel,
  type MiniappModelOption,
  type MiniappModelsResult,
} from '../rpc';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner, buildLoadingNode, buildViewHeader } from './ui';

// Role labels for the picker header (main/lightweight/fallback).
const ROLE_LABELS: Record<string, string> = {
  main: '메인 (대화)',
  lightweight: '경량 (메일분석·요약·스킬)',
  fallback: '폴백 (메인 실패 시)',
};

// The role this picker is scoped to. Set at the top of each render and read
// synchronously when a row is tapped, so it is always correct for the view.
let pickerRole = 'main';

export function renderModelSelect(root: HTMLElement, initData: string, role = 'main'): void {
  pickerRole = role;
  const expectedHash = location.hash;
  root.innerHTML = '';
  root.appendChild(
    buildViewHeader({
      title: `모델 교체 — ${ROLE_LABELS[role] ?? role}`,
      left: { label: '← 설정', onClick: () => navigate({ name: 'settings' }) },
    }),
  );

  const status = document.createElement('div');
  status.className = 'settings-status';
  status.textContent = '이 역할에 사용할 모델을 선택하세요';

  const listCard = document.createElement('div');
  listCard.className = 'section-card settings-card';
  listCard.appendChild(buildLoadingNode('모델 불러오는 중…'));
  root.appendChild(listCard);
  root.appendChild(buildDirectModelForm(initData, listCard, status));
  root.appendChild(status);

  void hydrateModelSelect(root, listCard, initData, status, expectedHash);
}

async function hydrateModelSelect(
  root: HTMLElement,
  card: HTMLElement,
  initData: string,
  status: HTMLElement,
  expectedHash: string,
): Promise<void> {
  try {
    const result = await listMiniappModels(initData);
    if (!isCurrentHash(expectedHash) || !card.isConnected) return;
    paintModelSelect(card, initData, status, result);
  } catch (err) {
    if (!isCurrentHash(expectedHash) || !card.isConnected) return;
    paintModelError(root, card, initData, status, err);
  }
}

function paintModelSelect(
  card: HTMLElement,
  initData: string,
  status: HTMLElement,
  result: MiniappModelsResult,
): void {
  card.innerHTML = '';
  // Highlight the model bound to THIS role (not the main model). The backend
  // marks model.current against main; for a role picker we recompute against
  // the role's own current model from result.roles.
  const roleCurrent = result.roles?.find((r) => r.role === pickerRole)?.model ?? result.current;
  card.appendChild(buildCurrentRow(roleCurrent));

  if (result.sections.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'settings-model-empty';
    empty.textContent = 'no models available';
    card.appendChild(empty);
    return;
  }

  for (const section of result.sections) {
    const group = document.createElement('div');
    group.className = 'settings-model-group';

    const title = document.createElement('div');
    title.className = 'settings-model-group-title';
    title.textContent = section.title;
    group.appendChild(title);

    for (const model of section.models) {
      group.appendChild(buildModelRow(model, initData, card, status, roleCurrent));
    }
    card.appendChild(group);
  }
}

function buildCurrentRow(modelID: string): HTMLElement {
  const current = document.createElement('div');
  current.className = 'settings-model-current';
  current.innerHTML = `
    <span class="icon-tile icon-tile-purple">🤖</span>
    <span class="settings-row-text">
      <span class="settings-row-title">현재 모델</span>
      <span class="settings-row-sub"></span>
    </span>
  `;
  (current.querySelector('.settings-row-sub') as HTMLElement).textContent = modelID || '—';
  return current;
}

function buildModelRow(
  model: MiniappModelOption,
  initData: string,
  card: HTMLElement,
  status: HTMLElement,
  roleCurrent: string,
): HTMLButtonElement {
  const isCurrent = model.id === roleCurrent;
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'settings-model-row' + (isCurrent ? ' settings-model-row-current' : '');
  btn.disabled = isCurrent;
  btn.innerHTML = `
    <span class="settings-model-check"></span>
    <span class="settings-row-text">
      <span class="settings-model-title-line">
        <span class="settings-row-title"></span>
        <span class="settings-model-health"></span>
      </span>
      <span class="settings-row-sub"></span>
    </span>
  `;
  const health = normalizeModelHealth(model.health);
  const healthLabel = modelHealthLabel(health);
  (btn.querySelector('.settings-model-check') as HTMLElement).textContent = isCurrent ? '✓' : '';
  (btn.querySelector('.settings-row-title') as HTMLElement).textContent =
    model.label || model.display || model.id;
  (btn.querySelector('.settings-row-sub') as HTMLElement).textContent = model.id;
  const healthDot = btn.querySelector('.settings-model-health') as HTMLElement;
  healthDot.className = `settings-model-health settings-model-health-${health}`;
  healthDot.title = healthLabel;
  healthDot.setAttribute('aria-label', healthLabel);
  healthDot.setAttribute('role', 'img');
  btn.addEventListener('click', () => {
    void switchModel(model.id, initData, card, status);
  });
  return btn;
}

function buildDirectModelForm(
  initData: string,
  card: HTMLElement,
  status: HTMLElement,
): HTMLFormElement {
  const form = document.createElement('form');
  form.className = 'section-card settings-card settings-model-add';
  form.innerHTML = `
    <div class="settings-model-add-title">직접 추가</div>
    <label class="settings-model-field">
      <span>엔드포인트</span>
      <input name="endpoint" type="url" inputmode="url" autocomplete="off" spellcheck="false" placeholder="http://127.0.0.1:8000/v1" required />
    </label>
    <label class="settings-model-field">
      <span>모델명</span>
      <input name="model" type="text" autocomplete="off" spellcheck="false" placeholder="qwen3.6-35b-a3b" required />
    </label>
    <button type="submit" class="primary settings-model-add-submit">추가 후 적용</button>
  `;
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    const endpoint = form.elements.namedItem('endpoint') as HTMLInputElement | null;
    const model = form.elements.namedItem('model') as HTMLInputElement | null;
    void addDirectModel({
      initData,
      card,
      status,
      form,
      endpoint,
      model,
    });
  });
  return form;
}

async function switchModel(
  modelID: string,
  initData: string,
  card: HTMLElement,
  status: HTMLElement,
): Promise<void> {
  const buttons = Array.from(card.querySelectorAll<HTMLButtonElement>('.settings-model-row'));
  buttons.forEach((button) => {
    button.disabled = true;
  });
  status.textContent = '모델 변경 중…';

  try {
    const result = await setMiniappModel(initData, modelID, pickerRole);
    triggerSelectionHaptic();
    status.textContent = `${shortModelName(result.current)} 적용됨`;
    await refreshModelSelect(card, initData, status);
  } catch (err) {
    status.textContent = `모델 변경 실패: ${errorMessage(err)}`;
    buttons.forEach((button) => {
      button.disabled = button.classList.contains('settings-model-row-current');
    });
  }
}

async function addDirectModel(args: {
  initData: string;
  card: HTMLElement;
  status: HTMLElement;
  form: HTMLFormElement;
  endpoint: HTMLInputElement | null;
  model: HTMLInputElement | null;
}): Promise<void> {
  const endpoint = args.endpoint?.value.trim() ?? '';
  const model = args.model?.value.trim() ?? '';
  if (!endpoint) {
    args.status.textContent = '엔드포인트를 입력해주세요';
    args.endpoint?.focus();
    return;
  }
  if (!model) {
    args.status.textContent = '모델명을 입력해주세요';
    args.model?.focus();
    return;
  }

  setFormDisabled(args.form, true);
  const buttons = Array.from(args.card.querySelectorAll<HTMLButtonElement>('.settings-model-row'));
  buttons.forEach((button) => {
    button.disabled = true;
  });
  args.status.textContent = '모델 추가 중…';

  try {
    const added = await addMiniappModel(args.initData, endpoint, model);
    args.status.textContent = '모델 적용 중…';
    const result = await setMiniappModel(args.initData, added.id, pickerRole);
    triggerSelectionHaptic();
    args.status.textContent = `${shortModelName(result.current)} 적용됨`;
    if (args.endpoint) args.endpoint.value = '';
    if (args.model) args.model.value = '';
    await refreshModelSelect(args.card, args.initData, args.status);
  } catch (err) {
    args.status.textContent = `모델 추가 실패: ${errorMessage(err)}`;
    buttons.forEach((button) => {
      button.disabled = button.classList.contains('settings-model-row-current');
    });
  } finally {
    setFormDisabled(args.form, false);
  }
}

function setFormDisabled(form: HTMLFormElement, disabled: boolean): void {
  for (const control of Array.from(form.elements)) {
    if (
      control instanceof HTMLInputElement ||
      control instanceof HTMLButtonElement ||
      control instanceof HTMLTextAreaElement ||
      control instanceof HTMLSelectElement
    ) {
      control.disabled = disabled;
    }
  }
}

async function refreshModelSelect(
  card: HTMLElement,
  initData: string,
  status: HTMLElement,
): Promise<void> {
  const result = await listMiniappModels(initData);
  if (!card.isConnected) return;
  paintModelSelect(card, initData, status, result);
}

function paintModelError(
  root: HTMLElement,
  card: HTMLElement,
  initData: string,
  status: HTMLElement,
  err: unknown,
): void {
  card.innerHTML = '';
  card.appendChild(buildErrorBanner(`모델 목록 로드 실패: ${formatRpcError(err)}`));

  const retry = document.createElement('button');
  retry.type = 'button';
  retry.className = 'primary';
  retry.textContent = '다시 불러오기';
  retry.addEventListener('click', () => {
    card.innerHTML = '';
    card.appendChild(buildLoadingNode('모델 불러오는 중…'));
    status.textContent = '모델 목록 새로고침 중…';
    void hydrateModelSelect(root, card, initData, status, location.hash);
  });
  card.appendChild(retry);
}

function shortModelName(model: string): string {
  const idx = model.lastIndexOf('/');
  if (idx >= 0 && idx < model.length - 1) return model.slice(idx + 1);
  return model;
}

function normalizeModelHealth(health: MiniappModelOption['health']): 'online' | 'offline' | 'unknown' {
  if (health === 'online' || health === 'offline') return health;
  return 'unknown';
}

function modelHealthLabel(health: 'online' | 'offline' | 'unknown'): string {
  switch (health) {
    case 'online':
      return '응답 가능';
    case 'offline':
      return '응답 없음';
    default:
      return '상태 미확인';
  }
}
