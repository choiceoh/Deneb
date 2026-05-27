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
import { buildErrorBanner } from './ui';
import type { Route } from '../router';

export async function renderHome(root: HTMLElement, initData: string): Promise<void> {
  const expectedHash = location.hash;
  root.innerHTML = '<div class="loading">로딩 중…</div>';
  try {
    const user = await whoami(initData);
    if (!isCurrentHash(expectedHash)) return;
    paint(root, user);
    // Notification fetch runs in the background after the menu paints —
    // the home screen never blocks on Gmail. Failures (gmail not
    // configured, transient outage) hide the section silently.
    void hydrateRecentNotice(root, initData, expectedHash);
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

  // Notice slot — populated asynchronously by hydrateRecentNotice once
  // Gmail returns. We paint a placeholder element here (rather than
  // inserting after the fact) so the menu's stagger animation isn't
  // disturbed when the notice arrives. The slot stays display:none
  // until there's something to show.
  const notice = document.createElement('section');
  notice.className = 'type-notice';
  notice.dataset.state = 'pending';
  notice.setAttribute('aria-live', 'polite');
  root.appendChild(notice);

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

// hydrateRecentNotice surfaces the single most-relevant unread mail at
// the top of home. Strategy: prefer messages Gmail has flagged
// `is:important`, fall back to plain unread when none exist. Anything
// older than 7 days is ignored — the home banner is for "right now",
// not an inbox snapshot. Errors (UNAVAILABLE when Gmail isn't
// configured, transient network failures) leave the slot empty so the
// home screen reads identically for operators without Gmail wired.
async function hydrateRecentNotice(
  root: HTMLElement,
  initData: string,
  expectedHash: string,
): Promise<void> {
  const slot = root.querySelector<HTMLElement>('.type-notice');
  if (!slot) return;
  const pick = await fetchTopMail(initData);
  if (!isCurrentHash(expectedHash) || !slot.isConnected) return;
  if (!pick) {
    slot.remove();
    return;
  }
  paintNotice(slot, pick);
}

async function fetchTopMail(initData: string): Promise<GmailMessageRow | null> {
  // Important-first, then any unread — keep both limits at 1 to avoid
  // doing the work of listing a full inbox just to throw 19 rows away.
  const important = await safeListRecent(initData, {
    query: 'is:important is:unread newer_than:7d',
    limit: 1,
  });
  if (important.length > 0) return important[0];
  const unread = await safeListRecent(initData, {
    query: 'is:unread newer_than:7d',
    limit: 1,
  });
  return unread[0] ?? null;
}

// safeListRecent swallows Gmail RPC errors so the home banner stays
// silent when the operator hasn't connected Gmail (UNAVAILABLE) or
// when there's a transient outage. Re-throwing here would leak red
// banners into the home screen on every launch for non-mail users.
async function safeListRecent(
  initData: string,
  opts: { query: string; limit: number },
): Promise<GmailMessageRow[]> {
  try {
    const res = await listRecent(initData, opts);
    return res.messages ?? [];
  } catch {
    return [];
  }
}

function paintNotice(slot: HTMLElement, mail: GmailMessageRow): void {
  slot.innerHTML = '';
  slot.dataset.state = 'ready';

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
