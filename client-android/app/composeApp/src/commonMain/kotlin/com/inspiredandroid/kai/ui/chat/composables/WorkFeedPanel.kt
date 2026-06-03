package com.inspiredandroid.kai.ui.chat.composables

import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.deneb.DenebEmpty
import com.inspiredandroid.kai.ui.chat.WorkFeedAction
import com.inspiredandroid.kai.ui.chat.WorkFeedItem
import com.inspiredandroid.kai.ui.handCursor
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.ic_close
import kai.composeapp.generated.resources.ic_file
import kai.composeapp.generated.resources.work_feed_title
import kotlinx.collections.immutable.ImmutableList
import org.jetbrains.compose.resources.stringResource
import org.jetbrains.compose.resources.vectorResource

/**
 * Bottom-sheet content for the work feed (action inbox). The [ModalBottomSheet]
 * is the container, so there is no Card wrapper here. Lists every item in a
 * scrollable LazyColumn (no 5-item cap) and shows an empty state when there is
 * nothing pending.
 */
@Composable
internal fun WorkFeedPanel(
    items: ImmutableList<WorkFeedItem>,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .navigationBarsPadding(),
    ) {
        Row(
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 12.dp),
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
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .handCursor()
            .clickable { onOpen(item.id) }
            .padding(horizontal = 16.dp, vertical = 10.dp),
    ) {
        Text(
            text = title,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
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
    if (actions.isEmpty()) return

    Row(
        modifier = modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState()),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        actions.forEach { action ->
            WorkFeedActionButton(
                action = action,
                onClick = { onRunAction(item.id, action.id) },
            )
            Spacer(Modifier.width(4.dp))
        }
    }
}

@Composable
private fun WorkFeedActionButton(
    action: WorkFeedAction,
    onClick: () -> Unit,
) {
    val icon = if (action.kind == "ack" || action.kind == "snooze") {
        Res.drawable.ic_close
    } else {
        Res.drawable.ic_file
    }
    TextButton(
        modifier = Modifier.handCursor(),
        onClick = onClick,
    ) {
        Icon(
            imageVector = vectorResource(icon),
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(14.dp),
        )
        Spacer(Modifier.width(4.dp))
        Text(
            text = action.label.ifBlank { action.kind },
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
