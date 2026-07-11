package ai.deneb.deneb

import kotlinx.datetime.TimeZone
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.time.Instant

class ProjectDigestDateLabelTest {

    @Test
    fun missingAndNegativeTimestampsAreBlank() {
        assertEquals("", projectDigestDateLabel(0, TimeZone.UTC))
        assertEquals("", projectDigestDateLabel(-1, TimeZone.UTC))
        assertEquals("", projectDigestDateLabel(Long.MIN_VALUE, TimeZone.UTC))
    }

    @Test
    fun labelUsesDayGranularityOnly() {
        val morning = Instant.parse("2000-01-01T00:01:00Z").toEpochMilliseconds()
        val evening = Instant.parse("2000-01-01T23:59:59Z").toEpochMilliseconds()

        assertEquals("1월 1일 기준", projectDigestDateLabel(morning, TimeZone.UTC))
        assertEquals("1월 1일 기준", projectDigestDateLabel(evening, TimeZone.UTC))
    }

    @Test
    fun timezoneConversionCanMoveDigestToNextOrPreviousDay() {
        val instant = Instant.parse("2000-01-01T16:30:00Z").toEpochMilliseconds()

        assertEquals("1월 1일 기준", projectDigestDateLabel(instant, TimeZone.UTC))
        assertEquals("1월 2일 기준", projectDigestDateLabel(instant, TimeZone.of("Asia/Seoul")))
        assertEquals("1월 1일 기준", projectDigestDateLabel(instant, TimeZone.of("America/Los_Angeles")))
    }

    @Test
    fun yearBoundaryStillUsesConvertedMonthAndDay() {
        val instant = Instant.parse("1999-12-31T23:30:00Z").toEpochMilliseconds()

        assertEquals("12월 31일 기준", projectDigestDateLabel(instant, TimeZone.UTC))
        assertEquals("1월 1일 기준", projectDigestDateLabel(instant, TimeZone.of("Asia/Seoul")))
    }
}
