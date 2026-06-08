package com.inspiredandroid.kai.ui.chat.composables

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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowForward
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.inspiredandroid.kai.deneb.DenebEmpty
import com.inspiredandroid.kai.ui.DenebRow
import com.inspiredandroid.kai.ui.DenebType
import com.inspiredandroid.kai.ui.chat.WorkFeedItem
import com.inspiredandroid.kai.ui.components.rememberHaptics
import com.inspiredandroid.kai.ui.denebHint
import com.inspiredandroid.kai.ui.handCursor
import kai.composeapp.generated.resources.Res
import kai.composeapp.generated.resources.ic_file
import kai.composeapp.generated.resources.ic_image
import kai.composeapp.generated.resources.work_feed_title
import kotlin.time.Clock
import kotlinx.collections.immutable.ImmutableList
import org.jetbrains.compose.resources.painterResource
import org.jetbrains.compose.resources.stringResource

/**
 * Bottom-sheet content for the work feed (action inbox), in the Deneb idiom:
 * typography on a flat surface (no card), [DenebRow] hairlines instead of
 * dividers, and Deneb type roles instead of Material's. Each row leads with a
 * source icon (mail report / image / audio / contacts), an unread item gets the
 * strong title weight, and the quick actions are a compact trailing icon row.
 * Lists every item in a scrollable LazyColumn (no cap); shows an empty state
 * when nothing is pending.
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
        // Header: Deneb subject title + pending count; close stays a Material control.
        Row(
            modifier = Modifier.fillMaxWidth().padding(start = 20.dp, top = 10.dp, end = 4.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(Res.string.work_feed_title),
                style = DenebType.subject.copy(fontSize = 20.sp),
                color = MaterialTheme.colorScheme.onBackground,
            )
            if (items.isNotEmpty()) {
                Text(
                    text = items.size.toString(),
                    style = DenebType.meta,
                    color = denebHint(),
                    modifier = Modifier.padding(start = 8.dp),
                )
            }
            Spacer(Modifier.weight(1f))
            IconButton(modifier = Modifier.handCursor(), onClick = onClose) {
                Icon(
                    imageVector = Icons.Filled.Close,
                    contentDescription = "닫기",
                    tint = denebHint(),
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
                itemsIndexed(items) { _, item ->
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
    val titleStyle = if (item.status == "unread") DenebType.rowTitleStrong else DenebType.rowTitle
    val actions = item.actions
        .filter { it.kind != "open" && it.id.isNotBlank() }
        .take(3)
    DenebRow(
        onClick = { haptics.tap(); onOpen(item.id) },
        modifier = Modifier.padding(horizontal = 20.dp),
    ) {
        Row(verticalAlignment = Alignment.Top) {
            Icon(
                painter = sourcePainter(item.source),
                contentDescription = null,
                tint = denebHint(),
                modifier = Modifier.padding(top = 1.dp).size(18.dp),
            )
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = title,
                        style = titleStyle,
                        color = MaterialTheme.colorScheme.onBackground,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    val stamp = relativeTime(item.createdAtMs)
                    if (stamp.isNotEmpty()) {
                        Text(
                            text = stamp,
                            style = DenebType.meta,
                            color = denebHint(),
                            modifier = Modifier.padding(start = 8.dp),
                        )
                    }
                }
                // Summary and quick actions share one row: the summary takes the
                // width, the actions sit at the trailing edge. This drops the
                // separate action line that was wasting row height.
                if (item.summary.isNotBlank() || actions.isNotEmpty()) {
                    Row(
                        modifier = Modifier.padding(top = 2.dp),
                        verticalAlignment = Alignment.Top,
                    ) {
                        if (item.summary.isNotBlank()) {
                            Text(
                                text = item.summary,
                                style = DenebType.snippet,
                                color = denebHint(),
                                maxLines = 2,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier
                                    .weight(1f)
                                    .padding(top = 4.dp, end = if (actions.isNotEmpty()) 4.dp else 0.dp),
                            )
                        } else {
                            Spacer(Modifier.weight(1f))
                        }
                        actions.forEach { action ->
                            IconButton(
                                modifier = Modifier.handCursor().size(32.dp),
                                onClick = { haptics.confirm(); onRunAction(item.id, action.id) },
                            ) {
                                Icon(
                                    imageVector = actionIcon(action.kind),
                                    contentDescription = action.label.ifBlank { action.kind },
                                    tint = denebHint(),
                                    modifier = Modifier.size(16.dp),
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

/** Leading icon by card source: a report for proactive briefings, and a concrete
 *  glyph for each capture kind (image / audio / contacts). */
@Composable
private fun sourcePainter(source: String): Painter = when (source) {
    "capture_image" -> painterResource(Res.drawable.ic_image)
    "capture_audio" -> rememberVectorPainter(Icons.Filled.Mic)
    "capture_contacts" -> rememberVectorPainter(Icons.Filled.Person)
    else -> painterResource(Res.drawable.ic_file)
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
