package ai.deneb.deneb

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.net.http.SslError
import android.os.Handler
import android.os.Looper
import android.os.Message
import android.view.View
import android.view.ViewGroup
import android.webkit.CookieManager
import android.webkit.JsPromptResult
import android.webkit.JsResult
import android.webkit.RenderProcessGoneDetail
import android.webkit.SslErrorHandler
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import android.widget.Toast
import kotlinx.coroutines.CoroutineScope
import java.util.concurrent.atomic.AtomicInteger

/**
 * Live `window.open` / `target=_blank` host.
 *
 * The previous hitch stole the first non-blank URL, destroyed the child WebView,
 * and loaded that URL in a new tab. That drops POST bodies (form target=_blank),
 * `window.opener` / postMessage (OAuth, 본인인증), and `about:blank` windows that
 * only exist after document.write. Keep the child attached until the page or the
 * user closes it.
 *
 * Each popup carries its own translate bridge: a link that opens a new window
 * must still translate, following the owning tab's toggle.
 */
internal class BrowserPopupLayer(
    private val state: DenebWebViewState,
    private val context: Context,
    private val scope: CoroutineScope,
    private val translate: TranslateFn,
    private val fileChooser: FileChooserHolder,
    private val launchFilePicker: (Intent) -> Unit,
    private val layer: FrameLayout,
) {
    private val stack = ArrayDeque<WebView>()
    private val bridges = HashMap<WebView, BrowserTranslateBridge>()
    private val adBlockHits = AtomicInteger(0)
    private val mainHandler = Handler(Looper.getMainLooper())

    val top: WebView? get() = stack.lastOrNull()

    fun createWindow(isUserGesture: Boolean, resultMsg: Message): Boolean {
        if (!isUserGesture) return false
        val transport = resultMsg.obj as? WebView.WebViewTransport ?: return false
        if (stack.size >= MAX_BROWSER_POPUP_DEPTH) {
            Toast.makeText(context, "창을 더 열 수 없습니다", Toast.LENGTH_SHORT).show()
            return false
        }
        val popup = WebView(context)
        applyDenebBrowserWebSettings(popup)
        val bridge = BrowserTranslateBridge(
            scope = scope,
            translateSegments = translate,
            state = state,
            webView = { popup },
            translationEnabled = { state.translateEnabled },
        )
        bridges[popup] = bridge
        popup.addJavascriptInterface(bridge, BROWSER_TRANSLATE_BRIDGE_NAME)
        attachClients(popup, bridge)
        layer.addView(
            popup,
            FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT,
            ),
        )
        layer.visibility = View.VISIBLE
        stack.addLast(popup)
        state.attachPopup(popup.url.orEmpty(), popup.title.orEmpty())
        transport.webView = popup
        resultMsg.sendToTarget()
        return true
    }

    fun closeWindow(window: WebView) {
        val idx = stack.indexOf(window)
        if (idx < 0) return
        while (stack.size > idx) destroyTop()
        publish()
    }

    /** System back: popup history first, then close the top window. */
    fun goBack() {
        val current = top ?: return
        if (current.canGoBack()) current.goBack() else closeTop()
    }

    fun goForward() {
        top?.takeIf { it.canGoForward() }?.goForward()
    }

    fun reload() {
        top?.reload()
    }

    fun stop() {
        top?.stopLoading()
        top?.let { publishLocation(it, loading = false) }
    }

    fun retry() {
        state.popupError = null
        top?.reload()
    }

    fun closeTop() {
        if (stack.isEmpty()) return
        destroyTop()
        publish()
    }

    fun destroyAll() {
        while (stack.isNotEmpty()) destroyTop()
        layer.visibility = View.GONE
        state.jsDialog?.cancel()
        state.jsDialog = null
        state.detachPopup()
    }

    private fun destroyTop() {
        val web = stack.removeLast()
        bridges.remove(web)?.cancelForNavigation()
        layer.removeView(web)
        web.removeJavascriptInterface(BROWSER_TRANSLATE_BRIDGE_NAME)
        web.stopLoading()
        web.destroy()
    }

    /** Pushes the owning tab's translate toggle into every live popup document. */
    fun setTranslationEnabled(enabled: Boolean) {
        val js = "window.DenebTranslate&&window.DenebTranslate.setEnabled($enabled);"
        for (web in stack) web.evaluateJavascript(js, null)
    }

    private fun publish() {
        val current = top
        if (current == null) {
            layer.visibility = View.GONE
            state.jsDialog?.cancel()
            state.jsDialog = null
            state.detachPopup()
            return
        }
        layer.visibility = View.VISIBLE
        publishLocation(current, loading = current.progress < 100)
    }

    private fun publishLocation(web: WebView, loading: Boolean) {
        state.updatePopup(
            url = web.url.orEmpty(),
            title = web.title.orEmpty(),
            canGoForward = web.canGoForward(),
            loading = loading,
        )
    }

    private fun attachClients(popup: WebView, bridge: BrowserTranslateBridge) {
        popup.setDownloadListener { url, userAgent, contentDisposition, mimeType, _ ->
            val name = browserDownloadFileName(url, contentDisposition, mimeType)
            val started = startBrowserDownload(context, url, userAgent, mimeType, name)
            Toast.makeText(
                context,
                if (started) "다운로드 시작: $name" else "다운로드를 시작할 수 없습니다",
                Toast.LENGTH_SHORT,
            ).show()
        }
        popup.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                val url = request.url?.toString().orEmpty()
                return handlePopupExternalUrl(view, url)
            }

            override fun shouldInterceptRequest(
                view: WebView,
                request: WebResourceRequest,
            ): android.webkit.WebResourceResponse? {
                if (!state.adBlockEnabled) return null
                val url = request.url?.toString().orEmpty()
                if (!shouldBlockBrowserAdRequest(url, isForMainFrame = request.isForMainFrame)) {
                    return null
                }
                val n = adBlockHits.incrementAndGet()
                mainHandler.post { state.adBlockedCount = n }
                return emptyBlockedResponse(url)
            }

            override fun onPageStarted(view: WebView, url: String, favicon: android.graphics.Bitmap?) {
                if (view !== top) return
                bridge.cancelForNavigation()
                adBlockHits.set(0)
                state.popupError = null
                publishLocation(view, loading = true)
            }

            override fun onPageFinished(view: WebView, url: String) {
                if (view !== top) return
                if (state.popupError == null) {
                    injectSiteQuirk(view, url)
                    injectTranslateScript(view)
                }
                bridge.resumeForDocument()
                view.evaluateJavascript(
                    "window.DenebTranslate&&window.DenebTranslate.setEnabled(${state.translateEnabled});",
                    null,
                )
                publishLocation(view, loading = false)
            }

            override fun doUpdateVisitedHistory(view: WebView, url: String, isReload: Boolean) {
                super.doUpdateVisitedHistory(view, url, isReload)
                if (view !== top) return
                injectSiteQuirk(view, url)
                publishLocation(view, loading = view.progress < 100)
            }

            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                if (!request.isForMainFrame || view !== top) return
                if (state.popupError == null) {
                    state.popupError = browserPageErrorMessage(error.errorCode, error.description?.toString())
                }
                publishLocation(view, loading = false)
            }

            override fun onReceivedHttpError(
                view: WebView,
                request: WebResourceRequest,
                errorResponse: WebResourceResponse,
            ) {
                if (!request.isForMainFrame || view !== top) return
                val message = browserHttpErrorMessage(errorResponse.statusCode) ?: return
                state.popupError = message
                publishLocation(view, loading = false)
            }

            override fun onReceivedSslError(view: WebView, handler: SslErrorHandler, error: SslError) {
                handler.cancel()
                if (view !== top) return
                val failing = error.url.orEmpty()
                if (
                    !browserSslErrorAffectsPage(view.url.orEmpty(), failing) &&
                    !browserSslErrorAffectsPage(state.popupUrl, failing)
                ) {
                    return
                }
                state.popupError = browserSslErrorMessage(error.primaryError)
                publishLocation(view, loading = false)
            }

            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                closeWindow(view)
                return true
            }
        }
        popup.webChromeClient = object : WebChromeClient() {
            override fun onReceivedTitle(view: WebView, title: String?) {
                if (view !== top) return
                if (!title.isNullOrBlank()) state.updatePopup(view.url.orEmpty(), title, view.canGoForward(), view.progress < 100)
            }

            override fun onProgressChanged(view: WebView, newProgress: Int) {
                if (view !== top) return
                publishLocation(view, loading = newProgress < 100)
            }

            override fun onJsAlert(view: WebView, url: String, message: String, result: JsResult): Boolean {
                state.jsDialog = BrowserJsDialog(BrowserJsDialog.Kind.ALERT, message) { _, _ -> result.confirm() }
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
            ): Boolean = createWindow(isUserGesture, resultMsg)

            override fun onCloseWindow(window: WebView) {
                closeWindow(window)
            }

            override fun onShowFileChooser(
                webView: WebView,
                callback: android.webkit.ValueCallback<Array<android.net.Uri>>,
                params: FileChooserParams,
            ): Boolean = fileChooser.start(callback) { launchFilePicker(params.createIntent()) }
        }
    }

    private fun handlePopupExternalUrl(view: WebView, url: String): Boolean {
        if (!isExternalSchemeUrl(url)) return false
        if (openExternalUrl(context, url)) return true
        intentFallbackUrl(url)?.let {
            view.loadUrl(it)
            return true
        }
        Toast.makeText(context, "이 링크를 열 앱이 없습니다", Toast.LENGTH_SHORT).show()
        return true
    }
}

internal const val MAX_BROWSER_POPUP_DEPTH = 3

@SuppressLint("SetJavaScriptEnabled")
internal fun applyDenebBrowserWebSettings(web: WebView) {
    web.settings.javaScriptEnabled = true
    web.settings.domStorageEnabled = true
    web.settings.useWideViewPort = true
    web.settings.loadWithOverviewMode = true
    web.settings.setSupportZoom(true)
    web.settings.builtInZoomControls = true
    web.settings.displayZoomControls = false
    web.settings.setSupportMultipleWindows(true)
    web.settings.javaScriptCanOpenWindowsAutomatically = false
    web.settings.userAgentString = browserUserAgent(web.settings.userAgentString)
    // Older Korean sites still serve http images/scripts on https pages; the
    // WebView default (NEVER_ALLOW) silently drops them and the page renders
    // broken. Chrome itself uses the compatibility behaviour.
    web.settings.mixedContentMode = WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE
    CookieManager.getInstance().setAcceptThirdPartyCookies(web, true)
}

/**
 * Persists cookies now instead of whenever Chromium next decides to.
 *
 * Chromium flushes its cookie store lazily. The app can be killed from the
 * background at any time — and the battery policy tears the connection down on
 * purpose — so a session cookie written just before that (a login the user just
 * completed) could be lost, silently signing them back out.
 */
internal fun flushBrowserCookies() {
    runCatching { CookieManager.getInstance().flush() }
}

internal fun newBrowserPopupLayerView(context: Context): FrameLayout = FrameLayout(context).apply {
    visibility = View.GONE
    isClickable = true
    setBackgroundColor(Color.WHITE)
}
