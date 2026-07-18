package ai.deneb.ui.dynamicui

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

/** wasm: no sandboxed web surface wired yet; placeholder. */
@Composable
actual fun DenebHtmlView(
    html: String,
    onSendPrompt: ((String) -> Unit)?,
    modifier: Modifier,
) = DenebHtmlUnsupported(modifier)
