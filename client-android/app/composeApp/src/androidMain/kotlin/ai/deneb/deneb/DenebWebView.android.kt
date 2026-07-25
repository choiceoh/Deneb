package ai.deneb.deneb

import android.annotation.SuppressLint
import android.app.DownloadManager
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.net.Uri
import android.os.Environment
import android.os.Handler
import android.os.Looper
import android.webkit.CookieManager
import android.webkit.JavascriptInterface
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
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
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.viewinterop.AndroidView
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
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
) {
    // Composition-scoped: translate round-trips launched from the JS bridge are
    // cancelled when the browser screen leaves (rememberCoroutineScope), so a
    // closed page can't post stale translations. We keep a WebView ref to post
    // evaluateJavascript back onto it on the main thread.
    val scope = rememberCoroutineScope()
    val holder = remember { WebViewHolder() }
    val context = LocalContext.current

    // Web forms with <input type="file"> need an activity result; without one the
    // "파일 선택" button does nothing at all. The callback must be answered even on
    // cancel, or the page's file input stays wedged for the rest of the session.
    val fileChooser = remember { FileChooserHolder() }
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
                val adBlockHits = AtomicInteger(0)
                val mainHandler = Handler(Looper.getMainLooper())
                web.settings.javaScriptEnabled = true
                web.settings.domStorageEnabled = true
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
                web.addJavascriptInterface(TranslateBridge(scope, translate, holder), BRIDGE_NAME)
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
                        state.currentUrl = url
                        state.pageTitle = ""
                        state.loading = true
                        adBlockHits.set(0)
                        state.adBlockedCount = 0
                        state.canGoBack = view.canGoBack()
                        state.canGoForward = view.canGoForward()
                    }

                    override fun onPageFinished(view: WebView, url: String) {
                        state.currentUrl = url
                        state.canGoBack = view.canGoBack()
                        state.canGoForward = view.canGoForward()
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
                    override fun doUpdateVisitedHistory(view: WebView, url: String, isReload: Boolean) {
                        super.doUpdateVisitedHistory(view, url, isReload)
                        state.currentUrl = url
                        state.canGoBack = view.canGoBack()
                        state.canGoForward = view.canGoForward()
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

                    override fun onProgressChanged(view: WebView, newProgress: Int) {
                        state.progress = newProgress
                        state.loading = newProgress < 100
                    }

                    override fun onShowFileChooser(
                        webView: WebView,
                        callback: ValueCallback<Array<Uri>>,
                        params: FileChooserParams,
                    ): Boolean = fileChooser.start(callback) { filePicker.launch(params.createIntent()) }
                }
                web.loadUrl(state.url)
            }
        },
        update = { /* navigation/commands handled via LaunchedEffect below */ },
        onRelease = { web ->
            web.removeJavascriptInterface(BRIDGE_NAME)
            web.destroy()
            holder.web = null
        },
    )

    LaunchedEffect(state.url) {
        holder.web?.let { if (it.url != state.url && state.url.isNotBlank()) it.loadUrl(state.url) }
    }
    LaunchedEffect(state.goBackTick) {
        if (state.goBackTick > 0) holder.web?.let { if (it.canGoBack()) it.goBack() }
    }
    LaunchedEffect(state.reloadTick) {
        if (state.reloadTick > 0) holder.web?.reload()
    }
    LaunchedEffect(state.goForwardTick) {
        if (state.goForwardTick > 0) holder.web?.let { if (it.canGoForward()) it.goForward() }
    }
    LaunchedEffect(state.stopTick) {
        if (state.stopTick > 0) holder.web?.stopLoading()
    }
    LaunchedEffect(state.translateEnabled) {
        holder.web?.evaluateJavascript(
            "window.DenebTranslate&&window.DenebTranslate.setEnabled(${state.translateEnabled});",
            null,
        )
    }
}

private const val BRIDGE_NAME = "DenebTranslateBridge"

private class WebViewHolder {
    var web: WebView? = null
}

/**
 * JS → native bridge. [translate] is called on a coroutine (the @JavascriptInterface
 * method runs on a binder thread), then the result is posted back into the page on
 * the main thread. A null/failed translation simply drops the batch — the page
 * keeps its originals, matching the gateway's count-preserving contract.
 */
private class TranslateBridge(
    private val scope: CoroutineScope,
    private val translate: TranslateFn,
    private val holder: WebViewHolder,
) {
    // Diagnostic + UX: when translation is enabled, the page reports how many
    // translatable nodes it found. 0 → nothing to translate (e.g. the page is already
    // Korean, or the DOM walk found nothing); >0 → translating. Surfaced as a brief
    // toast so a silent no-op is visible to the user (and pinpoints where it breaks).
    @JavascriptInterface
    fun onEnable(count: Int) {
        toast(if (count == 0) "DeepL로 번역할 텍스트를 찾지 못했습니다" else "DeepL 번역 중… ${count}개")
    }

    @JavascriptInterface
    fun translate(requestId: String, segmentsJson: String) {
        val segments = decodeStringList(segmentsJson)
        if (segments.isEmpty()) return
        scope.launch {
            val translated = runCatching { translate(segments, "ko") }.getOrNull()
            if (translated == null) {
                toast("DeepL 번역 실패 — 서버 응답 없음")
                return@launch
            }
            if (translated.size != segments.size) {
                toast("DeepL 번역 응답 개수 불일치")
                return@launch
            }
            val ridLiteral = jsStringLiteral(requestId)
            val payloadLiteral = jsStringLiteral(encodeStringList(translated))
            withContext(Dispatchers.Main) {
                holder.web?.evaluateJavascript(
                    "window.DenebTranslate&&window.DenebTranslate.applyBatch($ridLiteral,$payloadLiteral);",
                    null,
                )
            }
        }
    }

    private fun toast(msg: String) {
        val ctx = holder.web?.context ?: return
        Handler(Looper.getMainLooper()).post { Toast.makeText(ctx, msg, Toast.LENGTH_SHORT).show() }
    }
}

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

private val stringListSerializer = ListSerializer(String.serializer())

private fun decodeStringList(json: String): List<String> = runCatching { webViewJson.decodeFromString(stringListSerializer, json) }.getOrDefault(emptyList())

private fun encodeStringList(values: List<String>): String = webViewJson.encodeToString(stringListSerializer, values)

/** Encodes [value] as a JS string literal (JSON string), safe to embed in an
 *  evaluateJavascript expression. */
private fun jsStringLiteral(value: String): String = webViewJson.encodeToString(String.serializer(), value)

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
