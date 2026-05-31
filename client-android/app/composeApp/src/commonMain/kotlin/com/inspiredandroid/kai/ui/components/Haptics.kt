package com.inspiredandroid.kai.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.hapticfeedback.HapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback

/**
 * Small wrapper over [LocalHapticFeedback] so call sites get consistent tactile
 * feedback without each one re-picking a feedback type. [tap] is a light tick
 * for routine taps (send, tab switch, list item); [confirm] is a firmer pulse
 * for commits/long-press. Mirrors the pattern the mail screen introduced, now
 * shared so every surface feels the same.
 */
class Haptics(private val hf: HapticFeedback) {
    fun tap() = hf.performHapticFeedback(HapticFeedbackType.TextHandleMove)
    fun confirm() = hf.performHapticFeedback(HapticFeedbackType.LongPress)
}

@Composable
fun rememberHaptics(): Haptics {
    val hf = LocalHapticFeedback.current
    return remember(hf) { Haptics(hf) }
}
