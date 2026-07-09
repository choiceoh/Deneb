package ai.deneb.deneb

import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.DayOfWeek
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.plus
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * Pure month-grid math — the place off-by-one and timezone bugs hide. All tests
 * pin behavior with a fixed TimeZone (UTC or a known offset) so they're
 * deterministic; the today-relative `stampOf` is intentionally left to live use.
 */
class CalendarMonthMathTest {

    // --- CalMonth arithmetic ---------------------------------------------

    @Test
    fun `plusMonths rolls the year in both directions`() {
        assertEquals(CalMonth(2026, 7), CalMonth(2026, 7).plusMonths(0))
        assertEquals(CalMonth(2026, 8), CalMonth(2026, 7).plusMonths(1))
        assertEquals(CalMonth(2027, 1), CalMonth(2026, 12).plusMonths(1))
        assertEquals(CalMonth(2025, 12), CalMonth(2026, 1).plusMonths(-1))
        assertEquals(CalMonth(2027, 7), CalMonth(2026, 7).plusMonths(12))
        assertEquals(CalMonth(2025, 6), CalMonth(2026, 7).plusMonths(-13))
    }

    @Test
    fun `monthsBetween is signed and inverse`() {
        assertEquals(2, monthsBetween(CalMonth(2026, 1), CalMonth(2026, 3)))
        assertEquals(-2, monthsBetween(CalMonth(2026, 3), CalMonth(2026, 1)))
        assertEquals(12, monthsBetween(CalMonth(2025, 7), CalMonth(2026, 7)))
    }

    @Test
    fun `monthOf reads a date's calendar month`() {
        assertEquals(CalMonth(2026, 7), monthOf(LocalDate(2026, 7, 9)))
        assertEquals(CalMonth(2026, 1), monthOf(LocalDate(2026, 1, 31)))
    }

    // --- grid construction -----------------------------------------------

    @Test
    fun `buildMonthGrid yields whole Mon-first weeks covering the month`() {
        for (m in listOf(CalMonth(2026, 7), CalMonth(2026, 2), CalMonth(2026, 12))) {
            val grid = buildMonthGrid(m)
            assertEquals(0, grid.cells.size % 7, "grid must be full weeks")
            assertEquals(DayOfWeek.MONDAY, grid.cells.first().dayOfWeek)
            assertEquals(DayOfWeek.SUNDAY, grid.cells.last().dayOfWeek)
            assertEquals(LocalDate(m.year, m.month, 1), grid.firstOfMonth)
            assertTrue(grid.cells.contains(grid.firstOfMonth), "first of month is in the grid")
            // Leading cells are the tail of the previous month, never after the 1st.
            assertTrue(grid.cells.first() <= grid.firstOfMonth)
        }
    }

    @Test
    fun `gridRangeIso spans first cell midnight to the day after the last, in UTC`() {
        val grid = buildMonthGrid(CalMonth(2026, 7))
        val (from, toExclusive) = gridRangeIso(grid, TimeZone.UTC)
        assertEquals("${grid.cells.first()}T00:00:00Z", from)
        assertEquals("${grid.cells.last().plus(1, DateTimeUnit.DAY)}T00:00:00Z", toExclusive)
    }

    // --- instant → local date --------------------------------------------

    @Test
    fun `eventLocalDate parses UTC and shifts into the target zone`() {
        assertEquals(LocalDate(2026, 7, 9), eventLocalDate("2026-07-09T15:30:00Z", TimeZone.UTC))
        // 15:30Z + 9h crosses midnight → next local day.
        assertEquals(LocalDate(2026, 7, 10), eventLocalDate("2026-07-09T15:30:00Z", TimeZone.of("Asia/Seoul")))
    }

    @Test
    fun `eventLocalDate is null for blank or garbage`() {
        assertNull(eventLocalDate("", TimeZone.UTC))
        assertNull(eventLocalDate("not-a-date", TimeZone.UTC))
    }

    // --- eventDays: the multi-day span convention ------------------------

    @Test
    fun `eventDays returns the single day for a timed event`() {
        // Unparseable end falls back to just the start day.
        assertEquals(listOf(LocalDate(2026, 7, 9)), eventDays("2026-07-09T10:00:00Z", "", false, TimeZone.UTC))
    }

    @Test
    fun `eventDays treats an all-day exclusive-end midnight as the previous day`() {
        // One all-day event: 07-09 with exclusive end 07-10T00:00 → covers only 07-09.
        assertEquals(
            listOf(LocalDate(2026, 7, 9)),
            eventDays("2026-07-09T00:00:00Z", "2026-07-10T00:00:00Z", true, TimeZone.UTC),
        )
        // Two all-day days: end 07-11T00:00 → 07-09..07-10.
        assertEquals(
            listOf(LocalDate(2026, 7, 9), LocalDate(2026, 7, 10)),
            eventDays("2026-07-09T00:00:00Z", "2026-07-11T00:00:00Z", true, TimeZone.UTC),
        )
    }

    @Test
    fun `eventDays keeps every day of a multi-day timed span inclusive`() {
        assertEquals(
            listOf(LocalDate(2026, 7, 9), LocalDate(2026, 7, 10), LocalDate(2026, 7, 11)),
            eventDays("2026-07-09T10:00:00Z", "2026-07-11T15:00:00Z", false, TimeZone.UTC),
        )
    }

    @Test
    fun `eventDays collapses an end at or before the start to one day`() {
        assertEquals(
            listOf(LocalDate(2026, 7, 9)),
            eventDays("2026-07-09T10:00:00Z", "2026-07-09T09:00:00Z", false, TimeZone.UTC),
        )
    }

    @Test
    fun `eventDays clamps a corrupt decade-long span to a year`() {
        val days = eventDays("2020-01-01T00:00:00Z", "2030-01-01T00:00:00Z", false, TimeZone.UTC)
        assertEquals(367, days.size) // 0..366 inclusive
    }

    // --- month bar lane packing ------------------------------------------

    private fun allDay(id: String, start: String, end: String, category: String) = CalendarEvent(id = id, title = id, location = "", start = start, end = end, allDay = true, category = category)

    @Test
    fun `layoutMonthBars packs overlapping spans into distinct lanes and flags ends`() {
        val grid = buildMonthGrid(CalMonth(2026, 7))
        val a = allDay("a", "2026-07-06T00:00:00Z", "2026-07-09T00:00:00Z", "others") // 06,07,08 → color 1
        val b = allDay("b", "2026-07-07T00:00:00Z", "2026-07-10T00:00:00Z", "deadline") // 07,08,09 → color 2
        // A single-day timed event is a dot, not a ribbon — must not appear in bars.
        val c = CalendarEvent("c", "c", "", "2026-07-15T10:00:00Z", "2026-07-15T11:00:00Z", allDay = false)

        val bars = layoutMonthBars(listOf(a, b, c), grid, TimeZone.UTC)

        // Shared days carry both ribbons on separate lanes.
        val shared = bars[LocalDate(2026, 7, 7)]!!
        assertEquals(setOf(0, 1), shared.map { it.lane }.toSet())
        // A owns the low lane and its first day is a start corner with its category color.
        val aStart = bars[LocalDate(2026, 7, 6)]!!.single()
        assertEquals(0, aStart.lane)
        assertTrue(aStart.isStart && !aStart.isEnd)
        assertEquals(1, aStart.colorIndex)
        // B's last day is an end corner with the deadline color.
        val bEnd = bars[LocalDate(2026, 7, 9)]!!.single()
        assertTrue(bEnd.isEnd)
        assertEquals(2, bEnd.colorIndex)
        // The timed single-day event drew no ribbon.
        assertNull(bars[LocalDate(2026, 7, 15)])
    }

    @Test
    fun `timedSingleDayDots dots only single-day timed events`() {
        val timed = CalendarEvent("t", "t", "", "2026-07-15T10:00:00Z", "2026-07-15T11:00:00Z", allDay = false)
        val spanning = allDay("s", "2026-07-06T00:00:00Z", "2026-07-09T00:00:00Z", "mine")
        val dots = timedSingleDayDots(listOf(timed, spanning), TimeZone.UTC)
        assertEquals(listOf(0), dots[LocalDate(2026, 7, 15)])
        assertNull(dots[LocalDate(2026, 7, 6)]) // the multi-day span is not a dot
    }

    // --- to-do due marks + color mapping ---------------------------------

    @Test
    fun `todoDueDays marks only pending, dated to-dos`() {
        val todos = listOf(
            Todo(id = "1", title = "pending", due = "2026-07-09T00:00:00Z", done = false),
            Todo(id = "2", title = "done", due = "2026-07-10T00:00:00Z", done = true),
            Todo(id = "3", title = "no-date", due = "", done = false),
        )
        assertEquals(setOf(LocalDate(2026, 7, 9)), todoDueDays(todos, TimeZone.UTC))
    }

    @Test
    fun `categoryColorIndex maps buckets with a fallback to mine`() {
        assertEquals(2, categoryColorIndex("deadline"))
        assertEquals(1, categoryColorIndex("others"))
        assertEquals(0, categoryColorIndex("mine"))
        assertEquals(0, categoryColorIndex(""))
        assertEquals(0, categoryColorIndex("unknown"))
    }
}
