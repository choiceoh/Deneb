package ai.deneb.deneb

import android.annotation.SuppressLint
import android.graphics.Bitmap
import android.os.Handler
import android.os.Looper
import android.webkit.JavascriptInterface
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
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
                web.addJavascriptInterface(TranslateBridge(scope, translate, holder), BRIDGE_NAME)
                web.webViewClient = object : WebViewClient() {
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
