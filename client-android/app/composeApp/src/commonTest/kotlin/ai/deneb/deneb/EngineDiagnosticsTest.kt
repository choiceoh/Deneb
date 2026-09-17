package ai.deneb.deneb

import ai.deneb.deneb.generated.EngineMeasure
import kotlin.test.Test
import kotlin.test.assertEquals
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
}
