package ai.deneb.ui

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The arm tick promises "let go now and the page switches". A tick without a
 * switch, or a switch without a tick, breaks that promise — so most of these pin
 * the two to each other.
 */
class SiblingSwipeTrackerTest {
    private val commit = 72f
    private fun both() = SiblingSwipeTracker(commit, canSwipeLeft = true, canSwipeRight = true)

    @Test
    fun armsOnTheMoveThatFirstReachesTheLineAndCommitsOnRelease() {
        val t = both()
        t.onDown()
        assertFalse(t.onMove(-30f))
        assertFalse(t.onMove(-71f))
        assertTrue(t.onMove(-72f))
        assertEquals(SiblingSwipeDirection.Left, t.onUp(-90f))
    }

    @Test
    fun armsOncePerLineEvenWhenTheFingerHoversAcrossIt() {
        val t = both()
        t.onDown()
        assertTrue(t.onMove(-80f))
        assertFalse(t.onMove(-60f)) // backed off
        assertFalse(t.onMove(-100f)) // came back — same decision, no second buzz
        assertEquals(SiblingSwipeDirection.Left, t.onUp(-100f))
    }

    @Test
    fun theOtherLineIsANewDecision() {
        val t = both()
        t.onDown()
        assertTrue(t.onMove(-80f))
        assertTrue(t.onMove(80f)) // reversed all the way to the right line
        assertFalse(t.onMove(-80f)) // back to the left line: already armed
        assertEquals(SiblingSwipeDirection.Right, t.onUp(80f))
    }

    @Test
    fun aReleaseShortOfTheLineSpringsBackAndNeverArmed() {
        val t = both()
        t.onDown()
        assertFalse(t.onMove(-40f))
        assertNull(t.onUp(-40f))
    }

    @Test
    fun aSideWithNoDestinationNeverArmsNorCommits() {
        // The 로그 lane can only swipe right (back to 결재); a leftward drag there
        // must stay silent — a tick would promise a page that does not exist.
        val t = SiblingSwipeTracker(commit, canSwipeLeft = false, canSwipeRight = true)
        t.onDown()
        assertFalse(t.onMove(-200f))
        assertNull(t.onUp(-200f))
        assertTrue(t.onMove(72f))
        assertEquals(SiblingSwipeDirection.Right, t.onUp(72f))
    }

    @Test
    fun theNextGestureArmsAgain() {
        val t = both()
        t.onDown()
        assertTrue(t.onMove(-80f))
        assertEquals(SiblingSwipeDirection.Left, t.onUp(-80f))
        t.onDown()
        assertTrue(t.onMove(-80f))
    }

    @Test
    fun aZeroLineNeverArmsNorCommits() {
        val t = SiblingSwipeTracker(0f, canSwipeLeft = true, canSwipeRight = true)
        t.onDown()
        assertFalse(t.onMove(-500f))
        assertNull(t.onUp(-500f))
    }
}
