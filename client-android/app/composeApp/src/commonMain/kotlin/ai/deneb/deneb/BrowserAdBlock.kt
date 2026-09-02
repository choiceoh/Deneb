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
    // --- 2026-09-03 leak sweep (mobile UA, real loads of topwar/donga/hankyung/
    // news.naver; each host below was observed serving ads or telemetry and
    // passing every existing rule). Volume in that sweep is in parentheses.
    "analytics.google.com", // GA4 collect — google-analytics.com was covered, this twin was not
    "fundingchoicesmessages.google.com", // (23) Google Funding Choices — anti-adblock / consent nag
    "imasdk.googleapis.com", // (4) IMA outstream video ads; the page's own video still plays
    "360yield.com", // Improve Digital SSP
    "sparteo.com",
    "anymind360.com", // (2) prebid bundle
    "ladsp.com", // (2) cookie sync
    "adsappier.com", // (3) Appier
    "gliastudios.com", // (4) Glia video ads
    "gliacloud.com", // (6) same stack, player host
    "ad4989.co.kr", // (2) KR display net
    "ad-stir.com", // JP display net
    "mediacategory.com",
    "dable.io", // KR native recommendation
    "mobon.net", // (6) KR retargeting
    // Naver's tag manager / logger. Only this host — pstatic.net also serves the
    // article images (mimgnews, ssl, static-nnews), which must keep loading.
    "ntm.pstatic.net",
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
    // Google's measurement endpoints live on www.google.com, which also serves
    // search, reCAPTCHA and embeds — so these are matched by PATH, never by
    // host (the same reasoning as connect.facebook.net/signals/ below).
    // 215 such requests passed on one hankyung.com load.
    "/ccm/collect",
    "/measurement/conversion",
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

internal fun isBrowserAdHost(host: String): Boolean {
    if (matchesAdHostSuffix(host, BROWSER_AD_HOST_SUFFIXES)) return true
    val remote = BrowserRuleRegistry.current().adHostSuffixes
    return remote.isNotEmpty() && matchesAdHostSuffix(host, remote)
}

/**
 * `host == s || host.endsWith(".$s")` for any s in [suffixes] — the same
 * relation as before, walked from the host side instead of the list side.
 *
 * This runs inside shouldInterceptRequest, once per subresource. Written as
 * `suffixes.any { host.endsWith(".$it") }` it built a fresh `".$it"` String for
 * every one of the 139 compiled-in suffixes on every request that was NOT
 * blocked — the common case, first-party content — so a Korean news page with
 * ~300 subresources produced ~42,000 short-lived strings per load, and the GC
 * pressure lands on a UI thread that is laying the page out.
 *
 * A hostname has 2-4 label boundaries, and a suffix can only match at one of
 * them, so checking those few positions against the set is both allocation-free
 * per entry and O(labels) instead of O(suffixes).
 */
private fun matchesAdHostSuffix(host: String, suffixes: Collection<String>): Boolean {
    if (suffixes.contains(host)) return true
    var dot = host.indexOf('.')
    while (dot >= 0 && dot + 1 < host.length) {
        if (suffixes.contains(host.substring(dot + 1))) return true
        dot = host.indexOf('.', dot + 1)
    }
    return false
}

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
