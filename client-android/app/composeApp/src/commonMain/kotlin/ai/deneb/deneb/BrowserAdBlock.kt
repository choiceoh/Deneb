package ai.deneb.deneb

/**
 * Lightweight network ad/tracker blocking for the in-app translation browser.
 * Host-suffix + path heuristics only — no EasyList parser, no cosmetic CSS
 * (keeps the translate DOM walker undisturbed).
 */

/** Host suffixes that are almost always ad/tracker delivery (not first-party content). */
internal val BROWSER_AD_HOST_SUFFIXES: Set<String> = setOf(
    "doubleclick.net",
    "googlesyndication.com",
    "googleadservices.com",
    "googletagservices.com",
    "pagead2.googlesyndication.com",
    "adservice.google.com",
    "adservice.google.co.kr",
    "amazon-adsystem.com",
    "ads-twitter.com",
    "ads.yahoo.com",
    "advertising.com",
    "adnxs.com",
    "adsafeprotected.com",
    "adform.net",
    "adcolony.com",
    "adsrvr.org",
    "adtrafficquality.google",
    "taboola.com",
    "outbrain.com",
    "criteo.com",
    "criteo.net",
    "casalemedia.com",
    "pubmatic.com",
    "rubiconproject.com",
    "openx.net",
    "moatads.com",
    "scorecardresearch.com",
    "quantserve.com",
    "2mdn.net",
    "media.net",
    "mgid.com",
    "revcontent.com",
    "smartadserver.com",
    "serving-sys.com",
    "yieldmo.com",
    "bidswitch.net",
    "contextweb.com",
    "exelator.com",
    "bluekai.com",
    "rlcdn.com",
    "liadm.com",
    "3lift.com",
    "sharethrough.com",
    "sovrn.com",
    "indexww.com",
    "spotxchange.com",
    "teads.tv",
    "inmobi.com",
    "unityads.unity3d.com",
    "applovin.com",
    "mopub.com",
    "chartbeat.com",
    "hotjar.com",
    "mouseflow.com",
    "fullstory.com",
    "clarity.ms",
    "branch.io",
    "appsflyer.com",
    "adjust.com",
    "kochava.com",
    "sentry-cdn.com",
)

/** Path / query fragments that usually mark ad creatives or trackers. */
private val BROWSER_AD_PATH_MARKERS: List<String> = listOf(
    "/pagead/",
    "/pagead2/",
    "/adsense/",
    "/ads/",
    "/adserver/",
    "/ad-delivery/",
    "/adx/",
    "googleads.",
    "googlesyndication",
    "doubleclick",
    "/pixel.",
    "/pixels/",
    "/beacon/",
    "adsystem",
)

/**
 * Returns true when [url] should be dropped by [android.webkit.WebViewClient.shouldInterceptRequest].
 * Main-frame navigations are never blocked.
 */
internal fun shouldBlockBrowserAdRequest(url: String, isForMainFrame: Boolean = false): Boolean {
    if (isForMainFrame) return false
    val raw = url.trim()
    if (raw.isEmpty()) return false
    val lower = raw.lowercase()
    // data:/blob:/about: never ads
    if (lower.startsWith("data:") || lower.startsWith("blob:") || lower.startsWith("about:")) return false

    val host = browserRequestHost(lower) ?: return false
    if (BROWSER_AD_HOST_SUFFIXES.any { host == it || host.endsWith(".$it") }) return true

    // Path heuristics — only on known ad-ish hosts or clear /ads/ paths on any host.
    val pathAndQuery = lower.substringAfter("://", missingDelimiterValue = lower).substringAfter('/', missingDelimiterValue = "")
    val marked = "/$pathAndQuery"
    if (BROWSER_AD_PATH_MARKERS.any { marked.contains(it) }) {
        // Avoid nuking first-party content paths like "/ads/" on obscure sites? Still usually ads.
        // Skip when the marker is only inside a non-ad host's document path that looks like content
        // (e.g. example.com/blog/ads-policy) — require slash-bounded "/ads/" or ad host already matched.
        if (marked.contains("/pagead/") ||
            marked.contains("/pagead2/") ||
            marked.contains("/adsense/") ||
            marked.contains("/adserver/") ||
            marked.contains("/ad-delivery/") ||
            marked.contains("googlesyndication") ||
            marked.contains("doubleclick") ||
            marked.contains("adsystem") ||
            marked.contains("/ads/") ||
            marked.contains("/adx/")
        ) {
            return true
        }
    }
    return false
}

internal fun browserRequestHost(urlLower: String): String? {
    val afterScheme = when {
        urlLower.startsWith("https://") -> urlLower.removePrefix("https://")
        urlLower.startsWith("http://") -> urlLower.removePrefix("http://")
        else -> return null
    }
    val authority = afterScheme.substringBefore('/').substringBefore('?').substringBefore('#')
    if (authority.isBlank()) return null
    // strip userinfo and port
    val hostPort = authority.substringAfter('@')
    return hostPort.substringBefore(':').trim('.').takeIf { it.isNotBlank() }
}
