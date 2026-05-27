// main.ts — Deneb Mini App entry + router shell.
//
// All real rendering lives in views/*.ts. This file:
//   1. Boots Telegram WebApp SDK and applies its theme params.
//   2. Validates that initData is present (otherwise show a friendly
//      "open from Telegram" banner).
//   3. Routes the current hash to the right view module and listens for
//      hashchange to re-render on navigation.
//   4. Manages Telegram's BackButton so it mirrors browser history.

import '@fontsource-variable/inter';
import './styles.css';
import { parseRoute, navigate, isTabRoute, type Route, type TabRouteName } from './router';
import { renderHome } from './views/home';
import { renderList } from './views/list';
import { renderDetail } from './views/detail';
import { renderMemory } from './views/memory';
import { renderSessions } from './views/sessions';
import { renderWikiPage } from './views/wiki_page';
import { renderSessionTranscript } from './views/session_transcript';
import { renderCalendar } from './views/calendar';
import { renderCalendarEvent } from './views/calendar_event';
import { renderMore } from './views/more';
import { renderSettings } from './views/settings';
import { renderModelSelect } from './views/model_select';
import { renderCategories } from './views/categories';
import { renderCategoryPages } from './views/category_pages';
import { renderDiary } from './views/diary';
import { renderCrons } from './views/crons';
import { renderPeople } from './views/people';
import { renderPersonDetail } from './views/person_detail';
import { renderWikiNew } from './views/wiki_new';
import { applyAppSettings, triggerSelectionHaptic } from './app_settings';
import { icon, type IconName } from './icons';

const root = document.getElementById('app')!;
const tabBarRoot = document.getElementById('tab-bar')!;
let cachedInitData: string | null = null;
let activeWebApp: WebApp | null = null;

const LOCAL_MOCK_HOSTS = new Set(['localhost', '127.0.0.1', '::1']);

function applyThemeFromTelegram(tg: WebApp): void {
  const params = tg.themeParams;
  const map: Record<string, string | undefined> = {
    '--tg-bg': params.bg_color,
    '--tg-text': params.text_color,
    '--tg-hint': params.hint_color,
    '--tg-link': params.link_color,
    '--tg-button': params.button_color,
    '--tg-button-text': params.button_text_color,
    '--tg-secondary-bg': params.secondary_bg_color,
  };
  const docEl = document.documentElement;
  const docStyle = docEl.style;
  for (const [name, value] of Object.entries(map)) {
    if (value) docStyle.setProperty(name, value);
  }
  // Stamp the active scheme so CSS can swap hairline/shadow tokens
  // without re-reading themeParams. Telegram exposes 'light' | 'dark';
  // default to light if undefined.
  if (tg.colorScheme === 'light' || tg.colorScheme === 'dark') {
    docEl.dataset.colorScheme = tg.colorScheme;
  }
}

function renderError(message: string): void {
  root.innerHTML = '';
  const banner = document.createElement('div');
  banner.className = 'error';
  banner.textContent = message;
  root.appendChild(banner);
  const muted = document.createElement('div');
  muted.className = 'muted';
  muted.textContent = 'Deneb Mini App — open me from Telegram';
  root.appendChild(muted);
}

// Tab bar visibility = exactly the top-level tab destinations. Drill-down
// views (detail, wikiPage, calendarEvent, etc.) hide the bar so the
// Telegram BackButton can take over.
function showsTabBar(route: Route): boolean {
  return isTabRoute(route);
}

function syncBackButton(route: Route): void {
  const back = activeWebApp?.BackButton;
  if (!back) return;
  if (showsTabBar(route)) {
    back.hide();
  } else {
    back.show();
  }
}

const TAB_DEFS: ReadonlyArray<{ name: TabRouteName; icon: IconName; label: string }> = [
  { name: 'home', icon: 'home', label: '홈' },
  { name: 'more', icon: 'menu', label: '더보기' },
  { name: 'settings', icon: 'settings', label: '설정' },
];

function renderTabBar(route: Route): void {
  const visible = showsTabBar(route);
  tabBarRoot.classList.toggle('tab-bar-visible', visible);
  document.body.classList.toggle('has-tab-bar', visible);
  if (!visible) {
    tabBarRoot.innerHTML = '';
    return;
  }
  tabBarRoot.innerHTML = '';
  for (const def of TAB_DEFS) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'tab-item' + (def.name === route.name ? ' tab-item-active' : '');
    btn.innerHTML = `
      <span class="tab-item-icon">${icon(def.icon)}</span>
      <span class="tab-item-label"></span>
    `;
    (btn.querySelector('.tab-item-label') as HTMLElement).textContent = def.label;
    btn.addEventListener('click', () => {
      if (def.name === route.name) return;
      triggerSelectionHaptic();
      navigate({ name: def.name });
    });
    tabBarRoot.appendChild(btn);
  }
}

async function dispatch(route: Route): Promise<void> {
  if (!cachedInitData) return;
  syncBackButton(route);
  renderTabBar(route);
  switch (route.name) {
    case 'home':
      await renderHome(root, cachedInitData);
      return;
    case 'inbox':
      await renderList(root, cachedInitData);
      return;
    case 'detail':
      await renderDetail(root, cachedInitData, route.messageId);
      return;
    case 'memory':
      renderMemory(root, cachedInitData);
      return;
    case 'sessions':
      await renderSessions(root, cachedInitData);
      return;
    case 'wikiPage':
      await renderWikiPage(root, cachedInitData, route.path);
      return;
    case 'sessionTranscript':
      await renderSessionTranscript(root, cachedInitData, route.sessionKey);
      return;
    case 'calendar':
      await renderCalendar(root, cachedInitData);
      return;
    case 'calendarEvent':
      await renderCalendarEvent(root, cachedInitData, route.eventId);
      return;
    case 'more':
      await renderMore(root, cachedInitData);
      return;
    case 'settings':
      renderSettings(root, cachedInitData);
      return;
    case 'modelSelect':
      renderModelSelect(root, cachedInitData);
      return;
    case 'categories':
      await renderCategories(root, cachedInitData);
      return;
    case 'categoryPages':
      await renderCategoryPages(root, cachedInitData, route.category);
      return;
    case 'diary':
      await renderDiary(root, cachedInitData);
      return;
    case 'crons':
      await renderCrons(root, cachedInitData);
      return;
    case 'people':
      await renderPeople(root, cachedInitData);
      return;
    case 'personDetail':
      await renderPersonDetail(root, cachedInitData, route.email);
      return;
    case 'wikiNew':
      renderWikiNew(root, cachedInitData, route.category ?? '');
      return;
  }
}

function handleHashChange(): void {
  const route = parseRoute(location.hash);
  // View Transitions API gives us a free Zune-style shear/fade between
  // any two route states without us having to hand-orchestrate exit +
  // enter animations on every component. We hand it our dispatch fn;
  // it snapshots the old DOM, lets the new DOM mount, and crossfades
  // through `::view-transition-old(root)` and `::view-transition-new(root)`
  // keyframes defined in styles.css. Telegram's WebView (Chromium-based,
  // recent enough on both Android and desktop) supports the API. Older
  // engines just fall through to the un-animated dispatch.
  const startTransition = (document as Document & {
    startViewTransition?: (cb: () => void | Promise<void>) => unknown;
  }).startViewTransition;
  if (typeof startTransition === 'function') {
    startTransition.call(document, () => {
      void dispatch(route);
    });
  } else {
    void dispatch(route);
  }
}

function boot(): void {
  const tg = resolveWebApp();
  if (!tg) {
    renderError(
      '이 페이지는 Telegram 클라이언트 안에서 열어야 합니다. 봇 메뉴 버튼을 눌러주세요.',
    );
    return;
  }
  tg.ready();
  applyThemeFromTelegram(tg);

  const initData = tg.initData;
  if (!initData) {
    renderError(
      'Telegram 이 launch 데이터를 보내지 않았습니다. 메뉴 버튼을 다시 눌러보세요.',
    );
    return;
  }
  cachedInitData = initData;
  activeWebApp = tg;
  applyAppSettings();

  // Wire Telegram's BackButton to the router. Detail views pop to their
  // list, list views pop to home, and everything else (including home
  // itself) pops to home.
  tg.BackButton?.onClick(() => {
    const route = parseRoute(location.hash);
    switch (route.name) {
      case 'detail':
        navigate({ name: 'inbox' });
        return;
      case 'wikiPage':
        navigate({ name: 'memory' });
        return;
      case 'sessionTranscript':
        navigate({ name: 'sessions' });
        return;
      case 'calendarEvent':
        navigate({ name: 'calendar' });
        return;
      case 'categoryPages':
        navigate({ name: 'categories' });
        return;
      case 'personDetail':
        navigate({ name: 'people' });
        return;
      case 'modelSelect':
        navigate({ name: 'settings' });
        return;
      case 'wikiNew':
        // Pop back to wherever the user came from — memory search or
        // a category page. history.back() is what wiki_new.ts's own
        // cancel button uses for the same reason.
        history.back();
        return;
      // List-level destinations now live under 더보기, so back pops
      // there (not home) — matches the path the user took to get in.
      case 'inbox':
      case 'memory':
      case 'sessions':
      case 'calendar':
      case 'categories':
      case 'diary':
      case 'crons':
      case 'people':
        navigate({ name: 'more' });
        return;
      default:
        navigate({ name: 'home' });
    }
  });

  window.addEventListener('hashchange', handleHashChange);
  void dispatch(parseRoute(location.hash));
}

boot();

function resolveWebApp(): WebApp | undefined {
  const tg = window.Telegram?.WebApp;
  if (tg?.initData || !shouldUseLocalTelegramMock()) return tg;

  return {
    ...(tg ?? {}),
    initData: 'mock-init-data',
    themeParams: {
      bg_color: '#ffffff',
      text_color: '#111111',
      hint_color: '#707579',
      link_color: '#2481cc',
      button_color: '#2481cc',
      button_text_color: '#ffffff',
      secondary_bg_color: '#f4f4f5',
      ...(tg?.themeParams ?? {}),
    },
    ready: tg?.ready?.bind(tg) ?? (() => undefined),
    BackButton: tg?.BackButton ?? {
      show: () => undefined,
      hide: () => undefined,
      onClick: () => undefined,
    },
    HapticFeedback: tg?.HapticFeedback ?? {
      selectionChanged: () => undefined,
    },
  } as WebApp;
}

function shouldUseLocalTelegramMock(): boolean {
  if (!LOCAL_MOCK_HOSTS.has(location.hostname)) return false;
  return new URLSearchParams(location.search).has('mockTelegram');
}
