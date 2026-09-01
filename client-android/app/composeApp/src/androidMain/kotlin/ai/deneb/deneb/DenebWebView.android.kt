package ai.deneb.deneb

import ai.deneb.ui.components.rememberHaptics
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
import android.view.MotionEvent
import android.view.ViewGroup
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
import android.widget.FrameLayout
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import java.io.ByteArrayInputStream
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

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
) {
    // Composition-scoped: translate round-trips launched from the JS bridge are
    // cancelled when the browser screen leaves (rememberCoroutineScope), so a
    // closed page can't post stale translations. We keep a WebView ref to post
    // evaluateJavascript back onto it on the main thread.
    val scope = rememberCoroutineScope()
    val holder = remember(state) { WebViewHolder(state) }
    // Pull-to-refresh. A WebView is not a Compose nested-scroll child, so
    // PullToRefreshBox's gesture never fires over one; and this file's scroll
    // behaviour is hard-won (reflow budget, restore, Reddit unlock), so the
    // gesture must not change touch handling at all. The listener below only
    // OBSERVES — it always returns false, so every event still reaches the
    // WebView exactly as before. Feedback while dragging is this indicator;
    // once the reload starts, the existing determinate progress bar in the
    // browser chrome takes over.
    var pullFraction by remember(state) { mutableFloatStateOf(0f) }
    val pullHaptics = rememberHaptics()
    val context = LocalContext.current

    // Web forms with <input type="file"> need an activity result; without one the
    // "파일 선택" button does nothing at all. The callback must be answered even on
    // cancel, or the page's file input stays wedged for the rest of the session.
    // Pages keep running timers, media and JS after the app goes to the background;
    // without this a video kept playing and a busy page kept the CPU awake behind a
    // black screen. Also the moment to persist cookies: Chromium writes them lazily
    // and a background kill can drop a login the user just completed.
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner, holder) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_STOP -> {
                    holder.web?.onPause()
                    holder.web?.pauseTimers()
                    flushBrowserCookies()
                    // The process can die at any moment in the background; park
                    // the session on disk so the next launch restores history.
                    holder.web?.let { BrowserTabStateDisk.save(context, state.tabId, it) }
                }

                Lifecycle.Event.ON_START -> {
                    holder.web?.onResume()
                    holder.web?.resumeTimers()
                }

                else -> Unit
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    val fileChooser = remember(state) { FileChooserHolder() }
    val filePicker = rememberLauncherForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        fileChooser.deliver(WebChromeClient.FileChooserParams.parseResult(result.resultCode, result.data))
    }

    Box(modifier) {
        AndroidView(
            modifier = Modifier.fillMaxSize(),
            factory = { ctx ->
                val root = FrameLayout(ctx)
                val web = WebView(ctx)
                val popupLayerView = newBrowserPopupLayerView(ctx)
                root.addView(
                    web,
                    FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT),
                )
                root.addView(
                    popupLayerView,
                    FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT),
                )
                holder.web = web
                holder.popups = BrowserPopupLayer(
                    state = state,
                    context = ctx,
                    scope = scope,
                    translate = translate,
                    fileChooser = fileChooser,
                    launchFilePicker = { filePicker.launch(it) },
                    layer = popupLayerView,
                )
                holder.lastCommandUrl = state.url
                holder.translationEnabled = state.translateEnabled
                val adBlockHits = AtomicInteger(0)
                val mainHandler = Handler(Looper.getMainLooper())
                applyDenebBrowserWebSettings(web)
                attachPullToRefresh(
                    web = web,
                    onPull = { pullFraction = it },
                    // Fires while the finger is still down, the instant the pull
                    // passes its threshold — every other pull-to-refresh surface
                    // buzzes there, and the browser was the one that stayed silent.
                    onArm = { pullHaptics.refresh() },
                    onTrigger = {
                        pullFraction = 0f
                        state.reload()
                    },
                )

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
                val translateBridge = BrowserTranslateBridge(
                    scope = scope,
                    translateSegments = translate,
                    state = state,
                    webView = { holder.web },
                    translationEnabled = { holder.translationEnabled },
                )
                holder.translateBridge = translateBridge
                web.addJavascriptInterface(translateBridge, BROWSER_TRANSLATE_BRIDGE_NAME)
                web.webViewClient = object : WebViewClient() {
                    // App/deep-link schemes (intent://, market://, kakaotalk://, tel:,
                    // mailto:, bank cert auth) cannot be rendered by a WebView. With no
                    // override the navigation just fails and the tap does nothing —
                    // which is most Korean login/payment flows.
                    override fun shouldOverrideUrlLoading(
                        view: WebView,
                        request: WebResourceRequest,
                    ): Boolean {
                        if (request.isForMainFrame && request.hasGesture()) {
                            holder.finishDetachedRestore()
                        }
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
                        holder.translateBridge?.cancelForNavigation()
                        val previousStable = stableBrowserTabUrl(state.currentUrl, state.url, state.rendererRecoveryUrl)
                        if (canBookmarkUrl(url)) {
                            state.markRendererRecoveryStarted(url)
                            if (url != previousStable) {
                                state.pageTitle = ""
                                state.pageFavicon = null
                                state.pagePreview = null
                                state.clearTranslationProgress()
                            }
                        } else if (!state.rendererRecoveryPending && canShowAsAddress(url)) {
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

                    // First paint of the main frame. Translation used to start only in
                    // onPageFinished, which waits for every ad, tracker and lazy image —
                    // so the reader stared at the source text for however long the
                    // slowest subresource took, on exactly the ad-heavy pages where that
                    // is longest. Starting here works against the DOM already on screen,
                    // and the viewport-first dispatch inside the script means the text
                    // being read is translated first.
                    //
                    // Running twice is free and deliberate: the script self-guards
                    // (`__installed`), its asset is read once into an AtomicReference,
                    // and resumeForDocument only sets a flag. The onPageFinished pass
                    // below stays as the backstop for pages that never commit a visible
                    // frame (immediate redirects, some error paths).
                    override fun onPageCommitVisible(view: WebView, url: String) {
                        if (state.loadError != null) return
                        injectTranslateScript(view)
                        holder.translateBridge?.resumeForDocument()
                        view.evaluateJavascript(
                            "window.DenebTranslate&&window.DenebTranslate.setEnabled(${state.translateEnabled});",
                            null,
                        )
                    }

                    override fun onPageFinished(view: WebView, url: String) {
                        if (canBookmarkUrl(url)) {
                            state.markRendererRecoveryStarted(url)
                        } else if (!state.rendererRecoveryPending && canShowAsAddress(url)) {
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
                        if (state.loadError != null) return
                        injectSiteQuirk(view, url)
                        injectTranslateScript(view)
                        holder.translateBridge?.resumeForDocument()
                        // Re-apply the toggle: a fresh page starts untranslated.
                        view.evaluateJavascript(
                            "window.DenebTranslate&&window.DenebTranslate.setEnabled(${state.translateEnabled});",
                            null,
                        )
                    }

                    // Main frame only: every ad-blocked subresource also reports an
                    // error, and surfacing those would cry wolf on every page.
                    override fun onReceivedError(
                        view: WebView,
                        request: WebResourceRequest,
                        error: WebResourceError,
                    ) {
                        if (!request.isForMainFrame) return
                        resumeBrowserTranslation(holder, state, view)
                        // onReceivedSslError may have already set a more specific reason.
                        if (state.loadError == null) {
                            state.markMainFrameFailed(
                                browserPageErrorMessage(error.errorCode, error.description?.toString()),
                            )
                        } else {
                            state.loading = false
                            state.progress = 100
                        }
                    }

                    override fun onReceivedHttpError(
                        view: WebView,
                        request: WebResourceRequest,
                        errorResponse: WebResourceResponse,
                    ) {
                        if (!request.isForMainFrame) return
                        val message = browserHttpErrorMessage(errorResponse.statusCode) ?: return
                        resumeBrowserTranslation(holder, state, view)
                        state.markMainFrameFailed(message)
                    }

                    // Always cancel: no "proceed anyway" on a device holding
                    // business mail and groupware sessions. Iframe/image cert
                    // failures must not paint the whole page as failed.
                    override fun onReceivedSslError(view: WebView, handler: SslErrorHandler, error: SslError) {
                        handler.cancel()
                        val failing = error.url.orEmpty()
                        if (
                            !browserSslErrorAffectsPage(view.url.orEmpty(), failing) &&
                            !browserSslErrorAffectsPage(state.currentUrl, failing) &&
                            !browserSslErrorAffectsPage(state.url, failing)
                        ) {
                            return
                        }
                        resumeBrowserTranslation(holder, state, view)
                        state.markMainFrameFailed(browserSslErrorMessage(error.primaryError))
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
                        val previousStable = stableBrowserTabUrl(state.currentUrl, state.url, state.rendererRecoveryUrl)
                        state.pageTitle = stableBrowserPageTitle(title, view.url.orEmpty(), previousStable, state.pageTitle)
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
                    ): Boolean = holder.popups?.createWindow(isUserGesture, resultMsg) == true

                    override fun onCloseWindow(window: WebView) {
                        holder.popups?.closeWindow(window)
                    }

                    override fun onShowFileChooser(
                        webView: WebView,
                        callback: ValueCallback<Array<Uri>>,
                        params: FileChooserParams,
                    ): Boolean = fileChooser.start(callback) { filePicker.launch(params.createIntent()) }
                }
                val savedPlatformState = state.platformState as? Bundle
                // A cold start after process death has no in-memory parked Bundle —
                // fall back to the disk-parked state (consumed on read) so the
                // back/forward list and scroll survive the restart.
                val diskState = if (savedPlatformState == null) BrowserTabStateDisk.load(ctx, state.tabId) else null
                diskState?.let {
                    state.platformScrollX = it.scrollX
                    state.platformScrollY = it.scrollY
                }
                val restoreBundle = savedPlatformState ?: diskState?.bundle
                holder.restoringDetachedTab = restoreBundle != null
                val restored = restoreBundle?.let { saved ->
                    state.platformState = null
                    runCatching { web.restoreState(saved) != null }.getOrDefault(false)
                } ?: false
                holder.restoringDetachedTab = restored
                holder.scrollRestorePending = state.platformScrollX != 0 || state.platformScrollY != 0
                if (!restored && !state.rendererRecoveryPending) {
                    state.currentUrl.ifBlank { state.url }.takeIf { it.isNotBlank() }?.let(web::loadUrl)
                }
                root
            },
            update = { /* navigation/commands handled via LaunchedEffect below */ },
            onRelease = { _ ->
                fileChooser.deliver(null)
                holder.popups?.destroyAll()
                holder.popups = null
                holder.translateBridge?.cancelForNavigation()
                holder.translateBridge = null
                val web = holder.web
                if (web != null && !holder.rendererGone) {
                    state.platformScrollX = web.scrollX
                    state.platformScrollY = web.scrollY
                    captureBrowserPreview(web)?.let { state.pagePreview = it }
                    val saved = Bundle()
                    if (runCatching { web.saveState(saved) }.getOrNull() != null) {
                        state.platformState = saved
                    }
                    // Also park on disk: this release may be the last one before a
                    // background kill, and a restore after that needs the disk copy.
                    BrowserTabStateDisk.save(context, state.tabId, web)
                    web.removeJavascriptInterface(BROWSER_TRANSLATE_BRIDGE_NAME)
                    web.destroy()
                }
                holder.web = null
            },
        )
        if (pullFraction > 0f) {
            PullToRefreshHint(pullFraction)
        }
    }

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

    LaunchedEffect(state.url, state.loadTick) {
        val target = browserWebViewCommandUrl(
            requestedUrl = state.url,
            currentUrl = state.currentUrl,
            rendererRecoveryUrl = state.rendererRecoveryUrl,
            rendererRecoveryPending = state.rendererRecoveryPending,
        )
        // A repeat load of the same address must still reach loadUrl, so the
        // lastCommandUrl guard is skipped when load() asked for it explicitly. The
        // guard still stops the initial composition from re-loading the page the
        // WebView already restored.
        val explicit = holder.commands.consumeLoad(state)
        if (target.isNotBlank() && (explicit || holder.lastCommandUrl != target)) {
            holder.popups?.destroyAll()
            holder.finishDetachedRestore()
            // A deliberate navigation retires the saved session — restoring the
            // old back/forward list after the user navigated away would be a lie.
            BrowserTabStateDisk.remove(context, state.tabId)
            holder.lastCommandUrl = target
            holder.web?.let { web ->
                // Right after (re)entering the browser the restored session is
                // often still loading, and its pending restore navigation can
                // supersede a loadUrl issued meanwhile — the first bookmark or
                // history tap looked dead. Stop it first; a no-op when idle.
                web.stopLoading()
                web.loadUrl(target)
            }
        }
    }
    LaunchedEffect(state.goBackTick) {
        if (holder.commands.consumeGoBack(state)) {
            val popups = holder.popups
            if (popups?.top != null) {
                popups.goBack()
            } else {
                holder.finishDetachedRestore()
                holder.web?.let { if (it.canGoBack()) it.goBack() }
            }
        }
    }
    LaunchedEffect(state.closePopupTick) {
        if (holder.commands.consumeClosePopup(state)) {
            holder.popups?.closeTop()
        }
    }
    LaunchedEffect(state.reloadTick) {
        if (holder.commands.consumeReload(state)) {
            val popup = holder.popups?.top
            if (popup != null) {
                popup.reload()
            } else {
                holder.finishDetachedRestore()
                holder.web?.reload()
            }
        }
    }
    LaunchedEffect(state.goForwardTick) {
        if (holder.commands.consumeGoForward(state)) {
            val popups = holder.popups
            if (popups?.top != null) {
                popups.goForward()
            } else {
                holder.finishDetachedRestore()
                holder.web?.let { if (it.canGoForward()) it.goForward() }
            }
        }
    }
    LaunchedEffect(state.stopTick) {
        if (holder.commands.consumeStop(state)) {
            val popups = holder.popups
            if (popups?.top != null) {
                popups.stop()
            } else {
                holder.web?.let { web ->
                    web.stopLoading()
                    resumeBrowserTranslation(holder, state, web)
                }
            }
        }
    }
    LaunchedEffect(state.retryTick) {
        if (!holder.commands.consumeRetry(state)) return@LaunchedEffect
        val popups = holder.popups
        if (popups?.top != null) {
            popups.retry()
            return@LaunchedEffect
        }
        val target = stableBrowserTabUrl(state.rendererRecoveryUrl, state.currentUrl, state.url)
        holder.web?.let { web ->
            if (target.isNotBlank()) {
                holder.finishDetachedRestore()
                holder.lastCommandUrl = target
                web.loadUrl(target)
            }
        }
    }
    LaunchedEffect(state.translateEnabled) {
        holder.translationEnabled = state.translateEnabled
        if (!state.translateEnabled) state.clearTranslationProgress()
        holder.popups?.setTranslationEnabled(state.translateEnabled)
        holder.web?.evaluateJavascript(
            "window.DenebTranslate&&window.DenebTranslate.setEnabled(${state.translateEnabled});",
            null,
        )
    }
}

private class WebViewHolder(state: DenebWebViewState) {
    @Volatile
    var web: WebView? = null
    var popups: BrowserPopupLayer? = null
    var lastCommandUrl: String = ""

    @Volatile
    var translationEnabled: Boolean = false
    var rendererGone: Boolean = false
    var translateBridge: BrowserTranslateBridge? = null
    val commands = BrowserCommandCursor(state)
    var scrollRestorePending: Boolean = false
    var restoringDetachedTab: Boolean = false

    fun finishDetachedRestore() {
        restoringDetachedTab = false
    }

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

private fun resumeBrowserTranslation(holder: WebViewHolder, state: DenebWebViewState, web: WebView) {
    holder.translateBridge?.resumeForDocument()
    web.evaluateJavascript(
        "window.DenebTranslate&&window.DenebTranslate.setEnabled(${state.translateEnabled});",
        null,
    )
}

/**
 * Runs the per-site compatibility quirk, if this URL has one. Separate from the
 * translator injection so a quirk still applies with translation switched off —
 * the modern Reddit scroll lock is present either way.
 */
internal fun injectSiteQuirk(view: WebView, url: String) {
    val js = browserSiteQuirkScript(url) ?: return
    view.evaluateJavascript(js, null)
}

// The translator asset is ~53KB and identical for the life of the process, but it
// was re-read from assets — blocking IO on the main thread — on every single page
// load. Read it once.
private val translateScriptSource = AtomicReference<String?>(null)

/** Shared with the popup layer: child windows translate too. */
internal fun injectTranslateScript(view: WebView) {
    val cached = translateScriptSource.get()
    val js = cached ?: runCatching {
        view.context.assets.open("deneb-translate.js").bufferedReader().use { it.readText() }
    }.getOrNull()?.also { translateScriptSource.set(it) } ?: return
    view.evaluateJavascript(js, null)
}

/** Empty successful response used to drop ad/tracker subresource requests. */
internal fun emptyBlockedResponse(url: String): WebResourceResponse = WebResourceResponse(
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
internal fun openExternalUrl(context: Context, url: String): Boolean {
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
internal fun startBrowserDownload(
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
internal class FileChooserHolder {
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

/** Pull distance, in dp of finger travel, that arms a reload. */
private const val PULL_REFRESH_THRESHOLD_DP = 84f

/**
 * Observe-only pull-to-refresh for a WebView.
 *
 * The listener ALWAYS returns false, so the WebView receives and handles every
 * event exactly as it did before — this cannot change scrolling, link taps, text
 * selection, or the overscroll glow. It only measures the drag and hands it to
 * [BrowserPullTracker], which owns the arming rules (and their tests).
 *
 * Why not [androidx.compose.material3.pulltorefresh.PullToRefreshBox]: its gesture
 * rides Compose nested scroll, and a WebView is not a Compose nested-scroll child,
 * so the box would render an indicator that never fires.
 */
private fun attachPullToRefresh(
    web: WebView,
    onPull: (Float) -> Unit,
    onArm: () -> Unit,
    onTrigger: () -> Unit,
) {
    val tracker = BrowserPullTracker(PULL_REFRESH_THRESHOLD_DP * web.resources.displayMetrics.density)
    web.setOnTouchListener { _, event ->
        val phase = when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> BrowserPullPhase.DOWN
            MotionEvent.ACTION_MOVE -> BrowserPullPhase.MOVE
            MotionEvent.ACTION_UP -> BrowserPullPhase.UP
            MotionEvent.ACTION_CANCEL -> BrowserPullPhase.CANCEL
            else -> null
        }
        if (phase != null) {
            val trigger = tracker.onEvent(phase, event.y, web.scrollY)
            onPull(tracker.fraction)
            if (tracker.justArmed) onArm()
            if (trigger) onTrigger()
        }
        false
    }
}

/** Material progress ring that fills as the pull approaches the threshold. */
@Composable
private fun BoxScope.PullToRefreshHint(fraction: Float) {
    CircularProgressIndicator(
        progress = { fraction },
        modifier = Modifier.align(Alignment.TopCenter).padding(top = 16.dp).size(28.dp),
    )
}
