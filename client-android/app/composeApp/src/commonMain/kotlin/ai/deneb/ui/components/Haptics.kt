package ai.deneb.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.hapticfeedback.HapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback

/**
 * Shared tactile vocabulary over [LocalHapticFeedback]. Each method maps an
 * interaction *meaning* to a platform haptic, so call sites pick by intent and
 * every surface feels consistent:
 *
 *  - [tap]       routine taps: list item, tab / nav, open, select, plain button
 *  - [toggleOn]  a switch / expander turning ON (sandbox open, thinking expand)
 *  - [toggleOff] the same turning OFF
 *  - [confirm]   a committing success: save, send, run an action
 *  - [reject]    a destructive / negative commit: delete, discard
 *  - [longPress] a long-press gesture landing. NOTE: combinedClickable (and thus
 *    denebPressable's onLongClick path) already fires this automatically since
 *    foundation 1.9 — only call it for hand-rolled long-press gesture detectors.
 *  - [segmentTick] crossing a discrete step while dragging (slider notches)
 *  - [segmentFrequentTick] same, but tuned for steps crossed in RAPID succession
 *    (fast-scroll index flick) — a lighter tick that stays crisp instead of buzzy
 *  - [arm]       a hand-rolled drag crossing its commit line — the MOVE on which a
 *    release WOULD act (sibling swipe). Fires once per line per gesture, never on
 *    the release: the moving surface is the release's feedback.
 *  - [refresh]   the same tick named for pull-to-refresh (PullToRefreshBox.onRefresh,
 *    BrowserPullTracker arming) — call sites pick the name that matches the gesture
 *
 * Back / cancel / dismiss stay silent (no call) by convention. A DECISION dialog
 * (one with a dismiss button) commits on its confirm button: [confirm], or [reject]
 * when the decision destroys or discards — and the button that opened a
 * destructive dialog already fires [reject] itself. The richer types
 * (Confirm/Reject/ToggleOn/ToggleOff) need Compose's expanded HapticFeedbackType
 * (Compose Multiplatform 1.7+); on Android they degrade gracefully to a sensible
 * vibration when the OS lacks the exact constant.
 */
class Haptics(private val hf: HapticFeedback) {
    // VirtualKey = the OS keyboard-tap strength (2026-07 햅틱 강화): TextHandleMove
    // was the faintest tick and read as "no feedback" on the daily-driver Galaxy.
    fun tap() = hf.performHapticFeedback(HapticFeedbackType.VirtualKey)
    fun toggleOn() = hf.performHapticFeedback(HapticFeedbackType.ToggleOn)
    fun toggleOff() = hf.performHapticFeedback(HapticFeedbackType.ToggleOff)

    /** Route to [toggleOn] / [toggleOff] from the new toggle state. */
    fun toggle(on: Boolean) = if (on) toggleOn() else toggleOff()
    fun confirm() = hf.performHapticFeedback(HapticFeedbackType.Confirm)
    fun reject() = hf.performHapticFeedback(HapticFeedbackType.Reject)
    fun longPress() = hf.performHapticFeedback(HapticFeedbackType.LongPress)
    fun segmentTick() = hf.performHapticFeedback(HapticFeedbackType.SegmentTick)
    fun segmentFrequentTick() = hf.performHapticFeedback(HapticFeedbackType.SegmentFrequentTick)
    fun arm() = hf.performHapticFeedback(HapticFeedbackType.GestureThresholdActivate)
    fun refresh() = arm()
}

@Composable
fun rememberHaptics(): Haptics {
    val hf = LocalHapticFeedback.current
    return remember(hf) { Haptics(hf) }
}
