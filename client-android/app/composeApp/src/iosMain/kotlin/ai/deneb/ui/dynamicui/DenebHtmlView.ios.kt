package ai.deneb.ui.dynamicui

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

/** iOS: WKWebView wiring can follow; placeholder until then. */
@Composable
actual fun DenebHtmlView(
    html: String,
    onSendPrompt: ((String) -> Unit)?,
    modifier: Modifier,
) = DenebHtmlUnsupported(modifier)
