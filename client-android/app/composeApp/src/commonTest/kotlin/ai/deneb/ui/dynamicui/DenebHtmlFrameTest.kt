package ai.deneb.ui.dynamicui

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Both failures this guards are silent. A frame that stops growing shows the top
 * of the card and nothing says the rest exists; a frame that cannot shrink keeps
 * whatever inflated number it heard first and leaves screens of blank under the
 * content. The repo shipped one of each, a day apart.
 */
class DenebHtmlFrameTest {
    @Test
    fun aTallCardGetsAFrameThatFitsIt() {
        // Roughly the briefing card that exposed the old 900dp ceiling: at 900dp it
        // showed 3 of its 9 sections and the rest was unreachable.
        val fitted = denebHtmlFrameHeight(2800)
        assertTrue(fitted >= 2800, "frame $fitted must cover the reported 2800dp")
    }

    @Test
    fun aLaterSmallerReportShrinksTheFrame() {
        // THE regression. A page's first measurement can land before its fonts or
        // its final width do; the settled report is smaller and must win, or the
        // card keeps the inflated height forever.
        val inflated = denebHtmlFrameHeight(3700)
        val settled = denebHtmlFrameHeight(2800)
        assertTrue(settled < inflated, "settled $settled must be below inflated $inflated")
        assertTrue(settled >= 2800)
    }

    @Test
    fun repeatingTheSameReportKeepsTheSameFrame() {
        assertEquals(denebHtmlFrameHeight(2800), denebHtmlFrameHeight(2800))
    }

    @Test
    fun aShortCardKeepsTheMinimumFrame() {
        assertEquals(DENEB_HTML_MIN_HEIGHT_DP, denebHtmlFrameHeight(40))
        assertEquals(DENEB_HTML_MIN_HEIGHT_DP, denebHtmlFrameHeight(0))
    }

    @Test
    fun theBackstopBoundsAViewportSizedPage() {
        // `min-height:100vh` reports back a height derived from the frame it was
        // given, so it climbs. This ceiling is the only thing that stops it.
        assertEquals(DENEB_HTML_MAX_HEIGHT_DP, denebHtmlFrameHeight(20_000))
        assertEquals(DENEB_HTML_MAX_HEIGHT_DP, denebHtmlFrameHeight(DENEB_HTML_MAX_HEIGHT_DP))
    }

    @Test
    fun nonsenseReportsCannotCollapseOrOverflowTheFrame() {
        assertEquals(DENEB_HTML_MIN_HEIGHT_DP, denebHtmlFrameHeight(-5000))
        assertEquals(DENEB_HTML_MAX_HEIGHT_DP, denebHtmlFrameHeight(Int.MAX_VALUE))
    }
}
