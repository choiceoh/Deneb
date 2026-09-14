package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineDay
import ai.deneb.deneb.generated.EngineTotals
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * The engine screen renders a decode rate for any day the engine served at all.
 * Production on 2026-09-14 held the case that motivated this: 2026-09-13 had
 * two requests and EIGHT generated tokens, and the panel showed "12 tok/s" in
 * the same type, with the same trend bar, as a day of real traffic.
 *
 * A decode rate is the mean per-output-token time, so its samples are the
 * inter-token intervals — generated minus requests, the first token of each
 * request being timed as TTFT. Six intervals do not make a rate.
 */
class EngineSampleMassTest {
    @Test
    fun countsIntervalsNotTokens() {
        // requests are subtracted: each request's first token is TTFT, not TPOT.
        val cases = listOf(
            Triple(2L, 8L, true), // the production day: 6 intervals
            Triple(1L, 1L, true), // one token, zero intervals
            Triple(0L, 0L, true), // nothing served
            Triple(1L, 100L, true), // 99 intervals — just under
            Triple(1L, 101L, false), // 100 intervals — the floor
            Triple(50L, 400L, false), // 350 intervals
            // Sample mass is intervals, not requests: many short replies can be
            // thin while one long reply is not.
            Triple(80L, 160L, true), // 80 requests, 80 intervals — still thin
            Triple(1L, 500L, false), // a single long reply is a settled sample
        )
        val failures = mutableListOf<String>()
        for ((requests, generated, wantThin) in cases) {
            val got = engineSampleIsThin(requests, generated)
            if (got != wantThin) {
                failures += "requests=$requests generated=$generated → thin=$got, want $wantThin"
            }
        }
        assertTrue(failures.isEmpty(), failures.joinToString("\n"))
    }

    @Test
    fun negativeIntervalsReadAsThin() {
        // The two counters come from different histograms and are differenced
        // independently, so a restart-adjacent window can leave generated below
        // requests. That is not a fast day; it is no sample at all.
        assertTrue(engineSampleIsThin(requests = 40L, generatedTokens = 3L))
    }

    @Test
    fun dayAndTotalUseTheSameRule() {
        val thinDay = EngineDay(day = "2026-09-13", measured = true, requests = 2L, generatedTokens = 8L)
        val fatDay = EngineDay(day = "2026-09-12", measured = true, requests = 12L, generatedTokens = 4_000L)
        assertTrue(thinDay.sampleIsThin())
        assertFalse(fatDay.sampleIsThin())

        assertTrue(EngineTotals(requests = 2L, generatedTokens = 8L).sampleIsThin())
        assertFalse(EngineTotals(requests = 12L, generatedTokens = 4_000L).sampleIsThin())

        // Same inputs, same verdict — the summary must not disagree with the
        // day rows it sums.
        assertEquals(thinDay.sampleIsThin(), EngineTotals(requests = 2L, generatedTokens = 8L).sampleIsThin())
    }
}
