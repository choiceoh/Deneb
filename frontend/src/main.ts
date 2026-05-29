// main.ts — Deneb Mini App entry + router shell.
//
// All real rendering lives in views/*.ts. This file:
//   1. Boots Telegram WebApp SDK and applies its theme params.
//   2. Validates that initData is present (otherwise show a friendly
//      "open from Telegram" banner).
//   3. Routes the current hash to the right view module and listens for
//      hashchange to re-render on navigation.
//   4. Manages Telegram's BackButton so it mirrors browser history.

// Pretendard Variable as the single lead face: its Latin/numerals are
// Inter-based (so the English look the previous Inter setup gave us is
// preserved) while its Hangul is first-class and metrically matched to the
// Latin — no weight/baseline jump between English and Korean in a mixed
// line, which the old Inter + Apple SD Gothic / Noto fallback suffered. The
// dynamic-subset variant @font-faces one variable file per unicode-range,
// so the WebView fetches only the glyph ranges actually on screen.
import 'pretendard/dist/web/variable/pretendardvariable-dynamic-subset.css';
import './styles.css';
import { parseRoute, navigate, isHomeRoute, type Route } from './router';
// Home stays statically imported so the first-paint chunk includes
// everything needed to render the index. Every other view is split out
// via dynamic import in dispatch() — the operator only pays the chunk-
// fetch cost the first time they navigate into a given destination, and
// prefetchOtherViews() warms the cache during idle time after boot so
// that first visit usually finds the chunk already there.
import { renderHome } from './views/home';
import { clearPullToRefreshHandler } from './pull_to_refresh';
import {
  currentShellMode,
  desktopEscapeTarget,
  dispatchDesktop,
  isDesktopShellActive,
  teardownDesktopShell,
} from './desktop_shell';

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
    const top = ext.contentSafeAreaInset?.top ?? ext.safeAreaInset?.top;
    // When neither inset API is exposed (older Telegram WebViews), leave
    // --tg-safe-top *unset* so the styles.css `env(safe-area-inset-top)`
    // fallback can take over. Writing an explicit 0px here would win over
    // that fallback and clip notch content in fullscreen — the exact case
    // the env() fallback exists to cover.
    if (typeof top !== 'number') return;
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
      const route = parseRoute(location.hash);
      // On the desktop shell, Esc first collapses an open detail pane back
      // to its family's list (mail/calendar/topics) — a second Esc from the
      // bare list (or any non-home view) then returns to home. Mirrors the
      // BackButton's smarter "pop to parent" path for keyboard users.
      const collapse = desktopEscapeTarget(route);
      if (collapse) {
        navigate(collapse);
        ev.preventDefault();
      } else if (route.name !== 'home') {
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
      '.type-item, .email-row, .event-row, .session-row, .flat-row-nav, ' +
        '.panorama-tab, .app-sidebar-item',
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

// --- Stale-chunk recovery --------------------------------------------------
//
// Every non-home view is a separate content-hashed chunk fetched on first
// navigation. When the operator already has the app open (or Telegram's
// WebView cached the old entry bundle) and a redeploy lands, the bundle in
// memory still points at the *old* hashes — e.g. assets/calendar-BMaDBaGq.js
// — which the new server no longer has. The import() then rejects with
// "Failed to fetch dynamically imported module".
//
// The durable fix is to reload once: index.html is served no-store (see
// miniapp_static.go), so the reload pulls a fresh entry document referencing
// the new hashes, and the re-navigation fetches the chunk that actually
// exists. We stamp sessionStorage so a genuinely broken deploy (the chunk
// 404s even after the reload) can't trap us in a reload loop — after one
// attempt inside the window we fall through to the visible banner instead.
const CHUNK_RELOAD_STAMP = 'deneb:chunkReloadAt';
const CHUNK_RELOAD_WINDOW_MS = 30_000;

function isChunkLoadError(err: unknown): boolean {
  const msg = String((err as { message?: string } | null)?.message ?? err);
  return (
    msg.includes('Failed to fetch dynamically imported module') ||
    msg.includes('error loading dynamically imported module') ||
    msg.includes('Importing a module script failed')
  );
}

// reloadForStaleChunk attempts exactly one reload per incident. Returns true
// when it kicked off a reload (caller should bail out and not paint a
// banner), false when a reload was already attempted recently or
// sessionStorage is unavailable — in which case we can't guarantee
// loop-safety, so we prefer the manual banner over risking a reload loop.
function reloadForStaleChunk(): boolean {
  let lastAt: number;
  try {
    lastAt = Number(sessionStorage.getItem(CHUNK_RELOAD_STAMP) ?? '0');
  } catch {
    return false;
  }
  const now = Date.now();
  if (Number.isFinite(lastAt) && now - lastAt < CHUNK_RELOAD_WINDOW_MS) {
    return false;
  }
  try {
    sessionStorage.setItem(CHUNK_RELOAD_STAMP, String(now));
  } catch {
    return false;
  }
  location.reload();
  return true;
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

// dispatch routes the current hash to the right layout — the PC-native
// desktop shell (Telegram Desktop/web, wide enough) or the mobile
// single-column path — and both mount the actual view via renderViewInto.
async function dispatch(route: Route): Promise<void> {
  if (!cachedInitData) return;
  syncBackButton(route);
  // Clear any pull-to-refresh handler from the previous view; views
  // that want to opt in re-register their own handler after rendering.
  clearPullToRefreshHandler();
  const initData = cachedInitData;

  // Desktop (Telegram Desktop / web) at >= 720px gets the persistent
  // sidebar shell, and at >= 1000px master-detail panes. The shell
  // renders views into its content region / panes via renderViewInto, so
  // nothing in views/*.ts has to know which layout it's in. Everything
  // narrower — every phone, and a deliberately-narrowed desktop window —
  // falls through to the untouched mobile column below.
  if (isDesktopShellActive()) {
    await dispatchDesktop(root, route, initData, renderViewInto);
    return;
  }
  // Leaving the shell (the desktop window was dragged narrow) drops the
  // shell chrome first so the mobile column renders into a clean #app.
  teardownDesktopShell(root);
  await renderViewInto(root, route, initData);
}

// renderViewInto awaits the view's dynamic chunk fetch, then invokes its
// render function as fire-and-forget (`void renderX(...)`). Every view
// paints its loading-state DOM synchronously before its first await, so by
// the time render's promise yields the new DOM is already in `container`.
// `container` is #app on mobile, or a shell content region / detail pane on
// desktop — views only ever touch the container they're handed, never #app
// directly, which is what lets the same modules drive both layouts.
//
// Failed dynamic imports are cached permanently by the browser's module
// map, so a transient blip would otherwise poison a route forever — we
// render an inline error banner on import failure instead.
async function renderViewInto(
  container: HTMLElement,
  route: Route,
  initData: string,
): Promise<void> {
  try {
    switch (route.name) {
      case 'home':
        void renderHome(container, initData);
        return;
      case 'inbox': {
        const { renderList } = await import('./views/list');
        void renderList(container, initData);
        return;
      }
      case 'detail': {
        const { renderDetail } = await import('./views/detail');
        void renderDetail(container, initData, route.messageId);
        return;
      }
      case 'search': {
        const { renderSearch } = await import('./views/search');
        renderSearch(container, initData);
        return;
      }
      case 'sessions': {
        const { renderSessions } = await import('./views/sessions');
        void renderSessions(container, initData);
        return;
      }
      case 'wikiPage': {
        const { renderWikiPage } = await import('./views/wiki_page');
        void renderWikiPage(container, initData, route.path);
        return;
      }
      case 'sessionTranscript': {
        const { renderSessionTranscript } = await import('./views/session_transcript');
        void renderSessionTranscript(container, initData, route.sessionKey);
        return;
      }
      case 'calendar': {
        const { renderCalendar } = await import('./views/calendar');
        void renderCalendar(container, initData);
        return;
      }
      case 'calendarEvent': {
        const { renderCalendarEvent } = await import('./views/calendar_event');
        void renderCalendarEvent(container, initData, route.eventId);
        return;
      }
      case 'settings': {
        const { renderSettings } = await import('./views/settings');
        renderSettings(container, initData);
        return;
      }
      case 'modelSelect': {
        const { renderModelSelect } = await import('./views/model_select');
        renderModelSelect(container, initData, route.role);
        return;
      }
      case 'categories': {
        const { renderCategories } = await import('./views/categories');
        void renderCategories(container, initData);
        return;
      }
      case 'categoryPages': {
        const { renderCategoryPages } = await import('./views/category_pages');
        void renderCategoryPages(container, initData, route.category);
        return;
      }
      case 'crons': {
        const { renderCrons } = await import('./views/crons');
        void renderCrons(container, initData);
        return;
      }
      case 'personDetail': {
        const { renderPersonDetail } = await import('./views/person_detail');
        void renderPersonDetail(container, initData, route.email);
        return;
      }
      case 'wikiNew': {
        const { renderWikiNew } = await import('./views/wiki_new');
        renderWikiNew(container, initData, route.category ?? '');
        return;
      }
      case 'topicNew': {
        const { renderTopicNew } = await import('./views/topic_new');
        renderTopicNew(container, initData);
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
    console.error('dispatch failed for route', route.name, err);
    // Asset hash drift after a redeploy is the dominant cause: the chunk
    // filename this bundle points at no longer exists. Reload once to
    // pick up the fresh entry bundle (served no-store) before falling
    // back to a visible banner. reloadForStaleChunk guards against loops.
    if (isChunkLoadError(err) && reloadForStaleChunk()) {
      return;
    }
    // Paint a visible error so they can refresh and retry.
    container.innerHTML = '';
    const banner = document.createElement('div');
    banner.className = 'error';
    banner.textContent = `화면을 불러오지 못했습니다 (${route.name}). 미니앱을 다시 열어주세요.`;
    container.appendChild(banner);
    const hint = document.createElement('div');
    hint.className = 'muted';
    hint.textContent = String((err as Error)?.message ?? err);
    container.appendChild(hint);
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
  // Skip the full-page crossfade on the desktop shell: there, navigation
  // often changes only the detail pane (clicking a second mail), and a
  // root-level View Transition would flash the persistent sidebar + master
  // list too. Desktop gets instant pane swaps, which is the PC expectation.
  if (typeof startTransition === 'function' && !isDesktopShellActive()) {
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
    // requestFullscreen is Bot API 8.0+. On older mobile clients the SDK
    // can still expose the method but throws WebAppMethodUnsupported when
    // it's called — and this runs synchronously inside boot() before the
    // router mounts, so an unguarded throw would blank the Mini App for
    // users on 7.x clients that previously worked. Gate on the reported
    // version and still wrap in try/catch as a belt-and-suspenders guard.
    const fs = tg as unknown as {
      requestFullscreen?: () => void;
      isVersionAtLeast?: (version: string) => boolean;
    };
    if (fs.isVersionAtLeast?.('8.0')) {
      try {
        fs.requestFullscreen?.();
      } catch {
        // Client claimed >= 8.0 but still rejected fullscreen — keep booting.
      }
    }
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
        // Wiki pages are reached from many parents — mail detail, a
        // category page, a person card, search results, or another wiki
        // page via a related-page chip. A hardcoded parent (this used to
        // force 'search') was right for exactly one of those entry points
        // and wrong for the rest. Pop the real history entry so back
        // returns to wherever the user actually came from, matching the
        // wikiNew case below. The app always boots at home (the startapp
        // launch sets no hash), so a wiki page always has an in-app screen
        // behind it — history.back() never escapes the Mini App.
        history.back();
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

  // The desktop shell layout (mobile column / sidebar / sidebar+panes) is
  // chosen by viewport width. Re-dispatch when a resize crosses a
  // breakpoint so dragging the Telegram Desktop window narrow/wide
  // re-lays-out live instead of only on the next navigation. Debounced so
  // a drag doesn't thrash dispatch; a no-op when the mode is unchanged.
  let shellMode = currentShellMode();
  let resizeTimer: number | undefined;
  window.addEventListener('resize', () => {
    if (resizeTimer !== undefined) window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => {
      const mode = currentShellMode();
      if (mode === shellMode) return;
      shellMode = mode;
      void dispatch(parseRoute(location.hash));
    }, 150);
  });

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
