package ai.deneb.ui.components

import ai.deneb.ui.auroraAzure
import ai.deneb.ui.auroraCyan
import ai.deneb.ui.auroraPeriwinkle
import ai.deneb.ui.auroraViolet
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

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

// Glow band depth (how far the aurora light reaches inward from each edge before
// fading to transparent) and how fast it flows while a reply streams. Faster than
// the calm 6s border sweep — this is the "it's thinking" liveliness.
private const val GLOW_SWEEP_DURATION_MS = 2800

/**
 * A lively aurora glow that rings the edges of the content while [active] (a reply
 * is being written) and dissolves when idle. Same cool aurora palette as
 * [animatedGradientBorder] — keeping the Deneb identity rather than a full rainbow —
 * but faster and brighter: the Gemini-style "it's responding" shimmer. Kept to a
 * soft edge glow that fades to transparent toward the center (not a full-screen
 * wash) so message text stays legible and the monochrome-restraint doctrine holds.
 *
 * Drawn over the content at the margins. Each edge samples a different point on the
 * rotating aurora loop, so the color appears to travel around the frame. When
 * [active] flips off the whole glow eases out over [intensity]; at rest it costs
 * only an early-return.
 */
@Composable
fun Modifier.streamingAuroraGlow(
    active: Boolean,
    glow: Dp = 56.dp,
    peakAlpha: Float = 0.5f,
): Modifier {
    val intensity by animateFloatAsState(
        targetValue = if (active) 1f else 0f,
        animationSpec = tween(durationMillis = 450, easing = LinearEasing),
        label = "auroraGlowIntensity",
    )
    val transition = rememberInfiniteTransition()
    val phase by transition.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = GLOW_SWEEP_DURATION_MS, easing = LinearEasing),
        ),
    )
    return this.drawWithCache {
        val g = glow.toPx()
        onDrawWithContent {
            drawContent()
            if (intensity <= 0.01f) return@onDrawWithContent
            val a = peakAlpha * intensity
            val p = phase
            // Top
            drawRect(
                brush = Brush.verticalGradient(
                    colors = listOf(auroraAt(p).copy(alpha = a), Color.Transparent),
                    startY = 0f,
                    endY = g,
                ),
                size = Size(size.width, g),
            )
            // Bottom
            drawRect(
                brush = Brush.verticalGradient(
                    colors = listOf(Color.Transparent, auroraAt(p + 0.5f).copy(alpha = a)),
                    startY = size.height - g,
                    endY = size.height,
                ),
                topLeft = Offset(0f, size.height - g),
                size = Size(size.width, g),
            )
            // Left
            drawRect(
                brush = Brush.horizontalGradient(
                    colors = listOf(auroraAt(p + 0.25f).copy(alpha = a), Color.Transparent),
                    startX = 0f,
                    endX = g,
                ),
                size = Size(g, size.height),
            )
            // Right
            drawRect(
                brush = Brush.horizontalGradient(
                    colors = listOf(Color.Transparent, auroraAt(p + 0.75f).copy(alpha = a)),
                    startX = size.width - g,
                    endX = size.width,
                ),
                topLeft = Offset(size.width - g, 0f),
                size = Size(g, size.height),
            )
        }
    }
}
