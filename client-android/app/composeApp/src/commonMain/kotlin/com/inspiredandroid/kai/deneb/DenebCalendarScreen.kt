package com.inspiredandroid.kai.deneb

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

    val daysWithEvents = remember(monthEvents, tz) {
        monthEvents.mapNotNull { eventLocalDate(it.start, tz) }.toSet()
    }
    val dayEvents = remember(monthEvents, selected, tz) {
        monthEvents.filter { eventLocalDate(it.start, tz) == selected }.sortedBy { it.start }
    }

    DenebScreenScaffold(title = "일정", onBack = onBack, tabBar = navigationTabBar) {
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = { scope.launch { refreshing = true; load(); refreshing = false } },
            modifier = Modifier.fillMaxWidth().weight(1f),
        ) {
            Column(
                Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp),
            ) {
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
                    daysWithEvents = daysWithEvents,
                    onSelect = { date ->
                        // Tapping a leading/trailing cell jumps to that month too.
                        selected = date
                        if (date.year != visible.year || date.month.ordinal + 1 != visible.month) {
                            visible = CalMonth(date.year, date.month.ordinal + 1)
                        }
                    },
                )
                Spacer(Modifier.height(12.dp))
                HorizontalDivider(color = denebHairline())
                Spacer(Modifier.height(8.dp))
                Text(
                    dayHeadLabel(selected, today, dayEvents.size),
                    style = DenebType.sectionLabel,
                    color = MaterialTheme.colorScheme.primary,
                )

                when {
                    loadOk == null && monthEvents.isEmpty() -> DenebLoading()
                    loadOk == false && monthEvents.isEmpty() -> DenebError(
                        "일정을 불러오지 못했어요.",
                        onRetry = { scope.launch { loadOk = null; load() } },
                    )
                    dayEvents.isEmpty() -> DenebEmpty("이 날 일정이 없어요.")
                    else -> CalendarDayList(dayEvents, onOpenEvent)
                }
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}

// --- stateless bodies (previewable) --------------------------------------

/** The month grid: 7-column weeks, a dot under days that have events, today and
 *  the selected day highlighted. Pure presentation — the shell owns selection. */
@Composable
internal fun CalendarMonthGrid(
    grid: MonthGrid,
    today: LocalDate,
    selected: LocalDate,
    daysWithEvents: Set<LocalDate>,
    onSelect: (LocalDate) -> Unit,
) {
    val haptics = rememberHaptics()
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
                        hasEvents = date in daysWithEvents,
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
    hasEvents: Boolean,
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
            .padding(2.dp)
            .clip(RoundedCornerShape(10.dp))
            // A ring marks today when it isn't the (filled) selection.
            .then(
                if (isToday && !isSelected) {
                    Modifier.border(1.dp, scheme.primary, RoundedCornerShape(10.dp))
                } else {
                    Modifier
                },
            )
            .clickable(onClick = onClick)
            .background(if (isSelected) scheme.primary else Color.Transparent)
            .padding(vertical = 6.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            date.day.toString(),
            style = DenebType.body,
            fontWeight = if (isToday || isSelected) FontWeight.Bold else FontWeight.Normal,
            color = numberColor,
        )
        Spacer(Modifier.height(3.dp))
        Box(
            Modifier
                .size(5.dp)
                .clip(CircleShape)
                .background(
                    when {
                        !hasEvents -> Color.Transparent
                        isSelected -> scheme.onPrimary
                        else -> scheme.primary
                    },
                ),
        )
    }
}

/** The selected day's events, one tappable row each (time | title · location). */
@Composable
internal fun CalendarDayList(events: List<CalendarEvent>, onOpen: (String) -> Unit) {
    val haptics = rememberHaptics()
    Column(Modifier.fillMaxWidth()) {
        events.forEachIndexed { index, event ->
            val stamp = stampOf(event.start, event.allDay)
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { haptics.tap(); onOpen(event.id) }
                    .padding(vertical = 12.dp),
                verticalAlignment = Alignment.Top,
            ) {
                Text(
                    stamp?.time ?: "—",
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
