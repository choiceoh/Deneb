package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineMeasure
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class EngineDiagnosticsTest {
    @Test
    fun zeroIsMeasuredAndMissingIsNotZero() {
        assertEquals("0.0 %", diagnosticValue(EngineMeasure(value = 0.0, unit = "%", available = true)))
        assertEquals("—", diagnosticValue(EngineMeasure(value = 0.0, unit = "%", available = false)))
        assertEquals("—", diagnosticValue(EngineMeasure(value = Double.NaN, available = true)))
    }

    @Test
    fun runtimeLabelsHaveExplicitUnknowns() {
        assertEquals("실행 식별 정보 없음", runtimeDescription(""))
        val text = runtimeDescription("model st:runtime_info|boot=abcdef123|build=abcdef123456789|k=7|precision=w4")
        assertTrue(text.contains("빌드 abcdef123456"))
        assertTrue(text.contains("부팅 abcdef12"))
        assertTrue(text.contains("K 7"))
    }

    @Test
    fun sampleCountsAreOnlyGroupedWhenTheirExactCountsMatch() {
        val measured = EngineMeasure(available = true, samples = 240.0)
        assertEquals(240.0, commonSampleCount(listOf(measured, measured.copy(key = "ttft95"))))
        assertNull(commonSampleCount(listOf(measured, measured.copy(samples = 230.0))))
        // Both format to 1K; rounding must never erase their different sample populations.
        assertNull(commonSampleCount(listOf(measured.copy(samples = 1000.0), measured.copy(samples = 1001.0))))
        assertNull(commonSampleCount(listOf(EngineMeasure(available = false))))
    }

    @Test
    fun collapsedSummaryKeepsMissingAndMeasuredZeroDistinct() {
        val measures = listOf(
            EngineMeasure(key = "queue50", label = "대기 P50", value = 0.0, unit = "ms", available = true),
            EngineMeasure(key = "ttft95", label = "첫 토큰 P95", available = false),
        )
        assertEquals("대기 P50 0.0 ms · 첫 토큰 P95 —", measureSummary(measures, "queue50", "ttft95"))
    }
}
