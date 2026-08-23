package ai.deneb.deneb

/**
 * Load-failure and JS-dialog models for the in-app browser.
 *
 * A WebView with no `onReceivedError` reports nothing when a page fails: the
 * user gets a blank screen and no reason. Likewise `WebChromeClient`'s default
 * `onJsAlert`/`onJsConfirm` silently CANCEL — `confirm()` returns false without
 * ever asking, so a page's "정말 삭제할까요?" button just appears not to work.
 * (Andromeda hit the same class with `window.confirm` under Tauri's WebView.)
 */

/** Android `WebViewClient.ERROR_*` codes we translate; others fall through. */
internal object BrowserErrorCode {
    const val UNKNOWN = -1
    const val HOST_LOOKUP = -2
    const val UNSUPPORTED_AUTH_SCHEME = -3
    const val AUTHENTICATION = -4
    const val PROXY_AUTHENTICATION = -5
    const val CONNECT = -6
    const val IO = -7
    const val TIMEOUT = -8
    const val REDIRECT_LOOP = -9
    const val UNSUPPORTED_SCHEME = -10
    const val FAILED_SSL_HANDSHAKE = -11
    const val BAD_URL = -12
    const val FILE = -13
    const val FILE_NOT_FOUND = -14
    const val TOO_MANY_REQUESTS = -15
}

/**
 * Korean explanation for a main-frame load failure. [description] is the
 * platform's English string — used only as a last resort so an unmapped code
 * still says something concrete instead of a bare number.
 */
internal fun browserPageErrorMessage(code: Int, description: String?): String = when (code) {
    BrowserErrorCode.HOST_LOOKUP -> "주소를 찾을 수 없습니다 — 도메인이 맞는지 확인해 주세요"

    BrowserErrorCode.CONNECT, BrowserErrorCode.IO -> "서버에 연결하지 못했습니다"

    BrowserErrorCode.TIMEOUT -> "응답이 없어 시간이 초과됐습니다"

    BrowserErrorCode.REDIRECT_LOOP -> "리디렉션이 반복돼 페이지를 열 수 없습니다"

    BrowserErrorCode.UNSUPPORTED_SCHEME -> "이 주소 형식은 열 수 없습니다"

    BrowserErrorCode.FAILED_SSL_HANDSHAKE -> "보안 연결(SSL)에 실패했습니다"

    BrowserErrorCode.BAD_URL -> "주소 형식이 올바르지 않습니다"

    BrowserErrorCode.FILE, BrowserErrorCode.FILE_NOT_FOUND -> "파일을 찾을 수 없습니다"

    BrowserErrorCode.TOO_MANY_REQUESTS -> "요청이 너무 많습니다 — 잠시 후 다시 시도해 주세요"

    BrowserErrorCode.AUTHENTICATION,
    BrowserErrorCode.PROXY_AUTHENTICATION,
    BrowserErrorCode.UNSUPPORTED_AUTH_SCHEME,
    -> "인증이 필요합니다"

    else -> description?.trim()?.takeIf { it.isNotEmpty() } ?: "페이지를 열지 못했습니다"
}

/** Android `SslError.SSL_*` primary error codes. */
internal object BrowserSslCode {
    const val NOT_YET_VALID = 0
    const val EXPIRED = 1
    const val ID_MISMATCH = 2
    const val UNTRUSTED = 3
    const val DATE_INVALID = 4
    const val INVALID = 5
}

/**
 * Korean explanation for an SSL failure. The browser always CANCELS on SSL
 * error — there is no "proceed anyway", because a single tap-through on a
 * personal assistant that holds business mail and groupware sessions is not a
 * trade worth offering.
 */
internal fun browserSslErrorMessage(primaryError: Int): String = when (primaryError) {
    BrowserSslCode.NOT_YET_VALID -> "인증서 유효기간이 아직 시작되지 않았습니다"
    BrowserSslCode.EXPIRED -> "인증서가 만료됐습니다"
    BrowserSslCode.ID_MISMATCH -> "인증서의 도메인이 주소와 일치하지 않습니다"
    BrowserSslCode.UNTRUSTED -> "신뢰할 수 없는 기관이 발급한 인증서입니다"
    BrowserSslCode.DATE_INVALID -> "인증서 날짜가 올바르지 않습니다"
    else -> "보안 인증서에 문제가 있어 연결하지 않았습니다"
}

internal fun browserRendererGoneMessage(crashed: Boolean): String = if (crashed) {
    "페이지 렌더러가 비정상 종료됐습니다"
} else {
    "메모리 확보를 위해 페이지가 종료됐습니다"
}

/** A pending `alert()` / `confirm()` / `prompt()` from the page. */
internal class BrowserJsDialog(
    val kind: Kind,
    val message: String,
    val defaultValue: String = "",
    private val respond: (confirmed: Boolean, value: String?) -> Unit,
) {
    enum class Kind { ALERT, CONFIRM, PROMPT }

    private var answered = false

    /**
     * Answers the page. Idempotent: a WebView keeps the JS thread blocked on the
     * result, and delivering twice throws, so a double-tap or a dismiss racing a
     * confirm must not double-answer.
     */
    fun answer(confirmed: Boolean, value: String? = null) {
        if (answered) return
        answered = true
        respond(confirmed, value)
    }

    /** Dismiss = cancel, which is what the page sees if the user taps outside. */
    fun cancel() = answer(false, null)
}

/**
 * True when a stolen popup URL is worth adopting as a navigation target.
 * Live popups no longer use this path — the child WebView stays attached —
 * but the predicate still documents which URLs are real vs hitch placeholders.
 */
internal fun browserAdoptPopupUrl(url: String): Boolean {
    val s = url.trim()
    if (s.isEmpty()) return false
    if (s.startsWith("about:", ignoreCase = true)) return false
    if (s.startsWith("javascript:", ignoreCase = true)) return false
    return true
}
