package ai.deneb.deneb

/**
 * Per-site compatibility quirks for the in-app browser.
 *
 * A quirk exists only when a site actively breaks in a WebView and the fix is
 * site-specific. Nothing here runs on a host that is not listed — a blanket
 * "force scrolling everywhere" rule would break every legitimate modal that
 * locks the background on purpose.
 *
 * Confirmed 2026-07-26 by operator test: `old.reddit.com` scrolls normally in
 * this browser while `www.reddit.com` renders the page and then refuses to
 * scroll. So the WebView is fine and the modern Reddit SPA is pinning the
 * document — the classic app-gate pattern where the page sets
 * `overflow:hidden` (and sometimes `position:fixed`) on html/body while its
 * "open in app" interstitial is up.
 */

/** Hosts that get the Reddit scroll-unlock quirk. */
private val REDDIT_HOSTS = setOf("reddit.com", "www.reddit.com", "np.reddit.com", "new.reddit.com", "sh.reddit.com")

/** Host of [url] in lowercase, without port; "" when it cannot be read. */
internal fun urlHost(url: String): String {
    val scheme = urlScheme(url)
    if (scheme != "http" && scheme != "https") return ""
    val afterScheme = url.substringAfter("://", "")
    if (afterScheme.isEmpty()) return ""
    val authority = afterScheme.substringBefore('/').substringBefore('?').substringBefore('#')
    // Drop userinfo and port.
    return authority.substringAfterLast('@').substringBefore(':').lowercase()
}

internal fun isRedditHost(url: String): Boolean = urlHost(url) in REDDIT_HOSTS

/**
 * JavaScript to run after page load for [url], or null when the site needs no
 * quirk. Kept as a pure function so the host matching is unit-tested; the
 * Android side only decides *when* to evaluate it.
 */
internal fun browserSiteQuirkScript(url: String): String? = if (isRedditHost(url)) REDDIT_SCROLL_UNLOCK else null

/**
 * Restores viewport scrolling.
 *
 * Measured on-device 2026-07-26 (⋮ → 스크롤 진단), on a thread that would not
 * scroll:
 *
 *     html: overflow visible            body: overflow-y HIDDEN
 *     scrollingElement: html            contentH 4675 / viewportH 683
 *     overlays: []                      touchmove/wheel handlers: none
 *     move: scrollTop 407 -> 607        moved: TRUE
 *
 * That is viewport overflow propagation: when the root element's computed
 * overflow is `visible`, the UA takes the viewport's overflow from `body`
 * instead. Reddit sets `body { overflow-y: hidden }`, so the VIEWPORT becomes
 * unscrollable — the finger does nothing while `scrollTop` still moves the
 * document programmatically, exactly what the probe recorded.
 *
 * Injected as an `!important` author stylesheet rather than inline styles,
 * because the first attempt (inline style + MutationObserver) lost twice:
 * its re-entry guard skipped re-application on SPA soft-nav, and the observer
 * watched only `style`/`class` attributes, so a lock re-imposed through a
 * stylesheet was invisible to it. An `!important` author rule outranks the
 * page's normal declarations, survives re-renders, and needs no observer.
 *
 * `visible` (not `auto`) is deliberate: it restores the default so the viewport
 * scrolls, without turning `body` into its own scroll container.
 */
private const val REDDIT_SCROLL_UNLOCK = """
(function () {
  var ID = '__deneb-scroll-unlock';
  if (document.getElementById(ID)) return;
  var css = 'html,body{overflow:visible !important;overflow-y:visible !important}';
  var st = document.createElement('style');
  st.id = ID;
  st.appendChild(document.createTextNode(css));
  (document.head || document.documentElement).appendChild(st);
})();
"""
