package ai.deneb.deneb

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import ai.deneb.ui.components.SkeletonList

/**
 * Shared loading / error / empty states + helpers for Deneb surface screens.
 *
 * The old DenebSurface / DenebViewHeader / DenebChip(Row) chrome was removed once
 * screens migrated to DenebScreenScaffold + hand-rolled headers (see
 * .claude/rules/native-design-system.md). Only the cross-screen state helpers and
 * humanBytes remain here.
 */

/**
 * Shimmering skeleton placeholder shown while a Deneb surface loads — content
 * fades in instead of replacing a "불러오는 중…" line. Shared by every Deneb
 * screen, so improving it here upgrades all of them at once.
 */
@Composable
fun DenebLoading(@Suppress("UNUSED_PARAMETER") text: String = "불러오는 중…") {
    SkeletonList(showAvatar = false)
}

/** Error banner with an optional retry button. */
@Composable
fun DenebError(text: String, onRetry: (() -> Unit)? = null) {
    Column(Modifier.fillMaxWidth().padding(vertical = 16.dp)) {
        Text(text, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
        if (onRetry != null) {
            Spacer(Modifier.height(8.dp))
            OutlinedButton(onClick = onRetry) { Text("다시 시도") }
        }
    }
}

/**
 * Empty-state placeholder: a quiet line with an optional call-to-action, so an
 * empty list guides the user instead of looking broken or still-loading. Shared
 * by every Deneb screen.
 */
@Composable
fun DenebEmpty(text: String, actionLabel: String? = null, onAction: (() -> Unit)? = null) {
    Column(Modifier.fillMaxWidth().padding(vertical = 24.dp)) {
        Text(text, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        if (actionLabel != null && onAction != null) {
            Spacer(Modifier.height(8.dp))
            OutlinedButton(onClick = onAction) { Text(actionLabel) }
        }
    }
}

/** Bytes -> short human size (integer units; KMP-safe). Shared by the mail and
 *  category screens so the formatter lives in one place. */
internal fun humanBytes(bytes: Long): String = when {
    bytes <= 0 -> "0B"
    bytes < 1024 -> "${bytes}B"
    bytes < 1024 * 1024 -> "${bytes / 1024}KB"
    else -> "${bytes / (1024 * 1024)}MB"
}
