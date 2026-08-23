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
 */
@Composable
fun rememberToday(tz: TimeZone): LocalDate {
    var today by remember(tz) { mutableStateOf(Clock.System.todayIn(tz)) }
    LaunchedEffect(tz) {
        while (true) {
            val nextMidnight = LocalDateTime(today.plus(1, DateTimeUnit.DAY), LocalTime(0, 0))
                .toInstant(tz)
                .toEpochMilliseconds()
            val waitMs = nextMidnight - Clock.System.now().toEpochMilliseconds()
            delay(waitMs.coerceAtLeast(1_000L))
            today = Clock.System.todayIn(tz)
        }
    }
    return today
}
