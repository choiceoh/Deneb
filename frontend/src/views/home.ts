// views/home.ts — the only landing screen.
//
// Idiom: pure black-and-white typography. No icons, no numbers, no
// tracked-caps captions, no dividers — the page is the words. The
// previous home/more split is gone: every domain surface lives in
// one column here so the operator can scan everything in one motion
// instead of toggling tabs. Model + status moved to the settings
// screen, and an explicit "refresh" button is no longer needed
// (each drill-down view has its own).

import { whoami } from '../rpc';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { buildErrorBanner } from './ui';
import type { Route } from '../router';

export async function renderHome(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;

  // Progressive paint: the type-menu doesn't depend on whoami, so we paint
  // it immediately. whoami still runs for auth validation, but its result
  // no longer feeds any visible UI — on failure we surface a quiet inline
  // banner so the operator notices without losing the menu.
  paint(root);
  try {
    await whoami(initData);
  } catch (err) {
    if (!isCurrentHash(expectedHash) || !root.isConnected) return;
    root.appendChild(buildErrorBanner(`백엔드 호출 실패: ${formatRpcError(err)}`));
  }
}

interface MenuEntry {
  label: string;
  route: Route;
}

function paint(root: HTMLElement): void {
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
