package ai.deneb.deneb

import ai.deneb.Platform
import ai.deneb.currentPlatform
import ai.deneb.ui.DenebScreenScaffold
import ai.deneb.ui.DenebSectionLabel
import ai.deneb.ui.DenebType
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.ElevatedCard
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.DayOfWeek
import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.daysUntil
import kotlinx.datetime.minus
import kotlinx.datetime.plus
import kotlinx.datetime.toInstant
import kotlinx.datetime.toLocalDateTime
import kotlinx.datetime.todayIn
import kotlin.time.Clock
import kotlin.time.Instant

/**
 * Month-grid calendar (`miniapp.calendar.list_range`): a tappable month grid on
 * top (dots mark days with events), the selected day's events below, and a "추가"
 * button that opens the manual add screen. Pull to refresh re-fetches the month.
 *
 * Design split (see .claude/rules/native-design-system.md): the frame + type are
 * the Deneb skin (DenebScreenScaffold + DenebType); buttons are Material. The grid
 * and day list are stateless bodies ([CalendarMonthGrid], [CalendarDayList]) the
 * render harness previews with mock data; this composable is the stateful shell
 * (month nav + fetch + loading/error states).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebCalendarScreen(
    client: DenebGatewayClient,
    onBack: () -> Unit,
    onOpenEvent: (String) -> Unit = {},
    onAddEvent: (LocalDate) -> Unit = {},
    onOpenTodos: () -> Unit = {},
    onOpenTodo: (String) -> Unit = {},
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val tz = remember { TimeZone.currentSystemDefault() }
    val today = remember { Clock.System.todayIn(tz) }
    // The grid is a finite, lazy pager of months centered on the current month:
    // page `startIndex` is today's month and each step is ±1 month. This makes
    // paging a real swipe — the grid tracks the finger and springs to the next
    // month — instead of the old instant state swap with no animation at all.
    val anchor = remember { CalMonth(today.year, today.month.ordinal + 1) }
    val startIndex = remember { MonthPageCount / 2 }
    val pagerState = rememberPagerState(initialPage = startIndex) { MonthPageCount }
    val scope = rememberCoroutineScope()

    fun monthForPage(page: Int): CalMonth = anchor.plusMonths(page - startIndex)
    fun pageForMonth(m: CalMonth): Int = startIndex + monthsBetween(anchor, m)

    // Per-month event cache so a neighbor is already populated when the user swipes
    // to it (no blank grid flashing in). `failed` records a per-month fetch error so
    // retry targets just that month. Both observable, so a late fetch recomposes the
    // page that shows it.
    val cache = remember { mutableStateMapOf<CalMonth, List<CalendarEvent>>() }
    val failed = remember { mutableStateMapOf<CalMonth, Boolean>() }
    var selected by remember { mutableStateOf(today) }
    var refreshing by remember { mutableStateOf(false) }
    // To-dos are independent of the visible month (a to-do has at most one due
    // date), so they're fetched once and filtered to the selected day below.
    var todos by remember { mutableStateOf<List<Todo>>(emptyList()) }

    // Calendar proposals (the bell): schedule-worthy items mail analysis surfaced,
    // shown as a count badge on the bell and an expanding popup the user accepts
    // from. Fetched once on entry and re-fetched after each accept/reject.
    val proposals by client.denebCalProposals.collectAsState()
    var showProposals by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { client.refreshCalendarProposals() }

    suspend fun loadMonth(m: CalMonth, force: Boolean) {
        if (!force && cache[m] != null) return
        val (from, to) = gridRangeIso(buildMonthGrid(m), tz)
        val ev = client.fetchCalendarRange(from, to)
        if (ev == null) {
            failed[m] = true
        } else {
            cache[m] = ev
            failed[m] = false
        }
    }
    suspend fun loadTodos() {
        client.fetchTodos()?.let { todos = it }
    }
    // To-dos load once and refresh on pull-to-refresh / toggle, independent of the
    // month so paging never drops them.
    LaunchedEffect(Unit) { loadTodos() }

    // Optimistic toggle: flip locally so the checkbox responds instantly, then
    // persist and re-fetch to settle completion state.
    fun toggleTodo(id: String, done: Boolean) {
        todos = todos.map { if (it.id == id) it.copy(done = done) else it }
        scope.launch {
            client.setTodoDone(id, done)
            loadTodos()
        }
    }

    // Prefetch the visible month and both neighbors whenever the pager moves, so a
    // swipe either way lands on a month that already has its dots/bars. currentPage
    // updates mid-drag (past halfway), so the next month is usually fetched before
    // the swipe settles. Launched on the screen scope (not this effect's) so a fast
    // flick past a month doesn't cancel its in-flight fetch.
    LaunchedEffect(pagerState.currentPage) {
        for (off in intArrayOf(0, -1, 1)) {
            val m = monthForPage(pagerState.currentPage + off)
            if (cache[m] == null && failed[m] != true) {
                scope.launch { loadMonth(m, force = false) }
            }
        }
    }

    // After a swipe settles on a new month, move the selection into it: today when
    // it lands there, else the month's first day. Skipped when the selection is
    // already in the settled month — that's a trailing/leading-cell tap from an
    // adjacent month, whose exact pick we must keep.
    LaunchedEffect(pagerState.settledPage) {
        val m = monthForPage(pagerState.settledPage)
        if (monthOf(selected) != m) {
            selected = if (today.year == m.year && today.month.ordinal + 1 == m.month) {
                today
            } else {
                LocalDate(m.year, m.month, 1)
            }
        }
    }

    val visible = monthForPage(pagerState.currentPage)
    val selMonth = monthOf(selected)
    val selEvents = cache[selMonth]

    // The day list is driven by the selected day's month (it differs from the
    // swiped-to month only for the moment a trailing-cell tap is animating the
    // pager). All-day/multi-day events first, then by start time.
    val dayEvents = remember(selEvents, selected, tz) {
        selEvents.orEmpty()
            .filter { selected in eventDays(it.start, it.end, it.allDay, tz) }
            .sortedWith(compareBy({ !it.allDay }, { it.start }))
    }
    // To-dos due on the selected day, incomplete first.
    val dayTodos = remember(todos, selected, tz) {
        todos
            .filter { it.due.isNotBlank() && eventLocalDate(it.due, tz) == selected }
            .sortedWith(compareBy({ it.done }, { it.due }))
    }

    // Top-level section: on desktop the sidebar is the navigation, so no back arrow.
    DenebScreenScaffold(title = "일정", onBack = onBack, tabBar = navigationTabBar, showBack = currentPlatform !is Platform.Desktop) {
        // Controls + weekday header stay pinned; the month grid pages horizontally,
        // and only the selected day's event list scrolls below it. The grid and the
        // list used to share one verticalScroll, so on a packed day the long list
        // pushed the whole grid off the top and the calendar looked like a plain list.
        Column(Modifier.fillMaxWidth().weight(1f).padding(horizontal = 16.dp)) {
            MonthControls(
                month = visible,
                proposalCount = proposals.size,
                onBell = { showProposals = !showProposals },
                onPrev = { scope.launch { pagerState.animateScrollToPage(pagerState.currentPage - 1) } },
                onNext = { scope.launch { pagerState.animateScrollToPage(pagerState.currentPage + 1) } },
                onToday = {
                    selected = today
                    scope.launch { pagerState.animateScrollToPage(startIndex) }
                },
                onAdd = { onAddEvent(selected) },
            )
            if (showProposals) {
                CalendarProposalsPopup(
                    proposals = proposals,
                    onAccept = { id ->
                        scope.launch {
                            client.acceptCalendarProposal(id)
                            loadMonth(visible, force = true) // re-fetch so the new event shows
                        }
                    },
                    onReject = { id -> scope.launch { client.rejectCalendarProposal(id) } },
                    onDismiss = { showProposals = false },
                )
            }
            Spacer(Modifier.height(4.dp))
            WeekdayHeader()
            // Each page builds its grid from the per-month cache, so the neighbor
            // pages the pager pre-composes already show their events.
            HorizontalPager(
                state = pagerState,
                modifier = Modifier.fillMaxWidth(),
                beyondViewportPageCount = 1,
            ) { page ->
                val pageMonth = monthForPage(page)
                val pageGrid = remember(pageMonth) { buildMonthGrid(pageMonth) }
                val events = cache[pageMonth]
                val bars = remember(events, pageGrid, tz) { layoutMonthBars(events.orEmpty(), pageGrid, tz) }
                val dots = remember(events, tz) { timedSingleDayDots(events.orEmpty(), tz) }
                CalendarMonthGrid(
                    grid = pageGrid,
                    today = today,
                    selected = selected,
                    bars = bars,
                    dots = dots,
                    onSelect = { date ->
                        // Tapping a leading/trailing cell selects that day and pages
                        // to its month.
                        selected = date
                        val dm = monthOf(date)
                        if (dm != monthForPage(pagerState.currentPage)) {
                            scope.launch { pagerState.animateScrollToPage(pageForMonth(dm)) }
                        }
                    },
                )
            }
            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = denebHairline())
            Spacer(Modifier.height(8.dp))
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(
                    dayHeadLabel(selected, today, dayEvents.size),
                    style = DenebType.sectionLabel,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = onOpenTodos, contentPadding = PaddingValues(horizontal = 8.dp)) {
                    Text("할 일")
                }
            }
            Spacer(Modifier.height(4.dp))
            PullToRefreshBox(
                isRefreshing = refreshing,
                onRefresh = {
                    scope.launch {
                        refreshing = true
                        loadMonth(selMonth, force = true)
                        loadTodos()
                        refreshing = false
                    }
                },
                modifier = Modifier.fillMaxWidth().weight(1f),
            ) {
                // Own scroll state so the list scrolls under the pinned grid; a
                // fillMaxSize column keeps pull-to-refresh working when empty.
                Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                    when {
                        selEvents == null && failed[selMonth] != true -> DenebLoading()

                        selEvents == null -> DenebError(
                            "일정을 불러오지 못했습니다.",
                            onRetry = { scope.launch { loadMonth(selMonth, force = true) } },
                        )

                        dayEvents.isEmpty() && dayTodos.isEmpty() -> CalendarEmptyDay(onAdd = { onAddEvent(selected) })

                        else -> {
                            if (dayEvents.isNotEmpty()) {
                                CalendarDayList(dayEvents, selected, tz, onOpenEvent)
                            }
                            if (dayTodos.isNotEmpty()) {
                                DenebSectionLabel("할 일")
                                dayTodos.forEach { TodoCheckRow(it, tz, ::toggleTodo, onOpenTodo) }
                            }
                        }
                    }
                    Spacer(Modifier.height(24.dp))
                }
            }
        }
    }
}

// --- stateless bodies (previewable) --------------------------------------

/** The month grid: 7-column weeks. Multi-day and all-day events are ribbons under
 *  the day number; single-day timed events are dots. Today and the selected day are
 *  highlighted. Pure presentation — the shell owns selection. */
@Composable
internal fun CalendarMonthGrid(
    grid: MonthGrid,
    today: LocalDate,
    selected: LocalDate,
    bars: Map<LocalDate, List<DayBar>>,
    dots: Map<LocalDate, Int>,
    onSelect: (LocalDate) -> Unit,
) {
    val haptics = rememberHaptics()
    val palette = barPalette()
    // Shared across the grid so every cell reserves equal height: bars on the same
    // lane line up row-to-row, and the dot row is present-or-absent grid-wide.
    val laneCount = bars.values.maxOfOrNull { day -> day.maxOfOrNull { it.lane + 1 } ?: 0 } ?: 0
    val showDotRow = dots.isNotEmpty()
    // Month paging is owned by the HorizontalPager in the screen shell; this grid is
    // pure presentation. Each cell owns its tap; the pager claims horizontal drags.
    Column(Modifier.fillMaxWidth()) {
        grid.cells.chunked(7).forEach { week ->
            Row(Modifier.fillMaxWidth()) {
                week.forEach { date ->
                    val inMonth = date.year == grid.firstOfMonth.year &&
                        date.month == grid.firstOfMonth.month
                    DayCell(
                        date = date,
                        inMonth = inMonth,
                        isToday = date == today,
                        isSelected = date == selected,
                        bars = bars[date].orEmpty(),
                        laneCount = laneCount,
                        dotCount = dots[date] ?: 0,
                        showDotRow = showDotRow,
                        palette = palette,
                        modifier = Modifier.weight(1f),
                        onClick = {
                            haptics.tap()
                            onSelect(date)
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun DayCell(
    date: LocalDate,
    inMonth: Boolean,
    isToday: Boolean,
    isSelected: Boolean,
    bars: List<DayBar>,
    laneCount: Int,
    dotCount: Int,
    showDotRow: Boolean,
    palette: List<Color>,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val scheme = MaterialTheme.colorScheme
    val numberColor = when {
        isSelected -> scheme.onPrimary
        !inMonth -> scheme.onSurface.copy(alpha = 0.28f)
        isToday -> scheme.primary
        date.dayOfWeek == DayOfWeek.SUNDAY -> scheme.error
        date.dayOfWeek == DayOfWeek.SATURDAY -> saturdayBlue
        else -> scheme.onSurface
    }
    Column(
        modifier
            .heightIn(min = 46.dp)
            .clickable(onClick = onClick)
            .padding(vertical = 6.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        // Day number: a filled disc when selected, a hairline ring for today. The
        // selection moved off the whole cell so bars below stay readable.
        Box(
            Modifier
                .size(26.dp)
                .clip(CircleShape)
                .then(
                    when {
                        isSelected -> Modifier.background(scheme.primary)
                        isToday -> Modifier.border(1.dp, scheme.primary, CircleShape)
                        else -> Modifier
                    },
                ),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                date.day.toString(),
                style = DenebType.body,
                fontWeight = if (isToday || isSelected) FontWeight.Bold else FontWeight.Normal,
                color = numberColor,
            )
        }
        // Ribbons: all-day and multi-day events, one fixed-height lane per row.
        // Bars run the full cell width (no horizontal inset) so a span connects
        // across adjacent days; first/last day get rounded outer corners.
        if (laneCount > 0) {
            Spacer(Modifier.height(3.dp))
            Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                for (lane in 0 until laneCount) {
                    val bar = bars.firstOrNull { it.lane == lane }
                    Box(
                        Modifier
                            .fillMaxWidth()
                            .height(4.dp)
                            .clip(barSegmentShape(bar))
                            .background(bar?.let { palette[it.colorIndex % palette.size] } ?: Color.Transparent),
                    )
                }
            }
        }
        // Dots: single-day timed events. A fixed-height row reserved grid-wide (when
        // any day has dots) so cells stay aligned; up to three dots per day.
        if (showDotRow) {
            Spacer(Modifier.height(3.dp))
            Row(
                Modifier.fillMaxWidth().height(5.dp),
                horizontalArrangement = Arrangement.spacedBy(3.dp, Alignment.CenterHorizontally),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                repeat(minOf(dotCount, 3)) {
                    Box(Modifier.size(5.dp).clip(CircleShape).background(scheme.primary))
                }
            }
        }
    }
}

/** The selected day's events, one tappable row each (time | title · location).
 *  The time column is day-aware: an all-day event shows 종일, a multi-day event
 *  shows 계속 on the days after it starts, and a timed event shows its start HH:mm. */
@Composable
internal fun CalendarDayList(
    events: List<CalendarEvent>,
    selected: LocalDate,
    tz: TimeZone,
    onOpen: (String) -> Unit,
) {
    val haptics = rememberHaptics()
    Column(Modifier.fillMaxWidth()) {
        events.forEachIndexed { index, event ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable {
                        haptics.tap()
                        onOpen(event.id)
                    }
                    .padding(vertical = 12.dp),
                verticalAlignment = Alignment.Top,
            ) {
                Text(
                    dayTimeLabel(event, selected, tz),
                    style = DenebType.meta,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.width(56.dp).padding(top = 2.dp),
                )
                Column(Modifier.weight(1f)) {
                    Text(
                        event.title.ifBlank { "(제목 없음)" },
                        style = DenebType.rowTitleStrong,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (event.location.isNotBlank()) {
                        Text(
                            event.location,
                            style = DenebType.meta,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
            }
            if (index < events.lastIndex) HorizontalDivider(color = denebHairline())
        }
    }
}

/** Empty state for a day with no events — a quiet line plus an inline CTA that
 *  opens the add screen pre-set to the selected day, so the obvious next action
 *  (add something here) is one tap away. */
@Composable
internal fun CalendarEmptyDay(onAdd: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().padding(vertical = 28.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text("이 날 일정이 없습니다.", style = DenebType.body, color = denebHint())
        Spacer(Modifier.height(8.dp))
        TextButton(onClick = onAdd) { Text("이 날 일정 추가") }
    }
}

@Composable
private fun MonthControls(
    month: CalMonth,
    proposalCount: Int,
    onBell: () -> Unit,
    onPrev: () -> Unit,
    onNext: () -> Unit,
    onToday: () -> Unit,
    onAdd: () -> Unit,
) {
    // Month control (‹ label ›) hugs the left at its natural (compact) width; a
    // weighted Spacer pushes the right-side actions to the edge and leaves a gap
    // for the bell badge. 추가 stays on one row via softWrap=false + maxLines=1,
    // so the narrower title can't force it to wrap.
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = onPrev, modifier = Modifier.size(36.dp)) {
            Text("‹", style = DenebType.subject, color = MaterialTheme.colorScheme.onBackground)
        }
        Text(
            "${month.year}년 ${month.month}월",
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 1,
        )
        IconButton(onClick = onNext, modifier = Modifier.size(36.dp)) {
            Text("›", style = DenebType.subject, color = MaterialTheme.colorScheme.onBackground)
        }
        Spacer(Modifier.weight(1f))
        CalendarBell(count = proposalCount, onClick = onBell)
        TextButton(onClick = onToday, contentPadding = PaddingValues(horizontal = 8.dp)) { Text("오늘") }
        FilledTonalButton(
            onClick = onAdd,
            contentPadding = PaddingValues(horizontal = 14.dp, vertical = 8.dp),
        ) { Text("추가", maxLines = 1, softWrap = false) }
    }
}

/** Calendar bell: a notifications icon with a count badge for pending calendar
 *  proposals. Muted (hint) when empty so it doesn't draw the eye. */
@Composable
private fun CalendarBell(count: Int, onClick: () -> Unit) {
    BadgedBox(
        badge = {
            if (count > 0) {
                Badge { Text(if (count > 9) "9+" else count.toString()) }
            }
        },
    ) {
        IconButton(onClick = onClick, modifier = Modifier.size(36.dp)) {
            Icon(
                Icons.Outlined.Notifications,
                contentDescription = "일정 제안 ${count}건",
                tint = if (count > 0) MaterialTheme.colorScheme.onBackground else denebHint(),
            )
        }
    }
}

/** Proposals modal that scales out from the bell (top-end), listing pending
 *  calendar proposals with accept/reject. A Popup so it floats above the grid. */
@Composable
private fun CalendarProposalsPopup(
    proposals: List<ai.deneb.deneb.generated.CalendarProposalOut>,
    onAccept: (String) -> Unit,
    onReject: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    // Expands from the bell (top-end): a right-aligned card that scales + fades
    // in via graphicsLayer (a draw-time transform, so its measured size is intact).
    // Rendered in-scene (not a Popup) so it shows on every platform — Compose
    // Desktop draws Popups in a separate window the test harness can't capture.
    var shown by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { shown = true }
    val scale by animateFloatAsState(if (shown) 1f else 0.82f, label = "proposalScale")
    val alpha by animateFloatAsState(if (shown) 1f else 0f, label = "proposalAlpha")
    Row(Modifier.fillMaxWidth().padding(top = 4.dp)) {
        Spacer(Modifier.weight(1f))
        ElevatedCard(
            modifier = Modifier
                .widthIn(max = 340.dp)
                .graphicsLayer {
                    scaleX = scale
                    scaleY = scale
                    this.alpha = alpha
                    transformOrigin = TransformOrigin(1f, 0f)
                },
        ) {
            Column(Modifier.padding(14.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("일정 제안", style = DenebType.subject, color = MaterialTheme.colorScheme.onBackground, modifier = Modifier.weight(1f))
                    TextButton(onClick = onDismiss, contentPadding = PaddingValues(horizontal = 8.dp)) { Text("닫기") }
                }
                Spacer(Modifier.height(6.dp))
                if (proposals.isEmpty()) {
                    Text("새 일정 제안이 없습니다.", style = DenebType.body, color = denebHint())
                } else {
                    // Cap the height and scroll so a large backlog can't overflow the
                    // card / push the grid off-screen. The header above stays pinned.
                    Column(
                        Modifier
                            .heightIn(max = 360.dp)
                            .verticalScroll(rememberScrollState()),
                    ) {
                        proposals.forEachIndexed { i, p ->
                            if (i > 0) {
                                Spacer(Modifier.height(8.dp))
                                HorizontalDivider(color = denebHairline())
                                Spacer(Modifier.height(8.dp))
                            }
                            CalendarProposalRow(p, onAccept = { onAccept(p.id) }, onReject = { onReject(p.id) })
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun CalendarProposalRow(
    p: ai.deneb.deneb.generated.CalendarProposalOut,
    onAccept: () -> Unit,
    onReject: () -> Unit,
) {
    Column(Modifier.fillMaxWidth()) {
        Text(p.title, style = DenebType.rowTitle, color = MaterialTheme.colorScheme.onBackground, maxLines = 2)
        Spacer(Modifier.height(2.dp))
        val when_ = if (p.allDay) p.start else p.start.replace("T", " ").take(16)
        val kindLabel = if (p.kind == "deadline") "기한" else "일정"
        Text("$when_ · $kindLabel", style = DenebType.meta, color = denebHint())
        if (p.sourceSubject.isNotBlank()) {
            Text("메일: ${p.sourceSubject}", style = DenebType.meta, color = denebHint(), maxLines = 1)
        }
        Spacer(Modifier.height(8.dp))
        Row {
            FilledTonalButton(
                onClick = onAccept,
                contentPadding = PaddingValues(horizontal = 16.dp, vertical = 6.dp),
            ) { Text("수락") }
            Spacer(Modifier.width(8.dp))
            TextButton(onClick = onReject, contentPadding = PaddingValues(horizontal = 12.dp)) { Text("거절") }
        }
    }
}

@Composable
private fun WeekdayHeader() {
    val scheme = MaterialTheme.colorScheme
    Row(Modifier.fillMaxWidth()) {
        koreanDayOfWeek.forEachIndexed { i, label ->
            val color = when (i) {
                5 -> saturdayBlue

                // Saturday — blue
                6 -> scheme.error

                // Sunday — red
                else -> denebHint()
            }
            Text(
                label,
                style = DenebType.meta,
                color = color,
                textAlign = TextAlign.Center,
                modifier = Modifier.weight(1f).padding(vertical = 4.dp),
            )
        }
    }
}

// --- date helpers --------------------------------------------------------

internal val koreanDayOfWeek = listOf("월", "화", "수", "목", "금", "토", "일")

// Korean calendar weekend tint: Saturday blue (Sunday reuses the theme's error red).
private val saturdayBlue = Color(0xFF4F8FF7)

/** How many month-pages the calendar pager spans, centered on the current month —
 *  ~100 years each way. Big enough to feel unbounded; the pager is lazy so the
 *  count itself costs nothing. */
private const val MonthPageCount = 2400

private fun dayHeadLabel(date: LocalDate, today: LocalDate, count: Int): String {
    val month = date.month.ordinal + 1
    val dow = koreanDayOfWeek.getOrElse(date.dayOfWeek.ordinal) { "" }
    val base = "${month}월 ${date.day}일 ($dow)"
    val head = if (date == today) "오늘 · $base" else base
    return if (count > 0) "$head · ${count}건" else head
}

/** Ribbon colors — calm, content-only hues from the active scheme. Deliberately
 *  excludes error red (reserved for Sunday / warnings) and the Saturday-blue header
 *  tint, so a bar never reads as a weekday/date color. */
@Composable
internal fun barPalette(): List<Color> {
    val s = MaterialTheme.colorScheme
    return listOf(s.primary, s.tertiary, s.secondary)
}

/** Rounds only a ribbon segment's outer corners: the start day's left, the end
 *  day's right; interior days stay square so the ribbon looks continuous. */
private fun barSegmentShape(bar: DayBar?): Shape {
    if (bar == null) return RectangleShape
    val r = 2.dp
    return RoundedCornerShape(
        topStart = if (bar.isStart) r else 0.dp,
        bottomStart = if (bar.isStart) r else 0.dp,
        topEnd = if (bar.isEnd) r else 0.dp,
        bottomEnd = if (bar.isEnd) r else 0.dp,
    )
}

/** Day-aware time label for the day list: 종일 for all-day, 계속 for a multi-day
 *  event on a day after it starts, else the start HH:mm. */
private fun dayTimeLabel(event: CalendarEvent, day: LocalDate, tz: TimeZone): String {
    if (event.allDay) return "종일"
    val startDate = eventLocalDate(event.start, tz)
    if (startDate != null && startDate != day) return "계속"
    return stampOf(event.start, false)?.time ?: "—"
}
