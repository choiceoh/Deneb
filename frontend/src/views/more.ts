// views/more.ts — Profile + workspace hub in the typography idiom.
//
// One screen, one tone: a header, a flat list of section labels and
// rows separated by hairlines. No card chrome, no color tiles, no
// chevrons. Status rows (read-only) and nav rows (destination) use
// the same row geometry — the chrome difference is just whether the
// row is a button or a div.

import { ping, whoami, type PingResult, type WhoamiResult } from '../rpc';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate, type Route } from '../router';
import { readAppSettings } from '../app_settings';
import { buildErrorBanner, buildViewHeader } from './ui';

export async function renderMore(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">로딩 중…</div>';
  try {
    const t0 = performance.now();
    const [user, pingResult] = await Promise.all([whoami(initData), ping(initData)]);
    if (!isCurrentHash(expectedHash)) return;
    const latencyMs = Math.round(performance.now() - t0);
    paint(root, user, pingResult, latencyMs);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    paintError(root, `백엔드 호출 실패: ${formatRpcError(err)}`);
  }
}

function paint(
  root: HTMLElement,
  user: WhoamiResult,
  pingResult: PingResult,
  latencyMs: number,
): void {
  root.innerHTML = '';
  root.appendChild(buildViewHeader({ title: 'more' }));

  const userLabel =
    [user.firstName, user.lastName].filter(Boolean).join(' ') ||
    (user.username ? `@${user.username}` : `id=${user.id}`);

  root.appendChild(
    section('profile', [
      infoRow('user', userLabel),
      infoRow('model', pingResult.model || '—'),
      infoRow('language', '한국어'),
    ]),
  );

  root.appendChild(
    section('workspace', [
      navRow('calendar', 'upcoming meetings', { name: 'calendar' }),
      navRow('mail', 'unread triage', { name: 'inbox' }),
      navRow('memory', 'wiki search', { name: 'memory' }),
      navRow('sessions', 'recent runs', { name: 'sessions' }),
    ]),
  );

  root.appendChild(
    section('browse', [
      navRow('categories', 'wiki by category', { name: 'categories' }),
      navRow('diary', 'daily timeline', { name: 'diary' }),
      navRow('people', 'frequent senders', { name: 'people' }),
    ]),
  );

  root.appendChild(
    section('automation', [navRow('crons', 'scheduled jobs', { name: 'crons' })]),
  );

  if (readAppSettings().showDiagnostics) {
    root.appendChild(
      section('status', [
        infoRow('version', `v${pingResult.version || '?'}`),
        infoRow('latency', `${latencyMs}ms`),
      ]),
    );
  }
}

// section returns a label + a hairline-bordered group of rows. The
// label tracks the page tone (small uppercase) while the rows
// themselves are typography-driven (see buildRow* helpers).
function section(label: string, rows: HTMLElement[]): HTMLElement {
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

function infoRow(label: string, value: string): HTMLElement {
  const row = document.createElement('div');
  row.className = 'flat-row';
  row.innerHTML = `
    <span class="flat-row-label"></span>
    <span class="flat-row-value"></span>
  `;
  (row.querySelector('.flat-row-label') as HTMLElement).textContent = label;
  (row.querySelector('.flat-row-value') as HTMLElement).textContent = value;
  return row;
}

function navRow(label: string, sub: string, target: Route): HTMLButtonElement {
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
  btn.addEventListener('click', () => navigate(target));
  return btn;
}

function paintError(root: HTMLElement, message: string): void {
  root.innerHTML = '';
  root.appendChild(buildErrorBanner(message));
}
