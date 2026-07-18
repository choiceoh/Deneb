package ai.deneb.ui.dynamicui

import android.annotation.SuppressLint
import android.graphics.Color
import android.webkit.JavascriptInterface
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.compose.foundation.layout.height
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView

// Base stylesheet every page gets for free: Korean-friendly system fonts,
// readable rhythm, bordered tables, sane margins on a light surface. Injected
// FIRST so the page's own styles override it naturally. Keep in sync with the
// desktop PRELUDE (DenebHtml.tsx) and docs/research/deneb-html.md.
private const val BASE_CSS =
    ":root{color-scheme:light}" +
        "body{margin:14px;font-family:'Pretendard','Noto Sans KR',system-ui,-apple-system,sans-serif;" +
        "font-size:14px;line-height:1.6;color:#1f2128;background:#fff}" +
        "h1,h2,h3,h4{line-height:1.3;margin:0.7em 0 0.35em}" +
        "h1{font-size:22px}h2{font-size:18px}h3{font-size:15px}" +
        "p{margin:0.4em 0}" +
        "table{border-collapse:collapse;width:100%}" +
        "th,td{padding:6px 10px;border:1px solid #e5e6ea;text-align:left}" +
        "th{background:#f7f7f9}" +
        "button{font:inherit;cursor:pointer}"

// Injected ahead of the document: mobile viewport + base style + the deneb
// bridge. window.deneb.send(text) → the native "choice" callback (a user chat
// message); the height reporter grows the frame to fit so the page never
// double-scrolls.
private const val PRELUDE =
    "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">" +
        "<style>$BASE_CSS</style>" +
        "<script>(function(){" +
        "window.deneb={send:function(t){DenebNative.send(String(t))}};" +
        "var r=function(){DenebNative.height(document.documentElement.scrollHeight)};" +
        "window.addEventListener(\"load\",r);" +
        "if(typeof ResizeObserver===\"function\"){new ResizeObserver(r).observe(document.documentElement)}" +
        "})()</script>"

private const val MIN_HEIGHT_DP = 160
private const val MAX_HEIGHT_DP = 900

/**
 * Real WebView, hard-sandboxed for agent-authored documents: every network
 * load is blocked (inline CSS/JS only, matching the authoring contract), no
 * file/content access, and any navigation attempt is swallowed. The page is
 * self-contained by contract, so blocking the network loses nothing.
 */
@SuppressLint("SetJavaScriptEnabled")
@Composable
actual fun DenebHtmlView(
    html: String,
    onSendPrompt: ((String) -> Unit)?,
    modifier: Modifier,
) {
    var heightDp by remember { mutableIntStateOf(MIN_HEIGHT_DP) }
    val send by rememberUpdatedState(onSendPrompt)

    AndroidView(
        modifier = modifier.height(heightDp.dp),
        factory = { ctx ->
            WebView(ctx).apply {
                settings.javaScriptEnabled = true
                settings.blockNetworkLoads = true
                settings.blockNetworkImage = true
                settings.allowFileAccess = false
                settings.allowContentAccess = false
                setBackgroundColor(Color.WHITE)
                isVerticalScrollBarEnabled = false
                webViewClient = object : WebViewClient() {
                    // The document is the whole world — no navigation escapes it.
                    override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?) = true
                }
                addJavascriptInterface(
                    object {
                        @JavascriptInterface
                        fun send(text: String) {
                            post { send?.invoke(text.trim().takeIf { it.isNotEmpty() } ?: return@post) }
                        }

                        @JavascriptInterface
                        fun height(h: Int) {
                            post { heightDp = (h + 8).coerceIn(MIN_HEIGHT_DP, MAX_HEIGHT_DP) }
                        }
                    },
                    "DenebNative",
                )
            }
        },
        update = { web ->
            if (web.tag != html) {
                web.tag = html
                web.loadDataWithBaseURL(null, PRELUDE + html, "text/html", "utf-8", null)
            }
        },
    )
}
