package ai.deneb.ui.chat.composables

import ai.deneb.deneb.DenebEmpty
import ai.deneb.ui.DenebRow
import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import ai.deneb.ui.denebPressable
import ai.deneb.ui.handCursor
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.MailOutline
import androidx.compose.material3.HorizontalDivider
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
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.ic_file
import deneb.composeapp.generated.resources.ic_image
import deneb.composeapp.generated.resources.work_feed_title
import kotlinx.collections.immutable.ImmutableList
import org.jetbrains.compose.resources.painterResource
import org.jetbrains.compose.resources.stringResource
import kotlin.time.Clock

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
                style = DenebType.subject,
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
            DenebEmpty("아직 업무 알림이 없습니다")
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
internal fun WorkFeedRow(
    item: WorkFeedItem,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
    expanded: Boolean = false,
    onLongAction: ((WorkFeedItem) -> Unit)? = null,
) {
    // The row already leads with a source icon, so a "📬 …" title would show two
    // icons side by side — strip the leading emoji/symbol run from the title.
    val title = if (item.title.isBlank()) stringResource(Res.string.work_feed_title) else stripLeadingIcon(item.title)
    val haptics = rememberHaptics()
    val titleStyle = if (item.status == "unread") DenebType.rowTitleStrong else DenebType.rowTitle
    DenebRow(
        onClick = {
            haptics.tap()
            onOpen(item.id)
        },
        onLongClick = onLongAction?.let {
            {
                haptics.longPress()
                it(item)
            }
        },
        modifier = Modifier.padding(horizontal = 12.dp),
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
                // Summary spans the full row width — the quick actions no longer share
                // its line, so it wraps cleanly and shows more of the snippet.
                if (item.summary.isNotBlank()) {
                    Text(
                        text = item.summary,
                        style = DenebType.snippet,
                        color = denebHint(),
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
                    )
                }
                // Quick actions appear only while the card is open, so collapsed rows
                // stay clean (icon · title · time · full-width summary). 보관 = archive
                // (ack → 읽음 section), 휴지통 = permanent delete; both ride onRunAction
                // (the gateway handles "trash" as a universal delete).
                if (expanded) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
                        horizontalArrangement = Arrangement.End,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        FeedActionButton(Icons.Outlined.Archive, "보관") {
                            haptics.confirm()
                            onRunAction(item.id, "ack")
                        }
                        FeedActionButton(Icons.Outlined.Delete, "휴지통") {
                            haptics.confirm()
                            onRunAction(item.id, "trash")
                        }
                    }
                }
            }
        }
    }
}

@Composable
internal fun WorkFeedActionSheetContent(
    item: WorkFeedItem,
    onOpen: () -> Unit,
    onRunAction: (String) -> Unit,
    onArchive: () -> Unit,
    onTrash: () -> Unit,
) {
    val title = if (item.title.isBlank()) stringResource(Res.string.work_feed_title) else stripLeadingIcon(item.title)
    val extraActions = item.actions.filter { action ->
        action.id.isNotBlank() &&
            action.id !in setOf("open", "ack", "trash") &&
            action.label.isNotBlank()
    }
    val archiveLabel = item.actions.firstOrNull { it.id == "ack" }?.label?.ifBlank { null } ?: "보관"
    val trashLabel = item.actions.firstOrNull { it.id == "trash" }?.label?.ifBlank { null } ?: "휴지통"
    Column(Modifier.fillMaxWidth().padding(bottom = 24.dp)) {
        Text(
            title,
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(horizontal = 24.dp, vertical = 12.dp),
        )
        if (item.summary.isNotBlank()) {
            Text(
                item.summary,
                style = DenebType.snippet,
                color = denebHint(),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(horizontal = 24.dp).padding(bottom = 12.dp),
            )
        }
        HorizontalDivider(color = denebHairline())
        WorkFeedSheetAction(Icons.Outlined.MailOutline, "열기", onOpen = onOpen)
        extraActions.forEach { action ->
            WorkFeedSheetAction(Icons.Outlined.AutoAwesome, action.label, onOpen = { onRunAction(action.id) })
        }
        WorkFeedSheetAction(Icons.Outlined.Archive, archiveLabel, onOpen = onArchive)
        WorkFeedSheetAction(Icons.Outlined.Delete, trashLabel, destructive = true, onOpen = onTrash)
    }
}

@Composable
private fun WorkFeedSheetAction(
    icon: ImageVector,
    label: String,
    destructive: Boolean = false,
    onOpen: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .denebPressable(onClick = onOpen)
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val color = if (destructive) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary
        Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(22.dp))
        Spacer(Modifier.width(16.dp))
        Text(label, style = DenebType.rowTitle, color = if (destructive) color else MaterialTheme.colorScheme.onBackground)
    }
}

/** Drop a leading emoji/symbol run from a card title so it isn't shown twice next to
 *  the row's source icon ("📬 메일 분석" → "메일 분석"). Stops at the first letter/digit
 *  (Hangul/Latin/CJK/number); returns the original if stripping would empty it. */
private fun stripLeadingIcon(s: String): String {
    var i = 0
    while (i < s.length && !s[i].isLetterOrDigit()) i++
    return s.substring(i).trimStart().ifBlank { s }
}

/** Leading icon by card source: an envelope for mail reports, a generic report
 *  page for other proactive briefings, and a concrete glyph for each capture kind
 *  (image / audio / contacts). */
@Composable
private fun sourcePainter(source: String): Painter = when (source) {
    "mail_report" -> rememberVectorPainter(Icons.Outlined.MailOutline)
    "capture_image" -> painterResource(Res.drawable.ic_image)
    "capture_audio" -> rememberVectorPainter(Icons.Filled.Mic)
    "capture_contacts" -> rememberVectorPainter(Icons.Filled.Person)
    else -> painterResource(Res.drawable.ic_file)
}

/** A compact trailing quick-action icon button (보관 / 휴지통), muted to denebHint. */
@Composable
private fun FeedActionButton(icon: ImageVector, label: String, onClick: () -> Unit) {
    IconButton(
        modifier = Modifier.handCursor().size(32.dp),
        onClick = onClick,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = label,
            tint = denebHint(),
            modifier = Modifier.size(16.dp),
        )
    }
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
