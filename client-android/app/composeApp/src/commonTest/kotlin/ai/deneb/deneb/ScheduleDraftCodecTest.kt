package ai.deneb.deneb

import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs
import kotlin.test.assertTrue
import kotlin.time.Instant

class ScheduleDraftCodecTest {

    private val today = LocalDate(2026, 7, 11)

    private fun draft(
        mode: SchedMode,
        time: String = "09:00",
        weekdays: Set<Int> = emptySet(),
        interval: String = "30",
        unit: IntervalUnit = IntervalUnit.MIN,
        onceDate: LocalDate = today,
        raw: String = "",
    ) = ScheduleDraft(mode, time, weekdays, interval, unit, onceDate, raw)

    @Test
    fun parsesFriendlyDailyCron() {
        val parsed = parseScheduleDraft("cron", " 05   9 * * * ", today)

        assertEquals(SchedMode.DAILY, parsed.mode)
        assertEquals("09:05", parsed.timeText)
        assertEquals(today, parsed.onceDate)
        assertEquals("05   9 * * *", parsed.rawSpec)
    }

    @Test
    fun parsesFriendlyWeeklyCronAndNormalizesSundaySeven() {
        val parsed = parseScheduleDraft("cron", "30 18 * * 7,1,5", today)

        assertEquals(SchedMode.WEEKLY, parsed.mode)
        assertEquals("18:30", parsed.timeText)
        assertEquals(setOf(0, 1, 5), parsed.weekdays)
    }

    @Test
    fun complexCronFormsStayLosslessInAdvancedMode() {
        for (spec in listOf("*/5 8-22 * * 1-6", "0,30 9 * * *", "0 9 1 * *", "0 9 * JAN *")) {
            val parsed = parseScheduleDraft("cron", spec, today)
            assertEquals(SchedMode.ADVANCED, parsed.mode, spec)
            assertEquals(spec, parsed.rawSpec, spec)
        }
    }

    @Test
    fun outOfRangeCronClockStaysAdvancedInsteadOfCreatingAnInvalidPickerTime() {
        for (spec in listOf("60 9 * * *", "0 24 * * *", "-1 9 * * *")) {
            assertEquals(SchedMode.ADVANCED, parseScheduleDraft("cron", spec, today).mode, spec)
        }
    }

    @Test
    fun invalidOrRangedWeekdaysStayAdvanced() {
        for (spec in listOf("0 9 * * 1-5", "0 9 * * MON", "0 9 * * 8", "0 9 * * 1,,3")) {
            assertEquals(SchedMode.ADVANCED, parseScheduleDraft("cron", spec, today).mode, spec)
        }
    }

    @Test
    fun parsesMinuteAndHourIntervals() {
        val minutes = parseScheduleDraft("every", "15m", today)
        assertEquals(SchedMode.INTERVAL, minutes.mode)
        assertEquals("15", minutes.intervalText)
        assertEquals(IntervalUnit.MIN, minutes.intervalUnit)

        val hours = parseScheduleDraft("every", "6h", today)
        assertEquals(SchedMode.INTERVAL, hours.mode)
        assertEquals("6", hours.intervalText)
        assertEquals(IntervalUnit.HOUR, hours.intervalUnit)
    }

    @Test
    fun malformedIntervalsStayAdvancedWithTheirRawValue() {
        for (spec in listOf("", "15", "15d", "15M", "every 5m")) {
            val parsed = parseScheduleDraft("every", spec, today)
            assertEquals(SchedMode.ADVANCED, parsed.mode, spec)
            assertEquals(spec.trim(), parsed.rawSpec, spec)
        }
    }

    @Test
    fun parsesOneShotInstantUsingTheSystemZone() {
        val source = Instant.parse("2026-07-11T00:05:00Z")
        val parsed = parseScheduleDraft("at", source.toString(), today)

        assertEquals(SchedMode.ONCE, parsed.mode)
        val rebuilt = buildScheduleSpec(parsed, TimeZone.currentSystemDefault()).getOrThrow()
        assertEquals(source, Instant.parse(rebuilt))
    }

    @Test
    fun malformedInstantAndUnknownKindStayAdvanced() {
        val malformed = parseScheduleDraft("at", "tomorrow morning", today)
        assertEquals(SchedMode.ADVANCED, malformed.mode)
        assertEquals("tomorrow morning", malformed.rawSpec)

        val unknown = parseScheduleDraft("calendar", "09:00", today)
        assertEquals(SchedMode.ADVANCED, unknown.mode)
        assertEquals("09:00", unknown.rawSpec)
    }

    @Test
    fun buildsDailyAndWeeklyCronSpecs() {
        assertEquals("5 9 * * *", buildScheduleSpec(draft(SchedMode.DAILY, time = " 9 : 05 "), TimeZone.UTC).getOrThrow())
        assertEquals(
            "45 18 * * 0,1,5",
            buildScheduleSpec(
                draft(SchedMode.WEEKLY, time = "18:45", weekdays = linkedSetOf(5, 0, 1)),
                TimeZone.UTC,
            ).getOrThrow(),
        )
    }

    @Test
    fun buildsMinuteAndHourIntervals() {
        assertEquals("15m", buildScheduleSpec(draft(SchedMode.INTERVAL, interval = " 15 "), TimeZone.UTC).getOrThrow())
        assertEquals(
            "6h",
            buildScheduleSpec(draft(SchedMode.INTERVAL, interval = "6", unit = IntervalUnit.HOUR), TimeZone.UTC).getOrThrow(),
        )
    }

    @Test
    fun buildsOneShotInstantInRequestedZone() {
        val once = draft(SchedMode.ONCE, time = "09:05", onceDate = LocalDate(2026, 7, 11))

        assertEquals("2026-07-11T09:05:00Z", buildScheduleSpec(once, TimeZone.UTC).getOrThrow())
        assertEquals("2026-07-11T00:05:00Z", buildScheduleSpec(once, TimeZone.of("Asia/Seoul")).getOrThrow())
    }

    @Test
    fun advancedModeTrimsButOtherwisePreservesTheExpression() {
        assertEquals(
            "*/5 8-22 * * 1-6",
            buildScheduleSpec(draft(SchedMode.ADVANCED, raw = "  */5 8-22 * * 1-6  "), TimeZone.UTC).getOrThrow(),
        )
    }

    @Test
    fun invalidClockValuesReturnActionableErrors() {
        for (time in listOf("", "9", "24:00", "23:60", "aa:bb")) {
            val result = buildScheduleSpec(draft(SchedMode.DAILY, time = time), TimeZone.UTC)
            assertTrue(result.isFailure, time)
            assertEquals("시간 형식을 확인해 주세요 (예: 09:00).", result.exceptionOrNull()?.message, time)
        }
    }

    @Test
    fun weeklyModeRequiresAtLeastOneWeekdayBeforeClockValidation() {
        val result = buildScheduleSpec(draft(SchedMode.WEEKLY, time = "bad", weekdays = emptySet()), TimeZone.UTC)

        assertTrue(result.isFailure)
        assertEquals("요일을 하나 이상 선택해 주세요.", result.exceptionOrNull()?.message)
    }

    @Test
    fun invalidIntervalsReturnActionableErrors() {
        for (interval in listOf("", "0", "-1", "1.5", "many")) {
            val result = buildScheduleSpec(draft(SchedMode.INTERVAL, interval = interval), TimeZone.UTC)
            assertTrue(result.isFailure, interval)
            assertEquals("주기를 숫자로 입력해 주세요.", result.exceptionOrNull()?.message, interval)
        }
    }

    @Test
    fun blankAdvancedExpressionReturnsActionableError() {
        val result = buildScheduleSpec(draft(SchedMode.ADVANCED, raw = "   "), TimeZone.UTC)

        assertTrue(result.isFailure)
        assertIs<IllegalArgumentException>(result.exceptionOrNull())
        assertEquals("일정을 입력해 주세요.", result.exceptionOrNull()?.message)
    }

    @Test
    fun friendlySpecsRoundTripWithoutChangingMeaning() {
        val cases = listOf(
            "cron" to "5 9 * * *",
            "cron" to "45 18 * * 1,3,5",
            "every" to "30m",
            "every" to "4h",
        )

        cases.forEach { (kind, spec) ->
            val parsed = parseScheduleDraft(kind, spec, today)
            assertEquals(spec, buildScheduleSpec(parsed, TimeZone.UTC).getOrThrow(), "$kind:$spec")
        }
    }
}
