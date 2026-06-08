package com.inspiredandroid.kai.deneb

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
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
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.DenebScreenScaffold
import com.inspiredandroid.kai.ui.DenebType
import com.inspiredandroid.kai.ui.components.rememberHaptics
import com.inspiredandroid.kai.ui.denebHairline
import com.inspiredandroid.kai.ui.denebHint
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
    navigationTabBar: (@Composable () -> Unit)? = null,
) {
    val tz = remember { TimeZone.currentSystemDefault() }
    val today = remember { Clock.System.todayIn(tz) }
    var visible by remember { mutableStateOf(CalMonth(today.year, today.month.ordinal + 1)) }
    var selected by remember { mutableStateOf(today) }
    var monthEvents by remember { mutableStateOf<List<CalendarEvent>>(emptyList()) }
    // null = load in flight, true = ok, false = fetch failed.
    var loadOk by remember { mutableStateOf<Boolean?>(null) }
    var refreshing by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    val grid = remember(visible) { buildMonthGrid(visible) }

    suspend fun load() {
        val (from, to) = gridRangeIso(grid, tz)
        val ev = client.fetchCalendarRange(from, to)
        if (ev == null) {
            loadOk = false
        } else {
            monthEvents = ev
            loadOk = true
        }
    }
    // Clear on month change so the old month's dots don't linger over the new
    // grid while the fetch is in flight. Pull-to-refresh (same month) doesn't
    // clear, so it never flickers to empty.
    LaunchedEffect(visible) { loadOk = null; monthEvents = emptyList(); load() }

    // Keep the selection visible after a month jump: today when it lands in the
    // shown month, otherwise that month's first day.
    fun showMonth(m: CalMonth) {
        visible = m
        selected = if (today.year == m.year && today.month.ordinal + 1 == m.month) {
            today
        } else {
            LocalDate(m.year, m.month, 1)
        }
    }

    // Bars span every day a (possibly multi-day) event covers, packed into lanes
    // so a span reads as one connected ribbon across the week. The day list shows
    // every event that touches the selected day, all-day/multi-day events first.
    val monthBars = remember(monthEvents, grid, tz) { layoutMonthBars(monthEvents, grid, tz) }
    val monthDots = remember(monthEvents, tz) { timedSingleDayDots(monthEvents, tz) }
    val dayEvents = remember(monthEvents, selected, tz) {
        monthEvents
            .filter { selected in eventDays(it.start, it.end, it.allDay, tz) }
            .sortedWith(compareBy({ !it.allDay }, { it.start }))
    }

    DenebScreenScaffold(title = "일정", onBack = onBack, tabBar = navigationTabBar) {
        // The month grid (controls + weekday header + grid + day heading) stays
        // pinned at the top; only the selected day's event list scrolls below it.
        // These used to share one verticalScroll, so on a packed day the long list
        // pushed the whole grid off the top and the calendar looked like a plain
        // list once a day had more events than fit on screen.
        Column(Modifier.fillMaxWidth().weight(1f).padding(horizontal = 16.dp)) {
            MonthControls(
                month = visible,
                onPrev = { showMonth(visible.prev()) },
                onNext = { showMonth(visible.next()) },
                onToday = { showMonth(CalMonth(today.year, today.month.ordinal + 1)) },
                onAdd = { onAddEvent(selected) },
            )
            Spacer(Modifier.height(8.dp))
            WeekdayHeader()
            CalendarMonthGrid(
                grid = grid,
                today = today,
                selected = selected,
                bars = monthBars,
                dots = monthDots,
                onSelect = { date ->
                    // Tapping a leading/trailing cell jumps to that month too.
                    selected = date
                    if (date.year != visible.year || date.month.ordinal + 1 != visible.month) {
                        visible = CalMonth(date.year, date.month.ordinal + 1)
                    }
                },
                onSwipePrev = { showMonth(visible.prev()) },
                onSwipeNext = { showMonth(visible.next()) },
            )
            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = denebHairline())
            Spacer(Modifier.height(8.dp))
            Text(
                dayHeadLabel(selected, today, dayEvents.size),
                style = DenebType.sectionLabel,
                color = MaterialTheme.colorScheme.primary,
            )
            Spacer(Modifier.height(4.dp))
            PullToRefreshBox(
                isRefreshing = refreshing,
                onRefresh = { scope.launch { refreshing = true; load(); refreshing = false } },
                modifier = Modifier.fillMaxWidth().weight(1f),
            ) {
                // Own scroll state so the list scrolls under the pinned grid; a
                // fillMaxSize column keeps pull-to-refresh working when empty.
                Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                    when {
                        loadOk == null && monthEvents.isEmpty() -> DenebLoading()
                        loadOk == false && monthEvents.isEmpty() -> DenebError(
                            "일정을 불러오지 못했어요.",
                            onRetry = { scope.launch { loadOk = null; load() } },
                        )
                        dayEvents.isEmpty() -> CalendarEmptyDay(onAdd = { onAddEvent(selected) })
                        else -> CalendarDayList(dayEvents, selected, tz, onOpenEvent)
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
    onSwipePrev: () -> Unit = {},
    onSwipeNext: () -> Unit = {},
) {
    val haptics = rememberHaptics()
    val palette = barPalette()
    // Shared across the grid so every cell reserves equal height: bars on the same
    // lane line up row-to-row, and the dot row is present-or-absent grid-wide.
    val laneCount = bars.values.maxOfOrNull { day -> day.maxOfOrNull { it.lane + 1 } ?: 0 } ?: 0
    val showDotRow = dots.isNotEmpty()
    Column(
        // Horizontal swipe flips months (swipe left → next, right → prev), the way
        // a mobile calendar is expected to page. Keyed on `grid` so each month's
        // gesture captures fresh callbacks. Vertical drags fall through to the day
        // list below, which owns its own scroll.
        Modifier.fillMaxWidth().pointerInput(grid) {
            val threshold = size.width / 4f
            var accum = 0f
            detectHorizontalDragGestures(
                onDragStart = { accum = 0f },
                onDragCancel = { accum = 0f },
                onDragEnd = {
                    if (accum > threshold) onSwipePrev() else if (accum < -threshold) onSwipeNext()
                },
            ) { _, amount -> accum += amount }
        },
    ) {
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
                        onClick = { haptics.tap(); onSelect(date) },
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
                    .clickable { haptics.tap(); onOpen(event.id) }
                    .padding(vertical = 12.dp),
                verticalAlignment = Alignment.Top,
            ) {
                Text(
                    dayTimeLabel(event, selected, tz),
                    style = DenebType.body,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.width(56.dp),
                )
                Column(Modifier.weight(1f)) {
                    Text(
                        event.title.ifBlank { "(제목 없음)" },
                        style = DenebType.body,
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
        Text("이 날 일정이 없어요.", style = DenebType.body, color = denebHint())
        Spacer(Modifier.height(8.dp))
        TextButton(onClick = onAdd) { Text("이 날 일정 추가") }
    }
}

@Composable
private fun MonthControls(
    month: CalMonth,
    onPrev: () -> Unit,
    onNext: () -> Unit,
    onToday: () -> Unit,
    onAdd: () -> Unit,
) {
    // Title takes the slack (weight) so the right-side actions keep their natural
    // width — at phone width (412dp) a Spacer-pushed layout squeezed "추가" into
    // two lines. Compact icon buttons + tight button padding keep it on one row.
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = onPrev, modifier = Modifier.size(36.dp)) {
            Text("‹", style = DenebType.subject, color = MaterialTheme.colorScheme.onBackground)
        }
        Text(
            "${month.year}년 ${month.month}월",
            style = DenebType.subject,
            color = MaterialTheme.colorScheme.onBackground,
            maxLines = 1,
            textAlign = TextAlign.Center,
            modifier = Modifier.weight(1f),
        )
        IconButton(onClick = onNext, modifier = Modifier.size(36.dp)) {
            Text("›", style = DenebType.subject, color = MaterialTheme.colorScheme.onBackground)
        }
        TextButton(onClick = onToday, contentPadding = PaddingValues(horizontal = 8.dp)) { Text("오늘") }
        FilledTonalButton(
            onClick = onAdd,
            contentPadding = PaddingValues(horizontal = 14.dp, vertical = 8.dp),
        ) { Text("추가", maxLines = 1, softWrap = false) }
    }
}

@Composable
private fun WeekdayHeader() {
    val scheme = MaterialTheme.colorScheme
    Row(Modifier.fillMaxWidth()) {
        koreanDayOfWeek.forEachIndexed { i, label ->
            val color = when (i) {
                5 -> saturdayBlue // Saturday — blue
                6 -> scheme.error // Sunday — red
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

/** A visible month (month is 1-12). Small holder so grid math stays in Int. */
internal data class CalMonth(val year: Int, val month: Int) {
    fun prev(): CalMonth = if (month == 1) CalMonth(year - 1, 12) else CalMonth(year, month - 1)
    fun next(): CalMonth = if (month == 12) CalMonth(year + 1, 1) else CalMonth(year, month + 1)
}

/** The grid days for a month: full weeks (Mon-first) covering the month, plus
 *  the month's first day so cells can be tagged in/out of month. */
internal data class MonthGrid(val cells: List<LocalDate>, val firstOfMonth: LocalDate)

internal fun buildMonthGrid(m: CalMonth): MonthGrid {
    val first = LocalDate(m.year, m.month, 1)
    val firstNext = if (m.month == 12) LocalDate(m.year + 1, 1, 1) else LocalDate(m.year, m.month + 1, 1)
    val daysInMonth = firstNext.minus(1, DateTimeUnit.DAY).day
    val leading = first.dayOfWeek.ordinal // Monday = 0
    val rows = (leading + daysInMonth + 6) / 7
    val gridStart = first.minus(leading, DateTimeUnit.DAY)
    val cells = (0 until rows * 7).map { gridStart.plus(it, DateTimeUnit.DAY) }
    return MonthGrid(cells, first)
}

/** RFC3339 (UTC) bounds covering the whole grid, for list_range. */
internal fun gridRangeIso(grid: MonthGrid, tz: TimeZone): Pair<String, String> {
    val from = LocalDateTime(grid.cells.first(), LocalTime(0, 0)).toInstant(tz).toString()
    val toExclusive = LocalDateTime(grid.cells.last().plus(1, DateTimeUnit.DAY), LocalTime(0, 0)).toInstant(tz).toString()
    return from to toExclusive
}

/** The local calendar date an event starts on (for dot/day-list membership). */
internal fun eventLocalDate(rfc3339: String, tz: TimeZone): LocalDate? {
    if (rfc3339.isBlank()) return null
    return runCatching { Instant.parse(rfc3339).toLocalDateTime(tz).date }.getOrNull()
}

private fun dayHeadLabel(date: LocalDate, today: LocalDate, count: Int): String {
    val month = date.month.ordinal + 1
    val dow = koreanDayOfWeek.getOrElse(date.dayOfWeek.ordinal) { "" }
    val base = "${month}월 ${date.day}일 ($dow)"
    val head = if (date == today) "오늘 · $base" else base
    return if (count > 0) "$head · ${count}건" else head
}

internal data class CalStamp(val dayKey: String, val dayLabel: String, val time: String)

/** Parse an RFC3339 UTC instant into a local day-grouping key + label + HH:mm (or 종일). */
internal fun stampOf(rfc3339: String, allDay: Boolean): CalStamp? {
    if (rfc3339.isBlank()) return null
    val tz = TimeZone.currentSystemDefault()
    val local = runCatching {
        Instant.parse(rfc3339).toLocalDateTime(tz)
    }.getOrNull() ?: return null
    val month = local.month.ordinal + 1
    val dow = koreanDayOfWeek.getOrElse(local.dayOfWeek.ordinal) { "" }
    val today = Clock.System.todayIn(tz)
    val dayLabel = when (local.date) {
        today -> "오늘"
        today.plus(1, DateTimeUnit.DAY) -> "내일"
        else -> "${month}월 ${local.day}일 ($dow)"
    }
    return CalStamp(
        dayKey = "${local.year}-$month-${local.day}",
        dayLabel = dayLabel,
        time = if (allDay) {
            "종일"
        } else {
            "${local.hour.toString().padStart(2, '0')}:${local.minute.toString().padStart(2, '0')}"
        },
    )
}

// --- multi-day event layout ----------------------------------------------

/** Max event ribbon lanes drawn per week; extra concurrent spans are dropped. */
private const val MaxBarLanes = 3

private const val BarPaletteSize = 3

/** A ribbon segment in one day cell: its lane (row), a stable per-event color
 *  index, and whether this day is the span's first/last (for corner rounding). */
internal data class DayBar(
    val lane: Int,
    val colorIndex: Int,
    val isStart: Boolean,
    val isEnd: Boolean,
)

/** The local days an event covers, inclusive. A single-day event yields one date;
 *  a multi-day event yields every day it spans. All-day spans carry an exclusive
 *  end (midnight after the last day — Google's and our local store's convention),
 *  so an end at local midnight resolves to the previous day. A corrupt span is
 *  clamped to a year so it can't generate an unbounded list. */
internal fun eventDays(startRfc: String, endRfc: String, allDay: Boolean, tz: TimeZone): List<LocalDate> {
    val startDate = eventLocalDate(startRfc, tz) ?: return emptyList()
    val endLdt = runCatching { Instant.parse(endRfc).toLocalDateTime(tz) }.getOrNull()
        ?: return listOf(startDate)
    var last = endLdt.date
    if (allDay && endLdt.time == LocalTime(0, 0) && last > startDate) {
        last = last.minus(1, DateTimeUnit.DAY)
    }
    if (last <= startDate) return listOf(startDate)
    val span = startDate.daysUntil(last).coerceAtMost(366)
    return (0..span).map { startDate.plus(it, DateTimeUnit.DAY) }
}

/** Packs each event's covering days into per-week lanes so a multi-day span reads
 *  as one connected ribbon. Within a week, spans are laid out left-to-right and
 *  take the lowest free lane; a span keeps a stable color (from its id) across
 *  weeks even if its lane shifts. Returns the bars to draw under each day. */
internal fun layoutMonthBars(
    events: List<CalendarEvent>,
    grid: MonthGrid,
    tz: TimeZone,
): Map<LocalDate, List<DayBar>> {
    data class Span(val first: LocalDate, val last: LocalDate, val colorIndex: Int)
    val spans = events.mapNotNull { ev ->
        val days = eventDays(ev.start, ev.end, ev.allDay, tz)
        if (days.isEmpty()) return@mapNotNull null
        // Single-day timed events are dots (timedSingleDayDots), not ribbons.
        if (!ev.allDay && days.size == 1) return@mapNotNull null
        Span(days.first(), days.last(), colorIndexFor(ev.id))
    }
    val out = HashMap<LocalDate, MutableList<DayBar>>()
    grid.cells.chunked(7).forEach { week ->
        val weekStart = week.first()
        val weekEnd = week.last()
        // Longer spans first (and earlier starts) so they claim low lanes and short
        // events fill the gaps — a tidy, stable packing.
        val inWeek = spans
            .filter { it.first <= weekEnd && it.last >= weekStart }
            .sortedWith(compareBy({ it.first }, { -it.first.daysUntil(it.last) }))
        val laneLastDay = ArrayList<LocalDate?>() // lane → last day it occupies this week
        for (span in inWeek) {
            val segFirst = maxOf(span.first, weekStart)
            val segLast = minOf(span.last, weekEnd)
            var lane = laneLastDay.indexOfFirst { it == null || it < segFirst }
            if (lane == -1) {
                if (laneLastDay.size >= MaxBarLanes) continue // overflow — drop (rare)
                lane = laneLastDay.size
                laneLastDay.add(null)
            }
            laneLastDay[lane] = segLast
            var d = segFirst
            while (d <= segLast) {
                out.getOrPut(d) { ArrayList() }.add(
                    DayBar(
                        lane = lane,
                        colorIndex = span.colorIndex,
                        isStart = d == span.first,
                        isEnd = d == span.last,
                    ),
                )
                d = d.plus(1, DateTimeUnit.DAY)
            }
        }
    }
    return out
}

/** Days with single-day timed events, counted — drawn as dots (not ribbons) so a
 *  brief meeting reads lighter than an all-day or multi-day commitment. */
internal fun timedSingleDayDots(events: List<CalendarEvent>, tz: TimeZone): Map<LocalDate, Int> {
    val out = HashMap<LocalDate, Int>()
    events.forEach { ev ->
        val days = eventDays(ev.start, ev.end, ev.allDay, tz)
        if (!ev.allDay && days.size == 1) {
            val d = days.first()
            out[d] = (out[d] ?: 0) + 1
        }
    }
    return out
}

/** Stable palette slot for an event id, so its ribbon keeps one color. */
private fun colorIndexFor(id: String): Int = (id.hashCode() and Int.MAX_VALUE) % BarPaletteSize

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
