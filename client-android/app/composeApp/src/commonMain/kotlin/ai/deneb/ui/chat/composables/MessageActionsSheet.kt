package ai.deneb.ui.chat.composables

import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import kotlinx.datetime.Instant
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toLocalDateTime

// Long-press action sheet for chat messages — the "touch a message and act on
// it" grammar of first-tier chat apps. Material ModalBottomSheet substrate,
// Deneb typography rows. Each row dismisses the sheet before running so the
// action never fights the sheet's exit animation.

internal data class MessageSheetAction(
    val icon: ImageVector,
    val label: String,
    val run: () -> Unit,
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun MessageActionsSheet(
    timestampMs: Long,
    actions: List<MessageSheetAction>,
    onDismiss: () -> Unit,
) {
    val haptics = rememberHaptics()
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 12.dp)) {
            // 보낸 시각 — timestamps stay out of the transcript (visual quiet) and
            // surface here on demand instead.
            messageTimestampLabel(timestampMs)?.let { stamp ->
                Text(
                    text = stamp,
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(start = 12.dp, bottom = 8.dp),
                )
            }
            for (action in actions) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .fillMaxWidth()
                        .denebPressable(onClick = {
                            onDismiss()
                            action.run()
                        })
                        .padding(horizontal = 12.dp, vertical = 14.dp),
                ) {
                    Icon(
                        imageVector = action.icon,
                        contentDescription = null,
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.size(20.dp).padding(end = 0.dp),
                    )
                    Spacer(Modifier.size(14.dp))
                    Text(text = action.label, style = DenebType.body)
                }
            }
            Spacer(Modifier.height(16.dp))
        }
    }
}

// "7월 17일 (금) 14:03" — absolute local stamp; null when the message carries no
// wall-clock (live streaming rows).
internal fun messageTimestampLabel(timestampMs: Long): String? {
    if (timestampMs <= 0) return null
    val local = runCatching {
        Instant.fromEpochMilliseconds(timestampMs).toLocalDateTime(TimeZone.currentSystemDefault())
    }.getOrNull() ?: return null
    val weekdays = listOf("월", "화", "수", "목", "금", "토", "일")
    val weekday = weekdays[local.dayOfWeek.ordinal]
    val minute = local.minute.toString().padStart(2, '0')
    return "${local.monthNumber}월 ${local.dayOfMonth}일 ($weekday) ${local.hour}:$minute"
}
