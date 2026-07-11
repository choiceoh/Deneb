package ai.deneb.deneb

import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.minus
import kotlinx.datetime.plus
import kotlinx.datetime.toInstant
import kotlinx.datetime.todayIn
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.time.Clock

class TodoDueLabelTest {

    private fun instantAt(
        date: LocalDate,
        hour: Int = 0,
        minute: Int = 0,
        zone: TimeZone = TimeZone.UTC,
    ): String = LocalDateTime(date, LocalTime(hour, minute)).toInstant(zone).toString()

    @Test
    fun missingAndMalformedDueValuesAreOmitted() {
        for (value in listOf("", "   ", "tomorrow", "2026-07-11", "null")) {
            assertNull(todoDueLabel(value, allDay = false, tz = TimeZone.UTC), value)
            assertNull(todoDueLabel(value, allDay = true, tz = TimeZone.UTC), value)
        }
    }

    @Test
    fun allDayDueTodayUsesTodayLabel() {
        val zone = TimeZone.UTC
        val today = Clock.System.todayIn(zone)

        assertEquals(
            "오늘",
            todoDueLabel(instantAt(today, hour = 23, minute = 59), allDay = true, tz = zone),
        )
    }

    @Test
    fun timedDueTodayIncludesZeroPaddedClock() {
        val zone = TimeZone.UTC
        val today = Clock.System.todayIn(zone)

        assertEquals(
            "오늘 09:05",
            todoDueLabel(instantAt(today, hour = 9, minute = 5), allDay = false, tz = zone),
        )
        assertEquals(
            "오늘 00:00",
            todoDueLabel(instantAt(today), allDay = false, tz = zone),
        )
    }

    @Test
    fun tomorrowUsesRelativeLabelWithOptionalClock() {
        val zone = TimeZone.UTC
        val tomorrow = Clock.System.todayIn(zone).plus(1, DateTimeUnit.DAY)
        val due = instantAt(tomorrow, hour = 7, minute = 3)

        assertEquals("내일", todoDueLabel(due, allDay = true, tz = zone))
        assertEquals("내일 07:03", todoDueLabel(due, allDay = false, tz = zone))
    }

    @Test
    fun olderDateUsesMonthDayAndKoreanWeekday() {
        val due = instantAt(LocalDate(2000, 1, 1), hour = 14, minute = 8)

        assertEquals(
            "1월 1일 (토)",
            todoDueLabel(due, allDay = true, tz = TimeZone.UTC),
        )
        assertEquals(
            "1월 1일 (토) 14:08",
            todoDueLabel(due, allDay = false, tz = TimeZone.UTC),
        )
    }

    @Test
    fun dateLabelUsesTheRequestedZoneAfterUtcBoundaryCrossing() {
        val instant = "2000-01-01T16:30:00Z"

        assertEquals(
            "1월 1일 (토) 16:30",
            todoDueLabel(instant, allDay = false, tz = TimeZone.UTC),
        )
        assertEquals(
            "1월 2일 (일) 01:30",
            todoDueLabel(instant, allDay = false, tz = TimeZone.of("Asia/Seoul")),
        )
        assertEquals(
            "1월 1일 (토) 08:30",
            todoDueLabel(instant, allDay = false, tz = TimeZone.of("America/Los_Angeles")),
        )
    }

    @Test
    fun allDayModeSuppressesClockButStillConvertsZone() {
        val instant = "2000-06-30T23:30:00Z"

        assertEquals(
            "7월 1일 (토)",
            todoDueLabel(instant, allDay = true, tz = TimeZone.of("Asia/Seoul")),
        )
    }

    @Test
    fun pastAndFutureDatesOutsideRelativeWindowUseAbsoluteLabels() {
        val zone = TimeZone.UTC
        val today = Clock.System.todayIn(zone)
        val past = today.minus(2, DateTimeUnit.DAY)
        val future = today.plus(2, DateTimeUnit.DAY)

        val pastLabel = requireNotNull(todoDueLabel(instantAt(past), allDay = true, tz = zone))
        val futureLabel = requireNotNull(todoDueLabel(instantAt(future), allDay = true, tz = zone))

        assertEquals("${past.month.ordinal + 1}월 ${past.day}일", pastLabel.substringBefore(" ("))
        assertEquals("${future.month.ordinal + 1}월 ${future.day}일", futureLabel.substringBefore(" ("))
    }

    @Test
    fun fractionalSecondsDoNotChangeMinutePrecision() {
        assertEquals(
            "1월 1일 (토) 12:34",
            todoDueLabel("2000-01-01T12:34:59.999999999Z", allDay = false, tz = TimeZone.UTC),
        )
    }
}
