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

    // The haptic tells the finger "let go now and it reloads", so it has to land
    // while the finger is still down — not on the release, which is too late to be
    // a cue, and not twice, which reads as a stutter rather than a threshold.
    @Test
    fun crossingTheThresholdArmsExactlyOnceWhileDragging() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 60f, scrollY = 0)
        assertFalse(t.justArmed, "below the threshold must not buzz")
        t.onEvent(BrowserPullPhase.MOVE, y = 120f, scrollY = 0)
        assertTrue(t.justArmed, "crossing the threshold must buzz")
        t.onEvent(BrowserPullPhase.MOVE, y = 160f, scrollY = 0)
        assertFalse(t.justArmed, "pulling further must not buzz again")
    }

    // Backing off and pulling in again is one decision, not two.
    @Test
    fun releasingBackUnderTheThresholdAndReturningDoesNotArmAgain() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 120f, scrollY = 0)
        assertTrue(t.justArmed)
        t.onEvent(BrowserPullPhase.MOVE, y = 40f, scrollY = 0)
        assertFalse(t.justArmed)
        t.onEvent(BrowserPullPhase.MOVE, y = 130f, scrollY = 0)
        assertFalse(t.justArmed, "the same gesture must buzz once")
    }

    // The release is not a second cue.
    @Test
    fun theReleaseItselfNeverArms() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 120f, scrollY = 0)
        assertTrue(t.onEvent(BrowserPullPhase.UP, y = 120f, scrollY = 0), "should still reload")
        assertFalse(t.justArmed, "the reload is the feedback at this point")
    }

    // A scroll that happens to pass the threshold distance is not a pull, so it
    // must stay silent — the same rule that keeps it from reloading.
    @Test
    fun aDragThatStartedMidPageNeverArms() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 400)
        t.onEvent(BrowserPullPhase.MOVE, y = 200f, scrollY = 400)
        assertFalse(t.justArmed)
    }

    // The page scrolling under the finger disarms the gesture; the buzz must not
    // survive that.
    @Test
    fun thePageMovingUnderTheFingerStopsTheArming() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 120f, scrollY = 30)
        assertFalse(t.justArmed)
    }

    // A new gesture is a new decision.
    @Test
    fun theNextGestureCanArmAgain() {
        val t = tracker()
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 120f, scrollY = 0)
        t.onEvent(BrowserPullPhase.UP, y = 120f, scrollY = 0)
        t.onEvent(BrowserPullPhase.DOWN, y = 0f, scrollY = 0)
        t.onEvent(BrowserPullPhase.MOVE, y = 120f, scrollY = 0)
        assertTrue(t.justArmed, "a fresh pull must buzz")
    }
}
