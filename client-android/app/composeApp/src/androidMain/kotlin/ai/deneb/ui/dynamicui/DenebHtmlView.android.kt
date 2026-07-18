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

// Injected ahead of the document: mobile viewport + the deneb bridge.
// window.deneb.send(text) → the native "choice" callback (a user chat message);
// the height reporter grows the frame to fit so the page never double-scrolls.
private const val PRELUDE =
    "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">" +
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
