package ai.deneb.deneb

/**
 * Navigation and download decisions for the in-app browser.
 *
 * A WebView only knows how to render http(s). Everything else — `intent://`,
 * `market://`, `tel:`, `mailto:`, `kakaotalk://`, bank auth schemes — has to be
 * handed to the OS, and a WebView with no `shouldOverrideUrlLoading` simply
 * fails the navigation: the tap does nothing at all. Korean sites lean on these
 * schemes for login, payment, and certificate auth, so the dead taps are common.
 *
 * These helpers are pure so the decision is unit-testable; the Android actual
 * owns only the Intent/DownloadManager plumbing.
 */

/** Schemes the WebView renders itself. Anything else belongs to the OS. */
private val IN_PAGE_SCHEMES = setOf("http", "https", "about", "data", "blob", "javascript")

internal enum class BrowserPopupRoute {
    NEW_TAB,
    SAME_TAB,
    EXTERNAL,
    IGNORE,
}

private fun Char.isAsciiAlpha(): Boolean = this in 'a'..'z' || this in 'A'..'Z'

/** Returns the lowercase scheme of [url], or "" when it has none. */

/**
 * Whether a URL may become the address the chrome shows and acts on.
 *
 * `onPageStarted`/`onPageFinished` also fire for navigations the app itself
 * refused — an aborted `tel:`, `intent://` or `kakaotalk://` link arrives with
 * that URL as the "finished" one. Committing it put an un-openable address in the
 * omnibox, and 'URL 복사' / '외부 브라우저로 열기' then acted on it. Only real web
 * pages (and the blob:/data: documents a page can produce) belong there.
 */
internal fun canShowAsAddress(url: String): Boolean = canBookmarkUrl(url) || urlScheme(url) == "blob" || urlScheme(url) == "data"

internal fun urlScheme(url: String): String {
    val trimmed = url.trim()
    val colon = trimmed.indexOf(':')
    if (colon <= 0) return ""
    val scheme = trimmed.substring(0, colon)
    // RFC 3986: scheme = ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ), and it is
    // ASCII-only. Kotlin's isLetter()/isLetterOrDigit() are Unicode-aware, so a
    // Korean phrase containing a colon ("재무:검토") would otherwise parse as a
    // scheme and get flung at the OS as an external navigation.
    if (!scheme[0].isAsciiAlpha()) return ""
    if (!scheme.all { it.isAsciiAlpha() || it in '0'..'9' || it == '+' || it == '-' || it == '.' }) return ""
    return scheme.lowercase()
}

/**
 * True when [url] must be handed to the OS instead of loaded in the WebView.
 * Blank or scheme-less URLs stay in-page (relative navigation).
 */
internal fun isExternalSchemeUrl(url: String): Boolean {
    val scheme = urlScheme(url)
    return scheme.isNotEmpty() && scheme !in IN_PAGE_SCHEMES
}

/** Decides how a resolved target=_blank URL is adopted without dropping it. */
internal fun browserPopupRoute(url: String): BrowserPopupRoute {
    if (!browserAdoptPopupUrl(url)) return BrowserPopupRoute.IGNORE
    return when (urlScheme(url)) {
        "http", "https" -> BrowserPopupRoute.NEW_TAB
        "blob", "data" -> BrowserPopupRoute.SAME_TAB
        else -> if (isExternalSchemeUrl(url)) BrowserPopupRoute.EXTERNAL else BrowserPopupRoute.IGNORE
    }
}

/**
 * Pulls `S.browser_fallback_url` out of an `intent://` URL. Android defines it
 * as the http(s) page to load when no app handles the intent — without it, a
 * missing app leaves the user on a dead tap.
 *
 * Only http(s) fallbacks are honoured: an `intent://` whose fallback is another
 * custom scheme would otherwise bounce the WebView straight back here.
 */
internal fun intentFallbackUrl(url: String): String? {
    if (urlScheme(url) != "intent") return null
    val marker = "S.browser_fallback_url="
    val start = url.indexOf(marker)
    if (start < 0) return null
    val valueStart = start + marker.length
    val end = url.indexOf(';', valueStart).let { if (it < 0) url.length else it }
    val raw = url.substring(valueStart, end)
    if (raw.isBlank()) return null
    val decoded = percentDecode(raw)
    return decoded.takeIf { urlScheme(it) == "http" || urlScheme(it) == "https" }
}

/**
 * Percent-decoding for the fallback URL, over BYTES rather than chars.
 *
 * `%EA%B0%80` is one UTF-8 character, not three. Turning each `%XX` straight into
 * a Char decoded it as Latin-1, so a Korean fallback ("…/검색?q=…") came out as
 * mojibake, which loadUrl then re-encoded as UTF-8 of the wrong characters — a
 * 404 instead of the page.
 */
internal fun percentDecode(value: String): String {
    if (!value.contains('%')) return value
    val bytes = ArrayList<Byte>(value.length)
    var i = 0
    while (i < value.length) {
        val c = value[i]
        if (c == '%' && i + 2 < value.length) {
            val code = value.substring(i + 1, i + 3).toIntOrNull(16)
            if (code != null) {
                bytes.add(code.toByte())
                i += 3
                continue
            }
        }
        // Anything not percent-encoded is already text; re-encode it so the byte
        // stream stays one consistent UTF-8 sequence.
        for (b in c.toString().encodeToByteArray()) bytes.add(b)
        i++
    }
    return bytes.toByteArray().decodeToString()
}
