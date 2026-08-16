package ai.deneb.ui

import kotlin.test.Test
import kotlin.test.assertEquals

class DenebMotionTokenTest {
    @Test
    fun denebUiRevealCadenceStaysAuthored() {
        assertEquals(280, DenebMotion.DurationCardEnter)
        assertEquals(320, DenebMotion.DurationCardSlide)
        assertEquals(60, DenebMotion.StaggerCard)
        assertEquals(700, DenebMotion.DurationChartDraw)
        assertEquals(600, DenebMotion.DurationStatCount)
    }
}
