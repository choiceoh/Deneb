// views/settings.ts — local Mini App settings in the flat typography idiom.
//
// Reset / toggle / nav rows all share the same .flat-row geometry the
// `more` view uses. The toggle's visual chrome lives entirely on the
// label markup; the underlying checkbox is hidden and the rest of the
// .flat-row reads as a label-value pair with a switch on the right.

import {
  readAppSettings,
  resetAppSettings,
  triggerSelectionHaptic,
  updateAppSettings,
  type AppSettings,
} from '../app_settings';
import { navigate } from '../router';
import { listMiniappModels } from '../rpc';
import { buildViewHeader } from './ui';

type ToggleKey = keyof AppSettings;

export function renderSettings(root: HTMLElement, initData: string): void {
  root.innerHTML = '';

  root.appendChild(buildViewHeader({ title: 'settings' }));

  const saved = readAppSettings();
  const status = document.createElement('div');
  status.className = 'settings-status';
  status.textContent = '표시는 이 기기에, 모델은 서버에 저장됩니다';

  // Model section — async-hydrated, shows current model + drills into
  // the modelSelect screen.
  const modelSection = flatSection('model');
  const modelList = modelSection.querySelector('.flat-list') as HTMLElement;
  modelList.appendChild(buildModelLoadingRow());
  root.appendChild(modelSection);
  void hydrateModelSummaryRow(modelList, initData);

  root.appendChild(
    flatSection('display', [
      toggleRow(
        'compactMode',
        'compact mode',
        '목록과 카드 간격을 더 촘촘하게 표시',
        saved.compactMode,
        (checked) => saveToggle('compactMode', checked, status),
      ),
      toggleRow(
        'showDiagnostics',
        'diagnostics',
        '버전, 응답 시간 같은 운영 정보를 표시',
        saved.showDiagnostics,
        (checked) => saveToggle('showDiagnostics', checked, status),
      ),
    ]),
  );

  root.appendChild(
    flatSection('input', [
      toggleRow(
        'enterToSend',
        'enter to send',
        '끄면 Ctrl/⌘+Enter 로 전송',
        saved.enterToSend,
        (checked) => saveToggle('enterToSend', checked, status),
      ),
      toggleRow(
        'hapticFeedback',
        'haptic feedback',
        'Telegram 이 지원할 때 탭 전환에 촉각 피드백 사용',
        saved.hapticFeedback,
        (checked) => saveToggle('hapticFeedback', checked, status),
      ),
    ]),
  );

  const resetBtn = resetRow(() => {
    resetAppSettings();
    triggerSelectionHaptic();
    renderSettings(root, initData);
  });
  root.appendChild(flatSection('reset', [resetBtn]));

  root.appendChild(status);
}

// flatSection returns a label + a hairline-bordered list group, with
// optional rows pre-attached. Returning the wrapper lets the caller
// hold a ref to .flat-list for async hydration (model summary).
function flatSection(label: string, rows: HTMLElement[] = []): HTMLElement {
  const wrap = document.createElement('section');
  wrap.className = 'flat-section';

  const labelEl = document.createElement('div');
  labelEl.className = 'flat-section-label';
  labelEl.textContent = label;
  wrap.appendChild(labelEl);

  const list = document.createElement('div');
  list.className = 'flat-list';
  for (const r of rows) list.appendChild(r);
  wrap.appendChild(list);

  return wrap;
}

function toggleRow(
  key: ToggleKey,
  label: string,
  sub: string,
  checked: boolean,
  onChange: (checked: boolean) => void,
): HTMLElement {
  const wrap = document.createElement('label');
  wrap.className = 'flat-row flat-row-toggle';
  wrap.htmlFor = `setting-${key}`;
  wrap.innerHTML = `
    <span class="flat-row-text">
      <span class="flat-row-label"></span>
      <span class="flat-row-sub"></span>
    </span>
    <span class="settings-switch">
      <input type="checkbox" />
      <span class="settings-switch-track"></span>
    </span>
  `;
  (wrap.querySelector('.flat-row-label') as HTMLElement).textContent = label;
  (wrap.querySelector('.flat-row-sub') as HTMLElement).textContent = sub;

  const input = wrap.querySelector('input') as HTMLInputElement;
  input.id = `setting-${key}`;
  input.checked = checked;
  input.addEventListener('change', () => onChange(input.checked));
  return wrap;
}

function resetRow(onClick: () => void): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'flat-row flat-row-nav';
  btn.innerHTML = `
    <span class="flat-row-text">
      <span class="flat-row-label"></span>
      <span class="flat-row-sub"></span>
    </span>
  `;
  (btn.querySelector('.flat-row-label') as HTMLElement).textContent = 'reset defaults';
  (btn.querySelector('.flat-row-sub') as HTMLElement).textContent =
    '표시와 입력 설정을 처음 상태로 복원';
  btn.addEventListener('click', onClick);
  return btn;
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
  row.className = 'flat-row';
  row.innerHTML = `
    <span class="flat-row-text">
      <span class="flat-row-label">model</span>
      <span class="flat-row-sub">loading…</span>
    </span>
  `;
  return row;
}

async function hydrateModelSummaryRow(list: HTMLElement, initData: string): Promise<void> {
  try {
    const result = await listMiniappModels(initData);
    if (!list.isConnected) return;
    paintModelSummary(list, result.current);
  } catch {
    if (!list.isConnected) return;
    paintModelSummary(list, '불러오기 실패');
  }
}

function paintModelSummary(list: HTMLElement, currentModel: string): void {
  list.innerHTML = '';
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'flat-row flat-row-nav';
  btn.innerHTML = `
    <span class="flat-row-text">
      <span class="flat-row-label">model</span>
      <span class="flat-row-sub"></span>
    </span>
  `;
  (btn.querySelector('.flat-row-sub') as HTMLElement).textContent = currentModel || '—';
  btn.addEventListener('click', () => {
    triggerSelectionHaptic();
    navigate({ name: 'modelSelect' });
  });
  list.appendChild(btn);
}
