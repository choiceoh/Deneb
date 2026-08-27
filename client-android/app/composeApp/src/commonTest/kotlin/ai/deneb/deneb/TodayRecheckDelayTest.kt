package ai.deneb.deneb

import ai.deneb.ui.MaxTodayRecheckMs
import ai.deneb.ui.todayRecheckDelayMs
import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toInstant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

private val KST = TimeZone.of("Asia/Seoul")

private fun ms(date: LocalDate, hour: Int, minute: Int): Long = LocalDateTime(date, LocalTime(hour, minute)).toInstant(KST).toEpochMilliseconds()

class TodayRecheckDelayTest {

    /**
     * The regression: an evening wait of hours is measured against a clock that
     * stops in deep sleep, so it was still pending at 10:23 the next morning and
     * the feed header read "오늘 · 8월 26일" on the 27th.
     */
    @Test
    fun eveningWaitIsCappedNotSleptToMidnight() {
        val today = LocalDate(2026, 8, 26)
        val delay = todayRecheckDelayMs(today, ms(today, 19, 0), KST)
        assertEquals(MaxTodayRecheckMs, delay)
        assertTrue(delay < 5 * 60 * 60 * 1000L, "an hours-long wait cannot survive deep sleep")
    }

    /** Close to midnight the wait still lands exactly on the boundary. */
    @Test
    fun waitLandsOnMidnightWhenItIsNear() {
        val today = LocalDate(2026, 8, 26)
        val delay = todayRecheckDelayMs(today, ms(today, 23, 59), KST)
        assertEquals(60_000L, delay)
    }

    /** A day already crossed (clock ahead of the displayed date) re-reads at once. */
    @Test
    fun crossedMidnightRechecksImmediately() {
        val today = LocalDate(2026, 8, 26)
        val delay = todayRecheckDelayMs(today, ms(LocalDate(2026, 8, 27), 10, 23), KST)
        assertEquals(1_000L, delay)
    }

    /** The bound holds regardless of how far behind the displayed date is. */
    @Test
    fun neverWaitsLongerThanTheCap() {
        val today = LocalDate(2026, 8, 26)
        for (hour in 0..23) {
            val delay = todayRecheckDelayMs(today, ms(today, hour, 0), KST)
            assertTrue(delay in 1_000L..MaxTodayRecheckMs, "hour=$hour delay=$delay")
        }
    }
}
