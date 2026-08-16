package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class BrowserTranslationRequestTest {

    @Test
    fun `envelope bounds reject blank ids and oversized json`() {
        assertTrue(isBrowserTranslateEnvelopeWithinLimits("7", 48_000))
        assertFalse(isBrowserTranslateEnvelopeWithinLimits("", 10))
        assertFalse(isBrowserTranslateEnvelopeWithinLimits("x".repeat(65), 10))
        assertFalse(isBrowserTranslateEnvelopeWithinLimits("7", 48_001))
    }

    @Test
    fun `segment bounds cap count individual and total characters`() {
        assertTrue(areBrowserTranslateSegmentsWithinLimits(List(40) { "a".repeat(800) }))
        assertTrue(areBrowserTranslateSegmentsWithinLimits(listOf("a".repeat(1_200))))
        assertFalse(areBrowserTranslateSegmentsWithinLimits(emptyList()))
        assertFalse(areBrowserTranslateSegmentsWithinLimits(List(41) { "ok" }))
        assertFalse(areBrowserTranslateSegmentsWithinLimits(listOf("a".repeat(1_201))))
        assertFalse(areBrowserTranslateSegmentsWithinLimits(List(40) { "a".repeat(801) }))
    }
}
