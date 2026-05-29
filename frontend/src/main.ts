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

// applySafeArea writes the Telegram-reported content safe-area top inset
// into `--tg-safe-top` on <html>. The styles.css `#app` padding adds this
// value to its base 20px so the page content clears the device status
// bar/notch *plus* the floating × / 네브 / ⋮ pills Telegram overlays in
// fullscreen mode (Bot API 8.0+). Outside fullscreen the inset is 0 and
// the base 20px takes over; on older clients without the API the CSS
// fallback `env(safe-area-inset-top, 0px)` covers iOS Safari and modern
// Telegram WebView, and the worst case is just 20px padding (no clip).
//
// We re-apply on `contentSafeAreaChanged` so a mid-session rotation or a
// late fullscreen request (the requestFullscreen() call below races the
// event) settles onto the right value without a reload.
function applySafeArea(tg: WebApp): void {
  type Inset = { top?: number };
  type WebAppExt = {
    contentSafeAreaInset?: Inset;
    safeAreaInset?: Inset;
    onEvent?: (event: string, cb: () => void) => void;
  };
  const ext = tg as unknown as WebAppExt;
  const write = (): void => {
    const top =
      ext.contentSafeAreaInset?.top ??
      ext.safeAreaInset?.top ??
      0;
    document.documentElement.style.setProperty('--tg-safe-top', `${top}px`);
  };
  write();
  ext.onEvent?.('contentSafeAreaChanged', write);
  ext.onEvent?.('safeAreaChanged', write);
}

// stampPlatformClass mirrors tg.platform onto body so CSS + JS can
// branch on it. The 'tg-desktop' / 'tg-mobile' grouping is the one
// most rules actually want — viewport scaling, hover, keyboard. The
// raw 'tg-platform-<x>' class is kept around for any future
// platform-specific edge case (e.g. macOS-only quirks). Returns
// isDesktop so boot() can decide whether to request fullscreen.
function stampPlatformClass(tg: WebApp): boolean {
  const platform = (tg.platform ?? 'unknown').toLowerCase();
  const desktopPlatforms = new Set(['tdesktop', 'macos', 'web', 'weba', 'webk']);
  const isDesktop = desktopPlatforms.has(platform);
  document.body.classList.toggle('tg-desktop', isDesktop);
  document.body.classList.toggle('tg-mobile', !isDesktop);
  document.body.dataset.tgPlatform = platform;
  return isDesktop;
}

// Keyboard navigation for Telegram-on-desktop. Bindings:
//   j / ArrowDown : focus next .type-item or .email-row
//   k / ArrowUp   : focus previous
//   Enter         : activate the focused element (default browser
//                   behavior; we no-op so we don't double-fire)
//   Esc           : back — pops to home (or the back stack via
//                   the BackButton handler that's already wired).
//   /             : focus the global search input if we're on
//                   search; navigate to search otherwise.
//
// We intentionally don't intercept anything when the focus is in a
// real text input — the keys above need to type literally there.
function handleKeyboardNav(ev: KeyboardEvent): void {
  if (!document.body.classList.contains('tg-desktop')) return;
  if (ev.metaKey || ev.ctrlKey || ev.altKey) return;

  const target = ev.target as HTMLElement | null;
  const inTextInput =
    target?.tagName === 'INPUT' || target?.tagName === 'TEXTAREA' || target?.isContentEditable;
  if (inTextInput && ev.key !== 'Escape') return;

  switch (ev.key) {
    case 'j':
    case 'ArrowDown':
      moveFocus(1);
      ev.preventDefault();
      return;
    case 'k':
    case 'ArrowUp':
      moveFocus(-1);
      ev.preventDefault();
      return;
    case 'Escape': {
      // From any drill-down or tab view, escape pops to home. The
      // BackButton handler installed during boot() handles the
      // smarter "pop to the parent list" path; this is a simpler
      // straight-shot for keyboard users who just want out.
      const route = parseRoute(location.hash);
      if (route.name !== 'home') {
        navigate({ name: 'home' });
        ev.preventDefault();
      }
      return;
    }
    case '/': {
      // / drops focus into the search input if we're already there,
      // otherwise navigates to the search view.
      const route = parseRoute(location.hash);
      if (route.name === 'search') {
        const input = document.querySelector('input[type="search"], .search-form input');
        (input as HTMLInputElement | null)?.focus();
      } else {
        navigate({ name: 'search' });
      }
      ev.preventDefault();
      return;
    }
    default:
      return;
  }
}

// moveFocus walks the page's set of focusable rows (menu items on
// home; email rows on mail; flat-rows in settings; etc.) and shifts
// focus by `delta`. Wraps around at either end so the operator can
// just keep pressing j to spin the cursor through the list.
function moveFocus(delta: number): void {
  const candidates = Array.from(
    document.querySelectorAll<HTMLElement>(
      '.type-item, .email-row, .session-row, .flat-row-nav, .panorama-tab',
    ),
  ).filter((el) => !el.hasAttribute('disabled') && el.offsetParent !== null);
  if (candidates.length === 0) return;
  const current = document.activeElement as HTMLElement | null;
  const idx = current ? candidates.indexOf(current) : -1;
  let next = idx + delta;
  if (next < 0) next = candidates.length - 1;
  if (next >= candidates.length) next = 0;
  candidates[next]?.focus();
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
//
// Failed dynamic imports are cached permanently by the browser's
// module map — once `import('./views/foo')` rejects, every subsequent
// call returns the same rejected promise without re-fetching. To
// avoid a single transient network blip permanently poisoning a
// route, we render an inline error banner on import failure so the
// operator gets visible feedback (instead of "the tap did nothing")
// and can refresh to retry.
async function dispatch(route: Route): Promise<void> {
  if (!cachedInitData) return;
  syncBackButton(route);
  // Clear any pull-to-refresh handler from the previous view; views
  // that want to opt in re-register their own handler after rendering.
  clearPullToRefreshHandler();
  const initData = cachedInitData;
  try {
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
      case 'search': {
        const { renderSearch } = await import('./views/search');
        renderSearch(root, initData);
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
      case 'crons': {
        const { renderCrons } = await import('./views/crons');
        void renderCrons(root, initData);
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
  } catch (err) {
    // Most likely: the dynamic import for this view's chunk failed
    // (network blip during prefetch, server hiccup, asset hash drift
    // after a deploy mid-session). Without this handler the awaited
    // chunk-load rejection would propagate up to the View Transitions
    // callback, which silently aborts — leaving the operator on the
    // previous view with no visible feedback that the tap landed.
    // Paint a visible error so they can refresh and retry.
    console.error('dispatch failed for route', route.name, err);
    root.innerHTML = '';
    const banner = document.createElement('div');
    banner.className = 'error';
    banner.textContent = `화면을 불러오지 못했습니다 (${route.name}). 미니앱을 다시 열어주세요.`;
    root.appendChild(banner);
    const hint = document.createElement('div');
    hint.className = 'muted';
    hint.textContent = String((err as Error)?.message ?? err);
    root.appendChild(hint);
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
  const isDesktop = stampPlatformClass(tg);
  applySafeArea(tg);

  // Bot API 8.0+: on mobile go fullscreen so Telegram's native title
  // bar ("네브", ⋮, ×) is hidden, reclaiming vertical real estate.
  // No-op on older clients. applySafeArea above already registered the
  // contentSafeAreaChanged listener, so the post-fullscreen inset
  // update lands without a second call.
  //
  // On desktop we deliberately skip this: Telegram opens Mini Apps in a
  // medium windowed panel (app header + close ×) by default, which is
  // what we want on PC. Requesting fullscreen there blows the panel up
  // to cover the whole client; staying windowed keeps it usable
  // alongside the rest of Telegram. Outside fullscreen the safe-area
  // inset is 0 and the base #app padding takes over (see applySafeArea).
  if (!isDesktop) {
    (tg as unknown as { requestFullscreen?: () => void }).requestFullscreen?.();
  }

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
        navigate({ name: 'search' });
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
        navigate({ name: 'search' });
        return;
      case 'modelSelect':
        navigate({ name: 'settings' });
        return;
      case 'wikiNew':
        // Pop back to wherever the user came from — search results or
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
      case 'search':
      case 'sessions':
      case 'calendar':
      case 'categories':
      case 'crons':
      case 'settings':
        navigate({ name: 'home' });
        return;
      default:
        navigate({ name: 'home' });
    }
  });

  window.addEventListener('hashchange', handleHashChange);

  // Keyboard navigation (desktop-class platforms). Bound at the window
  // level so it works across page navigations without re-attaching per
  // view. We gate on body.tg-desktop so phones with an attached BT
  // keyboard still behave like phones — the cadence there is touch-first.
  window.addEventListener('keydown', handleKeyboardNav);

  void dispatch(parseRoute(location.hash));

  // Note: an idle-time prefetchOtherViews() used to fire dynamic
  // imports for every non-home view chunk on boot, to warm the
  // browser's module cache. It was removed because the browser
  // permanently caches FAILED dynamic imports in its module map:
  // any chunk that lost a coin-flip against a network blip during
  // prefetch would return the same rejected promise forever, and
  // the user's later tap would land on that rejected promise
  // instead of triggering a fresh fetch — manifesting as "every
  // menu item taps but nothing happens". Each chunk is small (a
  // few KB gzipped) and only fetched the first time the operator
  // navigates into it; the latency cost on cold first-tap is
  // smaller than the regression risk of a poisoned module map.
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
