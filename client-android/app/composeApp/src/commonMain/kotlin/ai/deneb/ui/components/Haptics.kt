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
 *  - [longPress] a long-press gesture landing
 *  - [segmentTick] crossing a discrete step while dragging (slider notches)
 *  - [segmentFrequentTick] same, but tuned for steps crossed in RAPID succession
 *    (fast-scroll index flick) — a lighter tick that stays crisp instead of buzzy
 *
 * Back / cancel / dismiss stay silent (no call) by convention. The richer types
 * (Confirm/Reject/ToggleOn/ToggleOff) need Compose's expanded HapticFeedbackType
 * (Compose Multiplatform 1.7+); on Android they degrade gracefully to a sensible
 * vibration when the OS lacks the exact constant.
 */
class Haptics(private val hf: HapticFeedback) {
    fun tap() = hf.performHapticFeedback(HapticFeedbackType.TextHandleMove)
    fun toggleOn() = hf.performHapticFeedback(HapticFeedbackType.ToggleOn)
    fun toggleOff() = hf.performHapticFeedback(HapticFeedbackType.ToggleOff)

    /** Route to [toggleOn] / [toggleOff] from the new toggle state. */
    fun toggle(on: Boolean) = if (on) toggleOn() else toggleOff()
    fun confirm() = hf.performHapticFeedback(HapticFeedbackType.Confirm)
    fun reject() = hf.performHapticFeedback(HapticFeedbackType.Reject)
    fun longPress() = hf.performHapticFeedback(HapticFeedbackType.LongPress)
    fun segmentTick() = hf.performHapticFeedback(HapticFeedbackType.SegmentTick)
    fun segmentFrequentTick() = hf.performHapticFeedback(HapticFeedbackType.SegmentFrequentTick)
}

@Composable
fun rememberHaptics(): Haptics {
    val hf = LocalHapticFeedback.current
    return remember(hf) { Haptics(hf) }
}
