package com.inspiredandroid.kai.ui.chat.composables

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.chat.WorkFeedItem
import com.inspiredandroid.kai.ui.handCursor
import com.inspiredandroid.kai.ui.kaiAdaptiveCardBorder
import com.inspiredandroid.kai.ui.kaiAdaptiveCardColors
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.ic_close
import kai.composeapp.generated.resources.ic_file
import kai.composeapp.generated.resources.work_feed_ack_content_description
import kai.composeapp.generated.resources.work_feed_title
import kotlinx.collections.immutable.ImmutableList
import org.jetbrains.compose.resources.stringResource
import org.jetbrains.compose.resources.vectorResource

@Composable
internal fun WorkFeedPanel(
    items: ImmutableList<WorkFeedItem>,
    onOpen: (String) -> Unit,
    onAck: (String) -> Unit,
) {
    if (items.isEmpty()) return

    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 6.dp),
        colors = kaiAdaptiveCardColors(),
        border = kaiAdaptiveCardBorder(),
    ) {
        Column(Modifier.fillMaxWidth()) {
            Row(
                modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
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
                    style = MaterialTheme.typography.labelLarge,
                    color = MaterialTheme.colorScheme.onBackground,
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
            items.take(5).forEachIndexed { index, item ->
                if (index > 0) {
                    HorizontalDivider(
                        modifier = Modifier.padding(start = 42.dp),
                        color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.5f),
                    )
                }
                WorkFeedRow(item = item, onOpen = onOpen, onAck = onAck)
            }
        }
    }
}

@Composable
private fun WorkFeedRow(
    item: WorkFeedItem,
    onOpen: (String) -> Unit,
    onAck: (String) -> Unit,
) {
    val title = if (item.title.isBlank()) stringResource(Res.string.work_feed_title) else item.title
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .handCursor()
            .clickable { onOpen(item.id) }
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(
            modifier = Modifier
                .weight(1f)
                .padding(end = 8.dp),
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
        }
        IconButton(
            modifier = Modifier.size(28.dp).handCursor(),
            onClick = { onAck(item.id) },
        ) {
            Icon(
                imageVector = vectorResource(Res.drawable.ic_close),
                contentDescription = stringResource(Res.string.work_feed_ack_content_description),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(16.dp),
            )
        }
    }
}
