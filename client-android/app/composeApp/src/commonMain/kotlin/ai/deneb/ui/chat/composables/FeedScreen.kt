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
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.minus
import kotlinx.datetime.plus
import kotlinx.datetime.toInstant
import kotlinx.datetime.toLocalDateTime
import kotlinx.datetime.todayIn
import kotlin.time.Clock
import kotlin.time.Instant

private const val EmptyFeedLookbackDays = 31

/**
 * The 업무 (work) home: the work feed as the main screen rather than a modal behind
 * the chat. A date stepper under the title scopes the feed to one day at a time —
 * ← / → move by calendar day, even when the selected day is empty. Within the
 * selected day, unread items sit on top; tapping one marks it 읽음 (seen,
 * client-side — distinct from the server ack the action buttons do) and expands its
 * full body inline, so the report is read here instead of being mirrored into the
 * chat transcript. Read items collect in a section at the bottom.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun FeedScreen(
    items: ImmutableList<WorkFeedItem>,
    loaded: Boolean,
    seenIds: Set<String>,
    onMarkSeen: (String) -> Unit,
    onLoadDateRange: (Long, Long) -> Unit,
    onRunAction: (String, String) -> Unit,
    onAnswer: (WorkFeedItem, String, String?) -> Unit,
    onSubmitFeedback: (String, String) -> Unit,
    onRewrite: (String) -> Unit,
    onAsk: (String) -> Unit,
) {
    DenebScreenScaffold(title = "피드", onBack = {}, showBack = false) {
        // Keep the selected date independent of the loaded item list. A ranged fetch
        // for today can legitimately return zero items; if selectedDate were derived
        // from items, the empty response would remove the date bar and trap the user
        // on today with no way to request yesterday.
        val tz = remember { TimeZone.currentSystemDefault() }
        val today = remember { Clock.System.todayIn(tz) }
        val dates = remember(items) { items.map { localDateOf(it.createdAtMs) } }
        var selectedDate by remember { mutableStateOf(today) }
        val nav = feedDateNavState(selectedDate, today, dates)
        LaunchedEffect(selectedDate) {
            onLoadDateRange(
                dayStartMs(selectedDate, tz),
                dayStartMs(selectedDate.plus(1, DateTimeUnit.DAY), tz),
            )
        }

        FeedDateBar(
            label = feedDateLabel(selectedDate, today),
            canGoPrev = nav.canGoPrev,
            canGoNext = nav.canGoNext,
            onPrev = { if (nav.canGoPrev) selectedDate = selectedDate.minus(1, DateTimeUnit.DAY) },
            onNext = { if (nav.canGoNext) selectedDate = selectedDate.plus(1, DateTimeUnit.DAY) },
        )

        if (items.isEmpty()) {
            // Before the first fetch finishes, show the skeleton instead of "no feed"
            // so a cold launch into the 업무 home doesn't flash an empty state.
            if (loaded) DenebEmpty(feedEmptyLabel(selectedDate, today)) else DenebLoading()
            return@DenebScreenScaffold
        }

        var expandedId by remember { mutableStateOf<String?>(null) }
        // Partition by a snapshot of seenIds taken when the feed's items load, not
        // live: tapping a row marks it seen (onMarkSeen) and expands it inline, and a
        // live re-partition would yank the tapped item from 안읽음 (top) down into the
        // 읽음 section mid-tap, so it expanded out of view and couldn't be read. Read
        // items re-sort into 읽음 the next time the feed's data reloads.
        val seenSnapshot = remember(items) { seenIds }
        val dayItems = items.filter { localDateOf(it.createdAtMs) == selectedDate }
        // Read = opened on this device (seen-set) OR on any device (gateway readAtMs,
        // arrives via List/sync) — so a card read on the desktop reads here too. The
        // seen-set stays snapshotted so the just-tapped row doesn't yank mid-tap.
        val unread = dayItems.filterNot { seenSnapshot.contains(it.id) || it.readAtMs > 0L }
        val read = dayItems.filter { seenSnapshot.contains(it.id) || it.readAtMs > 0L }
        var actionItem by remember { mutableStateOf<WorkFeedItem?>(null) }
        var feedbackItem by remember { mutableStateOf<WorkFeedItem?>(null) }

        val open: (String) -> Unit = { id ->
            expandedId = if (expandedId == id) null else id
            onMarkSeen(id)
        }

        if (dayItems.isEmpty()) {
            DenebEmpty(feedEmptyLabel(selectedDate, today))
        } else {
            LazyColumn(Modifier.fillMaxWidth().weight(1f)) {
                items(unread.size) { i ->
                    FeedRowWithBody(unread[i], expandedId == unread[i].id, open, onRunAction, onAnswer) { actionItem = it }
                }
                if (read.isNotEmpty()) {
                    item { DenebSectionLabel("읽음", Modifier.padding(start = 12.dp)) }
                    items(read.size) { i ->
                        FeedRowWithBody(read[i], expandedId == read[i].id, open, onRunAction, onAnswer) { actionItem = it }
                    }
                }
            }
        }

        actionItem?.let { item ->
            ModalBottomSheet(onDismissRequest = { actionItem = null }) {
                WorkFeedActionSheetContent(
                    item = item,
                    onFeedback = {
                        actionItem = null
                        feedbackItem = item
                    },
                    onRewrite = {
                        actionItem = null
                        onRewrite(item.id)
                    },
                    onAsk = {
                        actionItem = null
                        onAsk(item.id)
                    },
                )
            }
        }

        feedbackItem?.let { item ->
            ModalBottomSheet(onDismissRequest = { feedbackItem = null }) {
                WorkFeedFeedbackSheetContent(
                    item = item,
                    onSubmit = { text -> onSubmitFeedback(item.id, text) },
                    onClose = { feedbackItem = null },
                )
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
        Modifier.fillMaxWidth().padding(start = 12.dp, end = 12.dp, top = 0.dp, bottom = 4.dp),
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
        // Wide-but-short hit area: keeps ← / → easy to tap horizontally while trimming
        // the bar's height (the 40dp square made the date band look too tall for one line).
        modifier = Modifier
            .size(width = 40.dp, height = 32.dp)
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

internal data class FeedDateNavState(
    val canGoPrev: Boolean,
    val canGoNext: Boolean,
)

internal fun feedDateNavState(
    selectedDate: LocalDate,
    today: LocalDate,
    loadedDates: List<LocalDate>,
): FeedDateNavState {
    val earliestLoaded = loadedDates.minOrNull()
    val latestLoaded = loadedDates.maxOrNull()
    val fallbackMinDate = today.minus(EmptyFeedLookbackDays, DateTimeUnit.DAY)
    val minDate = minOf(fallbackMinDate, earliestLoaded ?: fallbackMinDate)
    val maxDate = maxOf(today, latestLoaded ?: today)
    return FeedDateNavState(
        canGoPrev = selectedDate > minDate,
        canGoNext = selectedDate < maxDate,
    )
}

/** The local calendar day a feed item was created on. */
private fun localDateOf(epochMs: Long): LocalDate = Instant.fromEpochMilliseconds(epochMs).toLocalDateTime(TimeZone.currentSystemDefault()).date

/** Local day boundary for a server-side work-feed date query. */
private fun dayStartMs(date: LocalDate, tz: TimeZone): Long = LocalDateTime(date, LocalTime(0, 0)).toInstant(tz).toEpochMilliseconds()

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

private fun feedEmptyLabel(date: LocalDate, today: LocalDate): String = when (date) {
    today -> "오늘 받은 피드가 없습니다"
    today.minus(1, DateTimeUnit.DAY) -> "어제 받은 피드가 없습니다"
    else -> "이 날 받은 피드가 없습니다"
}

@Composable
private fun FeedRowWithBody(
    item: WorkFeedItem,
    expanded: Boolean,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
    onAnswer: (WorkFeedItem, String, String?) -> Unit,
    onLongAction: (WorkFeedItem) -> Unit,
) {
    WorkFeedRow(item = item, onOpen = onOpen, onRunAction = onRunAction, expanded = expanded, onLongAction = onLongAction)
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
                    .padding(start = 12.dp, end = 12.dp, top = 4.dp, bottom = 12.dp),
            )
        }
    }
    // A question card the agent is waiting on: inline answer chips / reply field.
    if (expanded && item.question) {
        WorkFeedAnswerBlock(item = item, onAnswer = onAnswer)
    }
}
