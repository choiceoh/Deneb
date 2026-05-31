package com.inspiredandroid.kai.ui.components

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.inspiredandroid.kai.ui.auroraAzure
import com.inspiredandroid.kai.ui.auroraCyan
import com.inspiredandroid.kai.ui.auroraPeriwinkle
import com.inspiredandroid.kai.ui.auroraViolet

// Closed aurora loop: four evenly spaced cool-spectrum colors that the border
// sweep rotates through. Treating it as a continuous, periodic function (see
// [auroraAt]) lets the sweep spin with no visible seam at the 0deg/360deg wrap.
private val auroraLoop = listOf(auroraAzure, auroraCyan, auroraPeriwinkle, auroraViolet)

// Sweep samples placed at evenly spaced angles around the border. More samples
// make the rotation look smoother; 13 keeps each aurora transition buttery at no
// real cost. The first sample equals the last, which is what closes the loop.
private const val SWEEP_SAMPLES = 13

// One full revolution every 6s — a slow, flowing sheen rather than a fast spin.
private const val SWEEP_DURATION_MS = 6000

// Sample the aurora loop at phase [t]. [t] wraps modulo 1 so the function is
// periodic: auroraAt(x) == auroraAt(x + 1).
private fun auroraAt(t: Float): Color {
    val n = auroraLoop.size
    val phase = ((t % 1f) + 1f) % 1f
    val scaled = phase * n
    val index = scaled.toInt() % n
    val frac = scaled - scaled.toInt()
    return lerp(auroraLoop[index], auroraLoop[(index + 1) % n], frac)
}

/**
 * Draws a slowly rotating iridescent "aurora" gradient border (azure -> cyan ->
 * periwinkle -> violet). When [backgroundColor] is set it is filled behind the
 * content first, so a single modifier can supply both the surface and the
 * animated edge.
 */
@Composable
fun Modifier.animatedGradientBorder(
    cornerRadius: Dp,
    borderWidth: Dp = 2.dp,
    backgroundColor: Color? = null,
): Modifier {
    val infiniteTransition = rememberInfiniteTransition()
    val progress by infiniteTransition.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = SWEEP_DURATION_MS, easing = LinearEasing),
        ),
    )
    // Reused across frames: the perpetual animation overwrites these colors in
    // place each draw instead of allocating a fresh list every frame.
    val sweepColors = remember { MutableList(SWEEP_SAMPLES) { Color.Transparent } }
    return this.drawWithCache {
        val cr = CornerRadius(cornerRadius.toPx())
        val strokeStyle = Stroke(width = borderWidth.toPx())
        onDrawWithContent {
            if (backgroundColor != null) {
                drawRoundRect(color = backgroundColor, cornerRadius = cr)
            }
            drawContent()

            // Shift every sample by `progress` so the whole spectrum rotates.
            // Sample 0 and sample `last` land on the same phase, so the sweep is
            // seamless where it wraps.
            val p = progress
            val last = SWEEP_SAMPLES - 1
            for (k in 0..last) {
                sweepColors[k] = auroraAt(k.toFloat() / last + p)
            }
            drawRoundRect(
                brush = Brush.sweepGradient(sweepColors),
                cornerRadius = cr,
                style = strokeStyle,
            )
        }
    }
}
