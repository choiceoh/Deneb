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

// Base stylesheet + micro design system every page gets for free: variable-
// driven so a single body class re-skins the whole page (theme-dark /
// theme-warm / theme-mono; default = clean light), plus utility classes
// (card, grid, stat, badge, bar, button.primary) the authoring contract
// teaches — diverse looks, consistent quality, near-zero model CSS. Injected
// FIRST so the page's own styles override it naturally. Keep in sync with the
// desktop PRELUDE (DenebHtml.tsx) and docs/research/deneb-html.md.
private const val BASE_CSS =
    ":root{color-scheme:light}" +
        "body{--bg:#fff;--ink:#1f2128;--muted:#6f747e;--line:#e5e6ea;--card:#f7f7f9;" +
        "--accent:#3b6ea5;--ok:#2e7d32;--warn:#b26a00;--bad:#c62828;" +
        "margin:14px;font-family:'Pretendard','Noto Sans KR',system-ui,-apple-system,sans-serif;" +
        "font-size:14px;line-height:1.6;color:var(--ink);background:var(--bg)}" +
        "body.theme-dark{color-scheme:dark;--bg:#111318;--ink:#e8eaf0;--muted:#9aa1ad;" +
        "--line:#2a2e37;--card:#1b1e26;--accent:#7fa8d0}" +
        "body.theme-warm{--card:#faf5f0;--line:#eadfd5;--accent:#c17a5b}" +
        "body.theme-mono{--accent:#1f2128}" +
        "h1,h2,h3,h4{line-height:1.3;margin:0.7em 0 0.35em}" +
        "h1{font-size:22px}h2{font-size:18px}h3{font-size:15px}" +
        "p{margin:0.4em 0}" +
        "table{border-collapse:collapse;width:100%}" +
        "th,td{padding:6px 10px;border:1px solid var(--line);text-align:left}" +
        "th{background:var(--card)}" +
        "button{font:inherit;cursor:pointer;border:1px solid var(--line);border-radius:8px;" +
        "padding:6px 12px;background:var(--card);color:var(--ink)}" +
        "button.primary{background:var(--accent);border-color:var(--accent);color:#fff}" +
        ".card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:12px 14px;margin:8px 0}" +
        ".grid{display:grid;gap:10px;grid-template-columns:repeat(auto-fit,minmax(140px,1fr))}" +
        ".stat-value{font-size:24px;font-weight:600;line-height:1.2}" +
        ".stat-label{font-size:12px;color:var(--muted)}" +
        ".badge{display:inline-block;padding:2px 8px;border-radius:999px;font-size:12px;background:var(--card)}" +
        ".badge.ok{background:#e6f4ea;color:var(--ok)}" +
        ".badge.warn{background:#fdf3e3;color:var(--warn)}" +
        ".badge.bad{background:#fdeaea;color:var(--bad)}" +
        ".bar{height:8px;border-radius:4px;background:var(--line);overflow:hidden}" +
        ".bar>i{display:block;height:100%;background:var(--accent)}" +
        ".muted{color:var(--muted)}.accent{color:var(--accent)}"

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
