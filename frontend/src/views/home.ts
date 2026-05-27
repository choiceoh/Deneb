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

// sessionStorage key for the per-session whoami success marker. We only
// store a literal "ok" — the call's result isn't consumed by the UI
// anymore (greeting was removed), so the cache exists purely to skip
// the network round-trip on subsequent home visits within the same
// Telegram WebView session. sessionStorage scope is exactly right:
// fresh on app launch, persisted across in-session re-renders, and
// auto-cleared when Telegram tears the WebView down.
const WHOAMI_OK_KEY = 'deneb.whoami.ok';

export async function renderHome(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;

  // Progressive paint: the type-menu doesn't depend on whoami, so we paint
  // it immediately. whoami still runs for auth validation, but its result
  // no longer feeds any visible UI — on failure we surface a quiet inline
  // banner so the operator notices without losing the menu.
  paint(root);
  try {
    await whoamiOnce(initData);
  } catch (err) {
    if (!isCurrentHash(expectedHash) || !root.isConnected) return;
    root.appendChild(buildErrorBanner(`백엔드 호출 실패: ${formatRpcError(err)}`));
  }
}

// whoamiOnce fires the RPC at most once per Telegram WebView session.
// The first call writes a success marker into sessionStorage; subsequent
// home visits in the same session return immediately. Failures don't
// poison the cache, so a transient backend hiccup still gets retried
// on the next navigation home.
async function whoamiOnce(initData: string): Promise<void> {
  if (readWhoamiOk()) return;
  await whoami(initData);
  writeWhoamiOk();
}

function readWhoamiOk(): boolean {
  // sessionStorage access can throw in some sandboxed contexts (older
  // Telegram WebView modes, private browsing on iOS) — treat that as a
  // cold cache so we just pay the RPC. Cheaper than a noisy try-catch
  // at every caller.
  try {
    return sessionStorage.getItem(WHOAMI_OK_KEY) === '1';
  } catch {
    return false;
  }
}

function writeWhoamiOk(): void {
  try {
    sessionStorage.setItem(WHOAMI_OK_KEY, '1');
  } catch {
    // Storage quota / disabled — fall through; the next visit just
    // re-fires whoami, same as if we'd never tried to cache.
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
  // triage); search is the single discovery surface for wiki + diary
  // + people (the per-domain listing views are gone — discovery
  // happens through the search box, not browsing); topics is the
  // active-conversation register; categories/crons sit lower because
  // they're reach-when-needed rather than reach-daily. settings is
  // last — preferences are touched rarely.
  const entries: MenuEntry[] = [
    { label: 'calendar', route: { name: 'calendar' } },
    { label: 'mail', route: { name: 'inbox' } },
    { label: 'search', route: { name: 'search' } },
    { label: 'topics', route: { name: 'sessions' } },
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
