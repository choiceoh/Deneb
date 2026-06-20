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
class DenebWebViewState(initialUrl: String) {
    /** The URL the WebView should load; setting it via [load] navigates. */
    var url by mutableStateOf(initialUrl)
        internal set

    /** The page's actual current URL (after redirects), shown in the URL bar. */
    var currentUrl by mutableStateOf(initialUrl)
        internal set

    var canGoBack by mutableStateOf(false)
        internal set

    var loading by mutableStateOf(false)
        internal set

    /** 0..100 page-load progress. */
    var progress by mutableStateOf(0)
        internal set

    /** In-place translation on/off. The chrome toggles this; the actual pushes
     *  it into the page's injected translator. */
    var translateEnabled by mutableStateOf(false)

    // Monotonic command ticks the actual observes via LaunchedEffect, so a
    // repeated tap (reload twice) still fires.
    internal var goBackTick by mutableStateOf(0)
        private set
    internal var reloadTick by mutableStateOf(0)
        private set

    fun load(newUrl: String) {
        url = newUrl
    }

    fun goBack() {
        goBackTick++
    }

    fun reload() {
        reloadTick++
    }
}

/**
 * Translate callback: given the page's text segments, return a SAME-length,
 * SAME-order list of translations (en/ru → ko), or null to keep the originals.
 * Wired to the gateway's miniapp.web.translate RPC (DenebGatewayClient.translateSegments).
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
