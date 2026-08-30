package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * A false trigger here reloads the page out from under someone who was reading, so
 * most of these tests are about when the gesture must NOT fire.
 */
class BrowserPullRefreshTest {
    private val threshold = 100f
    private fun tracker() = BrowserPullTracker(threshold)

    @Test
    fun aFullPullFromTheTopTriggers() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 60f, scrollY = 0)
        assertEquals(0.6f, t.fraction)
        assertTrue(t.onEvent(BrowserPullPhase.UP, y = 120f, scrollY = 0))
    }

    @Test
    fun aShortPullDoesNotTrigger() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 40f, scrollY = 0)
        assertFalse(t.onEvent(BrowserPullPhase.UP, y = 40f, scrollY = 0))
        assertEquals(0f, t.fraction)
    }

    @Test
    fun aDragThatStartsMidPageNeverArms() {
        // Reading a long page, scrolling up past the top, and letting go must not
        // reload — otherwise every long read ends in one.
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 400)
        t.onEvent(BrowserPullPhase.MOVE, y = 300f, scrollY = 0)
        assertEquals(0f, t.fraction)
        assertFalse(t.onEvent(BrowserPullPhase.UP, y = 300f, scrollY = 0))
    }

    @Test
    fun scrollingUnderTheFingerDisarmsThePull() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 30f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 10f, scrollY = 120) // page moved
        assertEquals(0f, t.fraction)
        // Even if the page comes back to the top before release.
        assertFalse(t.onEvent(BrowserPullPhase.UP, y = 400f, scrollY = 0))
    }

    @Test
    fun releasingAwayFromTheTopDoesNotTrigger() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        assertFalse(t.onEvent(BrowserPullPhase.UP, y = 300f, scrollY = 5))
    }

    @Test
    fun aCancelledGestureDoesNotTrigger() {
        // The system steals the gesture (edge back, notification shade, multi-touch).
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 300f, scrollY = 0)
        t.onEvent(BrowserPullPhase.CANCEL, y = 300f, scrollY = 0)
        assertEquals(0f, t.fraction)
        assertFalse(t.onEvent(BrowserPullPhase.UP, y = 300f, scrollY = 0))
    }

    @Test
    fun anUpwardDragReportsNoPull() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 200f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 100f, scrollY = 0)
        assertEquals(0f, t.fraction)
        assertFalse(t.onEvent(BrowserPullPhase.UP, y = 100f, scrollY = 0))
    }

    @Test
    fun fractionIsClampedAtOne() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 5000f, scrollY = 0)
        assertEquals(1f, t.fraction)
    }

    @Test
    fun aDegenerateThresholdNeverTriggers() {
        // Guards the density lookup: a zero threshold would otherwise make every
        // touch a reload, since any drag is >= 0.
        val t = BrowserPullTracker(0f)
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 10f, scrollY = 0)
        assertEquals(0f, t.fraction)
        assertFalse(t.onEvent(BrowserPullPhase.UP, y = 10f, scrollY = 0))
    }

    @Test
    fun aSecondPullWorksAfterTheFirst() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        assertTrue(t.onEvent(BrowserPullPhase.UP, y = 150f, scrollY = 0))
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        assertTrue(t.onEvent(BrowserPullPhase.UP, y = 150f, scrollY = 0))
    }
}
