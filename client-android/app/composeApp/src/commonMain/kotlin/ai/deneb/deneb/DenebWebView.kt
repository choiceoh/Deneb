package ai.deneb.deneb

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier

/**
 * Shared, platform-agnostic state for the in-app browser WebView. The Android
 * [DenebWebView] actual drives a real `android.webkit.WebView`; the other
 * targets render a stub (the in-app browser is an Android-only feature for now).
 *
 * The chrome (DenebBrowserScreen) reads the observable fields for the URL bar,
 * progress, and back-enablement, and issues commands via [load]/[goBack]/[reload]
 * and the [translateEnabled] toggle.
 */
class DenebWebViewState(
    initialUrl: String,
    translateEnabled: Boolean = false,
    adBlockEnabled: Boolean = true,
) {
    /** The URL the WebView should load; setting it via [load] navigates. */
    var url by mutableStateOf(initialUrl)
        internal set

    /** The page's actual current URL (after redirects), shown in the URL bar. */
    var currentUrl by mutableStateOf(initialUrl)
        internal set

    /** The page title reported by the platform WebView, used for bookmarks. */
    var pageTitle by mutableStateOf("")
        internal set

    var canGoBack by mutableStateOf(false)
        internal set

    var canGoForward by mutableStateOf(false)
        internal set

    var loading by mutableStateOf(false)
        internal set

    /** 0..100 page-load progress. */
    var progress by mutableStateOf(0)
        internal set

    /** In-place translation on/off. The chrome toggles this; the actual pushes
     *  it into the page's injected translator. Seeded from AppSettings when the
     *  browser screen opens so the preference survives leave → re-enter. */
    var translateEnabled by mutableStateOf(translateEnabled)

    /** Drop known ad/tracker network requests in the Android WebView. */
    var adBlockEnabled by mutableStateOf(adBlockEnabled)

    /** Subresource requests dropped by adblock since the current page started. */
    var adBlockedCount by mutableStateOf(0)
        internal set

    /** Main-frame load failure, or null when the page loaded. Cleared on every
     *  new navigation. Subresource failures (a blocked ad, a dead image) never
     *  set this — only the page the user asked for. */
    var loadError by mutableStateOf<String?>(null)
        internal set

    /** Pending `alert()`/`confirm()`/`prompt()` the chrome must render. The page's
     *  JS thread is blocked until it is answered. */
    internal var jsDialog by mutableStateOf<BrowserJsDialog?>(null)

    /** Last scroll-diagnostic result, or null when none has been run. The chrome
     *  copies it out so it can be pasted into a session. */
    internal var diagnostics by mutableStateOf<String?>(null)

    internal var diagnosticsTick by mutableStateOf(0)
        private set

    /** Runs the on-device scroll probe against the live page. */
    internal fun runDiagnostics() {
        diagnosticsTick++
    }

    // Monotonic command ticks the actual observes via LaunchedEffect, so a
    // repeated tap (reload twice) still fires.
    internal var goBackTick by mutableStateOf(0)
        private set
    internal var goForwardTick by mutableStateOf(0)
        private set
    internal var reloadTick by mutableStateOf(0)
        private set
    internal var stopTick by mutableStateOf(0)
        private set

    fun load(newUrl: String) {
        url = newUrl
    }

    fun goBack() {
        goBackTick++
    }

    fun goForward() {
        goForwardTick++
    }

    fun reload() {
        reloadTick++
    }

    fun stop() {
        stopTick++
    }
}

/**
 * Translate callback: given the page's text segments, return a SAME-length,
 * SAME-order list of translations to Korean, or null to keep the originals.
 * Wired to the gateway's DeepL-first miniapp.web.translate RPC
 * (DenebGatewayClient.translateSegments).
 */
typealias TranslateFn = suspend (segments: List<String>, targetLang: String) -> List<String>?

/**
 * Renders the page. Android: a real WebView with the in-place translator injected
 * and bridged to [translate]. Other platforms: an Android-only stub.
 */
@Composable
expect fun DenebWebView(
    state: DenebWebViewState,
    translate: TranslateFn,
    modifier: Modifier,
)
