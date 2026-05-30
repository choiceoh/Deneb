// desktop_shell.ts — PC-native layout for Telegram Desktop / web.
//
// The Mini App is mobile-first: on a phone every route is a full-screen
// single-column view that swaps in #app (see main.ts `dispatch`). That
// reads as cramped on Telegram Desktop, which opens the Mini App in a
// wide, resizable, mouse+keyboard window. This module adds a PC-native
// shell *on top of the exact same view modules* — nothing in views/*.ts
// changes, because every view already renders into whatever container it
// is handed (it never reaches for #app directly). We just hand it a
// content region or a detail pane instead.
//
// Two breakpoints, both driven from JS (the .desktop-shell / .two-pane
// classes are the CSS gate — styles.css keys every desktop rule off them,
// so when they're absent the mobile column is byte-for-byte untouched):
//
//   < 720px  (or any tg-mobile platform) → mobile column (main.ts path)
//   >= 720px tg-desktop                  → sidebar + single content region
//   >= 1000px tg-desktop                 → sidebar + master-detail panes
//                                          for the mail / calendar / topics
//                                          families (list left, detail right)
//
// The master pane *persists* across detail navigations within a family:
// clicking a second mail re-renders only the right pane + moves the
// selection highlight, so the list keeps its scroll position and doesn't
// re-fetch — the whole point of master-detail.

import { navigate, type Route } from './router';

// SHELL_MIN_WIDTH: at/above this (on a desktop-class platform) the
// persistent sidebar shell replaces the mobile column. Chosen to match
// the existing desktop CSS breakpoint (styles.css used min-width:720px),
// which also fixes the old gap where a narrow desktop window fell back to
// the 480px mobile column.
const SHELL_MIN_WIDTH = 720;
// TWO_PANE_MIN_WIDTH: at/above this the master-detail second pane appears.
// Below it (720-999) the shell shows the sidebar + a single content view.
const TWO_PANE_MIN_WIDTH = 1000;

// A view renderer (main.ts's renderViewInto) — mounts the view for a
// route into a given container. Injected so this module never imports
// main.ts (which would form an import cycle with main.ts's `boot()`).
export type ViewRenderer = (
  container: HTMLElement,
  route: Route,
  initData: string,
) => Promise<void>;

// --- Sidebar nav -----------------------------------------------------------
//
// The sidebar is the home menu (views/home.ts) made persistent: same
// labels, same order, same routes. On mobile, home *is* the menu; on
// desktop the sidebar carries it so every destination is one click away
// from anywhere. `family` is the top-level grouping used for the active
// highlight (a mail detail still highlights "mail", etc.).
interface NavEntry {
  label: string;
  route: Route;
  family: string;
}

const NAV_ENTRIES: NavEntry[] = [
  { label: 'calendar', route: { name: 'calendar' }, family: 'calendar' },
  { label: 'mail', route: { name: 'inbox' }, family: 'inbox' },
  { label: 'search', route: { name: 'search' }, family: 'search' },
  { label: 'topics', route: { name: 'sessions' }, family: 'sessions' },
  { label: 'categories', route: { name: 'categories' }, family: 'categories' },
  { label: 'crons', route: { name: 'crons' }, family: 'crons' },
  { label: 'settings', route: { name: 'settings' }, family: 'settings' },
];

// topLevelFamily maps any route to the sidebar family it belongs under, so
// drill-downs keep their parent destination highlighted. Broader than
// twoPaneTarget below: every route resolves to a family here (for the
// highlight), but only clean list↔leaf pairs get a second pane.
function topLevelFamily(route: Route): string {
  switch (route.name) {
    case 'inbox':
    case 'detail':
      return 'inbox';
    case 'calendar':
    case 'calendarEvent':
      return 'calendar';
    case 'sessions':
    case 'sessionTranscript':
    case 'topicNew':
      return 'sessions';
    case 'search':
    case 'personDetail':
      return 'search';
    case 'categories':
    case 'categoryPages':
    case 'wikiPage':
    case 'wikiNew':
      return 'categories';
    case 'crons':
      return 'crons';
    case 'settings':
    case 'modelSelect':
      return 'settings';
    case 'home':
      return 'home';
  }
  return 'home';
}

// --- Master-detail families ------------------------------------------------
//
// Only clean list↔leaf-detail pairs become two-pane: a flat list on the
// left, a single leaf on the right. Mail (inbox↔detail), calendar
// (calendar↔calendarEvent), and topics (sessions↔sessionTranscript) fit.
// Search is heterogeneous (wiki page / person), and categories→wiki is
// multi-level — those render single-pane in the content region instead.
interface MDFamily {
  family: string;
  // The list route rendered into the master pane.
  listRoute: Route;
  // The data-* attribute each list row carries (added in the list views)
  // so the shell can mark the selected row. Kebab form for getAttribute.
  rowAttr: string;
  // selectedId returns the leaf id when `route` is a detail in this
  // family, or null when `route` is the bare list (→ empty detail pane).
  selectedId(route: Route): string | null;
}

const MD_FAMILIES: MDFamily[] = [
  {
    family: 'inbox',
    listRoute: { name: 'inbox' },
    rowAttr: 'data-message-id',
    selectedId: (r) => (r.name === 'detail' ? r.messageId : null),
  },
  {
    family: 'calendar',
    listRoute: { name: 'calendar' },
    rowAttr: 'data-event-id',
    selectedId: (r) => (r.name === 'calendarEvent' ? r.eventId : null),
  },
  {
    family: 'sessions',
    listRoute: { name: 'sessions' },
    rowAttr: 'data-session-key',
    selectedId: (r) => (r.name === 'sessionTranscript' ? r.sessionKey : null),
  },
];

// Korean placeholder shown in the detail pane when no leaf is selected
// (e.g. the bare inbox list, or right after archiving a mail).
const EMPTY_DETAIL_TEXT: Record<string, string> = {
  inbox: '메일을 선택하세요',
  calendar: '일정을 선택하세요',
  sessions: '토픽을 선택하세요',
};

// twoPaneTarget returns the family to lay out as master-detail, but only
// when `route` is that family's bare list OR a recognized leaf detail.
// Auxiliary routes that merely share a family (topicNew, wikiNew, …) are
// NOT swallowed into a pane — they return null and render as a full view.
function twoPaneTarget(route: Route): MDFamily | null {
  for (const f of MD_FAMILIES) {
    if (route.name === f.listRoute.name) return f;
    if (f.selectedId(route) !== null) return f;
  }
  return null;
}

// --- Shell state -----------------------------------------------------------
//
// The master pane persists within a family across detail navigations, so
// we remember what's currently mounted. Reset whenever the shell is
// (re)built or torn down.
interface ShellState {
  family: string | null;
  twoPane: boolean;
  master: HTMLElement | null;
  detail: HTMLElement | null;
}

let state: ShellState = { family: null, twoPane: false, master: null, detail: null };

function resetState(): void {
  state = { family: null, twoPane: false, master: null, detail: null };
}

// --- Public API ------------------------------------------------------------

export type ShellMode = 'mobile' | 'shell1' | 'shell2';

// currentShellMode reports which layout the current viewport wants. main.ts
// re-dispatches when this changes across a resize so dragging the Telegram
// Desktop window narrow/wide re-lays-out live.
export function currentShellMode(): ShellMode {
  if (!isDesktopShellActive()) return 'mobile';
  return window.innerWidth >= TWO_PANE_MIN_WIDTH ? 'shell2' : 'shell1';
}

// isDesktopShellActive: desktop-class platform (body.tg-desktop, stamped in
// main.ts from tg.platform) AND wide enough for the sidebar shell.
export function isDesktopShellActive(): boolean {
  return (
    document.body.classList.contains('tg-desktop') &&
    window.innerWidth >= SHELL_MIN_WIDTH
  );
}

// teardownDesktopShell strips the shell chrome so the mobile column can
// render clean (e.g. the desktop window was dragged below SHELL_MIN_WIDTH).
// No-op when the shell isn't mounted. The caller renders the mobile view
// into #app immediately after, which repopulates the cleared container.
export function teardownDesktopShell(root: HTMLElement): void {
  if (!root.classList.contains('desktop-shell')) return;
  root.classList.remove('desktop-shell');
  root.innerHTML = '';
  resetState();
}

// desktopEscapeTarget: where Esc should go on the shell. From an open leaf
// detail it collapses back to that family's list (mail/calendar/topics);
// otherwise null, and the caller falls back to home. Width-agnostic so a
// narrow desktop window still collapses detail→list sensibly.
export function desktopEscapeTarget(route: Route): Route | null {
  for (const f of MD_FAMILIES) {
    if (f.selectedId(route) !== null) return f.listRoute;
  }
  return null;
}

// dispatchDesktop renders `route` inside the shell, building the shell
// chrome on first use. `render` is main.ts's renderViewInto.
export async function dispatchDesktop(
  root: HTMLElement,
  route: Route,
  initData: string,
  render: ViewRenderer,
): Promise<void> {
  const content = ensureShell(root);
  setActiveNav(root, topLevelFamily(route));

  const md = twoPaneTarget(route);
  const wide = window.innerWidth >= TWO_PANE_MIN_WIDTH;

  if (md && wide) {
    // (Re)build the two panes only when entering the family or on first
    // mount — otherwise the existing master pane is reused so its scroll
    // position and fetched rows survive the detail navigation.
    if (!state.twoPane || state.family !== md.family || !state.master?.isConnected) {
      content.classList.add('two-pane');
      content.innerHTML = '';
      const master = document.createElement('section');
      master.className = 'master-pane';
      const detail = document.createElement('section');
      detail.className = 'detail-pane';
      content.append(master, detail);
      state = { family: md.family, twoPane: true, master, detail };
      await render(master, md.listRoute, initData);
    }

    const id = md.selectedId(route);
    markSelected(state.master!, md.rowAttr, id);
    if (id === null) {
      renderDetailEmpty(state.detail!, md.family);
    } else {
      await render(state.detail!, route, initData);
    }
    return;
  }

  // Single-pane: sidebar + one full-width view. Covers narrow desktop
  // windows, non master-detail routes (search, categories, settings, …),
  // and the auxiliary forms (topicNew, wikiNew).
  content.classList.remove('two-pane');
  resetState();
  state.family = topLevelFamily(route);
  content.innerHTML = '';
  if (route.name === 'home') {
    renderDesktopHome(content);
  } else {
    await render(content, route, initData);
  }
}

// --- Shell DOM -------------------------------------------------------------

// ensureShell returns the content region, building the shell (sidebar +
// content) on first use. When the shell already exists it returns the
// existing content WITHOUT touching state, so the persisted master pane
// survives across dispatches.
function ensureShell(root: HTMLElement): HTMLElement {
  const existing = root.querySelector<HTMLElement>(':scope > .app-content');
  if (root.classList.contains('desktop-shell') && existing) return existing;

  root.classList.add('desktop-shell');
  root.innerHTML = '';
  root.appendChild(buildSidebar());
  const content = document.createElement('main');
  content.className = 'app-content';
  root.appendChild(content);
  resetState();
  return content;
}

function buildSidebar(): HTMLElement {
  const aside = document.createElement('aside');
  aside.className = 'app-sidebar';

  const brand = document.createElement('button');
  brand.type = 'button';
  brand.className = 'app-sidebar-brand';
  brand.textContent = 'deneb';
  brand.addEventListener('click', () => navigate({ name: 'home' }));
  aside.appendChild(brand);

  const nav = document.createElement('nav');
  nav.className = 'app-sidebar-nav';
  nav.setAttribute('aria-label', '주요 영역');
  for (const entry of NAV_ENTRIES) {
    const item = document.createElement('button');
    item.type = 'button';
    item.className = 'app-sidebar-item';
    item.dataset.family = entry.family;
    item.textContent = entry.label;
    item.addEventListener('click', () => navigate(entry.route));
    nav.appendChild(item);
  }
  aside.appendChild(nav);
  return aside;
}

function setActiveNav(root: HTMLElement, family: string): void {
  root.querySelectorAll<HTMLElement>('.app-sidebar-item').forEach((el) => {
    el.classList.toggle('is-active', el.dataset.family === family);
  });
}

// markSelected highlights the master row whose data-* id matches `id`
// (clearing any previous highlight). `id === null` just clears. We compare
// getAttribute values rather than building an attribute selector so ids
// with special characters (session keys, message ids) need no escaping.
function markSelected(master: HTMLElement, rowAttr: string, id: string | null): void {
  master.querySelectorAll<HTMLElement>(`[${rowAttr}]`).forEach((row) => {
    row.classList.toggle('is-selected', id !== null && row.getAttribute(rowAttr) === id);
  });
}

function renderDetailEmpty(detail: HTMLElement, family: string): void {
  detail.innerHTML = '';
  const box = document.createElement('div');
  box.className = 'detail-empty';
  box.textContent = EMPTY_DETAIL_TEXT[family] ?? '항목을 선택하세요';
  detail.appendChild(box);
}

// renderDesktopHome paints a calm welcome in the content region for the
// home route — on desktop the sidebar already carries the menu, so the
// big mobile type-menu would just duplicate it.
function renderDesktopHome(content: HTMLElement): void {
  content.innerHTML = '';
  const box = document.createElement('div');
  box.className = 'desktop-home';
  const brand = document.createElement('div');
  brand.className = 'desktop-home-brand';
  brand.textContent = 'deneb';
  const hint = document.createElement('div');
  hint.className = 'desktop-home-hint';
  hint.textContent = '왼쪽에서 영역을 선택하세요';
  box.append(brand, hint);
  content.appendChild(box);
}
