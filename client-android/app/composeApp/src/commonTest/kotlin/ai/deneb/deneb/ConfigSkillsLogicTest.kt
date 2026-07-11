package ai.deneb.deneb

import ai.deneb.deneb.generated.SkillLifecycleEvent
import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.minus
import kotlinx.datetime.toInstant
import kotlinx.datetime.toLocalDateTime
import kotlinx.datetime.todayIn
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlin.time.Clock
import kotlin.time.Instant

class ConfigSkillsLogicTest {

    private fun event(type: String, at: Long): SkillLifecycleEvent = SkillLifecycleEvent(
        type = type,
        skillName = "test-skill",
        at = at,
    )

    private fun todayAtNoon(): Long {
        val zone = TimeZone.currentSystemDefault()
        val today = Clock.System.todayIn(zone)
        return LocalDateTime(today, LocalTime(12, 0)).toInstant(zone).toEpochMilliseconds()
    }

    @Test
    fun sourceLabelsCoverEveryGatewayOrigin() {
        val expected = linkedMapOf(
            "managed" to "관리형",
            "workspace" to "워크스페이스",
            "agents-skills-personal" to "개인",
            "agents-skills-project" to "프로젝트",
            "bundled" to "기본 제공",
            "plugin" to "플러그인",
            "extra" to "추가",
        )

        expected.forEach { (source, label) ->
            assertEquals(label, skillSourceLabel(source), source)
        }
    }

    @Test
    fun unknownSourceFallsBackVerbatim() {
        for (source in listOf("", "future", " Managed ", "PLUGIN")) {
            assertEquals(source, skillSourceLabel(source))
        }
    }

    @Test
    fun lifecycleRoutesDistinguishAutomaticAndManualCreation() {
        assertEquals("판정: 변경 없음", lifecycleRouteLabel("no-op"))
        assertEquals("판정: 기존 스킬 진화", lifecycleRouteLabel("evolve"))
        assertEquals("판정: 새 스킬 생성(자동)", lifecycleRouteLabel("genesis"))
        assertEquals("판정: 새 스킬 생성(수동)", lifecycleRouteLabel("create"))
    }

    @Test
    fun lifecycleRouteOmissionAndForwardCompatibilityAreExplicit() {
        assertNull(lifecycleRouteLabel(""))
        assertNull(lifecycleRouteLabel("  "))
        assertEquals("판정: future-route", lifecycleRouteLabel("future-route"))
        assertEquals("판정: EVOLVE", lifecycleRouteLabel("EVOLVE"))
    }

    @Test
    fun absoluteLifecycleTimestampUsesSystemZoneAndMinutePrecision() {
        val epoch = Instant.parse("2000-01-01T12:34:59.999Z").toEpochMilliseconds()
        val local = Instant.fromEpochMilliseconds(epoch).toLocalDateTime(TimeZone.currentSystemDefault())
        val expected = "${local.date} ${local.hour.toString().padStart(2, '0')}:${local.minute.toString().padStart(2, '0')}"

        assertEquals(expected, lifecycleDateTime(epoch))
    }

    @Test
    fun missingLifecycleTimestampIsBlank() {
        assertEquals("", lifecycleDateTime(0))
        assertEquals("", lifecycleDateTime(-1))
        assertEquals("", lifecycleDateTime(Long.MIN_VALUE))
    }

    @Test
    fun futureRelativeTimestampIsBlank() {
        val future = Clock.System.now().toEpochMilliseconds() + 60_000

        assertEquals("", lifecycleTime(future))
    }

    @Test
    fun recentRelativeTimestampUsesJustNow() {
        val tenSecondsAgo = Clock.System.now().toEpochMilliseconds() - 10_000

        assertEquals("방금", lifecycleTime(tenSecondsAgo))
    }

    @Test
    fun relativeTimestampMovesAcrossMinuteHourAndDayUnits() {
        val now = Clock.System.now().toEpochMilliseconds()

        assertEquals("2분 전", lifecycleTime(now - 125_000))
        assertEquals("3시간 전", lifecycleTime(now - 3 * 3_600_000L - 5_000))
        assertEquals("2일 전", lifecycleTime(now - 2 * 86_400_000L - 5_000))
    }

    @Test
    fun invalidRelativeTimestampIsBlank() {
        assertEquals("", lifecycleTime(0))
        assertEquals("", lifecycleTime(-100))
    }

    @Test
    fun emptyOrOutOfDayEventsProduceNoActivityLine() {
        val zone = TimeZone.currentSystemDefault()
        val yesterday = Clock.System.todayIn(zone).minus(1, DateTimeUnit.DAY)
        val yesterdayAtNoon = LocalDateTime(yesterday, LocalTime(12, 0)).toInstant(zone).toEpochMilliseconds()

        assertEquals("오늘 활동 없음", propusTodayLine(emptyList()))
        assertEquals(
            "오늘 활동 없음",
            propusTodayLine(listOf(event("evolved", 0), event("genesis", yesterdayAtNoon))),
        )
    }

    @Test
    fun todayLineCountsEveryLifecycleBucketInDisplayOrder() {
        val today = todayAtNoon()
        val events = listOf(
            event("review", today),
            event("no-op", today),
            event("evolved", today),
            event("genesis", today),
            event("evolve_rejected", today),
            event("evolve_rolled_back", today),
        )

        assertEquals("오늘 검토 2 · 진화 1 · 생성 1 · 주의 2", propusTodayLine(events))
    }

    @Test
    fun todayLineOmitsZeroCountBuckets() {
        val today = todayAtNoon()

        assertEquals("진화 2", propusTodayLine(listOf(event("evolved", today), event("evolved", today))))
        assertEquals("주의 1", propusTodayLine(listOf(event("evolve_rejected", today))))
        assertEquals("오늘 검토 1", propusTodayLine(listOf(event("future-type", today))))
    }

    @Test
    fun eventOrderDoesNotAffectTodaySummary() {
        val today = todayAtNoon()
        val events = listOf(event("genesis", today), event("review", today), event("evolved", today))

        assertEquals(propusTodayLine(events), propusTodayLine(events.reversed()))
        assertTrue(propusTodayLine(events).startsWith("오늘 검토"))
    }
}
