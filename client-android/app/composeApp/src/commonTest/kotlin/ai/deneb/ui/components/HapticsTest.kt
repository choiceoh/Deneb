package ai.deneb.ui.components

import androidx.compose.ui.hapticfeedback.HapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import kotlin.test.Test
import kotlin.test.assertEquals

class HapticsTest {

    private class RecordingFeedback : HapticFeedback {
        val events = mutableListOf<HapticFeedbackType>()

        override fun performHapticFeedback(hapticFeedbackType: HapticFeedbackType) {
            events += hapticFeedbackType
        }
    }

    @Test
    fun everySemanticActionMapsToItsPlatformHaptic() {
        val feedback = RecordingFeedback()
        val haptics = Haptics(feedback)

        haptics.tap()
        haptics.toggleOn()
        haptics.toggleOff()
        haptics.confirm()
        haptics.reject()
        haptics.longPress()
        haptics.segmentTick()
        haptics.segmentFrequentTick()
        haptics.arm()
        haptics.refresh()

        assertEquals(
            listOf(
                HapticFeedbackType.VirtualKey,
                HapticFeedbackType.ToggleOn,
                HapticFeedbackType.ToggleOff,
                HapticFeedbackType.Confirm,
                HapticFeedbackType.Reject,
                HapticFeedbackType.LongPress,
                HapticFeedbackType.SegmentTick,
                HapticFeedbackType.SegmentFrequentTick,
                HapticFeedbackType.GestureThresholdActivate,
                HapticFeedbackType.GestureThresholdActivate,
            ),
            feedback.events,
        )
    }

    @Test
    fun toggleRoutesFromTheNewBooleanState() {
        val feedback = RecordingFeedback()
        val haptics = Haptics(feedback)

        haptics.toggle(true)
        haptics.toggle(false)
        haptics.toggle(false)
        haptics.toggle(true)

        assertEquals(
            listOf(
                HapticFeedbackType.ToggleOn,
                HapticFeedbackType.ToggleOff,
                HapticFeedbackType.ToggleOff,
                HapticFeedbackType.ToggleOn,
            ),
            feedback.events,
        )
    }

    @Test
    fun oneActionProducesExactlyOneFeedbackEvent() {
        val feedback = RecordingFeedback()
        val haptics = Haptics(feedback)

        haptics.confirm()
        assertEquals(1, feedback.events.size)

        haptics.reject()
        assertEquals(2, feedback.events.size)
    }
}
