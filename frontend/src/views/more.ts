// views/more.ts — Hub for the surfaces home doesn't show.
//
// Same idiom as home: a lowercase brand mark, a stack of huge
// English menu words, and a quiet footer line. The split is by
// scope, not by section header: home holds the four daily surfaces
// (calendar, mail, memory, sessions); more holds everything else
// the operator might reach for less often (categories, diary,
// people, crons). Profile + diagnostics collapse into the footer.

import { ping, whoami, type PingResult, type WhoamiResult } from '../rpc';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate, type Route } from '../router';
import { readAppSettings } from '../app_settings';
import { buildErrorBanner } from './ui';

interface MenuEntry {
  label: string;
  route: Route;
}

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

  // Brand mark omitted — see home.ts: the panorama tab strip already
  // says "more" at the top, no need to repeat it.

  const entries: MenuEntry[] = [
    { label: 'categories', route: { name: 'categories' } },
    { label: 'diary', route: { name: 'diary' } },
    { label: 'people', route: { name: 'people' } },
    { label: 'crons', route: { name: 'crons' } },
  ];

  const list = document.createElement('nav');
  list.className = 'type-menu';
  list.setAttribute('aria-label', '더보기');
  entries.forEach((entry, i) => list.appendChild(buildMenuItem(entry, i)));
  root.appendChild(list);

  // Footer: profile + status compressed into two quiet lines, same shape
  // as home's greeting + meta block.
  const footer = document.createElement('footer');
  footer.className = 'type-footer';

  const userLabel =
    [user.firstName, user.lastName].filter(Boolean).join(' ') ||
    (user.username ? `@${user.username}` : `id=${user.id}`);

  const profile = document.createElement('p');
  profile.className = 'type-greeting';
  profile.textContent = userLabel;
  footer.appendChild(profile);

  const meta = document.createElement('p');
  meta.className = 'type-status';
  const model = prettyModel(pingResult.model);
  const parts: string[] = [];
  if (model) parts.push(model);
  if (readAppSettings().showDiagnostics) {
    if (pingResult.version) parts.push(`v${pingResult.version}`);
    parts.push(`${latencyMs}ms`);
  }
  meta.textContent = parts.join(' · ') || 'offline';
  footer.appendChild(meta);

  root.appendChild(footer);
}

function buildMenuItem(entry: MenuEntry, index: number): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'type-item';
  btn.style.setProperty('--enter-delay', `${index * 70}ms`);
  btn.textContent = entry.label;
  btn.addEventListener('click', () => navigate(entry.route));
  return btn;
}

function prettyModel(raw?: string): string {
  if (!raw) return '';
  return raw.split('/').pop()?.trim() ?? '';
}

function paintError(root: HTMLElement, message: string): void {
  root.innerHTML = '';
  root.appendChild(buildErrorBanner(message));
}
