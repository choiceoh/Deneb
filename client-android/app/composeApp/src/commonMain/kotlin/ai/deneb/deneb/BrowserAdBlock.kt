package ai.deneb.deneb

/**
 * Lightweight network ad/tracker blocking for the in-app translation browser.
 * Host-suffix + path/query heuristics only — no EasyList parser, no cosmetic CSS
 * (keeps the translate DOM walker undisturbed).
 */

/** Host suffixes that are almost always ad/tracker delivery (not first-party content). */
internal val BROWSER_AD_HOST_SUFFIXES: Set<String> = setOf(
    // Google / DoubleClick
    "doubleclick.net",
    "googlesyndication.com",
    "googleadservices.com",
    "googletagservices.com",
    "adservice.google.com",
    "adservice.google.co.kr",
    "adtrafficquality.google",
    "2mdn.net",
    // Amazon / social ads
    "amazon-adsystem.com",
    "ads-twitter.com",
    "ads.linkedin.com",
    "ads.yahoo.com",
    "advertising.com",
    "pixel.facebook.com",
    "an.facebook.com",
    // Exchanges / SSPs
    "adnxs.com",
    "adsafeprotected.com",
    "adform.net",
    "adcolony.com",
    "adsrvr.org",
    "casalemedia.com",
    "pubmatic.com",
    "rubiconproject.com",
    "openx.net",
    "moatads.com",
    "media.net",
    "smartadserver.com",
    "serving-sys.com",
    "yieldmo.com",
    "bidswitch.net",
    "contextweb.com",
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
    "creativecdn.com",
    "targetingsource.com",
    // Content recommendation / native ads
    "taboola.com",
    "outbrain.com",
    "criteo.com",
    "criteo.net",
    "mgid.com",
    "revcontent.com",
    // Analytics / session replay (noise for reading)
    "scorecardresearch.com",
    "quantserve.com",
    "chartbeat.com",
    "hotjar.com",
    "mouseflow.com",
    "fullstory.com",
    "clarity.ms",
    "google-analytics.com",
    "googletagmanager.com",
    // Mobile attribution
    "branch.io",
    "appsflyer.com",
    "adjust.com",
    "kochava.com",
    // Korea / APAC common ad nets (news & commerce)
    "ad.daum.net",
    "display.ad.daum.net",
    "analytics.ad.daum.net",
    "ad.kakao.com",
    "ads.kakao.com",
    "displayad.kakao.com",
    "ad.naver.com",
    "adn.naver.com",
    "siape.veta.naver.com",
    "veta.naver.com",
    "realclick.co.kr",
    "adison.co",
    "adison.biz",
    "ad.about.co.kr",
    "ads-partners.coupang.com",
    "linkprice.com",
    "ad.mail.ru",
    "yandexadexchange.net",
    "ads.yahoo.co.jp",
    // Yandex RTB / AdFox / RU counters — topwar.ru, topcor.ru, and similar RU news
    "an.yandex.ru",
    "adfox.ru",
    "ads.adfox.ru",
    "adfox.yandex.ru",
    "matchid.adfox.yandex.ru",
    "awaps.yandex.ru",
    "adfstat.yandex.ru",
    "adsdk.yandex.ru",
    "mc.yandex.ru",
    "metrika.yandex.ru",
    "informer.yandex.ru",
    "adriver.ru",
    "content.adriver.ru",
    "ad.adriver.ru",
    "counter.yadro.ru",
    "top-fwz1.mail.ru",
    "rs.mail.ru",
    "relap.io",
    "relap.mail.ru",
    // Forumotion (russiadefence.net) — Taboola/Criteo already covered; these are the rest
    "viously.com",
    "cdn.viously.com",
    "smilewanted.com",
    "consentframework.com",
    "cache.consentframework.com",
    "choices.consentframework.com",
    // Substack publisher pixels / common newsletter trackers.
    // NOT connect.facebook.net as a whole host: it serves the Facebook Login SDK
    // (sdk.js) alongside the pixel, and blocking the host broke every
    // "Facebook으로 로그인" button. The pixel is matched by path below instead.
    "analytics.twitter.com",
    "static.ads-twitter.com",
    "parsely.com",
    "cdn.parsely.com",
    "p1.parsely.com",
    "amplitude.com",
    "cdn.amplitude.com",
    "api.amplitude.com",
    "api2.amplitude.com",
    "cdn.segment.com",
    "api.segment.io",
    "cdn.segment.io",
    // WordPress Jetpack stats (eurasiantimes.com Newspaper theme, etc.)
    "stats.wp.com",
    "pixel.wp.com",
    // Video / native stacks listed in eurasiantimes.com ads.txt
    "vdo.ai",
    "atlas5.co",
)

/** Slash-bounded path segments that mark ad creatives / delivery.
 *  Avoid bare "/ad/" — it false-positives on "/admin/". */
private val BROWSER_AD_PATH_SEGMENTS: List<String> = listOf(
    "/pagead/",
    "/pagead2/",
    "/adsense/",
    "/adserver/",
    "/ad-delivery/",
    "/adx/",
    "/gampad/",
    "/adsbygoogle",
    "/ads/",
    "/ads/system/",
    "/partner-code-bundles/",
    "/adfox",
    "/metrika/",
    "/libtrc/",
    "/prebid/",
)

/** Host-ish tokens that appear inside ad CDN URLs. */
private val BROWSER_AD_PATH_HOSTISH: List<String> = listOf(
    "googlesyndication",
    "doubleclick",
    "googleads.",
    "adsystem",
    "amazon-adsystem",
    "yandex.ru/ads",
    "yandex.ru/an/",
    "adfox",
    "adriver",
    "taboola.com/libtrc",
    "viously.com",
    "facebook.com/tr",
    // Path-scoped so sdk.js (Facebook Login) still loads from the same host.
    // Matched against the lowercased URL, so no upper-case locale segments here.
    "connect.facebook.net/signals/",
    "fbevents.js",
    "wprp.sovrn.com",
)

/** Query keys that almost always mean ad or tracking pixels. */
private val BROWSER_AD_QUERY_MARKERS: List<String> = listOf(
    "google_ads",
    "google_ad_",
    "ad_slot=",
    "adunit=",
    "adunitid=",
    "ad_type=",
    "adsense",
    "gampad",
    "pagead",
    "fb_pixel",
    "fbevents",
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
    if (lower.startsWith("data:") || lower.startsWith("blob:") || lower.startsWith("about:")) return false

    val host = browserRequestHost(lower) ?: return false
    if (isBrowserAdHost(host)) return true

    // Remote rules (miniapp.browser.config.get) ADD entries to the compiled-in
    // lists — an empty registry reads as two empty lists and changes nothing.
    val remote = BrowserRuleRegistry.current()

    val pathAndQuery = lower.substringAfter("://", missingDelimiterValue = lower).substringAfter('/', missingDelimiterValue = "")
    val marked = "/$pathAndQuery"

    if (BROWSER_AD_PATH_SEGMENTS.any { marked.contains(it) } || remote.adPathSegments.any { marked.contains(it) }) return true
    // Domain-ish tokens (e.g. facebook.com/tr) need the full URL — path-only
    // marked drops the hostname after the first slash of the authority.
    if (BROWSER_AD_PATH_HOSTISH.any { token ->
            marked.contains(token) || (token.contains('.') && lower.contains(token))
        } || remote.adPathTokens.any { token ->
            marked.contains(token) || (token.contains('.') && lower.contains(token))
        }
    ) {
        return true
    }
    if (marked.contains("/pixel.") || marked.contains("/pixels/") || marked.contains("/beacon/")) return true

    val query = marked.substringAfter('?', missingDelimiterValue = "")
    if (query.isNotEmpty() && BROWSER_AD_QUERY_MARKERS.any { query.contains(it) }) return true
    if (query.isNotEmpty() && remote.adQueryMarkers.any { query.contains(it) }) return true
    return false
}

internal fun isBrowserAdHost(host: String): Boolean = BROWSER_AD_HOST_SUFFIXES.any { host == it || host.endsWith(".$it") } ||
    BrowserRuleRegistry.current().adHostSuffixes.any { host == it || host.endsWith(".$it") }

/**
 * MIME hint for the empty [android.webkit.WebResourceResponse] so script/CSS
 * intercepts don't trip type mismatches as often as text/plain.
 */
internal fun browserBlockedResponseMime(url: String): String {
    val path = url.lowercase().substringBefore('?').substringBefore('#')
    return when {
        path.endsWith(".js") || path.endsWith(".mjs") -> "application/javascript"
        path.endsWith(".css") -> "text/css"
        path.endsWith(".json") -> "application/json"
        path.endsWith(".svg") -> "image/svg+xml"
        path.endsWith(".png") -> "image/png"
        path.endsWith(".gif") -> "image/gif"
        path.endsWith(".jpg") || path.endsWith(".jpeg") || path.endsWith(".webp") -> "image/jpeg"
        path.endsWith(".woff") || path.endsWith(".woff2") || path.endsWith(".ttf") -> "application/octet-stream"
        else -> "text/plain"
    }
}

internal fun browserRequestHost(urlLower: String): String? {
    val afterScheme = when {
        urlLower.startsWith("https://") -> urlLower.removePrefix("https://")
        urlLower.startsWith("http://") -> urlLower.removePrefix("http://")
        else -> return null
    }
    val authority = afterScheme.substringBefore('/').substringBefore('?').substringBefore('#')
    if (authority.isBlank()) return null
    val hostPort = authority.substringAfter('@')
    return hostPort.substringBefore(':').trim('.').takeIf { it.isNotBlank() }
}
