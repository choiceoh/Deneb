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

  // Topic docs — per-topic knowledge file editor (<workspace>/topics/*.md).
  // Lives in settings, not home: it's management/config, not a daily
  // destination like calendar/mail/search.
  root.appendChild(
    flatSection('knowledge', [
      buildNavRow('topic docs', 'topics/*.md 편집', () => {
        triggerSelectionHaptic();
        navigate({ name: 'topicDocs' });
      }),
    ]),
  );
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

// buildNavRow returns a tappable flat-row that drills into another view —
// same shape as the model-role rows but with a fixed label/sub.
function buildNavRow(label: string, sub: string, onClick: () => void): HTMLElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'flat-row flat-row-nav';
  btn.innerHTML = `
    <span class="flat-row-text">
      <span class="flat-row-label"></span>
      <span class="flat-row-sub"></span>
    </span>
  `;
  (btn.querySelector('.flat-row-label') as HTMLElement).textContent = label;
  (btn.querySelector('.flat-row-sub') as HTMLElement).textContent = sub;
  btn.addEventListener('click', onClick);
  return btn;
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

// Role labels shown in the settings list. main/lightweight/fallback each get
// a one-line hint of what they power so the operator knows what they're
// changing before drilling into the picker.
const ROLE_LABELS: Record<string, string> = {
  main: '메인 (대화)',
  lightweight: '경량 (메일분석·요약·스킬)',
  fallback: '폴백 (메인 실패 시)',
};

function roleLabel(role: string): string {
  return ROLE_LABELS[role] ?? role;
}

async function hydrateModelSummaryRow(list: HTMLElement, initData: string): Promise<void> {
  try {
    const result = await listMiniappModels(initData);
    if (!list.isConnected) return;
    const roles =
      result.roles && result.roles.length > 0
        ? result.roles
        : [{ role: 'main', model: result.current }];
    paintModelRoles(list, roles);
  } catch {
    if (!list.isConnected) return;
    paintModelRoles(list, [{ role: 'main', model: '불러오기 실패' }]);
  }
}

// One nav row per model role; tap drills into the picker scoped to that role.
function paintModelRoles(list: HTMLElement, roles: { role: string; model: string }[]): void {
  list.innerHTML = '';
  for (const entry of roles) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'flat-row flat-row-nav';
    btn.innerHTML = `
      <span class="flat-row-text">
        <span class="flat-row-label"></span>
        <span class="flat-row-sub"></span>
      </span>
    `;
    (btn.querySelector('.flat-row-label') as HTMLElement).textContent = roleLabel(entry.role);
    (btn.querySelector('.flat-row-sub') as HTMLElement).textContent = entry.model || '—';
    btn.addEventListener('click', () => {
      triggerSelectionHaptic();
      navigate({ name: 'modelSelect', role: entry.role });
    });
    list.appendChild(btn);
  }
}
