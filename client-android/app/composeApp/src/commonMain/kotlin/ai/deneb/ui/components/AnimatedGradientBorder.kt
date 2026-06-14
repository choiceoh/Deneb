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
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.sin

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
 * Drawn over the content at the margins, but NOT as an even four-sided frame: the
 * light is concentrated at a bright "head" that travels around the perimeter and
 * fades away from it (see [edgeHotspot]), so it flows around the message like an
 * aurora rather than lighting all four edges at once. Its inward reach also undulates
 * along each edge and drifts over time (see [glowDepthAt]) — thick here, thin there —
 * and the hue rotates around the loop. When [active] flips off the whole glow eases
 * out over [intensity]; at rest it costs only an early-return.
 */
@Composable
fun Modifier.streamingAuroraGlow(
    active: Boolean,
    glow: Dp = 64.dp,
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
    // A separate, slower cycle: the perimeter position of the bright head as it
    // travels around the frame. Slower than the hue sweep so the light glides.
    val head by transition.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = GLOW_TRAVEL_DURATION_MS, easing = LinearEasing),
        ),
    )
    return this.drawWithCache {
        val maxDepth = glow.toPx()
        val minDepth = maxDepth * GLOW_MIN_DEPTH_FRACTION
        val step = GLOW_SLICE_DP.dp.toPx()
        onDrawWithContent {
            drawContent()
            if (intensity <= 0.01f) return@onDrawWithContent
            val a = peakAlpha * intensity
            // The light is concentrated at [head] and fades around the perimeter, so it
            // flows around the message instead of lighting all four edges at once.
            drawAuroraEdge(GlowEdge.TOP, minDepth, maxDepth, step, phase, head, a)
            drawAuroraEdge(GlowEdge.RIGHT, minDepth, maxDepth, step, phase, head, a)
            drawAuroraEdge(GlowEdge.BOTTOM, minDepth, maxDepth, step, phase, head, a)
            drawAuroraEdge(GlowEdge.LEFT, minDepth, maxDepth, step, phase, head, a)
        }
    }
}

// The glow is intentionally uneven: each edge's depth undulates between
// GLOW_MIN_DEPTH_FRACTION*max and max along its length and drifts over time, so the
// light ripples like an aurora curtain rather than sitting as a flat band. Smaller
// slices = smoother wave at a little more draw cost; 14dp reads as continuous once
// the gradient fade softens the slice seams.
private const val GLOW_MIN_DEPTH_FRACTION = 0.22f
private const val GLOW_SLICE_DP = 14f

// One trip of the bright head around the perimeter — slower than the hue sweep so the
// light glides gracefully rather than spinning.
private const val GLOW_TRAVEL_DURATION_MS = 5200

// Half-width of the lit arc as a fraction of the perimeter: the head fades to dark
// this far away on each side, so roughly HOTSPOT_HALF*2 of the frame glows at once.
private const val HOTSPOT_HALF = 0.2f

private enum class GlowEdge { TOP, BOTTOM, LEFT, RIGHT }

// Undulating band depth at [t01] (0..1 along the edge), drifting with [phase]. Two
// sine harmonics at different spatial frequencies give an organic, non-repeating
// wave; normalized to 0..1 then mapped into [minDepth, maxDepth].
private fun glowDepthAt(t01: Float, phase: Float, minDepth: Float, maxDepth: Float): Float {
    val w1 = sin((2.0 * PI * (t01 * 1.3 + phase)).toFloat())
    val w2 = sin((2.0 * PI * (t01 * 2.7 - phase * 0.7f)).toFloat())
    val n = (((w1 + w2) / 2f) + 1f) / 2f
    return minDepth + (maxDepth - minDepth) * n
}

// Draws one aurora edge as thin slices. Each slice's inward depth undulates
// ([glowDepthAt]) and its alpha is gated by [edgeHotspot] — how close the slice sits
// to the traveling [head] on the rectangle's perimeter — so only the stretch near the
// head glows and the light appears to flow around the frame. Slices outside the lit
// arc are skipped, so this is cheaper than a full four-sided band.
private fun DrawScope.drawAuroraEdge(
    edge: GlowEdge,
    minDepth: Float,
    maxDepth: Float,
    step: Float,
    phase: Float,
    head: Float,
    baseAlpha: Float,
) {
    val w = size.width
    val h = size.height
    val perimeter = 2f * (w + h)
    val horizontal = edge == GlowEdge.TOP || edge == GlowEdge.BOTTOM
    val span = if (horizontal) w else h
    var pos = 0f
    while (pos < span) {
        val sliceLen = minOf(step, span - pos)
        val mid = pos + sliceLen / 2f
        // Clockwise perimeter position: TL -> TR -> BR -> BL -> TL.
        val arc = when (edge) {
            GlowEdge.TOP -> mid
            GlowEdge.RIGHT -> w + mid
            GlowEdge.BOTTOM -> w + h + (w - mid)
            GlowEdge.LEFT -> 2f * w + h + (h - mid)
        }
        val s01 = arc / perimeter
        val lit = edgeHotspot(s01, head)
        if (lit > 0.02f) {
            val t01 = mid / span
            val depth = glowDepthAt(t01, phase, minDepth, maxDepth)
            val color = auroraAt(phase + s01 * 0.6f).copy(alpha = baseAlpha * lit)
            when (edge) {
                GlowEdge.TOP -> drawRect(
                    brush = Brush.verticalGradient(listOf(color, Color.Transparent), startY = 0f, endY = depth),
                    topLeft = Offset(pos, 0f),
                    size = Size(sliceLen, depth),
                )

                GlowEdge.BOTTOM -> drawRect(
                    brush = Brush.verticalGradient(listOf(Color.Transparent, color), startY = h - depth, endY = h),
                    topLeft = Offset(pos, h - depth),
                    size = Size(sliceLen, depth),
                )

                GlowEdge.LEFT -> drawRect(
                    brush = Brush.horizontalGradient(listOf(color, Color.Transparent), startX = 0f, endX = depth),
                    topLeft = Offset(0f, pos),
                    size = Size(depth, sliceLen),
                )

                GlowEdge.RIGHT -> drawRect(
                    brush = Brush.horizontalGradient(listOf(Color.Transparent, color), startX = w - depth, endX = w),
                    topLeft = Offset(w - depth, pos),
                    size = Size(depth, sliceLen),
                )
            }
        }
        pos += sliceLen
    }
}

// Brightness of the traveling head at perimeter position [s] (0..1) given the head at
// [head] (0..1). Cosine falloff to zero at HOTSPOT_HALF away (circular distance), so a
// single soft arc of light glows and the rest of the frame stays dark.
private fun edgeHotspot(s: Float, head: Float): Float {
    var d = abs(s - head)
    if (d > 0.5f) d = 1f - d
    if (d >= HOTSPOT_HALF) return 0f
    return (cos((d / HOTSPOT_HALF) * PI.toFloat()) + 1f) / 2f
}
