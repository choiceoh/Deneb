package ai.deneb.deneb

import kotlinx.datetime.DateTimeUnit
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
 * Pure month-grid math for the calendar screen: CalMonth arithmetic, grid
 * construction, RFC3339 day mapping, multi-day bar lane layout, and dot counts.
 * No composables — UI-free logic split from DenebCalendarScreen.kt so it can be
 * reasoned about (and unit-tested) without Compose.
 */
/** A visible month (month is 1-12). Small holder so grid math stays in Int. */
internal data class CalMonth(val year: Int, val month: Int) {
    /** This month shifted by [n] months (negative = earlier). floorDiv/mod keep the
     *  month in 1-12 and roll the year correctly in both directions. */
    fun plusMonths(n: Int): CalMonth {
        val zeroBased = year * 12 + (month - 1) + n
        return CalMonth(zeroBased.floorDiv(12), zeroBased.mod(12) + 1)
    }
}

/** Signed month distance from [a] to [b] (b − a), for mapping a month to a pager page. */
internal fun monthsBetween(a: CalMonth, b: CalMonth): Int =
    (b.year * 12 + b.month) - (a.year * 12 + a.month)

/** The month a date falls in. */
internal fun monthOf(date: LocalDate): CalMonth = CalMonth(date.year, date.month.ordinal + 1)

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
