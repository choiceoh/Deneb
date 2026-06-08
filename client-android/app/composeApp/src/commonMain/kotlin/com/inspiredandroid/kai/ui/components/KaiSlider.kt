@file:OptIn(ExperimentalMaterial3Api::class)

package com.inspiredandroid.kai.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RangeSlider
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.handCursor
import kotlin.math.roundToInt

@Composable
fun KaiSlider(
    value: Float,
    onValueChange: (Float) -> Unit,
    onValueChangeFinished: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
    valueRange: ClosedFloatingPointRange<Float> = 0f..1f,
    steps: Int = 0,
) {
    val haptics = rememberHaptics()
    var lastNotch by remember { mutableIntStateOf(notchIndex(value, valueRange, steps)) }
    Slider(
        value = value,
        onValueChange = {
            // One tick per discrete notch crossed while dragging; continuous
            // sliders (steps == 0) stay silent to avoid a buzz on every pixel.
            // Compare notch indices, not raw values: `value` lags a frame during
            // a drag, so `it != value` would fire on nearly every callback.
            if (steps > 0) {
                val notch = notchIndex(it, valueRange, steps)
                if (notch != lastNotch) {
                    lastNotch = notch
                    haptics.segmentTick()
                }
            }
            onValueChange(it)
        },
        onValueChangeFinished = onValueChangeFinished,
        modifier = modifier.handCursor(),
        valueRange = valueRange,
        steps = steps,
        colors = kaiSliderColors(),
        thumb = { KaiSliderThumb() },
        track = { sliderState ->
            SliderDefaults.Track(
                sliderState = sliderState,
                colors = kaiSliderTrackColors(),
                drawStopIndicator = null,
                drawTick = { _, _ -> },
            )
        },
    )
}

@Composable
fun KaiRangeSlider(
    value: ClosedFloatingPointRange<Float>,
    onValueChange: (ClosedFloatingPointRange<Float>) -> Unit,
    onValueChangeFinished: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
    valueRange: ClosedFloatingPointRange<Float> = 0f..1f,
    steps: Int = 0,
) {
    val haptics = rememberHaptics()
    var lastStart by remember { mutableIntStateOf(notchIndex(value.start, valueRange, steps)) }
    var lastEnd by remember { mutableIntStateOf(notchIndex(value.endInclusive, valueRange, steps)) }
    RangeSlider(
        value = value,
        onValueChange = {
            // Tick once per notch crossed by either thumb (see KaiSlider note on
            // why notch indices are compared instead of the raw range).
            if (steps > 0) {
                val s = notchIndex(it.start, valueRange, steps)
                val e = notchIndex(it.endInclusive, valueRange, steps)
                if (s != lastStart || e != lastEnd) {
                    lastStart = s
                    lastEnd = e
                    haptics.segmentTick()
                }
            }
            onValueChange(it)
        },
        onValueChangeFinished = onValueChangeFinished,
        modifier = modifier.handCursor(),
        valueRange = valueRange,
        steps = steps,
        startThumb = { KaiSliderThumb() },
        endThumb = { KaiSliderThumb() },
        track = { rangeSliderState ->
            SliderDefaults.Track(
                rangeSliderState = rangeSliderState,
                colors = kaiSliderTrackColors(),
                drawStopIndicator = null,
                drawTick = { _, _ -> },
            )
        },
    )
}

@Composable
private fun KaiSliderThumb() {
    Box(
        modifier = Modifier
            .size(20.dp)
            .background(MaterialTheme.colorScheme.primary, CircleShape),
    )
}

@Composable
private fun kaiSliderColors() = SliderDefaults.colors(
    thumbColor = MaterialTheme.colorScheme.primary,
    activeTrackColor = MaterialTheme.colorScheme.primary,
    inactiveTrackColor = MaterialTheme.colorScheme.surfaceVariant,
    activeTickColor = Color.Transparent,
    inactiveTickColor = Color.Transparent,
)

@Composable
private fun kaiSliderTrackColors() = SliderDefaults.colors(
    activeTrackColor = MaterialTheme.colorScheme.primary,
    inactiveTrackColor = MaterialTheme.colorScheme.surfaceVariant,
)

// notchIndex maps a slider value to its discrete step index (0..steps+1) so the
// segment haptic fires once per crossed notch instead of on every drag callback.
private fun notchIndex(v: Float, range: ClosedFloatingPointRange<Float>, steps: Int): Int {
    val span = range.endInclusive - range.start
    if (span <= 0f) return 0
    val frac = ((v - range.start) / span).coerceIn(0f, 1f)
    return (frac * (steps + 1)).roundToInt()
}
