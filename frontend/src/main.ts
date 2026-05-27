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

const root = document.getElementById('app')!;
const tabBarRoot = document.getElementById('tab-bar')!;
let cachedInitData: string | null = null;
let activeWebApp: WebApp | null = null;

const LOCAL_MOCK_HOSTS = new Set(['localhost', '127.0.0.1', '::1']);

function applyThemeFromTelegram(tg: WebApp): void {
  const params = tg.themeParams;
  // In dark mode we want real AMOLED black, not Telegram's muted blue-gray
  // bg_color (~#17212b). The CSS [data-color-scheme='dark'] block already
  // sets --tg-bg: #000, but an inline JS style.setProperty here would beat
  // that selector on specificity. So in dark mode we skip the bg/secondary
  // overrides and let CSS run them; in light mode we apply everything.
  const isDark = tg.colorScheme === 'dark';
  const map: Record<string, string | undefined> = {
    '--tg-bg': isDark ? undefined : params.bg_color,
    '--tg-text': params.text_color,
    '--tg-hint': params.hint_color,
    '--tg-link': params.link_color,
    '--tg-button': params.button_color,
    '--tg-button-text': params.button_text_color,
    '--tg-secondary-bg': isDark ? undefined : params.secondary_bg_color,
  };
  const docEl = document.documentElement;
  const docStyle = docEl.style;
  for (const [name, value] of Object.entries(map)) {
    if (value) docStyle.setProperty(name, value);
  }
  // Stamp the active scheme so CSS can swap hairline/shadow tokens
  // without re-reading themeParams. Default to dark when Telegram doesn't
  // hand us a scheme — modern operators are on dark, and that's also where
  // our typography idiom looks intended.
  docEl.dataset.colorScheme = tg.colorScheme === 'light' ? 'light' : 'dark';

  // Push the same black to Telegram's own chrome (header, bottom safe
  // area, swipe-back background) so the Mini App appears framed by the
  // same color it paints. Both APIs are no-ops on platforms that haven't
  // implemented them yet.
  if (isDark) {
    tg.setHeaderColor?.('#000000');
    tg.setBackgroundColor?.('#000000');
  } else if (params.bg_color) {
    tg.setHeaderColor?.(params.bg_color);
    tg.setBackgroundColor?.(params.bg_color);
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

// Tab labels are English lowercase to match the page typography idiom.
// The Korean fallback in the system font stack still kicks in if any
// label gets localized later; we just don't need it for these three.
const TAB_DEFS: ReadonlyArray<{ name: TabRouteName; label: string }> = [
  { name: 'home', label: 'home' },
  { name: 'settings', label: 'settings' },
];

function renderTabBar(route: Route): void {
  const visible = showsTabBar(route);
  tabBarRoot.classList.toggle('tab-bar-visible', visible);
  document.body.classList.toggle('has-tab-bar', visible);
  if (!visible) {
    tabBarRoot.innerHTML = '';
    return;
  }
  // Zune-style panorama: big lowercase words in a horizontal scroller.
  // The active tab gets full weight + full opacity; siblings ride dim
  // and slightly to the side, with the trailing one prebleeding past
  // the right edge so a hint of "next" peeks into the active surface.
  tabBarRoot.innerHTML = '';
  for (const def of TAB_DEFS) {
    const btn = document.createElement('button');
    btn.type = 'button';
    const isActive = def.name === route.name;
    btn.className = 'panorama-tab' + (isActive ? ' panorama-tab-active' : '');
    btn.textContent = def.label;
    btn.addEventListener('click', () => {
      if (isActive) return;
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

// Stash the previous route so the next transition can decide whether
// to flow left→right (going forward in the tab order, e.g. home →
// settings) or right→left (going back). For drill-down enters we
// flow in from below; for drill-down exits we flow out below.
let lastRoute: Route | null = null;

const TRANSITION_ORDER: Record<string, number> = {
  home: 0,
  settings: 1,
};

function transitionDirection(from: Route | null, to: Route): 'forward' | 'back' | 'deep' | 'shallow' {
  if (!from) return 'forward';
  const fromOrder = TRANSITION_ORDER[from.name];
  const toOrder = TRANSITION_ORDER[to.name];
  if (fromOrder !== undefined && toOrder !== undefined) {
    return toOrder > fromOrder ? 'forward' : 'back';
  }
  // tab → drill-down = deep; drill-down → tab = shallow.
  if (fromOrder !== undefined && toOrder === undefined) return 'deep';
  if (fromOrder === undefined && toOrder !== undefined) return 'shallow';
  // drill-down → drill-down (e.g., mail detail → wiki page) treats as
  // forward — the keyframes pick a neutral slide direction.
  return 'forward';
}

function handleHashChange(): void {
  const route = parseRoute(location.hash);
  // View Transitions API gives us a free Zune-style shear/fade between
  // any two route states without us having to hand-orchestrate exit +
  // enter animations on every component. We hand it our dispatch fn;
  // it snapshots the old DOM, lets the new DOM mount, and crossfades
  // through `::view-transition-old(root)` and `::view-transition-new(root)`
  // keyframes defined in styles.css. Direction is stamped onto
  // <html data-transition-dir="…"> so the keyframes can pick the
  // matching slide vector.
  const dir = transitionDirection(lastRoute, route);
  document.documentElement.dataset.transitionDir = dir;
  lastRoute = route;

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
        // The "more" hub is gone — every former more-tab destination
        // now pops back to home, where its entry word lives.
        navigate({ name: 'home' });
        return;
      default:
        navigate({ name: 'home' });
    }
  });

  window.addEventListener('hashchange', handleHashChange);

  // Auto-hide the panorama tab strip when the user scrolls down, slide
  // it back into view when they scroll up. Only fires when the page is
  // tall enough to scroll (no point hiding a strip the user can already
  // see in full). 8px threshold avoids flickering on bouncy iOS
  // momentum scrolling. Class lives on <body> so CSS can shift the
  // strip via transform/opacity without re-querying refs.
  let lastScrollY = window.scrollY;
  let scrollTicking = false;
  window.addEventListener(
    'scroll',
    () => {
      if (scrollTicking) return;
      scrollTicking = true;
      requestAnimationFrame(() => {
        const y = window.scrollY;
        const dy = y - lastScrollY;
        if (Math.abs(dy) > 8) {
          if (dy > 0 && y > 80) {
            document.body.classList.add('panorama-hidden');
          } else {
            document.body.classList.remove('panorama-hidden');
          }
          lastScrollY = y;
        }
      });
      // Reset on next frame so subsequent rafs can fire.
      requestAnimationFrame(() => {
        scrollTicking = false;
      });
    },
    { passive: true },
  );

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
