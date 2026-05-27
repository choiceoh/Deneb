// views/home.ts — the only landing screen.
//
// Idiom: pure black-and-white typography. No icons, no numbers, no
// tracked-caps captions, no dividers — the page is the words. The
// previous home/more split is gone: every domain surface lives in
// one column here so the operator can scan everything in one motion
// instead of toggling tabs. Footer keeps just the greeting; model +
// status moved to the settings screen, and an explicit "refresh"
// button is no longer needed (each drill-down view has its own).

import { whoami, type WhoamiResult } from '../rpc';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner } from './ui';
import type { Route } from '../router';

export async function renderHome(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">로딩 중…</div>';
  try {
    const user = await whoami(initData);
    if (!isCurrentHash(expectedHash)) return;
    paint(root, user);
  } catch (err) {
    if (!isCurrentHash(expectedHash)) return;
    renderHomeError(root, `백엔드 호출 실패: ${formatRpcError(err)}`);
  }
}

interface MenuEntry {
  label: string;
  route: Route;
}

function paint(root: HTMLElement, user: WhoamiResult): void {
  root.innerHTML = '';

  // Full destination list — ordered by how often the operator hits
  // each one. Calendar + mail are time-pressured (D-15 push, unread
  // triage); memory is the daily search surface; topics is the
  // active-conversation register; diary/people/categories/crons sit
  // lower because they're reach-when-needed rather than reach-daily.
  // settings is last — preferences are touched rarely, but they have
  // to live somewhere now that the panorama tab strip is gone.
  const entries: MenuEntry[] = [
    { label: 'calendar', route: { name: 'calendar' } },
    { label: 'mail', route: { name: 'inbox' } },
    { label: 'memory', route: { name: 'memory' } },
    { label: 'topics', route: { name: 'sessions' } },
    { label: 'diary', route: { name: 'diary' } },
    { label: 'people', route: { name: 'people' } },
    { label: 'categories', route: { name: 'categories' } },
    { label: 'crons', route: { name: 'crons' } },
    { label: 'settings', route: { name: 'settings' } },
  ];

  const list = document.createElement('nav');
  list.className = 'type-menu';
  list.setAttribute('aria-label', '주요 영역');
  entries.forEach((entry, i) =>
    list.appendChild(buildMenuItem(entry, i, entries.length)),
  );
  root.appendChild(list);

  // Footer: just the greeting now. Model + refresh removed — model
  // lives on the settings page; refresh either lives on each drill-
  // down view's header link, or the user pulls a tab again.
  const footer = document.createElement('footer');
  footer.className = 'type-footer';

  const greet = document.createElement('p');
  greet.className = 'type-greeting';
  greet.textContent = greeting(user.firstName);
  footer.appendChild(greet);

  root.appendChild(footer);
}

function buildMenuItem(entry: MenuEntry, index: number, total: number): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'type-item';
  // Stagger delay with ease-out spacing: early rows fire quickly,
  // later rows stretch out. The denominator is total-1 so the last
  // row always lands at the same delay regardless of how many menu
  // items there are.
  const denom = Math.max(1, total - 1);
  const ease = 1 - Math.pow(1 - index / denom, 2.2);
  const delay = Math.round(ease * 380);
  btn.style.setProperty('--enter-delay', `${delay}ms`);
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

function renderHomeError(root: HTMLElement, message: string): void {
  root.innerHTML = '';
  root.appendChild(buildErrorBanner(message));
}
