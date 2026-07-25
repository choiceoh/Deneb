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
 * Undoes the document-level scroll lock. Re-applied on DOM mutation because the
 * SPA re-imposes it on navigation and when its interstitial reopens; the
 * observer is cheap (it only touches two elements) and installs once per page.
 *
 * Deliberately narrow: it clears the lock on `html`/`body` only. It does not
 * remove overlays or click anything — an automated "dismiss" would fight the
 * site's own UI and break as soon as their markup changes.
 */
private const val REDDIT_SCROLL_UNLOCK = """
(function () {
  if (window.__denebScrollUnlock) return;
  window.__denebScrollUnlock = true;
  var LOCKED = { overflow: 'hidden', overflowY: 'hidden', position: 'fixed' };
  function unlock(el) {
    if (!el || !el.style) return;
    var s = window.getComputedStyle(el);
    if (s.overflow === LOCKED.overflow || s.overflowY === LOCKED.overflowY) {
      el.style.setProperty('overflow', 'auto', 'important');
      el.style.setProperty('overflow-y', 'auto', 'important');
    }
    if (s.position === LOCKED.position) {
      el.style.setProperty('position', 'static', 'important');
      el.style.setProperty('top', 'auto', 'important');
    }
    if (s.height === '100%' && s.overflow === LOCKED.overflow) {
      el.style.setProperty('height', 'auto', 'important');
    }
  }
  function pass() {
    unlock(document.documentElement);
    unlock(document.body);
  }
  pass();
  try {
    var pending = false;
    new MutationObserver(function () {
      if (pending) return;
      pending = true;
      requestAnimationFrame(function () { pending = false; pass(); });
    }).observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['style', 'class'],
      subtree: true,
    });
  } catch (e) { /* observer is best-effort */ }
})();
"""
