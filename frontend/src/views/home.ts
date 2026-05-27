// views/home.ts — landing screen.
//
// Idiom: pure black-and-white typography. No icons, no numbers, no
// tracked-caps captions, no dividers — the page is the words. Menu
// labels are lowercase English because that's the typography the user
// brought up (Zune HD home screen) and because Korean shapes don't
// flow the same way at 48-72px ultralight weight. The Korean greeting
// stays — it's a one-line read, not a hero element.

import { ping, whoami, type PingResult, type WhoamiResult } from '../rpc';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { readAppSettings } from '../app_settings';
import { buildErrorBanner } from './ui';
import type { Route } from '../router';

export async function renderHome(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">로딩 중…</div>';
  try {
    const t0 = performance.now();
    const [user, pingResult] = await Promise.all([whoami(initData), ping(initData)]);
    if (!isCurrentHash(expectedHash)) return;
    const latencyMs = Math.round(performance.now() - t0);
    paint(root, initData, user, pingResult, latencyMs);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    renderHomeError(root, `백엔드 호출 실패: ${formatRpcError(err)}`);
  }
}

interface MenuEntry {
  label: string;
  route: Route;
}

function paint(
  root: HTMLElement,
  initData: string,
  user: WhoamiResult,
  pingResult: PingResult,
  latencyMs: number,
): void {
  root.innerHTML = '';

  // No brand mark here — the panorama tab strip pinned to the top of
  // the viewport already identifies the active page ("home"), so a
  // separate "deneb" sub-line would be redundant. The menu IS the page.

  // Menu: four lowercase English words, set huge. No numbers, no
  // sublabels, no divider rules. Just the words. The stagger animation
  // does the work of "list-ness".
  const entries: MenuEntry[] = [
    { label: 'calendar', route: { name: 'calendar' } },
    { label: 'mail', route: { name: 'inbox' } },
    { label: 'memory', route: { name: 'memory' } },
    { label: 'sessions', route: { name: 'sessions' } },
  ];

  const list = document.createElement('nav');
  list.className = 'type-menu';
  list.setAttribute('aria-label', '주요 영역');
  entries.forEach((entry, i) => {
    list.appendChild(buildMenuItem(entry, i));
  });
  root.appendChild(list);

  // Footer block: the greeting + live status read, all in one quiet line
  // beneath the menu. Sits in the page's negative space so the eye only
  // lands here after taking in the menu.
  const footer = document.createElement('footer');
  footer.className = 'type-footer';

  const greet = document.createElement('p');
  greet.className = 'type-greeting';
  greet.textContent = greeting(user.firstName);
  footer.appendChild(greet);

  const status = document.createElement('p');
  status.className = 'type-status';
  const model = prettyModel(pingResult.model);
  status.textContent = model ? `${model.toLowerCase()} · online` : 'offline';
  footer.appendChild(status);

  const refresh = document.createElement('button');
  refresh.type = 'button';
  refresh.className = 'type-refresh';
  refresh.textContent = 'refresh';
  refresh.addEventListener('click', () => {
    void renderHome(root, initData);
  });
  footer.appendChild(refresh);

  root.appendChild(footer);

  if (readAppSettings().showDiagnostics) {
    const muted = document.createElement('div');
    muted.className = 'muted';
    const userLabel =
      [user.firstName, user.lastName].filter(Boolean).join(' ') ||
      (user.username ? `@${user.username}` : `id=${user.id}`);
    muted.textContent = `${userLabel} · v${pingResult.version || '?'} · ${latencyMs}ms`;
    root.appendChild(muted);
  }
}

function buildMenuItem(entry: MenuEntry, index: number): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'type-item';
  // Per-row stagger delay drives the CSS keyframes — keeps the animation
  // declarative without baking nth-child rules into the stylesheet.
  btn.style.setProperty('--enter-delay', `${index * 70}ms`);
  btn.textContent = entry.label;
  btn.addEventListener('click', () => {
    navigate(entry.route);
  });
  return btn;
}

// greeting picks a Korean phase-of-day phrase. Suffixes the user's first
// name when we have one so the page reads as a personal landing.
function greeting(firstName?: string): string {
  const h = new Date().getHours();
  const phase = h < 5 ? '안녕하세요' : h < 12 ? '좋은 아침' : h < 18 ? '좋은 오후' : '좋은 저녁';
  const who = firstName?.trim();
  return who ? `${phase}, ${who}` : phase;
}

// prettyModel strips the provider prefix so e.g. "zai/glm-5.1" reads as
// "glm-5.1" — the part the operator actually cares about.
function prettyModel(raw?: string): string {
  if (!raw) return '';
  return raw.split('/').pop()?.trim() ?? '';
}

function renderHomeError(root: HTMLElement, message: string): void {
  root.innerHTML = '';
  root.appendChild(buildErrorBanner(message));
  const muted = document.createElement('div');
  muted.className = 'muted';
  muted.textContent = 'Deneb Mini App';
  root.appendChild(muted);
}
