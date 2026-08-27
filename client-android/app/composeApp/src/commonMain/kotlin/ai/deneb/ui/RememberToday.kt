package ai.deneb.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import kotlinx.coroutines.delay
import kotlinx.datetime.DateTimeUnit
import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlinx.datetime.LocalTime
import kotlinx.datetime.TimeZone
import kotlinx.datetime.plus
import kotlinx.datetime.toInstant
import kotlinx.datetime.todayIn
import kotlin.time.Clock

/**
 * Today's date in [tz], kept current across midnight.
 *
 * A plain `remember { todayIn(tz) }` is wrong on this app specifically: the five
 * bottom tabs live in an always-alive pane (LiveTabPane), so their `remember`s
 * survive for the whole process — and the phone keeps that process for days. The
 * date they captured on first open then freezes, and every "오늘" ring, header and
 * overdue rollup silently describes the day the screen was first shown.
 *
 * Re-reads at each local midnight, and immediately when one was crossed while the
 * app was backgrounded (the wait goes non-positive and is coerced to a short
 * delay).
 *
 * The wait is CAPPED rather than slept in one shot to midnight. On Android a
 * coroutine `delay` on the main dispatcher is `Handler.postDelayed`, measured
 * against `SystemClock.uptimeMillis()` — a clock that STOPS while the device is
 * in deep sleep. A timer armed at 19:00 for "5 hours until midnight" therefore
 * has barely advanced after a night's sleep, and the loop never reaches the
 * re-read above: measured 2026-08-27 10:23 KST, the feed still headed 8월 26일
 * with "오늘 ·" while the real today was the 27th. Re-reading the WALL clock on a
 * bounded interval is immune to that skew — the sleep-crossing path in the
 * paragraph above only ever runs if the previous wait actually completed.
 */
@Composable
fun rememberToday(tz: TimeZone): LocalDate {
    var today by remember(tz) { mutableStateOf(Clock.System.todayIn(tz)) }
    LaunchedEffect(tz) {
        while (true) {
            delay(todayRecheckDelayMs(today, Clock.System.now().toEpochMilliseconds(), tz))
            today = Clock.System.todayIn(tz)
        }
    }
    return today
}

/**
 * Longest safe wait before re-reading the wall clock, given the day currently
 * displayed. Midnight when that is soon, [MaxTodayRecheckMs] otherwise.
 *
 * Split out as a pure function so the cap is testable: the bug it exists for is
 * invisible in a composable test, since a virtual-time scheduler advances the
 * very clock that deep sleep freezes on a real device.
 */
internal fun todayRecheckDelayMs(today: LocalDate, nowMs: Long, tz: TimeZone): Long {
    val nextMidnight = LocalDateTime(today.plus(1, DateTimeUnit.DAY), LocalTime(0, 0))
        .toInstant(tz)
        .toEpochMilliseconds()
    return (nextMidnight - nowMs).coerceIn(1_000L, MaxTodayRecheckMs)
}

/**
 * Ceiling on the re-read interval. One wakeup a minute, and only while a screen
 * using [rememberToday] is composed — against a stale date header that survived
 * an entire morning.
 */
internal const val MaxTodayRecheckMs: Long = 60_000L
