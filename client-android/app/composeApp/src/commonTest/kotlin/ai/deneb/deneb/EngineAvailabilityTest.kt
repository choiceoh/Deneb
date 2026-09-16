package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineGlance
import ai.deneb.deneb.generated.EngineOutage
import ai.deneb.deneb.generated.EngineRoutingRow
import kotlinx.datetime.TimeZone
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The 24-hour strip is the card the 2026-09-16 flapping was invisible
 * without: 22 outages, eight hours, behind one green dot. Its rules — blank
 * before tracking began, never green by default, an open outage running to
 * now — are pure functions, pinned here.
 */
class EngineAvailabilityTest {
    private val h = 3_600_000L
    private val now = 1_787_270_400_000L // the frozen preview clock

    @Test
    fun untrackedTimeIsUnknownNotUp() {
        val nothing = availabilitySegments(emptyList(), trackedSinceMs = 0L, nowMs = now)
        assertEquals(listOf(AvailabilitySegment(0f, 1f, AvailabilityState.UNKNOWN)), nothing)

        // Tracking began 6h ago: the first three quarters are blank, the rest up.
        val partial = availabilitySegments(emptyList(), trackedSinceMs = now - 6 * h, nowMs = now)
        assertEquals(2, partial.size)
        assertEquals(AvailabilityState.UNKNOWN, partial[0].state)
        assertEquals(0.75f, partial[0].end, 0.001f)
        assertEquals(AvailabilityState.UP, partial[1].state)
        assertEquals(1f, partial[1].end)
    }

    @Test
    fun outagesClipToTheWindowAndAnOpenOneRunsToNow() {
        val outages = listOf(
            EngineOutage(sinceMs = now - 1 * h, untilMs = 0L, reason = "connection refused"), // ongoing
            EngineOutage(sinceMs = now - 13 * h, untilMs = now - 11 * h), // 2h, mid-window
            EngineOutage(sinceMs = now - 30 * h, untilMs = now - 22 * h), // ends 2h into the window
        )
        val segs = availabilitySegments(outages, trackedSinceMs = now - 48 * h, nowMs = now)
        val states = segs.map { it.state }
        assertEquals(
            listOf(AvailabilityState.DOWN, AvailabilityState.UP, AvailabilityState.DOWN, AvailabilityState.UP, AvailabilityState.DOWN),
            states,
        )
        assertEquals(0f, segs[0].start)
        assertEquals(2f / 24f, segs[0].end, 0.001f)
        assertEquals(1f, segs.last().end)
        // 2h + 2h + 1h of the window were down.
        assertEquals(5 * 3600.0, downSecondsIn(segs), 1.0)
        // The window's outage list keeps the newest-first order and drops the
        // one that ended before the window opened.
        val within = outagesWithin(outages + EngineOutage(sinceMs = now - 40 * h, untilMs = now - 36 * h), now)
        assertEquals(3, within.size)
        assertEquals(now - 1 * h, within[0].sinceMs)
    }

    @Test
    fun overlappingOutagesDoNotDoubleCount() {
        // A gateway restart can record a down inside an open outage; the
        // server folds those, but the strip must survive an overlap anyway.
        val outages = listOf(
            EngineOutage(sinceMs = now - 3 * h, untilMs = now - 2 * h),
            EngineOutage(sinceMs = now - 4 * h, untilMs = now - 2 * h - 1_800_000L),
        )
        val segs = availabilitySegments(outages, trackedSinceMs = now - 24 * h, nowMs = now)
        assertEquals(2 * 3600.0, downSecondsIn(segs), 1.0)
        assertTrue(segs.zipWithNext().all { (a, b) -> a.end <= b.start + 0.0001f })
    }

    @Test
    fun tileLineFollowsTheRoutingVerdict() {
        assertNull(engineTileStatus(null))
        assertNull(engineTileStatus(EngineGlance(configured = false)))
        assertEquals(
            EngineTileStatus("상태 미확인", null, null),
            engineTileStatus(EngineGlance(configured = true, livenessTracked = false)),
        )
        val down = engineTileStatus(
            EngineGlance(
                nowMs = now,
                configured = true,
                livenessTracked = true,
                engineDown = true,
                sinceMs = now - 12 * 60_000L,
                downReason = "connection refused",
                todayOutages = 3,
                todayDownSeconds = 5400.0,
            ),
        )
        assertEquals("클라우드로 우회 중", down?.text)
        assertEquals(false, down?.up)
        assertEquals("끊김 12분째 · 오늘 3회 · 1시간 30분", down?.subtitle)
        val up = engineTileStatus(
            EngineGlance(
                nowMs = now, configured = true, livenessTracked = true, engineDown = false,
                sinceMs = now - h, decodeTokensPerSec = 54.6, todayLocalRequests = 1618, todayRemoteRequests = 720, todayOutages = 3,
            ),
        )
        assertEquals("가동 중", up?.text)
        assertEquals(true, up?.up)
        assertEquals("55 tok/s · 오늘 로컬 69% · 끊김 3회", up?.subtitle)
    }

    @Test
    fun reasonsAndClocksReadAsTheOperatorDoes() {
        assertEquals("연결 거부 — 엔진 프로세스가 없습니다", downReasonLabel("connection refused"))
        assertEquals("핸드오버 드레인 중 → campaign/b12x", downReasonLabel("health 503 draining (handing over to campaign/b12x)"))
        assertEquals("핸드오버 드레인 중", downReasonLabel("health 503 draining"))
        assertEquals("엔진 비정상 응답 (health 500)", downReasonLabel("health 500"))
        assertEquals("", downReasonLabel(""))
        assertEquals("방금", formatSinceFor(now - 10_000L, now))
        assertEquals("12분째", formatSinceFor(now - 12 * 60_000L, now))
        assertEquals("1시간 17분째", formatSinceFor(now - (77 * 60_000L), now))
        assertEquals("55분 전", formatAgo(now - 55 * 60_000L, now))
        assertEquals("09:00", formatClock(now, TimeZone.of("Asia/Seoul")))
        assertEquals("00:00", formatClock(now, TimeZone.UTC))
        assertEquals("9/16", formatDayShort("2026-09-16"))
        assertEquals("17.8 GB", formatGigabytes(17_813_172_224L))
        assertEquals("128.5 GB", formatGigabytes(128_520_081_408L))
        assertEquals("—", formatGigabytes(0L))
        assertEquals("0%", formatPercent(0.0))
        assertEquals("6.1%", formatPercent(6.1))
        val kst = TimeZone.of("Asia/Seoul")
        assertEquals("08:48", formatClockOrDay(now - 12 * 60_000L, now, kst))
        // The frozen clock is 2026-08-21 09:00 KST; 16 hours earlier is the day before.
        assertEquals("8/20 17:00", formatClockOrDay(now - 16 * h, now, kst))
    }

    @Test
    fun routingSuffixNamesTheRoutersHold() {
        val open = EngineRoutingRow(model = "glm-5.3-flash", local = true, known = true, circuitState = "open", retryAfterMs = 540_000)
        assertEquals(" · 서킷 열림 (9분 후 재시도)", routingStateSuffix(open))
        val dead = EngineRoutingRow(model = "glm-5.3", known = true, circuitState = "closed", keyHealth = "unreachable", upstreamMissing = true)
        assertEquals(" · 업스트림 불통 · 업스트림에 모델 없음", routingStateSuffix(dead))
        assertEquals("", routingStateSuffix(EngineRoutingRow(model = "k3", known = true, circuitState = "closed", keyHealth = "ok")))
    }
}
