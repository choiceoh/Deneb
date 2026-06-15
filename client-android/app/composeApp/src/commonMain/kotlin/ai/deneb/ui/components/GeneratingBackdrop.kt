package ai.deneb.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloatAsState
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
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.unit.dp
import kotlin.math.PI
import kotlin.math.sin

// --- Tunables ------------------------------------------------------------------

private const val BACKDROP_CYCLE_S = 6.0f // seconds for one full hue revolution
private const val BACKDROP_SATURATION = 0.72f
private const val BACKDROP_VALUE = 0.95f

// Degrees of hue variation across the width. Kept subtle: a gentle gradient (e.g. blue
// -> cyan) rather than a wide rainbow, so it reads as one cohesive light that shifts.
private const val BACKDROP_HUE_SPREAD = 50f

private const val BACKDROP_PEAK_ALPHA = 0.62f
private const val BACKDROP_HEIGHT_FRACTION = 0.9f // deepest curtain reach
private const val BACKDROP_HEIGHT_MIN = 0.6f // shallowest curtain reach
private const val BACKDROP_START_HUE = 240f // blue
private const val BACKDROP_BREATH_S = 3.5f // brightness breath period (seconds)
private const val BACKDROP_SHAPE_S = 3.2f // curtain-ripple period (seconds)
private const val BACKDROP_SLICE_DP = 10f // column width

// Keep the backdrop on this long AFTER generation ends, so a fast reply (챗봇 answers
// quickly) still shows it instead of a flicker, and it lingers into the answer's first
// moments before fading out.
private const val BACKDROP_HOLD_MS = 600L

// A long linear clock (seconds) that resets when generation starts (so the hue starts
// at blue). ~10 min is far longer than any generating window, so the wrap is never seen.
private const val BACKDROP_CLOCK_MAX_S = 600f

/**
 * The "generating" backdrop: while [active] (a reply is being thought up, before its
 * text starts rendering) a soft aurora glows from the top of the screen — a gentle hue
 * band across the width and an undulating curtain edge — fading to black toward the
 * bottom. Drawn BEHIND the content (then [drawContent]), so the top bar, chat, and
 * input bar sit over it.
 *
 * It holds for a short [BACKDROP_HOLD_MS] after generation ends (so a fast reply still
 * shows it instead of a flicker) then dissolves gently, lingering into the answer's
 * first moments. The hue cycles (~6s) and the whole thing breathes, so it reads alive.
 *
 * This is a deliberate, owner-requested exception to the monochrome-restraint doctrine,
 * bounded to the (brief) generating window.
 */
@Composable
fun Modifier.generatingBackdrop(active: Boolean): Modifier {
    // Visibility latch: keep showing BACKDROP_HOLD_MS after generation ends so an
    // instant reply doesn't flicker the glow.
    var holding by remember { mutableStateOf(false) }
    LaunchedEffect(active) {
        if (active) {
            holding = true
        } else if (holding) {
            kotlinx.coroutines.delay(BACKDROP_HOLD_MS)
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
    // Elapsed-seconds clock, reset to 0 (blue) each time it (re)starts. Keyed on `show`
    // so it keeps ticking through the post-generation hold.
    val clock = remember { Animatable(0f) }
    LaunchedEffect(show) {
        if (show) {
            clock.snapTo(0f)
            clock.animateTo(
                targetValue = BACKDROP_CLOCK_MAX_S,
                animationSpec = tween(durationMillis = (BACKDROP_CLOCK_MAX_S * 1000).toInt(), easing = LinearEasing),
            )
        }
    }

    return this.drawWithContent {
        val a = intensity
        if (a > 0.01f) {
            drawAuroraSlices(clock.value, a)
        }
        drawContent()
    }
}

/**
 * Paints the aurora as thin vertical columns — a gentle hue band across the width and an
 * undulating curtain edge — from the [timeSeconds] clock. Top-bright, fading to
 * transparent toward the bottom; hue cycles and the whole thing breathes.
 */
internal fun DrawScope.drawAuroraSlices(timeSeconds: Float, intensity: Float) {
    val baseHue = BACKDROP_START_HUE - (timeSeconds / BACKDROP_CYCLE_S) * 360f
    val breathPhase = timeSeconds / BACKDROP_BREATH_S
    val breath = 0.85f + 0.15f * (0.5f + 0.5f * sin((2.0 * PI * breathPhase).toFloat()))
    val peak = BACKDROP_PEAK_ALPHA * intensity * breath
    val shapePhase = timeSeconds / BACKDROP_SHAPE_S
    val w = size.width
    val h = size.height
    val step = BACKDROP_SLICE_DP.dp.toPx()
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

// Smooth 0..1 curtain-height profile across the width [x01], drifting with [phase].
// Two sine harmonics at different spatial frequencies give an organic, non-repeating
// undulation of the glow's lower edge that slowly morphs.
private fun curtainReach(x01: Float, phase: Float): Float {
    val w1 = sin((2.0 * PI * (x01 * 1.4 + phase)).toFloat())
    val w2 = sin((2.0 * PI * (x01 * 2.6 - phase * 0.6f)).toFloat())
    return (((w1 + w2) / 2f) + 1f) / 2f
}
