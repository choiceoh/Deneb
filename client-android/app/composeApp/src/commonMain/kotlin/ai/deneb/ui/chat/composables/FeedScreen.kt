package ai.deneb.ui.chat.composables

import ai.deneb.consumeFeedItemOpen
import ai.deneb.deneb.DenebEmpty
import ai.deneb.deneb.DenebLoading
import ai.deneb.ui.DenebFeedApprovalPage
import ai.deneb.ui.DenebFeedApprovalPivots
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebSiblingSwipeHost
import ai.deneb.ui.DenebType
import ai.deneb.ui.chat.WorkFeedAction
import ai.deneb.ui.chat.WorkFeedItem
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebBannerEnter
import ai.deneb.ui.denebBannerExit
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import ai.deneb.ui.markdown.MarkdownContent
import ai.deneb.ui.rememberToday
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.collections.immutable.ImmutableList
import kotlinx.coroutines.launch
import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.minus
import kotlinx.datetime.plus
import kotlinx.datetime.toInstant
import kotlinx.datetime.toLocalDateTime
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
    onLoadDateRange: suspend (Long, Long) -> Boolean,
    onRunAction: (String, String) -> Unit,
    onAnswer: (WorkFeedItem, String, String?, String?) -> Unit,
    onSubmitFeedback: (String, String) -> Unit,
    onRewrite: (String) -> Unit,
    onAsk: (String) -> Unit,
    initialOpenItemId: String? = null,
    initialOpenItemCreatedAtMs: Long = 0L,
    // Bumped by the host per open request. The screen stays alive across tab
    // switches (LiveTabPane), so re-opening the SAME item needs a fresh key to
    // re-arm the rememberSaveable consumption below.
    openRequestKey: Long = 0L,
    onOpenApprovals: (() -> Unit)? = null,
    onOpenLog: (() -> Unit)? = null,
    onOpenFeed: (() -> Unit)? = null,
    lane: FeedLane = FeedLane.Work,
    // groupware-approval cards: open the Amaranth detail for item.refId.
    onOpenApprovalDetail: ((docId: String, title: String) -> Unit)? = null,
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val haptics = rememberHaptics()
    val laneItems = remember(items, lane) { items.forFeedLane(lane) }
    // Bare destination lambdas: DenebTitlePivot taps for the header labels and the
    // swipe host fires its own arm tick, so neither needs (nor may have) a tap here.
    DenebSiblingSwipeHost(
        onSwipeLeft = when (lane) {
            FeedLane.Work -> onOpenApprovals
            FeedLane.Log -> null
        },
        onSwipeRight = when (lane) {
            FeedLane.Work -> null
            FeedLane.Log -> onOpenApprovals
        },
    ) {
        DenebScreenScaffold(
            title = if (lane == FeedLane.Log) "로그" else "피드",
            onBack = {},
            showBack = false,
            tabBar = navigationTabBar,
            titleContent = {
                DenebFeedApprovalPivots(
                    active = if (lane == FeedLane.Log) DenebFeedApprovalPage.Log else DenebFeedApprovalPage.Feed,
                    onOpenFeed = onOpenFeed,
                    onOpenApprovals = onOpenApprovals,
                    onOpenLog = onOpenLog,
                )
            },
        ) {
            // Keep the selected date independent of the loaded item list. A ranged fetch
            // for today can legitimately return zero items; if selectedDate were derived
            // from items, the empty response would remove the date bar and trap the user
            // on today with no way to request yesterday.
            val tz = remember { TimeZone.currentSystemDefault() }
            val today = rememberToday(tz)
            val dates = remember(laneItems) { laneItems.map { localDateOf(it.createdAtMs) } }
            val initialDate = remember(initialOpenItemId, initialOpenItemCreatedAtMs, today) {
                if (initialOpenItemId.isNullOrBlank() || initialOpenItemCreatedAtMs <= 0L) {
                    today
                } else {
                    localDateOf(initialOpenItemCreatedAtMs)
                }
            }
            var selectedDateIso by rememberSaveable(initialOpenItemId, openRequestKey) { mutableStateOf(initialDate.toString()) }
            // Follow the midnight rollover: if the user is sitting on what was "오늘" when
            // the day flips, advance to the new today so 오늘 keeps meaning the real today.
            // A manual ← / → leaves selectedDate off last-known-today, so it isn't dragged.
            var lastKnownToday by rememberSaveable { mutableStateOf(today.toString()) }
            LaunchedEffect(today) {
                selectedDateIso = rolledOverSelectedDate(selectedDateIso, lastKnownToday, today.toString())
                lastKnownToday = today.toString()
            }
            val selectedDate = runCatching { LocalDate.parse(selectedDateIso) }.getOrDefault(today)
            var expandedId by rememberSaveable(initialOpenItemId, openRequestKey) { mutableStateOf<String?>(null) }
            var pendingOpenItemId by rememberSaveable(initialOpenItemId, openRequestKey) {
                mutableStateOf(initialOpenItemId?.trim()?.takeIf(String::isNotEmpty))
            }
            val nav = feedDateNavState(selectedDate, today, dates)
            // A day-fetch that fails (boot race, gateway mid-redeploy, VPN waking) must
            // say so — the feed is the 업무 home, and a silent failure reads as "피드
            // 없음" while the server has every card (2026-07-05 field report).
            var loadFailed by remember { mutableStateOf(false) }
            val refreshScope = rememberCoroutineScope()
            val loadSelectedDay: suspend () -> Unit = {
                loadFailed = !onLoadDateRange(
                    dayStartMs(selectedDate, tz),
                    dayStartMs(selectedDate.plus(1, DateTimeUnit.DAY), tz),
                )
            }
            LaunchedEffect(selectedDate) { loadSelectedDay() }

            FeedDateBar(
                label = feedDateLabel(selectedDate, today),
                canGoPrev = nav.canGoPrev,
                canGoNext = nav.canGoNext,
                onPrev = {
                    if (nav.canGoPrev) selectedDateIso = selectedDate.minus(1, DateTimeUnit.DAY).toString()
                },
                onNext = {
                    if (nav.canGoNext) selectedDateIso = selectedDate.plus(1, DateTimeUnit.DAY).toString()
                },
            )

            AnimatedVisibility(visible = loadFailed, enter = denebBannerEnter, exit = denebBannerExit) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp),
                ) {
                    Text(
                        "피드를 불러오지 못했습니다",
                        style = DenebType.meta,
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.weight(1f),
                    )
                    TextButton(onClick = { refreshScope.launch { loadSelectedDay() } }) { Text("다시 시도") }
                }
            }

            // Partition by a snapshot of seenIds taken when the feed's items load, not
            // live: tapping a row marks it seen (onMarkSeen) and expands it inline, and a
            // live re-partition would yank the tapped item from 안읽음 (top) down into the
            // 읽음 section mid-tap, so it expanded out of view and couldn't be read. Read
            // items re-sort into 읽음 the next time the feed's data reloads.
            val seenSnapshot = remember(laneItems) { seenIds }
            val dayItems = laneItems.filter { localDateOf(it.createdAtMs) == selectedDate }
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
            LaunchedEffect(pendingOpenItemId, laneItems) {
                val consumption = consumeFeedItemOpen(pendingOpenItemId, laneItems.map(WorkFeedItem::id))
                pendingOpenItemId = consumption.pendingItemId
                val itemId = consumption.openedItemId ?: return@LaunchedEffect
                val item = laneItems.firstOrNull { it.id == itemId } ?: return@LaunchedEffect
                selectedDateIso = localDateOf(item.createdAtMs).toString()
                expandedId = itemId
                onMarkSeen(itemId)
            }

            // Pull-to-refresh is the feed's user-driven recovery path. The empty state
            // scrolls so the gesture works there too. The spinner tracks the actual
            // fetch (no fixed window), and a failure raises the banner above.
            var refreshing by remember { mutableStateOf(false) }
            PullToRefreshBox(
                isRefreshing = refreshing,
                onRefresh = {
                    haptics.refresh()
                    refreshScope.launch {
                        refreshing = true
                        loadSelectedDay()
                        refreshing = false
                    }
                },
                modifier = Modifier.fillMaxWidth().weight(1f),
            ) {
                when {
                    items.isEmpty() && !loaded -> DenebLoading()

                    dayItems.isEmpty() -> Box(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                        DenebEmpty(
                            feedEmptyLabel(selectedDate, today, lane),
                            icon = Icons.Outlined.Notifications,
                            hint = if (lane == FeedLane.Log) {
                                "모델·자가개선 기록이 여기에 모입니다"
                            } else {
                                "데네브가 분석한 업무 카드가 여기에 도착합니다"
                            },
                        )
                    }

                    else -> LazyColumn(Modifier.fillMaxSize()) {
                        items(unread, key = { it.id }) { item ->
                            FeedRowWithBody(
                                item,
                                expandedId == item.id,
                                open,
                                onRunAction,
                                onAnswer,
                                onOpenApprovalDetail,
                            ) { actionItem = it }
                        }
                        if (read.isNotEmpty()) {
                            item { DenebSectionLabel("읽음", Modifier.padding(start = 12.dp)) }
                            items(read, key = { it.id }) { item ->
                                FeedRowWithBody(
                                    item,
                                    expandedId == item.id,
                                    open,
                                    onRunAction,
                                    onAnswer,
                                    onOpenApprovalDetail,
                                ) { actionItem = it }
                            }
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

// On a midnight rollover the feed follows to the new today ONLY if the user was
// sitting on the previous today (so "오늘" keeps meaning the real today after the app
// sat in memory past midnight); a manually-picked ← / → date is left untouched.
internal fun rolledOverSelectedDate(selectedIso: String, lastKnownTodayIso: String, newTodayIso: String): String = if (selectedIso == lastKnownTodayIso) newTodayIso else selectedIso

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

private fun feedEmptyLabel(date: LocalDate, today: LocalDate, lane: FeedLane = FeedLane.Work): String {
    if (lane == FeedLane.Log) {
        return when (date) {
            today -> "오늘 로그가 없습니다"
            today.minus(1, DateTimeUnit.DAY) -> "어제 로그가 없습니다"
            else -> "이 날 로그가 없습니다"
        }
    }
    return when (date) {
        today -> "오늘 받은 피드가 없습니다"
        today.minus(1, DateTimeUnit.DAY) -> "어제 받은 피드가 없습니다"
        else -> "이 날 받은 피드가 없습니다"
    }
}

@Composable
private fun FeedRowWithBody(
    item: WorkFeedItem,
    expanded: Boolean,
    onOpen: (String) -> Unit,
    onRunAction: (String, String) -> Unit,
    onAnswer: (WorkFeedItem, String, String?, String?) -> Unit,
    onOpenApprovalDetail: ((docId: String, title: String) -> Unit)?,
    onLongAction: (WorkFeedItem) -> Unit,
) {
    val usesInlineApprovalActions = remember(item) { item.hasInlineApprovalActions() }
    // Morning-letter deadline rows carry a longpress="deadline_done" callback:
    // long-press a deadline line in the feed card to mark it handled (writes
    // due_done to the wiki so it stops nagging). Detected from the body marker.
    val usesDeadlineLongPress = remember(item) { item.body.contains("deadline_done") }
    var pendingApproval by remember(item.id) { mutableStateOf<WorkFeedAction?>(null) }
    pendingApproval?.let { action ->
        WorkFeedApprovalDialog(
            item = item,
            action = action,
            onDismiss = { pendingApproval = null },
            onAnswer = onAnswer,
        )
    }
    WorkFeedRow(item = item, onOpen = onOpen, onRunAction = onRunAction, expanded = expanded, onLongAction = onLongAction)
    if (expanded && item.body.isNotBlank()) {
        // Proactive reports are markdown (tables, headings, lists), so render with
        // the full chat renderer — a plain Text leaked raw "| 항목 | 내용 |" pipes and
        // "##" markers (broken tables). Groupware approval cards are the one interactive
        // exception: their inline buttons route into the same guarded action dialog as
        // the fallback chips. Everything else remains read-only and copyable.
        SelectionContainer {
            MarkdownContent(
                content = item.body,
                isInteractive = usesInlineApprovalActions || usesDeadlineLongPress,
                onUiCallback = cb@{ event, data ->
                    // Deadline row long-press → run the matching card action
                    // (deadline_done:<wiki-path>); the gateway stamps due_done.
                    if (event == "deadline_done") {
                        val path = data["path"].orEmpty()
                        if (path.isNotEmpty()) onRunAction(item.id, "deadline_done:$path")
                        return@cb
                    }
                    val actionId = approvalActionIdForUiEvent(event) ?: return@cb
                    pendingApproval = item.actions.firstOrNull { action -> action.id == actionId }
                        ?: return@cb
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 12.dp, end = 12.dp, top = 4.dp, bottom = 12.dp),
            )
        }
    }
    if (expanded && canOpenApprovalDetail(item) && onOpenApprovalDetail != null) {
        val haptics = rememberHaptics()
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = 12.dp, end = 12.dp, bottom = 8.dp),
            horizontalArrangement = Arrangement.End,
        ) {
            TextButton(
                onClick = {
                    haptics.tap()
                    onOpenApprovalDetail(item.refId.trim(), item.title.trim())
                },
            ) {
                Text("결재 상세", style = DenebType.button)
            }
        }
    }
    // Card-specific operations (a dream card's 전체/페이지별 되돌리기) that are not
    // part of the universal inbox lifecycle. Question cards already render their
    // chips through WorkFeedAnswerBlock; groupware approvals keep inline buttons.
    if (expanded && !item.question && !usesInlineApprovalActions) {
        WorkFeedActionChips(item = item, onRunAction = onRunAction)
    }
    // A question card the agent is waiting on: inline answer chips / reply field.
    if (expanded && item.question && !usesInlineApprovalActions) {
        WorkFeedAnswerBlock(item = item, onAnswer = onAnswer)
    }
}

/** True when a feed card can deep-link into [DenebApprovalDetail] via refId. */
internal fun canOpenApprovalDetail(item: WorkFeedItem): Boolean = item.source == "groupware-approval" && item.refId.isNotBlank()
