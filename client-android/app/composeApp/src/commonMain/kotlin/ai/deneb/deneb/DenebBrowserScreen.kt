package ai.deneb.deneb

import ai.deneb.PlatformBackHandler
import ai.deneb.openUrl
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebType
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/**
 * In-app browser for external links, with one-tap in-place translation (en/ru →
 * ko). The URL bar always shows the real current URL (link safety — an in-app
 * WebView otherwise hides where you are), and "열기" escapes to the system
 * browser. Translation toggles the injected DOM translator (see deneb-translate.js).
 *
 * Android renders a real WebView; other platforms show an Android-only stub
 * (the chrome and navigation still render, so the desktop harness can exercise them).
 */
@Composable
fun DenebBrowserScreen(
    url: String,
    client: DenebGatewayClient,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val state = remember(url) { DenebWebViewState(url) }

    // Back pops the page's own history first; the screen is left only at the root.
    PlatformBackHandler(enabled = state.canGoBack) { state.goBack() }

    DenebScreenScaffold(
        title = "브라우저",
        onBack = onBack,
        modifier = modifier,
        fillWidth = true,
        actions = {
            TextButton(onClick = { state.translateEnabled = !state.translateEnabled }) {
                Text(if (state.translateEnabled) "원문" else "번역", style = DenebType.button)
            }
            TextButton(onClick = { openUrl(state.currentUrl) }) {
                Text("열기", style = DenebType.button)
            }
        },
    ) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            TextButton(onClick = { state.reload() }) { Text("↻", style = DenebType.button) }
            Text(
                text = state.currentUrl,
                style = DenebType.meta,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
        }
        if (state.loading) {
            LinearProgressIndicator(
                progress = { state.progress / 100f },
                modifier = Modifier.fillMaxWidth(),
            )
        }
        DenebWebView(
            state = state,
            translate = { segments, lang -> client.translateSegments(segments, lang) },
            modifier = Modifier.fillMaxWidth().weight(1f),
        )
    }
}
