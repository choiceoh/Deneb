package ai.deneb.deneb

/** Touch phases [BrowserPullTracker] reacts to, named apart from any platform type. */
internal enum class BrowserPullPhase { DOWN, MOVE, UP, CANCEL }

/**
 * Decides when a downward drag over the page is a pull-to-refresh.
 *
 * Entry point: [onEvent], fed by the Android touch observer in
 * `DenebWebView.android.kt`. Tests: `commonTest/.../BrowserPullRefreshTest.kt`.
 * Verify: `make ci ARGS=--kotlin`.
 *
 * Kept here, off the platform, because the arming rules are the whole feature and
 * they are easy to get subtly wrong: a pull must start at the very top, must stop
 * being a pull the moment the page scrolls under the finger, and must never fire
 * from a fling or a cancelled gesture. None of that is observable in a screenshot.
 *
 * This type only *decides*. It never consumes the gesture — the caller's listener
 * returns false for every event, so the WebView keeps handling scrolling, taps and
 * text selection exactly as before.
 */
internal class BrowserPullTracker(private val thresholdPx: Float) {
    private var startY = 0f
    private var armed = false
    private var buzzed = false

    /** 0f..1f — how close the current drag is to triggering. 0f when not pulling. */
    var fraction: Float = 0f
        private set

    /**
     * True on exactly the one MOVE where the drag first reaches the threshold —
     * the moment the release WOULD reload. That is when the haptic fires, not on
     * the release itself: the point of the tick is to tell the finger "let go now"
     * while it can still change its mind, which is the same contract every other
     * pull-to-refresh surface has (Haptics.refresh, `PTR 트리거=refresh`).
     *
     * Once per gesture. A drag that hovers at the threshold, backs off and returns
     * must not buzz again — the second crossing is the same decision.
     */
    var justArmed: Boolean = false
        private set

    /** True exactly once, on the release that should reload. */
    fun onEvent(phase: BrowserPullPhase, y: Float, scrollY: Int): Boolean {
        when (phase) {
            BrowserPullPhase.DOWN -> {
                justArmed = false
                buzzed = false
                startY = y
                // Only a drag that BEGINS at the top can be a pull. Scrolling up to
                // the top and continuing must stay a scroll, or every read of a long
                // page would end in an accidental reload.
                armed = scrollY == 0
                fraction = 0f
            }

            BrowserPullPhase.MOVE -> {
                justArmed = false
                if (!armed) return false
                if (scrollY != 0) {
                    // The page moved under the finger; this gesture is a scroll now.
                    armed = false
                    fraction = 0f
                } else {
                    fraction = if (thresholdPx <= 0f) 0f else ((y - startY) / thresholdPx).coerceIn(0f, 1f)
                    if (fraction >= 1f && !buzzed) {
                        justArmed = true
                        buzzed = true
                    }
                }
            }

            BrowserPullPhase.UP -> {
                justArmed = false
                val trigger = armed && scrollY == 0 && thresholdPx > 0f && y - startY >= thresholdPx
                armed = false
                fraction = 0f
                return trigger
            }

            BrowserPullPhase.CANCEL -> {
                justArmed = false
                armed = false
                fraction = 0f
            }
        }
        return false
    }
}
