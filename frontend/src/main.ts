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
import { parseRoute, navigate, isHomeRoute, type Route } from './router';
// Home stays statically imported so the first-paint chunk includes
// everything needed to render the index. Every other view is split out
// via dynamic import in dispatch() — the operator only pays the chunk-
// fetch cost the first time they navigate into a given destination, and
// prefetchOtherViews() warms the cache during idle time after boot so
// that first visit usually finds the chunk already there.
import { renderHome } from './views/home';
import { applyAppSettings } from './app_settings';
import { clearPullToRefreshHandler } from './pull_to_refresh';

const root = document.getElementById('app')!;
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

// Home is the only no-back route; every other view is a drill-down
// where Telegram's BackButton takes the operator to its parent.
function syncBackButton(route: Route): void {
  const back = activeWebApp?.BackButton;
  if (!back) return;
  if (isHomeRoute(route)) {
    back.hide();
  } else {
    back.show();
  }
}

// dispatch awaits the dynamic chunk fetch, then invokes the view's
// render function as fire-and-forget (`void renderX(...)`). Every view
// paints its loading-state DOM synchronously at the top of its body
// before its first await — so by the time render's promise yields,
// the new DOM is already in place and dispatch can return. The View
// Transitions API caller awaits dispatch, snapshots the new DOM, and
// animates. RPC settles afterward and hydrates content into the
// already-visible layout, with no transition animation.
async function dispatch(route: Route): Promise<void> {
  if (!cachedInitData) return;
  syncBackButton(route);
  // Clear any pull-to-refresh handler from the previous view; views
  // that want to opt in re-register their own handler after rendering.
  clearPullToRefreshHandler();
  const initData = cachedInitData;
  switch (route.name) {
    case 'home':
      void renderHome(root, initData);
      return;
    case 'inbox': {
      const { renderList } = await import('./views/list');
      void renderList(root, initData);
      return;
    }
    case 'detail': {
      const { renderDetail } = await import('./views/detail');
      void renderDetail(root, initData, route.messageId);
      return;
    }
    case 'memory': {
      const { renderMemory } = await import('./views/memory');
      renderMemory(root, initData);
      return;
    }
    case 'sessions': {
      const { renderSessions } = await import('./views/sessions');
      void renderSessions(root, initData);
      return;
    }
    case 'wikiPage': {
      const { renderWikiPage } = await import('./views/wiki_page');
      void renderWikiPage(root, initData, route.path);
      return;
    }
    case 'sessionTranscript': {
      const { renderSessionTranscript } = await import('./views/session_transcript');
      void renderSessionTranscript(root, initData, route.sessionKey);
      return;
    }
    case 'calendar': {
      const { renderCalendar } = await import('./views/calendar');
      void renderCalendar(root, initData);
      return;
    }
    case 'calendarEvent': {
      const { renderCalendarEvent } = await import('./views/calendar_event');
      void renderCalendarEvent(root, initData, route.eventId);
      return;
    }
    case 'settings': {
      const { renderSettings } = await import('./views/settings');
      renderSettings(root, initData);
      return;
    }
    case 'modelSelect': {
      const { renderModelSelect } = await import('./views/model_select');
      renderModelSelect(root, initData);
      return;
    }
    case 'categories': {
      const { renderCategories } = await import('./views/categories');
      void renderCategories(root, initData);
      return;
    }
    case 'categoryPages': {
      const { renderCategoryPages } = await import('./views/category_pages');
      void renderCategoryPages(root, initData, route.category);
      return;
    }
    case 'diary': {
      const { renderDiary } = await import('./views/diary');
      void renderDiary(root, initData);
      return;
    }
    case 'crons': {
      const { renderCrons } = await import('./views/crons');
      void renderCrons(root, initData);
      return;
    }
    case 'people': {
      const { renderPeople } = await import('./views/people');
      void renderPeople(root, initData);
      return;
    }
    case 'personDetail': {
      const { renderPersonDetail } = await import('./views/person_detail');
      void renderPersonDetail(root, initData, route.email);
      return;
    }
    case 'wikiNew': {
      const { renderWikiNew } = await import('./views/wiki_new');
      renderWikiNew(root, initData, route.category ?? '');
      return;
    }
    case 'topicNew': {
      const { renderTopicNew } = await import('./views/topic_new');
      renderTopicNew(root, initData);
      return;
    }
  }
}

// Prefetch every lazy-loaded view module during idle time after the
// home view paints. The first-time navigation cost for each
// destination is the chunk fetch (~5-20 KB gzipped, ~50-200ms cold),
// and the operator will hit most of them during a typical session —
// so we warm the cache once on boot. Each `import()` returns a Promise
// the browser can park; we don't need the resolved modules here
// because dispatch will re-`import()` them later, but module-loading
// is idempotent so the second call hits the cache.
function prefetchOtherViews(): void {
  const work = () => {
    void import('./views/list');
    void import('./views/detail');
    void import('./views/memory');
    void import('./views/sessions');
    void import('./views/wiki_page');
    void import('./views/session_transcript');
    void import('./views/calendar');
    void import('./views/calendar_event');
    void import('./views/settings');
    void import('./views/model_select');
    void import('./views/categories');
    void import('./views/category_pages');
    void import('./views/diary');
    void import('./views/crons');
    void import('./views/people');
    void import('./views/person_detail');
    void import('./views/wiki_new');
    void import('./views/topic_new');
  };
  // requestIdleCallback isn't on Safari < 17 and isn't in lib.dom.d.ts
  // by default; fall back to a generous setTimeout so we still kick the
  // prefetch eventually on platforms that lack it.
  const ric = (window as Window & {
    requestIdleCallback?: (cb: () => void, opts?: { timeout?: number }) => number;
  }).requestIdleCallback;
  if (typeof ric === 'function') {
    ric(work, { timeout: 4000 });
  } else {
    setTimeout(work, 800);
  }
}

// Stash the previous route so the next transition can pick a slide
// direction. Home is the single index, so transitions are read as:
//   home → anywhere   = deep    (push down into a destination)
//   anywhere → home   = shallow (pop back to the index)
//   X → Y (non-home)  = forward (neutral lateral slide)
let lastRoute: Route | null = null;

function transitionDirection(from: Route | null, to: Route): 'forward' | 'deep' | 'shallow' {
  if (!from) return 'forward';
  if (isHomeRoute(from) && !isHomeRoute(to)) return 'deep';
  if (!isHomeRoute(from) && isHomeRoute(to)) return 'shallow';
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
    // The callback is async because dispatch now awaits a dynamic
    // import before it can run the view's render function. View
    // Transitions holds the old snapshot until the callback resolves,
    // so the snapshot of "new DOM" happens after the chunk fetch +
    // the render's synchronous paint phase have both completed —
    // which is exactly the state we want to animate into.
    startTransition.call(document, async () => {
      await dispatch(route);
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

  // Bot API 7.7+. Without this, dragging down inside the Mini App also
  // tugs Telegram's own swipe-to-minimize gesture, so pull-to-refresh
  // ends up shrinking the whole app instead of just nudging the page.
  // The optional chain handles older Telegram clients gracefully —
  // overscroll-behavior in CSS is the secondary fallback.
  tg.disableVerticalSwipes?.();

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
      case 'topicNew':
        navigate({ name: 'sessions' });
        return;
      // Every top-level drill-down off home pops back to home —
      // matches the path the user took to get in. No "more" hub
      // exists; home is the single index.
      case 'inbox':
      case 'memory':
      case 'sessions':
      case 'calendar':
      case 'categories':
      case 'diary':
      case 'crons':
      case 'people':
      case 'settings':
        navigate({ name: 'home' });
        return;
      default:
        navigate({ name: 'home' });
    }
  });

  window.addEventListener('hashchange', handleHashChange);

  void dispatch(parseRoute(location.hash));

  // Warm the chunk cache for every non-home view in the background.
  // The operator hits most destinations during a typical session, so
  // paying ~5-20 KB per chunk in idle time is cheaper than paying it
  // synchronously the first time they tap each menu row.
  prefetchOtherViews();
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
