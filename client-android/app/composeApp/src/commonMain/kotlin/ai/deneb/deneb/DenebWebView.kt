package ai.deneb.deneb

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap

internal data class BrowserNavigationCommit(val serial: Int, val url: String)

internal data class BrowserTranslationProgress(
    val scanned: Boolean = false,
    val total: Int = 0,
    val applied: Int = 0,
    val pending: Int = 0,
    val failed: Int = 0,
)

internal fun browserTranslationStatusText(progress: BrowserTranslationProgress): String = when {
    !progress.scanned -> "번역할 텍스트 찾는 중"
    progress.total == 0 -> "번역할 텍스트 없음"
    progress.pending > 0 -> "번역 중 · ${progress.applied}/${progress.total}"
    progress.failed > 0 -> "번역 일부 실패 · ${progress.applied}/${progress.total}"
    progress.applied >= progress.total -> "번역 완료 · ${progress.total}개"
    else -> "번역 준비 중 · ${progress.total}개"
}

internal class BrowserCommandCursor(state: DenebWebViewState) {
    private var diagnostics = state.diagnosticsTick
    private var goBack = state.goBackTick
    private var goForward = state.goForwardTick
    private var reload = state.reloadTick
    private var stop = state.stopTick
    private var retry = state.retryTick

    fun consumeDiagnostics(state: DenebWebViewState): Boolean = consume(state.diagnosticsTick, diagnostics) { diagnostics = it }

    fun consumeGoBack(state: DenebWebViewState): Boolean = consume(state.goBackTick, goBack) { goBack = it }

    fun consumeGoForward(state: DenebWebViewState): Boolean = consume(state.goForwardTick, goForward) { goForward = it }

    fun consumeReload(state: DenebWebViewState): Boolean = consume(state.reloadTick, reload) { reload = it }

    fun consumeStop(state: DenebWebViewState): Boolean = consume(state.stopTick, stop) { stop = it }

    fun consumeRetry(state: DenebWebViewState): Boolean = consume(state.retryTick, retry) { retry = it }

    private inline fun consume(current: Int, consumed: Int, update: (Int) -> Unit): Boolean {
        if (current <= consumed) return false
        update(current)
        return true
    }
}

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
    initialPageTitle: String = "",
    translateEnabled: Boolean = false,
    adBlockEnabled: Boolean = true,
) {
    /** The URL the WebView should load; setting it via [load] navigates. */
    var url by mutableStateOf(initialUrl)
        internal set

    /** The page's actual current URL (after redirects), shown in the URL bar. */
    var currentUrl by mutableStateOf(initialUrl)
        internal set

    /** Per-tab omnibox edit. [omniboxSyncedUrl] keeps tab re-selection from
     * replacing an unfinished draft merely because the composable re-attached. */
    internal var omniboxDraft by mutableStateOf(initialUrl)
        private set
    private var omniboxSyncedUrl = initialUrl

    internal fun editOmnibox(value: String) {
        omniboxDraft = value
    }

    internal fun syncOmniboxWithCurrentUrl() {
        if (currentUrl == omniboxSyncedUrl) return
        omniboxSyncedUrl = currentUrl
        omniboxDraft = currentUrl
    }

    /** The page title reported by the platform WebView, used for bookmarks. */
    var pageTitle by mutableStateOf(initialPageTitle)
        internal set

    /** Runtime-only tab visuals. They are intentionally not persisted as browsing data. */
    var pageFavicon by mutableStateOf<ImageBitmap?>(null)
        internal set

    var pagePreview by mutableStateOf<ImageBitmap?>(null)
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

    internal var translationProgress by mutableStateOf(BrowserTranslationProgress())
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
    internal var retryTick by mutableStateOf(0)
        private set

    private var navigationSerial = 0
    private var lastCommittedNavigationUrl = ""
    internal var pendingNavigationCommit by mutableStateOf<BrowserNavigationCommit?>(null)
        private set

    /** Opaque platform state. Android stores a Bundle here while a background
     * tab is detached, preserving its back/forward list and scroll position. */
    internal var platformState: Any? = null

    /** WebView.saveState omits the visual scroll offset, so preserve it explicitly. */
    internal var platformScrollX: Int = 0
    internal var platformScrollY: Int = 0

    /** Changes when Android must discard a dead renderer and create a new
     * WebView. The chrome keys the platform view with this generation. */
    internal var rendererGeneration by mutableStateOf(0)
        private set

    internal var rendererRecoveryPending by mutableStateOf(false)
        private set

    internal var rendererRecoveryUrl: String = stableBrowserTabUrl(initialUrl, initialUrl, "")
        private set

    fun load(newUrl: String) {
        rendererRecoveryPending = false
        loadError = null
        platformState = null
        platformScrollX = 0
        platformScrollY = 0
        pageFavicon = null
        pagePreview = null
        translationProgress = BrowserTranslationProgress()
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

    fun retry() {
        rendererRecoveryPending = false
        loadError = null
        retryTick++
    }

    internal fun commitNavigation(url: String, force: Boolean = false) {
        val stable = url.trim().takeIf(::canBookmarkUrl) ?: return
        if (!force && stable == lastCommittedNavigationUrl) return
        lastCommittedNavigationUrl = stable
        navigationSerial++
        pendingNavigationCommit = BrowserNavigationCommit(navigationSerial, stable)
    }

    internal fun consumeNavigationCommit(commit: BrowserNavigationCommit): String? {
        if (pendingNavigationCommit != commit) return null
        pendingNavigationCommit = null
        return commit.url
    }

    internal fun updateTranslationProgress(total: Int, applied: Int, pending: Int, failed: Int) {
        val cleanTotal = total.coerceAtLeast(0)
        translationProgress = BrowserTranslationProgress(
            scanned = true,
            total = cleanTotal,
            applied = applied.coerceIn(0, cleanTotal),
            pending = pending.coerceAtLeast(0),
            failed = failed.coerceAtLeast(0),
        )
    }

    internal fun clearTranslationProgress() {
        translationProgress = BrowserTranslationProgress()
    }

    internal fun markRendererGone(crashed: Boolean) {
        rendererRecoveryUrl = stableBrowserTabUrl(currentUrl, url, rendererRecoveryUrl)
        platformState = null
        rendererRecoveryPending = true
        loadError = browserRendererGoneMessage(crashed)
        loading = false
        progress = 0
        canGoBack = false
        canGoForward = false
        rendererGeneration++
    }

    internal fun markRendererRecoveryStarted(url: String) {
        if (!canBookmarkUrl(url)) return
        rendererRecoveryUrl = url.trim()
        currentUrl = url
        rendererRecoveryPending = false
        loadError = null
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
    onOpenNewTab: (String) -> Unit = { state.load(it) },
)
