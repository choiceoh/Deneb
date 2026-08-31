package ai.deneb.ui.dynamicui

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * The failure this guards is silent: a frame that stops growing shows the top of
 * the card and nothing tells the reader the rest exists. The mirror failure is a
 * frame that grows on its own echo forever. Both are here.
 */
class DenebHtmlFrameTest {
    @Test
    fun aTallCardGetsAFrameThatFitsIt() {
        // Roughly the briefing card that exposed the old 900dp ceiling: at 900dp it
        // showed 3 of its 9 sections and the rest was unreachable.
        val fitted = denebHtmlFrameHeight(DENEB_HTML_MIN_HEIGHT_DP, 2800)
        assertTrue(fitted >= 2800, "frame $fitted must cover the reported 2800dp")
    }

    @Test
    fun theEchoAfterFittingDoesNotGrowTheFrame() {
        // Once the frame fits, documentElement.scrollHeight reports the frame's own
        // height back. Growing on that would ratchet the card upward forever.
        val fitted = denebHtmlFrameHeight(DENEB_HTML_MIN_HEIGHT_DP, 2800)
        assertEquals(fitted, denebHtmlFrameHeight(fitted, fitted))
        assertEquals(fitted, denebHtmlFrameHeight(fitted, fitted - 1))
    }

    @Test
    fun aViewportSizedPageIsStable() {
        // `min-height:100vh` reports exactly whatever frame it was given, at every
        // step. It must settle, not climb.
        var h = DENEB_HTML_MIN_HEIGHT_DP
        repeat(50) { h = denebHtmlFrameHeight(h, h) }
        assertEquals(DENEB_HTML_MIN_HEIGHT_DP, h)
    }

    @Test
    fun aPageThatMultipliesTheViewportStopsAtTheBackstop() {
        // `min-height:200vh` genuinely does report more than it was given, forever.
        // This is the one case the ceiling exists for.
        var h = DENEB_HTML_MIN_HEIGHT_DP
        repeat(100) { h = denebHtmlFrameHeight(h, h * 2) }
        assertEquals(DENEB_HTML_MAX_HEIGHT_DP, h)
    }

    @Test
    fun aShortCardKeepsTheMinimumFrame() {
        assertEquals(DENEB_HTML_MIN_HEIGHT_DP, denebHtmlFrameHeight(DENEB_HTML_MIN_HEIGHT_DP, 40))
        assertEquals(DENEB_HTML_MIN_HEIGHT_DP, denebHtmlFrameHeight(DENEB_HTML_MIN_HEIGHT_DP, 0))
    }

    @Test
    fun contentThatAppearsLaterStillGrowsTheFrame() {
        // A ResizeObserver report after an image or a script fills the page in.
        val first = denebHtmlFrameHeight(DENEB_HTML_MIN_HEIGHT_DP, 600)
        val second = denebHtmlFrameHeight(first, 1400)
        assertTrue(second > first)
        assertTrue(second >= 1400)
    }

    @Test
    fun nonsenseReportsCannotCollapseOrOverflowTheFrame() {
        val fitted = denebHtmlFrameHeight(DENEB_HTML_MIN_HEIGHT_DP, 1200)
        assertEquals(fitted, denebHtmlFrameHeight(fitted, -5000))
        assertEquals(DENEB_HTML_MAX_HEIGHT_DP, denebHtmlFrameHeight(fitted, Int.MAX_VALUE))
    }
}
