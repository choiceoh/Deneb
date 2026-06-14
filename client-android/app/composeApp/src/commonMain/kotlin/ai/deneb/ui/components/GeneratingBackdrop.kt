package ai.deneb.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color

// One full hue revolution while a reply is being generated. ~6s matches the
// reference: blue -> cyan -> green -> yellow -> orange -> red -> magenta -> violet
// -> blue. Because a full turn lands back on the same hue, the repeat is seamless.
private const val BACKDROP_CYCLE_MS = 6000

// A single saturated hue at a time (not a multi-hue gradient). Luminous but not
// garish over the AMOLED black base.
private const val BACKDROP_SATURATION = 0.72f
private const val BACKDROP_VALUE = 0.95f

// Peak opacity at the very top of the screen, easing to nothing before the bottom.
// Bright enough to read clearly as the reference's "light from above" while the
// fast inward falloff keeps message text legible.
private const val BACKDROP_PEAK_ALPHA = 0.62f

// Hue starts at blue (240 deg) and decreases a full turn, matching the observed
// order (blue -> cyan -> green -> ...). Reset to this each time generation starts.
private const val BACKDROP_START_HUE = 240f

/**
 * The "generating" backdrop: while [active] (a reply is being thought up, before its
 * text starts rendering) a soft ambient glow rises from the top of the screen and
 * fades to black toward the bottom, its single hue slowly cycling the full color
 * wheel (~6s/turn). Modeled on the Gemini-style loading shimmer: one cycling color,
 * brightest up top, diffuse with no hard edge.
 *
 * Drawn BEHIND the content (gradient first, then [drawContent]), so the top bar,
 * chat, and input bar sit over it. It eases in from black on send and out to black
 * the moment the answer begins, synced to the generating state by [active].
 *
 * This is a deliberate, owner-requested exception to the monochrome-restraint
 * doctrine — but bounded: it only shows during the brief generating window and
 * dissolves to black before the answer (and any sustained reading) renders.
 */
@Composable
fun Modifier.generatingBackdrop(active: Boolean): Modifier {
    val intensity by animateFloatAsState(
        targetValue = if (active) 1f else 0f,
        // Rise a touch faster than it dissolves, so it feels like it "charges up".
        animationSpec = tween(durationMillis = if (active) 500 else 650, easing = LinearEasing),
        label = "generatingBackdropIntensity",
    )
    // Reset to blue each time generation starts, then cycle the wheel forever while
    // active. snapTo(0) on (re)activation guarantees the "blue rising from black" start.
    val cycle = remember { Animatable(0f) }
    LaunchedEffect(active) {
        if (active) {
            cycle.snapTo(0f)
            cycle.animateTo(
                targetValue = 1f,
                animationSpec = infiniteRepeatable(tween(BACKDROP_CYCLE_MS, easing = LinearEasing)),
            )
        }
    }
    return this.drawWithContent {
        if (intensity > 0.01f) {
            val hueDeg = ((BACKDROP_START_HUE - cycle.value * 360f) % 360f + 360f) % 360f
            val hue = Color.hsv(hueDeg, BACKDROP_SATURATION, BACKDROP_VALUE)
            val peak = BACKDROP_PEAK_ALPHA * intensity
            drawRect(
                brush = Brush.verticalGradient(
                    0f to hue.copy(alpha = peak),
                    0.4f to hue.copy(alpha = peak * 0.45f),
                    0.9f to Color.Transparent,
                    startY = 0f,
                    endY = size.height,
                ),
            )
        }
        drawContent()
    }
}
