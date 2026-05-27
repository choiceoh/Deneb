// views/home.ts — landing screen.
//
// Design idiom: Zune HD / Windows Phone "Metro" — typography carries the
// hierarchy, no icons compete with the words, motion is the texture. The
// hero is a giant ultralight wordmark + a personal greeting, the menu is
// a numbered list of huge labels each backed by a hairline divider. On
// mount the items stagger in (~60ms per row) so the screen reads as
// "composed" rather than "snapped together".

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
  meta: string;
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

  // Hero block: ultralight wordmark, full-weight greeting, tiny meta.
  // Three weight classes in three lines = the whole identity.
  const hero = document.createElement('header');
  hero.className = 'hero';
  hero.innerHTML = `
    <h1 class="hero-title">Deneb</h1>
    <p class="hero-greeting"></p>
    <p class="hero-status"></p>
  `;
  (hero.querySelector('.hero-greeting') as HTMLElement).textContent = greeting(user.firstName);
  const model = prettyModel(pingResult.model);
  (hero.querySelector('.hero-status') as HTMLElement).textContent = model
    ? `${model} · 정상`
    : '연결 안 됨';
  root.appendChild(hero);

  // Menu: numbered list of huge labels. Order is intentional, by product
  // priority — calendar (time-pressured) first, mail steady-state, then
  // memory + sessions as reference.
  const entries: MenuEntry[] = [
    { label: '일정', meta: '다가오는 회의 · D-15분 알림', route: { name: 'calendar' } },
    { label: '메일', meta: '미처리 트리아지', route: { name: 'inbox' } },
    { label: '메모리', meta: '위키 / 빠른 검색', route: { name: 'memory' } },
    { label: '세션', meta: '실행 중 · 완료', route: { name: 'sessions' } },
  ];

  const list = document.createElement('nav');
  list.className = 'metro-menu';
  list.setAttribute('aria-label', '주요 영역');
  entries.forEach((entry, i) => {
    list.appendChild(buildMenuItem(entry, i));
  });
  root.appendChild(list);

  // Subtle, low-weight "refresh" affordance below the menu — kept around
  // because the user still asks for it, but tones down from the old
  // chunky primary button so it doesn't compete with the menu.
  const refresh = document.createElement('button');
  refresh.type = 'button';
  refresh.className = 'metro-refresh';
  refresh.textContent = '새로고침';
  refresh.addEventListener('click', () => {
    void renderHome(root, initData);
  });
  root.appendChild(refresh);

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
  btn.className = 'metro-item';
  // Stagger fade-in is driven by a CSS custom property the keyframes
  // animation reads — keeps the cascade declarative and means we don't
  // have to bake a fixed N-item table into stylesheet selectors.
  btn.style.setProperty('--enter-delay', `${index * 60}ms`);
  btn.innerHTML = `
    <span class="metro-num"></span>
    <span class="metro-text">
      <span class="metro-label"></span>
      <span class="metro-meta"></span>
    </span>
  `;
  (btn.querySelector('.metro-num') as HTMLElement).textContent = String(index + 1).padStart(2, '0');
  (btn.querySelector('.metro-label') as HTMLElement).textContent = entry.label;
  (btn.querySelector('.metro-meta') as HTMLElement).textContent = entry.meta;
  btn.addEventListener('click', () => {
    navigate(entry.route);
  });
  return btn;
}

// greeting picks a Korean phase-of-day phrase. Suffixes the user's first
// name when we have one so the screen reads as a personal landing rather
// than a generic dashboard.
function greeting(firstName?: string): string {
  const h = new Date().getHours();
  const phase = h < 5 ? '안녕하세요' : h < 12 ? '좋은 아침' : h < 18 ? '좋은 오후' : '좋은 저녁';
  const who = firstName?.trim();
  return who ? `${phase}, ${who}` : phase;
}

// prettyModel strips the provider prefix and uppercases the model name
// so e.g. "zai/glm-5.1" reads as "GLM-5.1" — the part the operator
// actually cares about. Returns "" when the gateway hasn't reported
// a model yet (caller falls back to "연결 안 됨").
function prettyModel(raw?: string): string {
  if (!raw) return '';
  const trimmed = raw.split('/').pop()?.trim() ?? '';
  return trimmed.toUpperCase();
}

function renderHomeError(root: HTMLElement, message: string): void {
  root.innerHTML = '';
  root.appendChild(buildErrorBanner(message));
  const muted = document.createElement('div');
  muted.className = 'muted';
  muted.textContent = 'Deneb Mini App';
  root.appendChild(muted);
}
