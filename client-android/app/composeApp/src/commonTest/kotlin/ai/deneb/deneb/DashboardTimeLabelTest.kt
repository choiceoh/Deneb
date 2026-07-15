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
import kotlin.time.Clock

class DashboardTimeLabelTest {

    private fun epoch(
        date: LocalDate,
        hour: Int,
        minute: Int,
        zone: TimeZone,
    ): Long = LocalDateTime(date, LocalTime(hour, minute)).toInstant(zone).toEpochMilliseconds()

    @Test
    fun missingAndNegativeTimestampsAreBlank() {
        assertEquals("", dashboardTimeLabel(0, TimeZone.UTC))
        assertEquals("", dashboardTimeLabel(-1, TimeZone.UTC))
        assertEquals("", dashboardTimeLabel(Long.MIN_VALUE, TimeZone.UTC))
    }

    @Test
    fun todayUsesRelativeDayAndZeroPaddedClock() {
        val zone = TimeZone.UTC
        val today = Clock.System.todayIn(zone)

        assertEquals("오늘 09:05", dashboardTimeLabel(epoch(today, 9, 5, zone), zone))
        assertEquals("오늘 00:00", dashboardTimeLabel(epoch(today, 0, 0, zone), zone))
        assertEquals("오늘 23:59", dashboardTimeLabel(epoch(today, 23, 59, zone), zone))
    }

    @Test
    fun tomorrowAndYesterdayUseRelativeLabels() {
        val zone = TimeZone.UTC
        val today = Clock.System.todayIn(zone)
        val tomorrow = today.plus(1, DateTimeUnit.DAY)
        val yesterday = today.minus(1, DateTimeUnit.DAY)

        assertEquals("내일 07:03", dashboardTimeLabel(epoch(tomorrow, 7, 3, zone), zone))
        assertEquals("어제 18:40", dashboardTimeLabel(epoch(yesterday, 18, 40, zone), zone))
    }

    @Test
    fun olderDateIncludesMonthDayAndKoreanWeekday() {
        val zone = TimeZone.UTC
        val old = LocalDate(2000, 1, 1)

        assertEquals("1월 1일 (토) 14:08", dashboardTimeLabel(epoch(old, 14, 8, zone), zone))
    }

    @Test
    fun sameInstantUsesRequestedTimezoneCalendarDay() {
        val instant = kotlin.time.Instant.parse("2000-01-01T16:30:00Z").toEpochMilliseconds()

        assertEquals("1월 1일 (토) 16:30", dashboardTimeLabel(instant, TimeZone.UTC))
        assertEquals("1월 2일 (일) 01:30", dashboardTimeLabel(instant, TimeZone.of("Asia/Seoul")))
        assertEquals("1월 1일 (토) 08:30", dashboardTimeLabel(instant, TimeZone.of("America/Los_Angeles")))
    }

    @Test
    fun secondAndMillisecondPrecisionAreSuppressed() {
        val epoch = kotlin.time.Instant.parse("2000-01-01T12:34:59.999Z").toEpochMilliseconds()

        assertEquals("1월 1일 (토) 12:34", dashboardTimeLabel(epoch, TimeZone.UTC))
    }
}
