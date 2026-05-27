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
import { listRecent, type GmailMessageRow } from '../gmail';
import { formatRpcError } from '../format';
import { isCurrentHash, navigate } from '../router';
import { setPullToRefreshHandler } from '../pull_to_refresh';
import { buildErrorBanner } from './ui';
import type { Route } from '../router';

export async function renderHome(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">로딩 중…</div>';
  try {
    const user = await whoami(initData);
    if (!isCurrentHash(expectedHash)) return;
    paint(root, user);
    // Mail notice is gated behind pull-to-refresh: the initial render
    // shows nothing about mail. The operator only sees the latest
    // unread subject after deliberately pulling the page — so the home
    // screen stays "the page is the words" by default, and the mail
    // peek is opt-in per session.
    setPullToRefreshHandler(() => refreshMailNotice(root, initData, expectedHash));
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
  const entries: MenuEntry[] = [
    { label: 'calendar', route: { name: 'calendar' } },
    { label: 'mail', route: { name: 'inbox' } },
    { label: 'memory', route: { name: 'memory' } },
    { label: 'topics', route: { name: 'sessions' } },
    { label: 'diary', route: { name: 'diary' } },
    { label: 'people', route: { name: 'people' } },
    { label: 'categories', route: { name: 'categories' } },
    { label: 'crons', route: { name: 'crons' } },
  ];

  const list = document.createElement('nav');
  list.className = 'type-menu';
  list.setAttribute('aria-label', '주요 영역');
  entries.forEach((entry, i) => list.appendChild(buildMenuItem(entry, i)));
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

function buildMenuItem(entry: MenuEntry, index: number): HTMLButtonElement {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'type-item';
  // Stagger delay with ease-out spacing: early rows fire quickly,
  // later rows stretch out. ~45ms for the first gap, expanding toward
  // ~85ms by the last row. Same arrival window everyone uses, but the
  // motion reads as "decelerating into place" instead of metronomic.
  const ease = 1 - Math.pow(1 - index / 7, 2.2);
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

// refreshMailNotice is invoked by the pull-to-refresh gesture. It fetches
// the single most-recent unread mail (no importance filter — Gmail's
// `is:important` label can be sticky and would surface a 5-day-old flagged
// mail over a fresh unread one, the opposite of what "pull for the latest"
// implies) and inserts/replaces the notice line above the menu. Errors —
// UNAVAILABLE when Gmail isn't configured, transient network failures —
// remove the slot silently so the home screen reads identically for
// operators without Gmail wired.
async function refreshMailNotice(
  root: HTMLElement,
  initData: string,
  expectedHash: string,
): Promise<void> {
  const pick = await safeFetchLatestUnread(initData);
  if (!isCurrentHash(expectedHash) || !root.isConnected) return;

  const existing = root.querySelector<HTMLElement>('.type-notice');
  if (!pick) {
    existing?.remove();
    return;
  }
  const slot = existing ?? buildNoticeSlot();
  paintNotice(slot, pick);
  if (!existing) {
    // First successful pull this session: insert above the menu so the
    // mail subject sits above the navigation, not in the footer area.
    const menu = root.querySelector<HTMLElement>('.type-menu');
    if (menu) {
      root.insertBefore(slot, menu);
    } else {
      root.prepend(slot);
    }
  }
}

async function safeFetchLatestUnread(initData: string): Promise<GmailMessageRow | null> {
  try {
    const res = await listRecent(initData, {
      query: 'is:unread newer_than:7d',
      limit: 1,
    });
    return res.messages?.[0] ?? null;
  } catch {
    return null;
  }
}

function buildNoticeSlot(): HTMLElement {
  const notice = document.createElement('section');
  notice.className = 'type-notice';
  notice.setAttribute('aria-live', 'polite');
  return notice;
}

function paintNotice(slot: HTMLElement, mail: GmailMessageRow): void {
  slot.innerHTML = '';

  // One quiet line: tap the subject to open the mail. Sender and time
  // are dropped — the rest of home is "the page is the words" and a
  // multi-field row would compete with the type-menu's typography.
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'type-notice-row';
  btn.textContent = mail.subject || '(제목 없음)';
  btn.addEventListener('click', () => {
    navigate({ name: 'detail', messageId: mail.id });
  });
  slot.appendChild(btn);
}
