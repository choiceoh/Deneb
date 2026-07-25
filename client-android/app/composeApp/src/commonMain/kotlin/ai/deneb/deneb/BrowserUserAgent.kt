package ai.deneb.deneb

/**
 * User-agent shaping for the in-app browser.
 *
 * Android's WebView advertises itself with a `; wv` token inside the platform
 * section of the UA (`... (Linux; Android 15; SM-S938N Build/AP3A.240905.015; wv)
 * AppleWebKit/...`). Sites that want you in their native app key off exactly that
 * token: Reddit answers a `wv` UA with its "open in app" gate, which pins an
 * interstitial over the article and locks page scrolling — the page renders but
 * will not move.
 *
 * We strip only that one token and keep everything else the device reported, so
 * the UA still names this device's real Android build and Chrome version. That
 * avoids inventing a UA string that silently goes stale as the WebView updates
 * (and avoids the version-mismatch fingerprint a hardcoded UA would create).
 */
internal fun browserUserAgent(defaultUserAgent: String): String {
    val ua = defaultUserAgent.trim()
    if (ua.isEmpty()) return ua
    // The token appears as "; wv)" at the end of the platform parenthetical, and
    // as "; wv;" when a vendor appends further fields after it.
    return ua
        .replace("; wv)", ")")
        .replace("; wv;", ";")
}
