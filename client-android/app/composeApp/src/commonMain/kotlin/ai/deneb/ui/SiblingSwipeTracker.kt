package ai.deneb.ui

/** Which sibling a released swipe navigates to. */
internal enum class SiblingSwipeDirection { Left, Right }

/**
 * Decides when a horizontal sibling swipe (피드 ⇄ 결재 ⇄ 로그) has crossed its
 * commit line — the MOVE on which a release WOULD switch pages — and which
 * sibling a release lands on.
 *
 * Entry points: [onDown], [onMove], [onUp], driven by [DenebSiblingSwipeHost].
 * Tests: `commonTest/.../SiblingSwipeTrackerTest.kt`. Verify: `make ci ARGS=--kotlin`.
 *
 * Kept off the pointer loop for the same reason BrowserPullTracker is: the arm
 * haptic has one contract — it fires on the MOVE that first reaches the line,
 * once per line per gesture, never on the release — and none of that is visible
 * in a screenshot. A drag that hovers at the line, backs off and returns is the
 * same decision, so it does not buzz again; the OTHER line (a swipe that reverses
 * direction) is a new decision, so it does. A side with no destination never arms:
 * dragging toward nothing must not promise anything.
 *
 * [dx] is the finger's raw travel since down (+right), NOT the damped translation
 * the content shows — the commit test uses the same raw travel, so what the finger
 * feels is exactly what the release will do.
 */
internal class SiblingSwipeTracker(
    private val commitPx: Float,
    private val canSwipeLeft: Boolean,
    private val canSwipeRight: Boolean,
) {
    private var armedLeft = false
    private var armedRight = false

    /** A new finger: both lines are fresh. */
    fun onDown() {
        armedLeft = false
        armedRight = false
    }

    /** True on exactly the MOVE where [dx] first reaches a line that has a destination. */
    fun onMove(dx: Float): Boolean {
        if (commitPx <= 0f) return false
        if (canSwipeLeft && dx <= -commitPx && !armedLeft) {
            armedLeft = true
            return true
        }
        if (canSwipeRight && dx >= commitPx && !armedRight) {
            armedRight = true
            return true
        }
        return false
    }

    /** The sibling a release at [dx] navigates to, or null to spring back. */
    fun onUp(dx: Float): SiblingSwipeDirection? = when {
        commitPx <= 0f -> null
        canSwipeLeft && dx <= -commitPx -> SiblingSwipeDirection.Left
        canSwipeRight && dx >= commitPx -> SiblingSwipeDirection.Right
        else -> null
    }
}
