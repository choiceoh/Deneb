// views/settings.ts — local Mini App settings.

import {
  readAppSettings,
  resetAppSettings,
  triggerSelectionHaptic,
  updateAppSettings,
  type AppSettings,
} from '../app_settings';
import { navigate } from '../router';
import { listMiniappModels } from '../rpc';

type ToggleKey = keyof AppSettings;

export function renderSettings(root: HTMLElement, initData: string): void {
  root.innerHTML = '';

  const header = document.createElement('div');
  header.className = 'brand-header';
  header.innerHTML = `
    <span class="brand-name">설정</span>
    <span class="brand-badge" title="로컬 저장">✓</span>
  `;
  root.appendChild(header);

  const saved = readAppSettings();
  const status = document.createElement('div');
  status.className = 'settings-status';
  status.textContent = '표시는 이 기기에, 모델은 서버에 저장됩니다';

  root.appendChild(sectionLabel('모델'));
  const modelCard = sectionCard();
  modelCard.appendChild(buildModelLoadingRow());
  root.appendChild(modelCard);
  void hydrateModelSummaryCard(modelCard, initData);

  root.appendChild(sectionLabel('표시'));
  const display = sectionCard();
  display.appendChild(
    buildToggleRow(
      'compactMode',
      'icon-tile-blue',
      '↕',
      '컴팩트 표시',
      '목록과 카드 간격을 더 촘촘하게 표시',
      saved.compactMode,
      (checked) => saveToggle('compactMode', checked, status),
    ),
  );
  display.appendChild(
    buildToggleRow(
      'showDiagnostics',
      'icon-tile-green',
      'ⓘ',
      '진단 정보',
      '버전, 응답 시간 같은 운영 정보를 표시',
      saved.showDiagnostics,
      (checked) => saveToggle('showDiagnostics', checked, status),
    ),
  );
  root.appendChild(display);

  root.appendChild(sectionLabel('입력'));
  const input = sectionCard();
  input.appendChild(
    buildToggleRow(
      'enterToSend',
      'icon-tile-violet',
      '↵',
      'Enter 로 전송',
      '끄면 Ctrl/⌘+Enter 로 전송',
      saved.enterToSend,
      (checked) => saveToggle('enterToSend', checked, status),
    ),
  );
  input.appendChild(
    buildToggleRow(
      'hapticFeedback',
      'icon-tile-pink',
      '◌',
      '탭 진동 피드백',
      'Telegram 이 지원할 때 탭 전환에 촉각 피드백 사용',
      saved.hapticFeedback,
      (checked) => saveToggle('hapticFeedback', checked, status),
    ),
  );
  root.appendChild(input);

  root.appendChild(sectionLabel('초기화'));
  const resetCard = sectionCard();
  const reset = document.createElement('button');
  reset.type = 'button';
  reset.className = 'settings-reset-row';
  reset.innerHTML = `
    <span class="icon-tile icon-tile-slate">↺</span>
    <span class="settings-row-text">
      <span class="settings-row-title">기본값으로 되돌리기</span>
      <span class="settings-row-sub">표시와 입력 설정을 처음 상태로 복원</span>
    </span>
  `;
  reset.addEventListener('click', () => {
    resetAppSettings();
    triggerSelectionHaptic();
    renderSettings(root, initData);
  });
  resetCard.appendChild(reset);
  root.appendChild(resetCard);
  root.appendChild(status);
}

function sectionLabel(text: string): HTMLElement {
  const label = document.createElement('div');
  label.className = 'section-label';
  label.textContent = text;
  return label;
}

function sectionCard(): HTMLElement {
  const card = document.createElement('div');
  card.className = 'section-card settings-card';
  return card;
}

function buildToggleRow(
  key: ToggleKey,
  tileClass: string,
  icon: string,
  title: string,
  sub: string,
  checked: boolean,
  onChange: (checked: boolean) => void,
): HTMLElement {
  const label = document.createElement('label');
  label.className = 'settings-toggle-row';
  label.htmlFor = `setting-${key}`;
  label.innerHTML = `
    <span class="icon-tile ${tileClass}"></span>
    <span class="settings-row-text">
      <span class="settings-row-title"></span>
      <span class="settings-row-sub"></span>
    </span>
    <span class="settings-switch">
      <input type="checkbox" />
      <span class="settings-switch-track"></span>
    </span>
  `;
  (label.querySelector('.icon-tile') as HTMLElement).textContent = icon;
  (label.querySelector('.settings-row-title') as HTMLElement).textContent = title;
  (label.querySelector('.settings-row-sub') as HTMLElement).textContent = sub;

  const input = label.querySelector('input') as HTMLInputElement;
  input.id = `setting-${key}`;
  input.checked = checked;
  input.addEventListener('change', () => onChange(input.checked));
  return label;
}

function saveToggle(key: ToggleKey, checked: boolean, status: HTMLElement): void {
  updateAppSettings({ [key]: checked });
  triggerSelectionHaptic();
  status.textContent = '저장됨';
  window.setTimeout(() => {
    status.textContent = '표시는 이 기기에, 모델은 서버에 저장됩니다';
  }, 1600);
}

function buildModelLoadingRow(): HTMLElement {
  const row = document.createElement('div');
  row.className = 'settings-model-loading';
  row.textContent = '모델 불러오는 중…';
  return row;
}

async function hydrateModelSummaryCard(card: HTMLElement, initData: string): Promise<void> {
  try {
    const result = await listMiniappModels(initData);
    if (!card.isConnected) return;
    paintModelSummaryCard(card, result.current);
  } catch (err) {
    if (!card.isConnected) return;
    paintModelSummaryCard(card, '불러오기 실패');
  }
}

function paintModelSummaryCard(card: HTMLElement, currentModel: string): void {
  card.innerHTML = '';
  const row = document.createElement('button');
  row.type = 'button';
  row.className = 'settings-model-nav';
  row.innerHTML = `
    <span class="icon-tile icon-tile-purple">🤖</span>
    <span class="settings-row-text">
      <span class="settings-row-title">모델</span>
      <span class="settings-row-sub"></span>
    </span>
    <span class="settings-model-chevron">›</span>
  `;
  (row.querySelector('.settings-row-sub') as HTMLElement).textContent = currentModel || '—';
  row.addEventListener('click', () => {
    triggerSelectionHaptic();
    navigate({ name: 'modelSelect' });
  });
  card.appendChild(row);
}
