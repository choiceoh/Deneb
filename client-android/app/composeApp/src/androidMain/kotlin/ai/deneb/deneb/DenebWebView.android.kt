package ai.deneb.deneb

import android.annotation.SuppressLint
import android.app.DownloadManager
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.Canvas
import android.net.Uri
import android.net.http.SslError
import android.os.Bundle
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.os.Message
import android.webkit.CookieManager
import android.webkit.JsPromptResult
import android.webkit.JsResult
import android.webkit.RenderProcessGoneDetail
import android.webkit.SslErrorHandler
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.viewinterop.AndroidView
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import java.io.ByteArrayInputStream
import java.util.concurrent.atomic.AtomicInteger

private val webViewJson = Json { ignoreUnknownKeys = true }

/**
 * Android in-app browser WebView with the in-place DeepL-first translator. On each page load
 * we inject deneb-translate.js (assets), which walks the DOM, skips Korean, and
 * calls back through the [BRIDGE_NAME] JavaScript interface; that hands the page's
 * text segments to [translate] (the gateway RPC) and applies the result in place.
 */
@SuppressLint("SetJavaScriptEnabled")
@Composable
actual fun DenebWebView(
    state: DenebWebViewState,
    translate: TranslateFn,
    modifier: Modifier,
    onOpenNewTab: (String) -> Unit,
) {
    // Composition-scoped: translate round-trips launched from the JS bridge are
    // cancelled when the browser screen leaves (rememberCoroutineScope), so a
    // closed page can't post stale translations. We keep a WebView ref to post
    // evaluateJavascript back onto it on the main thread.
    val scope = rememberCoroutineScope()
    val holder = remember(state) { WebViewHolder(state) }
    val context = LocalContext.current
    val latestOpenNewTab = rememberUpdatedState(onOpenNewTab)

    // Web forms with <input type="file"> need an activity result; without one the
    // "파일 선택" button does nothing at all. The callback must be answered even on
    // cancel, or the page's file input stays wedged for the rest of the session.
    val fileChooser = remember(state) { FileChooserHolder() }
    val filePicker = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        fileChooser.deliver(WebChromeClient.FileChooserParams.parseResult(result.resultCode, result.data))
    }

    AndroidView(
        modifier = modifier,
        factory = { ctx ->
            WebView(ctx).also { web ->
                holder.web = web
                holder.lastCommandUrl = state.url
                holder.translationEnabled = state.translateEnabled
                val adBlockHits = AtomicInteger(0)
                val mainHandler = Handler(Looper.getMainLooper())
                web.settings.javaScriptEnabled = true
                web.settings.domStorageEnabled = true
                // Honour the page's <meta name="viewport">. Off by default in a
                // WebView, which lays a responsive site out against the raw view
                // width instead of the width the page asked for — so mobile
                // breakpoints and device-pixel-ratio images resolve wrong.
                // loadWithOverviewMode then fits that viewport to the screen on
                // first paint instead of opening zoomed into the top-left.
                web.settings.useWideViewPort = true
                web.settings.loadWithOverviewMode = true
                // Pinch-zoom: a phone browser needs it for dense tables and
                // scanned documents. displayZoomControls stays off so the legacy
                // on-screen +/- buttons never overlay the page.
                web.settings.setSupportZoom(true)
                web.settings.builtInZoomControls = true
                web.settings.displayZoomControls = false
                // User-initiated target=_blank/window.open navigations are routed
                // into a Deneb tab by onCreateWindow below. Script-only popups stay
                // blocked by javaScriptCanOpenWindowsAutomatically=false.
                web.settings.setSupportMultipleWindows(true)
                web.settings.javaScriptCanOpenWindowsAutomatically = false
                // Present as the mobile browser this is, not as an embedded
                // WebView: Reddit answers the default `; wv` UA with its
                // open-in-app gate, which pins an interstitial over the page and
                // locks scrolling (the page draws but will not move). See
                // browserUserAgent — only that token is dropped, so the UA keeps
                // this device's real Android/Chrome build.
                web.settings.userAgentString = browserUserAgent(web.settings.userAgentString)
                // Third-party cookies are off by default in a WebView, which breaks
                // SSO/social sign-in on sites that hand the session off across hosts.
                CookieManager.getInstance().setAcceptThirdPartyCookies(web, true)

                // Downloads: a WebView with no listener drops the navigation with no
                // error, so tapping a PDF/xlsx link does nothing at all. Hand it to
                // DownloadManager with the page's cookies + UA so authenticated
                // attachments (groupware, mail) actually come down.
                web.setDownloadListener { url, userAgent, contentDisposition, mimeType, _ ->
                    val name = browserDownloadFileName(url, contentDisposition, mimeType)
                    val started = startBrowserDownload(context, url, userAgent, mimeType, name)
                    Toast.makeText(
                        context,
                        if (started) "다운로드 시작: $name" else "다운로드를 시작할 수 없습니다",
                        Toast.LENGTH_SHORT,
                    ).show()
                }
                web.addJavascriptInterface(
                    BrowserTranslateBridge(
                        scope = scope,
                        translateSegments = translate,
                        state = state,
                        webView = { holder.web },
                        translationEnabled = { holder.translationEnabled },
                    ),
                    BROWSER_TRANSLATE_BRIDGE_NAME,
                )
                web.webViewClient = object : WebViewClient() {
                    // App/deep-link schemes (intent://, market://, kakaotalk://, tel:,
                    // mailto:, bank cert auth) cannot be rendered by a WebView. With no
                    // override the navigation just fails and the tap does nothing —
                    // which is most Korean login/payment flows.
                    override fun shouldOverrideUrlLoading(
                        view: WebView,
                        request: WebResourceRequest,
                    ): Boolean {
                        val url = request.url?.toString().orEmpty()
                        if (!isExternalSchemeUrl(url)) return false
                        if (openExternalUrl(context, url)) return true
                        // No app installed: intent:// may name a web page to use instead.
                        intentFallbackUrl(url)?.let {
                            view.loadUrl(it)
                            return true
                        }
                        Toast.makeText(context, "이 링크를 열 앱이 없습니다", Toast.LENGTH_SHORT).show()
                        return true
                    }

                    override fun shouldInterceptRequest(
                        view: WebView,
                        request: WebResourceRequest,
                    ): WebResourceResponse? {
                        if (!state.adBlockEnabled) return null
                        val url = request.url?.toString().orEmpty()
                        if (!shouldBlockBrowserAdRequest(url, isForMainFrame = request.isForMainFrame)) {
                            return null
                        }
                        val n = adBlockHits.incrementAndGet()
                        mainHandler.post { state.adBlockedCount = n }
                        return emptyBlockedResponse(url)
                    }

                    @Deprecated("Deprecated in Java")
                    override fun shouldInterceptRequest(view: WebView, url: String): WebResourceResponse? {
                        if (!state.adBlockEnabled) return null
                        if (!shouldBlockBrowserAdRequest(url, isForMainFrame = false)) return null
                        val n = adBlockHits.incrementAndGet()
                        mainHandler.post { state.adBlockedCount = n }
                        return emptyBlockedResponse(url)
                    }

                    override fun onPageStarted(view: WebView, url: String, favicon: Bitmap?) {
                        val previousStable = stableBrowserTabUrl(state.currentUrl, state.url, state.rendererRecoveryUrl)
                        if (canBookmarkUrl(url)) {
                            state.markRendererRecoveryStarted(url)
                            if (url != previousStable) {
                                state.pageTitle = ""
                                state.pageFavicon = null
                                state.pagePreview = null
                                state.clearTranslationProgress()
                            }
                        } else if (!state.rendererRecoveryPending && !(urlScheme(url) == "about" && previousStable.isNotBlank())) {
                            state.currentUrl = url
                        }
                        favicon?.let(::browserFaviconImage)?.let { state.pageFavicon = it }
                        if (!state.rendererRecoveryPending) state.loadError = null
                        state.loading = true
                        adBlockHits.set(0)
                        state.adBlockedCount = 0
                        state.canGoBack = view.canGoBack()
                        state.canGoForward = view.canGoForward()
                    }

                    override fun onPageFinished(view: WebView, url: String) {
                        if (canBookmarkUrl(url)) {
                            state.markRendererRecoveryStarted(url)
                        } else if (!state.rendererRecoveryPending && urlScheme(url) != "about") {
                            state.currentUrl = url
                        }
                        state.canGoBack = view.canGoBack()
                        state.canGoForward = view.canGoForward()
                        view.post {
                            holder.restoreScroll(view, state)
                            captureBrowserPreview(view)?.let { state.pagePreview = it }
                            if (holder.restoringDetachedTab) {
                                holder.restoringDetachedTab = false
                            } else {
                                state.commitNavigation(url)
                            }
                        }
                        injectSiteQuirk(view, url)
                        injectTranslateScript(view)
                        // Re-apply the toggle: a fresh page starts untranslated.
                        view.evaluateJavascript(
                            "window.DenebTranslate&&window.DenebTranslate.setEnabled(${state.translateEnabled});",
                            null,
                        )
                    }

                    // SPA soft-nav often updates history without a full reload. Hint the
                    // injected translator so it re-collects when JS history hooks miss.
                    // Without this a failed load is a blank screen with no reason.
                    // Main frame only: every ad-blocked subresource also reports an
                    // error, and surfacing those would cry wolf on every page.
                    override fun onReceivedError(
                        view: WebView,
                        request: WebResourceRequest,
                        error: WebResourceError,
                    ) {
                        if (!request.isForMainFrame) return
                        state.loadError = browserPageErrorMessage(error.errorCode, error.description?.toString())
                    }

                    // Always cancel: no "proceed anyway" on a device holding
                    // business mail and groupware sessions.
                    override fun onReceivedSslError(view: WebView, handler: SslErrorHandler, error: SslError) {
                        handler.cancel()
                        state.loadError = browserSslErrorMessage(error.primaryError)
                    }

                    override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                        // A dead renderer makes this WebView permanently unusable.
                        // Mark the state first so the keyed caller replaces the
                        // platform view, then destroy this exact instance.
                        holder.rendererGone = true
                        if (holder.web === view) holder.web = null
                        state.markRendererGone(detail.didCrash())
                        view.removeJavascriptInterface(BROWSER_TRANSLATE_BRIDGE_NAME)
                        view.destroy()
                        return true
                    }

                    override fun doUpdateVisitedHistory(view: WebView, url: String, isReload: Boolean) {
                        super.doUpdateVisitedHistory(view, url, isReload)
                        val previousStable = stableBrowserTabUrl(state.currentUrl, state.url, state.rendererRecoveryUrl)
                        if (canBookmarkUrl(url)) {
                            state.markRendererRecoveryStarted(url)
                        } else if (!(urlScheme(url) == "about" && previousStable.isNotBlank())) {
                            state.currentUrl = url
                        }
                        state.canGoBack = view.canGoBack()
                        state.canGoForward = view.canGoForward()
                        if (!holder.restoringDetachedTab) {
                            view.post { state.commitNavigation(url, force = isReload) }
                        }
                        // SPA soft-nav keeps the document, so the quirk's observer
                        // survives; re-running is a no-op behind its re-entry guard
                        // and covers the case where the lock lands only after the
                        // first client-side route change.
                        injectSiteQuirk(view, url)
                        view.evaluateJavascript(
                            "window.DenebTranslate&&window.DenebTranslate.onLocationChange&&window.DenebTranslate.onLocationChange();",
                            null,
                        )
                    }
                }
                web.webChromeClient = object : WebChromeClient() {
                    override fun onReceivedTitle(view: WebView, title: String?) {
                        state.pageTitle = title.orEmpty()
                    }

                    override fun onReceivedIcon(view: WebView, icon: Bitmap) {
                        browserFaviconImage(icon)?.let { state.pageFavicon = it }
                    }

                    override fun onProgressChanged(view: WebView, newProgress: Int) {
                        state.progress = newProgress
                        state.loading = newProgress < 100
                    }

                    // The default WebChromeClient silently CANCELS these, so a page's
                    // confirm() returns false without ever asking the user — the
                    // button simply looks broken (same trap Andromeda hit).
                    override fun onJsAlert(view: WebView, url: String, message: String, result: JsResult): Boolean {
                        state.jsDialog = BrowserJsDialog(BrowserJsDialog.Kind.ALERT, message) { _, _ ->
                            result.confirm()
                        }
                        return true
                    }

                    override fun onJsConfirm(view: WebView, url: String, message: String, result: JsResult): Boolean {
                        state.jsDialog = BrowserJsDialog(BrowserJsDialog.Kind.CONFIRM, message) { ok, _ ->
                            if (ok) result.confirm() else result.cancel()
                        }
                        return true
                    }

                    override fun onJsPrompt(
                        view: WebView,
                        url: String,
                        message: String,
                        defaultValue: String?,
                        result: JsPromptResult,
                    ): Boolean {
                        state.jsDialog = BrowserJsDialog(
                            BrowserJsDialog.Kind.PROMPT,
                            message,
                            defaultValue.orEmpty(),
                        ) { ok, value ->
                            if (ok) result.confirm(value.orEmpty()) else result.cancel()
                        }
                        return true
                    }

                    override fun onCreateWindow(
                        view: WebView,
                        isDialog: Boolean,
                        isUserGesture: Boolean,
                        resultMsg: Message,
                    ): Boolean {
                        if (!isUserGesture) return false
                        val transport = resultMsg.obj as? WebView.WebViewTransport ?: return false
                        val popup = WebView(view.context)
                        var dispatched = false
                        val abandonPopup = Runnable {
                            if (!dispatched) {
                                dispatched = true
                                popup.destroy()
                            }
                        }

                        fun dispatch(url: String): Boolean {
                            if (dispatched || !browserAdoptPopupUrl(url)) return false
                            val route = browserPopupRoute(url)
                            if (route == BrowserPopupRoute.IGNORE) return false
                            dispatched = true
                            mainHandler.removeCallbacks(abandonPopup)
                            when (route) {
                                BrowserPopupRoute.NEW_TAB -> latestOpenNewTab.value(url)

                                BrowserPopupRoute.SAME_TAB -> view.loadUrl(url)

                                BrowserPopupRoute.EXTERNAL -> {
                                    if (!openExternalUrl(context, url)) {
                                        val fallback = intentFallbackUrl(url)
                                        if (fallback != null) {
                                            latestOpenNewTab.value(fallback)
                                        } else {
                                            Toast.makeText(context, "이 링크를 열 앱이 없습니다", Toast.LENGTH_SHORT).show()
                                        }
                                    }
                                }

                                BrowserPopupRoute.IGNORE -> Unit
                            }
                            popup.stopLoading()
                            popup.destroy()
                            return true
                        }

                        popup.settings.javaScriptEnabled = true
                        popup.settings.domStorageEnabled = true
                        popup.webViewClient = object : WebViewClient() {
                            override fun shouldOverrideUrlLoading(
                                view: WebView,
                                request: WebResourceRequest,
                            ): Boolean = dispatch(request.url?.toString().orEmpty())

                            override fun onPageStarted(view: WebView, url: String, favicon: Bitmap?) {
                                dispatch(url)
                            }
                        }
                        transport.webView = popup
                        // A target=_blank page may stay on about:blank forever.
                        // Do not retain that detached renderer indefinitely.
                        mainHandler.postDelayed(abandonPopup, POPUP_RESOLUTION_TIMEOUT_MS)
                        resultMsg.sendToTarget()
                        return true
                    }

                    override fun onShowFileChooser(
                        webView: WebView,
                        callback: ValueCallback<Array<Uri>>,
                        params: FileChooserParams,
                    ): Boolean = fileChooser.start(callback) { filePicker.launch(params.createIntent()) }
                }
                val savedPlatformState = state.platformState as? Bundle
                holder.restoringDetachedTab = savedPlatformState != null
                val restored = savedPlatformState?.let { saved ->
                    state.platformState = null
                    runCatching { web.restoreState(saved) != null }.getOrDefault(false)
                } ?: false
                holder.restoringDetachedTab = restored
                if (restored) {
                    web.postDelayed({ holder.restoringDetachedTab = false }, DETACHED_RESTORE_GUARD_MS)
                }
                holder.scrollRestorePending = state.platformScrollX != 0 || state.platformScrollY != 0
                if (!restored && !state.rendererRecoveryPending) {
                    state.currentUrl.ifBlank { state.url }.takeIf { it.isNotBlank() }?.let(web::loadUrl)
                }
            }
        },
        update = { /* navigation/commands handled via LaunchedEffect below */ },
        onRelease = { web ->
            fileChooser.deliver(null)
            if (!holder.rendererGone) {
                state.platformScrollX = web.scrollX
                state.platformScrollY = web.scrollY
                captureBrowserPreview(web)?.let { state.pagePreview = it }
                val saved = Bundle()
                if (runCatching { web.saveState(saved) }.getOrNull() != null) {
                    state.platformState = saved
                }
                web.removeJavascriptInterface(BROWSER_TRANSLATE_BRIDGE_NAME)
                web.destroy()
            }
            if (holder.web === web) holder.web = null
        },
    )

    LaunchedEffect(state.diagnosticsTick) {
        if (!holder.commands.consumeDiagnostics(state)) return@LaunchedEffect
        holder.web?.evaluateJavascript(BROWSER_SCROLL_DIAGNOSTIC_JS) { raw ->
            // evaluateJavascript hands back a JSON-encoded string; unwrap one level.
            val page = runCatching {
                webViewJson.decodeFromString(String.serializer(), raw)
            }.getOrDefault(raw)
            // Stamp the build. Reading a capture without knowing whether the fix
            // under test was even installed has already cost a round of
            // diagnosis; the version has to travel with the measurement.
            state.diagnostics = if (page.trimStart().startsWith("{")) {
                """{"app":{"versionCode":$DENEB_VERSION_CODE},"page":$page}"""
            } else {
                page
            }
        }
    }

    LaunchedEffect(state.url) {
        val target = state.url
        if (target.isNotBlank() && holder.lastCommandUrl != target) {
            holder.lastCommandUrl = target
            holder.web?.loadUrl(target)
        }
    }
    LaunchedEffect(state.goBackTick) {
        if (holder.commands.consumeGoBack(state)) holder.web?.let { if (it.canGoBack()) it.goBack() }
    }
    LaunchedEffect(state.reloadTick) {
        if (holder.commands.consumeReload(state)) holder.web?.reload()
    }
    LaunchedEffect(state.goForwardTick) {
        if (holder.commands.consumeGoForward(state)) holder.web?.let { if (it.canGoForward()) it.goForward() }
    }
    LaunchedEffect(state.stopTick) {
        if (holder.commands.consumeStop(state)) holder.web?.stopLoading()
    }
    LaunchedEffect(state.retryTick) {
        if (!holder.commands.consumeRetry(state)) return@LaunchedEffect
        val target = stableBrowserTabUrl(state.rendererRecoveryUrl, state.currentUrl, state.url)
        holder.web?.let { web ->
            if (target.isNotBlank()) {
                holder.lastCommandUrl = target
                web.loadUrl(target)
            }
        }
    }
    LaunchedEffect(state.translateEnabled) {
        holder.translationEnabled = state.translateEnabled
        if (!state.translateEnabled) state.clearTranslationProgress()
        holder.web?.evaluateJavascript(
            "window.DenebTranslate&&window.DenebTranslate.setEnabled(${state.translateEnabled});",
            null,
        )
    }
}

private const val POPUP_RESOLUTION_TIMEOUT_MS = 10_000L
private const val DETACHED_RESTORE_GUARD_MS = 2_000L

private class WebViewHolder(state: DenebWebViewState) {
    @Volatile
    var web: WebView? = null
    var lastCommandUrl: String = ""

    @Volatile
    var translationEnabled: Boolean = false
    var rendererGone: Boolean = false
    val commands = BrowserCommandCursor(state)
    var scrollRestorePending: Boolean = false
    var restoringDetachedTab: Boolean = false

    fun restoreScroll(web: WebView, state: DenebWebViewState) {
        if (!scrollRestorePending) return
        scrollRestorePending = false
        web.scrollTo(state.platformScrollX, state.platformScrollY)
    }
}

/** Draws a small runtime-only card image without allocating a full-screen bitmap. */
private fun captureBrowserPreview(web: WebView): ImageBitmap? {
    if (web.width <= 0 || web.height <= 0) return null
    return runCatching {
        val bitmap = Bitmap.createBitmap(BROWSER_PREVIEW_WIDTH, BROWSER_PREVIEW_HEIGHT, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(bitmap)
        canvas.scale(
            BROWSER_PREVIEW_WIDTH.toFloat() / web.width.toFloat(),
            BROWSER_PREVIEW_HEIGHT.toFloat() / web.height.toFloat(),
        )
        web.draw(canvas)
        bitmap.asImageBitmap()
    }.getOrNull()
}

private const val BROWSER_PREVIEW_WIDTH = 320
private const val BROWSER_PREVIEW_HEIGHT = 180

private fun browserFaviconImage(icon: Bitmap): ImageBitmap? = runCatching {
    val largest = maxOf(icon.width, icon.height).coerceAtLeast(1)
    val scale = minOf(1f, BROWSER_FAVICON_SIZE.toFloat() / largest.toFloat())
    val owned = if (scale < 1f) {
        Bitmap.createScaledBitmap(
            icon,
            (icon.width * scale).toInt().coerceAtLeast(1),
            (icon.height * scale).toInt().coerceAtLeast(1),
            true,
        )
    } else {
        icon.copy(Bitmap.Config.ARGB_8888, false) ?: icon
    }
    owned.asImageBitmap()
}.getOrNull()

private const val BROWSER_FAVICON_SIZE = 64

/**
 * Runs the per-site compatibility quirk, if this URL has one. Separate from the
 * translator injection so a quirk still applies with translation switched off —
 * the modern Reddit scroll lock is present either way.
 */
private fun injectSiteQuirk(view: WebView, url: String) {
    val js = browserSiteQuirkScript(url) ?: return
    view.evaluateJavascript(js, null)
}

private fun injectTranslateScript(view: WebView) {
    val js = runCatching {
        view.context.assets.open("deneb-translate.js").bufferedReader().use { it.readText() }
    }.getOrNull() ?: return
    view.evaluateJavascript(js, null)
}

/** Empty successful response used to drop ad/tracker subresource requests. */
private fun emptyBlockedResponse(url: String): WebResourceResponse = WebResourceResponse(
    browserBlockedResponseMime(url),
    "utf-8",
    ByteArrayInputStream(ByteArray(0)),
)

/**
 * Hands an app/deep-link URL to the OS. Returns false when nothing can handle it,
 * so the caller can fall back to the intent's browser_fallback_url.
 *
 * `intent://` URLs are parsed with [Intent.URI_INTENT_SCHEME] and then hardened:
 * a page-supplied intent must not be able to name an explicit component or ride a
 * selector into a non-browsable activity — that is how a WebView gets used to
 * poke at private activities of other apps (and of Deneb itself). Forcing
 * CATEGORY_BROWSABLE with no component leaves normal deep links working while
 * limiting the reachable surface to activities that opted into web navigation.
 */
private fun openExternalUrl(context: Context, url: String): Boolean {
    val intent = runCatching {
        if (urlScheme(url) == "intent") {
            Intent.parseUri(url, Intent.URI_INTENT_SCHEME).apply {
                component = null
                selector = null
                addCategory(Intent.CATEGORY_BROWSABLE)
            }
        } else {
            Intent(Intent.ACTION_VIEW, Uri.parse(url)).apply {
                addCategory(Intent.CATEGORY_BROWSABLE)
            }
        }
    }.getOrNull() ?: return false
    intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    // Any launch failure means "not handled" so the caller can fall back to
    // browser_fallback_url or tell the user: ActivityNotFoundException (no app
    // installed) and SecurityException (an activity that is not really exported)
    // are both dead ends for this navigation.
    return runCatching {
        context.startActivity(intent)
        true
    }.getOrDefault(false)
}

/**
 * Queues a download with the system DownloadManager, carrying the page's cookies
 * and user-agent so authenticated attachments (groupware, mail) resolve instead
 * of returning a login page.
 */
private fun startBrowserDownload(
    context: Context,
    url: String,
    userAgent: String?,
    mimeType: String?,
    fileName: String,
): Boolean = runCatching {
    val request = DownloadManager.Request(Uri.parse(url)).apply {
        setMimeType(mimeType)
        CookieManager.getInstance().getCookie(url)?.let { addRequestHeader("cookie", it) }
        userAgent?.takeIf { it.isNotBlank() }?.let { addRequestHeader("User-Agent", it) }
        setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
        setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, fileName)
    }
    val dm = context.getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager
    dm.enqueue(request)
    true
}.getOrDefault(false)

/**
 * Holds the pending `<input type="file">` callback across the activity result.
 * A WebView keeps at most one file request open, and it MUST be answered — a
 * dropped callback wedges the page's file input until reload, so cancel delivers
 * null rather than nothing.
 */
private class FileChooserHolder {
    private var pending: ValueCallback<Array<Uri>>? = null

    /** Replaces any stale request (the page reloaded mid-pick) and launches. */
    fun start(callback: ValueCallback<Array<Uri>>, launch: () -> Unit): Boolean {
        pending?.onReceiveValue(null)
        pending = callback
        return runCatching {
            launch()
            true
        }.getOrElse {
            pending = null
            callback.onReceiveValue(null)
            false
        }
    }

    fun deliver(uris: Array<Uri>?) {
        val cb = pending ?: return
        pending = null
        cb.onReceiveValue(uris)
    }
}
