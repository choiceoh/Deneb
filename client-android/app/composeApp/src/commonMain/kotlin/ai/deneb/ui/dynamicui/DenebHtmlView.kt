package ai.deneb.ui.dynamicui

import androidx.compose.foundation.border
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp

/**
 * Sandboxed inline renderer for a webpage-style HTML answer (```deneb-html).
 * The Android actual drives a real WebView with ALL network loads blocked and
 * only inline CSS/JS live; other targets render a quiet placeholder (the
 * desktop target is a headless verification harness, iOS support can follow).
 *
 * [onSendPrompt] bridges the page's `window.deneb.send(text)` back into the
 * chat as a user message; null = the answer is stale/read-only (an older
 * transcript row) and page sends are ignored.
 */
@Composable
expect fun DenebHtmlView(
    html: String,
    onSendPrompt: ((String) -> Unit)?,
    modifier: Modifier = Modifier,
)

/** Quiet placeholder for targets without a sandboxed web surface. */
@Composable
internal fun DenebHtmlUnsupported(modifier: Modifier = Modifier) {
    Text(
        text = "웹 응답 — 이 기기에서는 미리보기가 지원되지 않습니다",
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = modifier.padding(16.dp),
    )
}

/** Frame chrome shared by every target: rounded hairline border, clipped. */
@Composable
internal fun DenebHtmlAnswerBlock(
    html: String,
    onSendPrompt: ((String) -> Unit)?,
    modifier: Modifier = Modifier,
) {
    DenebHtmlView(
        html = html,
        onSendPrompt = onSendPrompt,
        modifier = modifier
            .fillMaxWidth()
            .padding(vertical = 8.dp)
            .clip(RoundedCornerShape(12.dp))
            .border(
                width = 1.dp,
                color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.6f),
                shape = RoundedCornerShape(12.dp),
            ),
    )
}
