package ai.deneb.ui.chat.composables

import ai.deneb.deneb.DenebEmpty
import ai.deneb.deneb.DenebLoading
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.MarkdownContent
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.collections.immutable.ImmutableList
import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.minus
import kotlinx.datetime.plus
import kotlinx.datetime.toLocalDateTime
import kotlinx.datetime.todayIn
import kotlin.time.Clock
import kotlin.time.Instant

/**
 * The 업무 (work) home: the work feed as the main screen rather than a modal behind
 * the chat. A date stepper under the title scopes the feed to one day at a time —
 * ← / → move between the days that actually have items (newest day first). Within
 * the selected day, unread items sit on top; tapping one marks it 읽음 (seen,
 * client-side — distinct from the server ack the action buttons do) and expands its
 * full body inline, so the report is read here instead of being mirrored into the
 * chat transcript. Read items collect in a section at the bottom.
 */
@Composable
internal fun FeedScreen(
    items: ImmutableList<WorkFeedItem>,
    loaded: Boolean,
    seenIds: Set<String>,
    onMarkSeen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
) {
    DenebScreenScaffold(title = "피드", onBack = {}, showBack = false) {
        if (items.isEmpty()) {
            // Before the first fetch finishes, show the skeleton instead of "no feed"
            // so a cold launch into the 업무 home doesn't flash an empty state.
            if (loaded) DenebEmpty("오늘 받은 피드가 없습니다") else DenebLoading()
            return@DenebScreenScaffold
        }

        // The feed spans several days of as-yet-unacked items. Group by local day and
        // step through the days that have items: default to the newest (so the screen
        // opens on content), bound navigation to [oldest, newest] so ← / → never page
        // into empty pre-/post-history. selectedDate resets to the newest day whenever
        // a newer item arrives (maxDate advances), but survives a same-day refresh.
        val today = remember { Clock.System.todayIn(TimeZone.currentSystemDefault()) }
        val dates = remember(items) { items.map { localDateOf(it.createdAtMs) } }
        val minDate = dates.minOrNull() ?: today
        val maxDate = dates.maxOrNull() ?: today
        var selectedDate by remember(maxDate) { mutableStateOf(maxDate) }

        FeedDateBar(
            label = feedDateLabel(selectedDate, today),
            canGoPrev = selectedDate > minDate,
            canGoNext = selectedDate < maxDate,
            onPrev = { if (selectedDate > minDate) selectedDate = selectedDate.minus(1, DateTimeUnit.DAY) },
            onNext = { if (selectedDate < maxDate) selectedDate = selectedDate.plus(1, DateTimeUnit.DAY) },
        )

        var expandedId by remember { mutableStateOf<String?>(null) }
        // Partition by a snapshot of seenIds taken when the feed's items load, not
        // live: tapping a row marks it seen (onMarkSeen) and expands it inline, and a
        // live re-partition would yank the tapped item from 안읽음 (top) down into the
        // 읽음 section mid-tap, so it expanded out of view and couldn't be read. Read
        // items re-sort into 읽음 the next time the feed's data reloads.
        val seenSnapshot = remember(items) { seenIds }
        val dayItems = items.filter { localDateOf(it.createdAtMs) == selectedDate }
        val unread = dayItems.filterNot { seenSnapshot.contains(it.id) }
        val read = dayItems.filter { seenSnapshot.contains(it.id) }

        val open: (String) -> Unit = { id ->
            expandedId = if (expandedId == id) null else id
            onMarkSeen(id)
        }

        if (dayItems.isEmpty()) {
            DenebEmpty("이 날 받은 피드가 없습니다")
        } else {
            LazyColumn(Modifier.fillMaxWidth().weight(1f)) {
                items(unread.size) { i ->
                    FeedRowWithBody(unread[i], expandedId == unread[i].id, open, onRunAction)
                }
                if (read.isNotEmpty()) {
                    item { DenebSectionLabel("읽음") }
                    items(read.size) { i ->
                        FeedRowWithBody(read[i], expandedId == read[i].id, open, onRunAction)
                    }
                }
            }
        }
    }
}

/**
 * Date stepper for the feed: ← [날짜 (요일)] →. Arrows dim and stop responding at
 * the ends of the available range. The label reads "오늘 · 6월 16일 (월)" for
 * today/yesterday, or the bare "6월 16일 (월)" for older days.
 */
@Composable
private fun FeedDateBar(
    label: String,
    canGoPrev: Boolean,
    canGoNext: Boolean,
    onPrev: () -> Unit,
    onNext: () -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().padding(start = 24.dp, end = 24.dp, top = 2.dp, bottom = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        FeedDateArrow(Icons.AutoMirrored.Filled.KeyboardArrowLeft, "이전 날", canGoPrev, onPrev)
        Text(
            text = label,
            style = DenebType.rowTitle,
            color = MaterialTheme.colorScheme.onBackground,
            textAlign = TextAlign.Center,
            modifier = Modifier.weight(1f),
        )
        FeedDateArrow(Icons.AutoMirrored.Filled.KeyboardArrowRight, "다음 날", canGoNext, onNext)
    }
}

@Composable
private fun FeedDateArrow(
    icon: ImageVector,
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .size(40.dp)
            .clickable(enabled = enabled, onClickLabel = label, role = Role.Button, onClick = onClick)
            .handCursor(),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = label,
            tint = if (enabled) denebHint() else denebHint().copy(alpha = 0.25f),
            modifier = Modifier.size(22.dp),
        )
    }
}

/** Korean short weekday, indexed by LocalDate.dayOfWeek.ordinal (0 = Monday). */
private val koreanWeekday = listOf("월", "화", "수", "목", "금", "토", "일")

/** The local calendar day a feed item was created on. */
private fun localDateOf(epochMs: Long): LocalDate = Instant.fromEpochMilliseconds(epochMs).toLocalDateTime(TimeZone.currentSystemDefault()).date

/** "오늘 · 6월 16일 (월)" / "어제 · …" / bare "6월 16일 (월)" for older days. */
private fun feedDateLabel(date: LocalDate, today: LocalDate): String {
    val dow = koreanWeekday.getOrElse(date.dayOfWeek.ordinal) { "" }
    val md = "${date.month.ordinal + 1}월 ${date.day}일 ($dow)"
    return when (date) {
        today -> "오늘 · $md"
        today.minus(1, DateTimeUnit.DAY) -> "어제 · $md"
        else -> md
    }
}

@Composable
private fun FeedRowWithBody(
    item: WorkFeedItem,
    expanded: Boolean,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
) {
    WorkFeedRow(item = item, onOpen = onOpen, onRunAction = onRunAction)
    if (expanded && item.body.isNotBlank()) {
        // Proactive reports are markdown (tables, headings, lists), so render with
        // the full chat renderer — a plain Text leaked raw "| 항목 | 내용 |" pipes and
        // "##" markers (broken tables). Read-only (isInteractive = false); wrapped in
        // SelectionContainer so the report stays copyable.
        SelectionContainer {
            MarkdownContent(
                content = item.body,
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 20.dp, end = 20.dp, top = 4.dp, bottom = 12.dp),
            )
        }
    }
}
