package ai.deneb.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlin.math.PI
import kotlin.math.sin

// One full hue revolution while a reply is being generated. ~6s matches the
// reference: blue -> cyan -> green -> yellow -> orange -> red -> magenta -> violet
// -> blue. Because a full turn lands back on the same hue, the repeat is seamless.
private const val BACKDROP_CYCLE_MS = 6000

// Saturated but luminous-not-garish over the AMOLED black base.
private const val BACKDROP_SATURATION = 0.72f
private const val BACKDROP_VALUE = 0.95f

// Hue varies across the WIDTH: the left edge sits at the base hue and the right edge
// BACKDROP_HUE_SPREAD degrees further along the wheel, so several colors show at once
// (more "다채로움") instead of one flat color. The whole spectrum still drifts over time.
private const val BACKDROP_HUE_SPREAD = 110f

// Column width for the horizontal hue sweep — smaller = smoother color transition at a
// little more draw cost. The vertical fade softens any seam between columns.
private const val BACKDROP_SLICE_DP = 10f

// Peak opacity at the very top of the screen, easing to nothing before the bottom.
// Bright enough to read clearly as the reference's "light from above" while the
// fast inward falloff keeps message text legible.
private const val BACKDROP_PEAK_ALPHA = 0.62f

// The glow lives in the top portion of the screen only, and its vertical reach is NOT
// uniform: each column reaches somewhere between BACKDROP_HEIGHT_MIN and
// BACKDROP_HEIGHT_FRACTION of the height (see [curtainReach]), so the lower edge
// undulates across the width like an aurora curtain instead of a flat band. The range
// is centered on ~0.75 (the original flat height) so the curtain undulates above AND
// below it rather than only shortening — the overall reach stays as tall as before.
private const val BACKDROP_HEIGHT_FRACTION = 0.9f
private const val BACKDROP_HEIGHT_MIN = 0.6f

// Hue starts at blue (240 deg) and decreases a full turn, matching the observed
// order (blue -> cyan -> green -> ...). Reset to this each time generation starts.
private const val BACKDROP_START_HUE = 240f

// Keep the backdrop on this long AFTER generation ends, so a fast reply (챗봇 answers
// quickly) still shows it instead of a flicker, and it lingers briefly into the
// answer's first moments before fading out.
private const val BACKDROP_HOLD_MS = 600L

// Slow "breathing" of the overall brightness and an independent ripple of the curtain
// shape, so the glow feels alive rather than statically lit. Different periods so they
// never beat in lockstep.
private const val BACKDROP_BREATH_MS = 3500
private const val BACKDROP_SHAPE_MS = 3200

/**
 * The "generating" backdrop: while [active] (a reply is being thought up, before its
 * text starts rendering) a soft ambient glow rises from the top of the screen and
 * fades to black toward the bottom. The hue varies across the width (a horizontal
 * spread, [BACKDROP_HUE_SPREAD]) AND drifts over time (a full wheel ~6s/turn), so the
 * top reads like a multi-color aurora curtain rather than one flat color. Modeled on
 * the Gemini-style loading shimmer: brightest up top, diffuse with no hard edge.
 *
 * Drawn BEHIND the content (gradient first, then [drawContent]), so the top bar,
 * chat, and input bar sit over it. It eases in from black on send; it holds for a
 * short [BACKDROP_HOLD_MS] after generation ends (so a fast reply still shows it
 * instead of a flicker) and then dissolves gently, lingering into the answer's first
 * moments rather than cutting the instant the answer begins.
 *
 * It also breathes (slow brightness pulse) and its curtain shape ripples on its own
 * faster cycle, so it reads as alive rather than statically lit.
 *
 * This is a deliberate, owner-requested exception to the monochrome-restraint
 * doctrine — but bounded: it only shows during (and just after) the generating window.
 */
@Composable
fun Modifier.generatingBackdrop(active: Boolean): Modifier {
    // Visibility latch: once generation starts, keep showing for at least
    // BACKDROP_HOLD_MS after it ends, so an instant reply doesn't flicker the glow.
    var holding by remember { mutableStateOf(false) }
    LaunchedEffect(active) {
        if (active) {
            holding = true
        } else if (holding) {
            delay(BACKDROP_HOLD_MS)
            holding = false
        }
    }
    val show = active || holding

    val intensity by animateFloatAsState(
        targetValue = if (show) 1f else 0f,
        // Charge up quickly, dissolve slowly so it overlaps the answer's first lines.
        animationSpec = tween(durationMillis = if (show) 450 else 850, easing = LinearEasing),
        label = "generatingBackdropIntensity",
    )
    // Reset to blue each time it (re)starts, then cycle the wheel forever while shown.
    // Keyed on `show` so the hue keeps cycling through the post-generation hold.
    val cycle = remember { Animatable(0f) }
    LaunchedEffect(show) {
        if (show) {
            cycle.snapTo(0f)
            cycle.animateTo(
                targetValue = 1f,
                animationSpec = infiniteRepeatable(tween(BACKDROP_CYCLE_MS, easing = LinearEasing)),
            )
        }
    }
    // Independent free-running motion: a brightness "breath" and a faster curtain-shape
    // ripple. Separate periods so the glow never looks like it pulses on a single beat.
    val motion = rememberInfiniteTransition(label = "generatingBackdropMotion")
    val breathPhase by motion.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(BACKDROP_BREATH_MS, easing = LinearEasing)),
        label = "breath",
    )
    val shapePhase by motion.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(BACKDROP_SHAPE_MS, easing = LinearEasing)),
        label = "shape",
    )
    return this.drawWithContent {
        if (intensity > 0.01f) {
            val baseHue = BACKDROP_START_HUE - cycle.value * 360f
            // Breathing: gentle ±~8% brightness pulse around the peak.
            val breath = 0.85f + 0.15f * (0.5f + 0.5f * sin((2.0 * PI * breathPhase).toFloat()))
            val peak = BACKDROP_PEAK_ALPHA * intensity * breath
            val w = size.width
            val h = size.height
            val step = BACKDROP_SLICE_DP.dp.toPx()
            // Paint the glow as thin vertical columns. Each column's hue is offset by its
            // horizontal position (a band of colors at once) AND its vertical reach
            // undulates across the width — so the top shows a multi-color curtain whose
            // lower edge ripples on its own [shapePhase] cycle while the hue band drifts.
            var x = 0f
            while (x < w) {
                val sliceW = minOf(step, w - x)
                val x01 = if (w > 0f) (x + sliceW / 2f) / w else 0f
                val hueDeg = ((baseHue + x01 * BACKDROP_HUE_SPREAD) % 360f + 360f) % 360f
                val hue = Color.hsv(hueDeg, BACKDROP_SATURATION, BACKDROP_VALUE)
                val reach = BACKDROP_HEIGHT_MIN +
                    (BACKDROP_HEIGHT_FRACTION - BACKDROP_HEIGHT_MIN) * curtainReach(x01, shapePhase)
                drawRect(
                    brush = Brush.verticalGradient(
                        0f to hue.copy(alpha = peak),
                        0.4f to hue.copy(alpha = peak * 0.45f),
                        0.9f to Color.Transparent,
                        startY = 0f,
                        endY = h * reach,
                    ),
                    topLeft = Offset(x, 0f),
                    size = Size(sliceW, h),
                )
                x += sliceW
            }
        }
        drawContent()
    }
}

// Smooth 0..1 curtain-height profile across the width [x01], drifting with [phase].
// Two sine harmonics at different spatial frequencies give an organic, non-repeating
// undulation of the glow's lower edge (taller here, shorter there) that slowly morphs.
private fun curtainReach(x01: Float, phase: Float): Float {
    val w1 = sin((2.0 * PI * (x01 * 1.4 + phase)).toFloat())
    val w2 = sin((2.0 * PI * (x01 * 2.6 - phase * 0.6f)).toFloat())
    return (((w1 + w2) / 2f) + 1f) / 2f
}
