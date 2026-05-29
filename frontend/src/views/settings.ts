// views/settings.ts — Mini App settings.
//
// Pared down to the one preference a single operator actually changes
// mid-use: the active model. The local cosmetic toggles that used to live
// here (compact mode, theme override, reduce motion, diagnostics,
// enter-to-send) were removed — they were set-once and added no value for a
// single user. Any genuinely in-use control we add later (e.g. a thinking-
// mode toggle) belongs here alongside the model row.

import { triggerSelectionHaptic } from '../app_settings';
import { navigate } from '../router';
import { listMiniappModels } from '../rpc';
import { buildViewHeader } from './ui';

export function renderSettings(root: HTMLElement, initData: string): void {
  root.innerHTML = '';

  // Drill-down off home: carry a back link in the header like every other
  // non-index view.
  root.appendChild(
    buildViewHeader({
      title: 'settings',
      left: { label: '← home', onClick: () => navigate({ name: 'home' }) },
    }),
  );

  // Model section — async-hydrated, shows the current model + drills into
  // the modelSelect screen.
  const modelSection = flatSection('model');
  const modelList = modelSection.querySelector('.flat-list') as HTMLElement;
  modelList.appendChild(buildModelLoadingRow());
  root.appendChild(modelSection);
  void hydrateModelSummaryRow(modelList, initData);
}

// flatSection returns a label + a hairline-bordered list group. Returning the
// wrapper lets the caller hold a ref to .flat-list for async hydration.
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
