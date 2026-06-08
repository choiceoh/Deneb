package com.inspiredandroid.kai.ui.chat.composables

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowForward
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.deneb.DenebEmpty
import com.inspiredandroid.kai.ui.chat.WorkFeedItem
import com.inspiredandroid.kai.ui.components.rememberHaptics
import com.inspiredandroid.kai.ui.handCursor
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.ic_file
import kai.composeapp.generated.resources.work_feed_title
import kotlin.time.Clock
import kotlinx.collections.immutable.ImmutableList
import org.jetbrains.compose.resources.stringResource
import org.jetbrains.compose.resources.vectorResource

/**
 * Bottom-sheet content for the work feed (action inbox). The [ModalBottomSheet]
 * is the container (no Card). The header shows the pending count + a close
 * button, each row shows a relative timestamp, and actions use a distinct icon
 * per kind. Lists every item in a scrollable LazyColumn (no 5-item cap) and
 * shows an empty state when there is nothing pending.
 */
@Composable
internal fun WorkFeedPanel(
    items: ImmutableList<WorkFeedItem>,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
    onClose: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .navigationBarsPadding(),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(start = 16.dp, top = 8.dp, end = 4.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = vectorResource(Res.drawable.ic_file),
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(18.dp),
            )
            Text(
                text = stringResource(Res.string.work_feed_title),
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onBackground,
                modifier = Modifier.padding(start = 8.dp),
            )
            if (items.isNotEmpty()) {
                Text(
                    text = items.size.toString(),
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 6.dp),
                )
            }
            Spacer(Modifier.weight(1f))
            IconButton(modifier = Modifier.handCursor(), onClick = onClose) {
                Icon(
                    imageVector = Icons.Filled.Close,
                    contentDescription = "닫기",
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (items.isEmpty()) {
            DenebEmpty("아직 업무 알림이 없어요")
        } else {
            // Cap the height so a long feed scrolls inside the sheet instead of
            // pushing the sheet past the screen.
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 520.dp),
            ) {
                // No key: the feed can carry duplicate item ids (server-side), and a
                // duplicate LazyColumn key crashes. Position-based identity is fine
                // for a short, rebuilt-on-refresh list.
                itemsIndexed(items) { index, item ->
                    if (index > 0) {
                        HorizontalDivider(
                            modifier = Modifier.padding(start = 16.dp),
                            color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f),
                        )
                    }
                    WorkFeedRow(item = item, onOpen = onOpen, onRunAction = onRunAction)
                }
            }
        }
    }
}

@Composable
private fun WorkFeedRow(
    item: WorkFeedItem,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
) {
    val title = if (item.title.isBlank()) stringResource(Res.string.work_feed_title) else item.title
    val haptics = rememberHaptics()
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .handCursor()
            .clickable { haptics.tap(); onOpen(item.id) }
            .padding(horizontal = 16.dp, vertical = 10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = title,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onBackground,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            val stamp = relativeTime(item.createdAtMs)
            if (stamp.isNotEmpty()) {
                Text(
                    text = stamp,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
        }
        if (item.summary.isNotBlank()) {
            Text(
                text = item.summary,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        WorkFeedActions(
            item = item,
            onRunAction = onRunAction,
            modifier = Modifier.padding(top = 4.dp),
        )
    }
}

@Composable
private fun WorkFeedActions(
    item: WorkFeedItem,
    onRunAction: (String, String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val actions = item.actions
        .filter { it.kind != "open" && it.id.isNotBlank() }
        .take(3)
    val haptics = rememberHaptics()
    if (actions.isEmpty()) return

    // Icon-only and trailing: keep the quick actions but drop the per-button
    // labels that turned every row into a wall of buttons. Each label survives
    // as the icon's accessibility description.
    Row(
        modifier = modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.End,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        actions.forEach { action ->
            IconButton(
                modifier = Modifier.handCursor(),
                onClick = { haptics.confirm(); onRunAction(item.id, action.id) },
            ) {
                Icon(
                    imageVector = actionIcon(action.kind),
                    contentDescription = action.label.ifBlank { action.kind },
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
    }
}

private fun actionIcon(kind: String): ImageVector = when (kind) {
    "ack" -> Icons.Filled.Check
    "snooze" -> Icons.Filled.Schedule
    "followup" -> Icons.Filled.ArrowForward
    else -> Icons.Filled.Check
}

/** Short Korean relative time ("방금" / "N분 전" / "N시간 전" / "N일 전"). Blank for
 *  missing/future timestamps so the row simply omits the stamp. */
private fun relativeTime(epochMs: Long): String {
    if (epochMs <= 0L) return ""
    val diff = Clock.System.now().toEpochMilliseconds() - epochMs
    return when {
        diff < 0L -> ""
        diff < 60_000L -> "방금"
        diff < 3_600_000L -> "${diff / 60_000L}분 전"
        diff < 86_400_000L -> "${diff / 3_600_000L}시간 전"
        else -> "${diff / 86_400_000L}일 전"
    }
}
