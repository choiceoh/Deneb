package ai.deneb.ui.dynamicui

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

/** Desktop target is the headless verification harness — no web engine. */
@Composable
actual fun DenebHtmlView(
    html: String,
    onSendPrompt: ((String) -> Unit)?,
    modifier: Modifier,
) = DenebHtmlUnsupported(modifier)
