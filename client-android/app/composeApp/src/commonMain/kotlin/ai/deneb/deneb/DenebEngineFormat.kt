package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineDay
import ai.deneb.deneb.generated.EngineGlance
import ai.deneb.deneb.generated.EngineOutage
import ai.deneb.deneb.generated.EngineTotals
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toLocalDateTime
import kotlin.math.roundToInt
import kotlin.time.Instant

/*
 * Pure helpers behind the engine screen — sample mass, formatting, the 24-hour
 * availability strip and the 더보기 tile line. No Compose here, so every rule
 * the screen relies on is unit-testable (EngineAvailabilityTest).
 */

// --- sample mass ---------------------------------------------------------

/**
 * A decode rate is the mean of the engine's per-output-token times, so its
 * sample count is the number of INTER-token intervals — roughly generated
 * tokens minus requests, since the first token of each request is timed as
 * TTFT instead. The relative standard error of such a mean falls as 1/sqrt(n),
 * so below about a hundred intervals the number moves by more than ten percent
 * on its own. That is wider than the day-to-day differences this panel exists
 * to show, so those days are rendered as an indication rather than a rate.
 *
 * The engine's counters are honest either way — this is a display threshold,
 * not a correction. Nothing is hidden: the rate still shows, marked.
 */
internal fun engineSampleIsThin(requests: Long, generatedTokens: Long): Boolean = generatedTokens - requests < ENGINE_MIN_DECODE_SAMPLES

private const val ENGINE_MIN_DECODE_SAMPLES = 100L

internal fun EngineDay.sampleIsThin(): Boolean = engineSampleIsThin(requests, generatedTokens)

internal fun EngineTotals.sampleIsThin(): Boolean = engineSampleIsThin(requests, generatedTokens)

// --- formatting ----------------------------------------------------------

/** One decimal below 10%, none above — a share is read, not audited. */
internal fun formatPercent(pct: Double): String = when {
    pct <= 0.0 -> "0%"
    pct >= 10.0 -> "${pct.roundToInt()}%"
    else -> "${(pct * 10).roundToInt() / 10.0}%"
}

/** Token rates round to whole tokens: the sampling interval is coarser than a
 *  decimal place would suggest. */
internal fun formatRate(perSec: Double): String = if (perSec <= 0.0) "—" else "${perSec.roundToInt()} tok/s"

internal fun formatConcurrency(c: Double): String = if (c <= 0.0) "—" else "${(c * 10).roundToInt() / 10.0}"

/** Sub-second latencies read in milliseconds; above that, one decimal second. */
internal fun formatSeconds(s: Double): String = when {
    s <= 0.0 -> "—"
    s < 1.0 -> "${(s * 1000).roundToInt()}ms"
    else -> "${(s * 10).roundToInt() / 10.0}초"
}

/** Busy/observed spans read as hours and minutes — a raw second count of a
 *  day-long window is unreadable. */
internal fun formatDuration(seconds: Double): String {
    if (seconds <= 0.0) return "—"
    val total = seconds.roundToInt()
    val h = total / 3600
    val m = (total % 3600) / 60
    return when {
        h > 0 -> "${h}시간 ${m}분"
        m > 0 -> "${m}분"
        else -> "${total}초"
    }
}

/** "12분째" — how long the current standing has held, against the gateway's
 *  clock (never the phone's, which may drift). Under a minute reads "방금". */
internal fun formatSinceFor(sinceMs: Long, nowMs: Long): String {
    if (sinceMs <= 0L || nowMs <= sinceMs) return "방금"
    val sec = (nowMs - sinceMs) / 1000.0
    return if (sec < 60.0) "방금" else "${formatDuration(sec)}째"
}

/** "55분 전" for a moment in the past. */
internal fun formatAgo(ms: Long, nowMs: Long): String {
    if (ms <= 0L || nowMs <= ms) return "방금"
    val sec = (nowMs - ms) / 1000.0
    return if (sec < 60.0) "방금" else "${formatDuration(sec)} 전"
}

/** GPU/host memory in decimal gigabytes with one decimal ("17.8 GB") — the
 *  unit the fleet's own numbers are quoted in (a 128 GB DGX). */
internal fun formatGigabytes(bytes: Long): String {
    if (bytes <= 0L) return "—"
    val gb = bytes / 1_000_000_000.0
    return "${(gb * 10).roundToInt() / 10.0} GB"
}

/** Wall-clock "HH:mm" in [zone]. The zone is a parameter so the preview
 *  golden renders the same on every machine (CI is UTC, the fleet is KST). */
internal fun formatClock(ms: Long, zone: TimeZone): String {
    val t = Instant.fromEpochMilliseconds(ms).toLocalDateTime(zone)
    return "${t.hour.toString().padStart(2, '0')}:${t.minute.toString().padStart(2, '0')}"
}

/** "HH:mm", prefixed with "M/D " when [ms] is not on the same local date as
 *  [nowMs] — an outage that ran across days must not read "17:00 ~ 17:00". */
internal fun formatClockOrDay(ms: Long, nowMs: Long, zone: TimeZone): String {
    val t = Instant.fromEpochMilliseconds(ms).toLocalDateTime(zone)
    val today = Instant.fromEpochMilliseconds(nowMs).toLocalDateTime(zone).date
    val clock = formatClock(ms, zone)
    return if (t.date == today) clock else "${t.date.monthNumber}/${t.date.dayOfMonth} $clock"
}

/** "2026-09-16" → "9/16". */
internal fun formatDayShort(day: String): String {
    val parts = day.split('-')
    if (parts.size != 3) return day
    return "${parts[1].trimStart('0')}/${parts[2].trimStart('0')}"
}

/**
 * The probe's reason as the operator reads it. "connection refused" is the
 * fleet's usual shape — nothing listening on the port, i.e. no engine process
 * (a deploy or a kernel campaign took the fleet) — and the ST engine's own
 * /health says "draining (handing over to …)" during a handover.
 */
internal fun downReasonLabel(reason: String): String = when {
    reason.isBlank() -> ""

    reason.contains("refused", ignoreCase = true) -> "연결 거부 — 엔진 프로세스가 없습니다"

    reason.contains("draining", ignoreCase = true) -> {
        val target = Regex("handing over to ([^)]+)").find(reason)?.groupValues?.get(1)
        if (target != null) "핸드오버 드레인 중 → $target" else "핸드오버 드레인 중"
    }

    reason.startsWith("health 5") -> "엔진 비정상 응답 ($reason)"

    reason.contains("timeout", ignoreCase = true) || reason.contains("deadline", ignoreCase = true) -> "응답 없음 (시간 초과)"

    else -> reason
}

/** A local share in percent, or null when nothing was metered. */
internal fun sharePercent(local: Long, other: Long): Double? {
    val total = local + other
    return if (total > 0) 100.0 * local / total else null
}

// --- availability strip --------------------------------------------------

internal enum class AvailabilityState { UP, DOWN, UNKNOWN }

/** One run of the strip, as fractions of the window ([start], [end] in 0..1). */
internal data class AvailabilitySegment(val start: Float, val end: Float, val state: AvailabilityState)

internal const val AVAILABILITY_WINDOW_MS: Long = 24L * 3_600_000L

/**
 * Folds the outages into a strip for the last [windowMs] ending at [nowMs].
 * Time before [trackedSinceMs] — or all of it when nothing is tracked — is
 * UNKNOWN, not UP: a blank ledger says nothing about the engine, and drawing
 * it green would be the same lie as the probe's one instant.
 */
internal fun availabilitySegments(
    outages: List<EngineOutage>,
    trackedSinceMs: Long,
    nowMs: Long,
    windowMs: Long = AVAILABILITY_WINDOW_MS,
): List<AvailabilitySegment> {
    if (windowMs <= 0L || nowMs <= 0L) return emptyList()
    val windowStart = nowMs - windowMs
    fun frac(ms: Long): Float = ((ms - windowStart).toDouble() / windowMs).coerceIn(0.0, 1.0).toFloat()
    val out = mutableListOf<AvailabilitySegment>()
    val knownFrom = if (trackedSinceMs <= 0L) nowMs else maxOf(windowStart, trackedSinceMs)
    if (knownFrom > windowStart) out += AvailabilitySegment(0f, frac(knownFrom), AvailabilityState.UNKNOWN)
    if (knownFrom >= nowMs) return out
    var cursor = knownFrom
    val downs = outages
        .map { o -> o.sinceMs to (if (o.untilMs > 0L) o.untilMs else nowMs) }
        .filter { (since, until) -> until > knownFrom && since < nowMs }
        .sortedBy { it.first }
    for ((since, until) in downs) {
        val s = maxOf(since, cursor)
        val e = minOf(until, nowMs)
        if (e <= s) continue
        if (s > cursor) out += AvailabilitySegment(frac(cursor), frac(s), AvailabilityState.UP)
        out += AvailabilitySegment(frac(s), frac(e), AvailabilityState.DOWN)
        cursor = e
    }
    if (cursor < nowMs) out += AvailabilitySegment(frac(cursor), frac(nowMs), AvailabilityState.UP)
    return out
}

/** The outages overlapping the last [windowMs] (input order kept: newest first). */
internal fun outagesWithin(outages: List<EngineOutage>, nowMs: Long, windowMs: Long = AVAILABILITY_WINDOW_MS): List<EngineOutage> {
    val windowStart = nowMs - windowMs
    return outages.filter { o -> (if (o.untilMs > 0L) o.untilMs else nowMs) > windowStart }
}

/** Seconds of the window the engine was down, from the strip's own segments. */
internal fun downSecondsIn(segments: List<AvailabilitySegment>, windowMs: Long = AVAILABILITY_WINDOW_MS): Double = segments.filter { it.state == AvailabilityState.DOWN }.sumOf { (it.end - it.start).toDouble() } * windowMs / 1000.0

// --- 더보기 tile ----------------------------------------------------------

/** The 더보기 engine row's status line. [up] is null when nothing is known. */
internal data class EngineTileStatus(val text: String, val up: Boolean?, val subtitle: String?)

internal fun engineTileStatus(glance: EngineGlance?): EngineTileStatus? {
    if (glance == null || !glance.configured) return null
    if (!glance.livenessTracked) return EngineTileStatus("상태 미확인", null, null)
    val share = sharePercent(glance.todayLocalRequests, glance.todayRemoteRequests)
    return if (glance.engineDown) {
        val detail = buildList {
            add("끊김 ${formatSinceFor(glance.sinceMs, glance.nowMs)}")
            if (glance.todayOutages > 0) add("오늘 ${glance.todayOutages}회 · ${formatDuration(glance.todayDownSeconds)}")
        }.joinToString(" · ")
        EngineTileStatus("클라우드로 우회 중", false, detail)
    } else {
        val detail = buildList {
            if (glance.decodeTokensPerSec > 0.0) add(formatRate(glance.decodeTokensPerSec))
            if (share != null) add("오늘 로컬 ${formatPercent(share)}")
            if (glance.todayOutages > 0) add("끊김 ${glance.todayOutages}회")
        }.joinToString(" · ").ifBlank { null }
        EngineTileStatus("가동 중", true, detail)
    }
}
