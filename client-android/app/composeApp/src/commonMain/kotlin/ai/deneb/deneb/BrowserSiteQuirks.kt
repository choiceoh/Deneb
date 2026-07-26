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
 * A second capture (2026-07-26, ⋮ → 스크롤 진단 on a 20,127px thread) named the
 * thing doing the pinning, after the first fix left a residue the operator
 * described as "화면이 어두워지고 댓글 더보기 누르기가 안되네":
 *
 *     column: y=34 … y=649  ->  div.rpl-bottom-sheet configured-xpromo
 *                               text "424만 평점"
 *     overlays: []          quirk: applied TRUE, bodyInline ""
 *     body: overflow-y HIDDEN
 *
 * Every probe point down the screen hits the same element, so Reddit's
 * app-install interstitial covers the entire viewport and swallows every tap —
 * that is the darkening, the dead button, and the dead scroll, all one cause.
 * It is not `position:fixed`, which is why an overlay scan reported nothing.
 *
 * The capture also convicted the first fix: our unlock was applied and body was
 * STILL computed `overflow-y: hidden`. Our `<style>` is appended last, so an
 * equal-specificity rule would lose to it; something with a higher specificity
 * and `!important` is winning. Hence `:root:root:root` — repeating a pseudo
 * class raises specificity without changing what is matched, so the unlock
 * outranks the interstitial's class-based lock.
 *
 * Suppression is scoped to `.configured-xpromo`: Reddit uses `rpl-bottom-sheet`
 * for legitimate sheets (share, sort) that must keep working.
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
  var css = ':root:root:root,:root:root:root body{overflow:visible !important;overflow-y:visible !important}'
          + '.rpl-bottom-sheet.configured-xpromo{display:none !important}';
  var st = document.createElement('style');
  st.id = ID;
  st.appendChild(document.createTextNode(css));
  (document.head || document.documentElement).appendChild(st);
})();
"""
