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
import type { Route } from '../router';

export async function renderHome(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;

  // Progressive paint: the type-menu doesn't depend on whoami, so we paint
  // it immediately with a placeholder greeting. The whoami RPC (~50-150ms
  // depending on backend warmth) then hydrates the greeting in-place. The
  // operator sees the navigable menu the moment the JS runs instead of
  // staring at "로딩 중…" for a network round-trip.
  paint(root, null);
  try {
    const user = await whoami(initData);
    if (!isCurrentHash(expectedHash) || !root.isConnected) return;
    hydrateGreeting(root, user);
  } catch (err) {
    if (!isCurrentHash(expectedHash) || !root.isConnected) return;
    // whoami fail is non-fatal for navigation — the menu is already
    // usable. We surface the error inline in the footer area so the
    // operator notices without losing the menu.
    hydrateGreetingError(root, formatRpcError(err));
  }
}

interface MenuEntry {
  label: string;
  route: Route;
}

function paint(root: HTMLElement, user: WhoamiResult | null): void {
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
  // While whoami is in flight we paint the phase-of-day phrase without a
  // name suffix. When the RPC settles, hydrateGreeting() swaps in the
  // personalized form. If the RPC never settles, the operator still
  // reads a sensible greeting.
  greet.textContent = greeting(user?.firstName);
  footer.appendChild(greet);

  root.appendChild(footer);
}

function hydrateGreeting(root: HTMLElement, user: WhoamiResult): void {
  const greet = root.querySelector<HTMLElement>('.type-greeting');
  if (!greet) return;
  greet.textContent = greeting(user.firstName);
}

function hydrateGreetingError(root: HTMLElement, message: string): void {
  const footer = root.querySelector<HTMLElement>('.type-footer');
  if (!footer) return;
  const err = document.createElement('p');
  err.className = 'type-greeting-error';
  err.textContent = `백엔드 호출 실패: ${message}`;
  footer.appendChild(err);
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

