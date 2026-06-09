package ai.deneb.ui

import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.Easing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.SpringSpec
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.LocalIndication
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.semantics.Role

/**
 * The single source of motion for the Deneb native client.
 *
 * Before this file every animation hardcoded its own `tween(800, FastOutSlowIn)`
 * and the codebase had zero springs — so nothing on screen had the physical
 * "give" that iOS and Material 3 Expressive surfaces use. The doctrine here:
 *
 *  - **Spatial channels** (position, size, scale, offset) animate with **springs**.
 *    Springs carry momentum, so a row that springs back or a sheet that settles
 *    reads as a physical object, not a value being interpolated.
 *  - **Effect channels** (alpha, color) animate with **tween + emphasized easing**.
 *    A spring on opacity just looks like a sloppy fade, so those stay duration-based.
 *  - **Durations and easings live here**, named by intent, so a screen never picks
 *    a magic number — and tuning the whole app's feel is a one-file edit.
 *
 * Easing curves are Google's Material 3 "emphasized" family (the curves behind
 * Material You / Expressive transitions). Spring constants are tuned to sit
 * between Material's standard (calm) and expressive (bouncy) spatial schemes.
 */
object DenebMotion {
    // ---- Easing (Material 3 "emphasized" family) ----
    /** Symmetric emphasized curve — the default for fades and color crossfades. */
    val emphasized: Easing = CubicBezierEasing(0.2f, 0f, 0f, 1f)

    /** Enters/reveals — slow start, gentle landing. Pair with incoming content. */
    val emphasizedDecelerate: Easing = CubicBezierEasing(0.05f, 0.7f, 0.1f, 1f)

    /** Exits/dismissals — quick departure, no lingering. Pair with leaving content. */
    val emphasizedAccelerate: Easing = CubicBezierEasing(0.3f, 0f, 0.8f, 0.15f)

    // ---- Durations (ms), named by intent ----
    /** Near-instant acknowledgement (press states, tiny toggles). */
    const val DurationQuick = 120

    /** Small UI changes — icon swaps, chip add/remove, exit fades. */
    const val DurationFast = 200

    /** Standard fade/crossfade between content. */
    const val DurationMedium = 300

    /** Large reveals where the eye should follow the change. */
    const val DurationSlow = 450

    /** One half-cycle of a live-status "breath" (in-flight dots, stop button). */
    const val DurationBreath = 1300
}

// ---- Spring specs (spatial channels only) ----
// Generic so the same feel applies to Float (scale/alpha), Dp (size), IntOffset
// (slide) and IntSize (expand) alike — Compose infers the type at the call site.

/** Standard spatial settle — most enters, size changes, and press release. Calm, no overshoot to speak of. */
fun <T> denebSpatialSpring(): SpringSpec<T> = spring(dampingRatio = 0.85f, stiffness = 380f)

/** Crisp and immediate — press-down, small state flips. High stiffness so it tracks the finger. */
fun <T> denebSnappySpring(): SpringSpec<T> = spring(dampingRatio = 0.9f, stiffness = 900f)

/** Playful overshoot — celebratory pops (scroll-to-bottom FAB, first appearance). Use sparingly. */
fun <T> denebBouncySpring(): SpringSpec<T> = spring(dampingRatio = 0.55f, stiffness = 420f)

/** Slow, heavy, no overshoot — large surfaces (sheets, full-screen) where bounce would feel cheap. */
fun <T> denebGentleSpring(): SpringSpec<T> = spring(dampingRatio = 1f, stiffness = 220f)

// ---- Enter / exit transition presets (AnimatedVisibility / AnimatedContent) ----

/** A top banner dropping in: slides down from above on a spring, fading as it settles. */
val denebBannerEnter: EnterTransition =
    slideInVertically(denebSpatialSpring()) { -it } +
        fadeIn(tween(DenebMotion.DurationMedium, easing = DenebMotion.emphasizedDecelerate))

/** A top banner leaving: lifts back up quickly and fades out — no lingering. */
val denebBannerExit: ExitTransition =
    slideOutVertically(tween(DenebMotion.DurationFast, easing = DenebMotion.emphasizedAccelerate)) { -it } +
        fadeOut(tween(DenebMotion.DurationFast))

/** An inline section revealing (reasoning block, expandable detail): grows and fades in. */
val denebExpandIn: EnterTransition =
    expandVertically(denebSpatialSpring()) +
        fadeIn(tween(DenebMotion.DurationFast, easing = DenebMotion.emphasizedDecelerate))

/** The same section collapsing: shrinks and fades out, accelerating away. */
val denebShrinkOut: ExitTransition =
    shrinkVertically(tween(DenebMotion.DurationMedium, easing = DenebMotion.emphasizedAccelerate)) +
        fadeOut(tween(DenebMotion.DurationQuick))

/** A small floating control popping in (scroll-to-bottom FAB): scales up from small with a bounce. */
val denebPopEnter: EnterTransition =
    scaleIn(denebBouncySpring(), initialScale = 0.7f) +
        fadeIn(tween(DenebMotion.DurationFast))

/** The same control popping out: scales back down and fades, quickly. */
val denebPopExit: ExitTransition =
    scaleOut(tween(DenebMotion.DurationFast, easing = DenebMotion.emphasizedAccelerate), targetScale = 0.7f) +
        fadeOut(tween(DenebMotion.DurationQuick))

/**
 * Press feedback for flat, cardless rows and tiles: a subtle scale-down while
 * held that springs back on release, layered over the normal ripple. This is the
 * tactile "give" iOS rows have and stock Material rows lack — applied once in
 * [DenebRow] it lifts every list in the app at once.
 *
 * Spatial press uses [denebSnappySpring] going down (tracks the finger) and
 * [denebSpatialSpring] coming back (a soft settle). Ripple is kept via
 * [LocalIndication] so touch still reads as Material; the scale is additive.
 */
@Composable
fun Modifier.denebPressable(
    onClick: () -> Unit,
    enabled: Boolean = true,
    pressedScale: Float = 0.98f,
    role: Role? = null,
): Modifier {
    val interactionSource = remember { MutableInteractionSource() }
    val pressed by interactionSource.collectIsPressedAsState()
    val scale by animateFloatAsState(
        targetValue = if (pressed) pressedScale else 1f,
        animationSpec = if (pressed) denebSnappySpring() else denebSpatialSpring(),
        label = "deneb-pressable-scale",
    )
    return this
        .graphicsLayer {
            scaleX = scale
            scaleY = scale
        }
        .clickable(
            interactionSource = interactionSource,
            indication = LocalIndication.current,
            enabled = enabled,
            role = role,
            onClick = onClick,
        )
}

/**
 * A slow "breathing" pulse (scale + alpha together) for live-status affordances —
 * the waiting dot, the stop button while a turn streams. One cadence shared by
 * every in-flight indicator, so nothing on screen pulses out of sync, and the
 * 1.3s period reads as a calm heartbeat rather than the old anxious 0.8s flicker.
 */
@Composable
fun Modifier.denebBreathing(
    minScale: Float = 0.82f,
    maxScale: Float = 1f,
    minAlpha: Float = 0.45f,
    maxAlpha: Float = 1f,
    periodMs: Int = DenebMotion.DurationBreath,
): Modifier {
    val transition = rememberInfiniteTransition(label = "deneb-breathing")
    val scale by transition.animateFloat(
        initialValue = minScale,
        targetValue = maxScale,
        animationSpec = infiniteRepeatable(tween(periodMs, easing = DenebMotion.emphasized), RepeatMode.Reverse),
        label = "deneb-breathing-scale",
    )
    val alpha by transition.animateFloat(
        initialValue = minAlpha,
        targetValue = maxAlpha,
        animationSpec = infiniteRepeatable(tween(periodMs, easing = DenebMotion.emphasized), RepeatMode.Reverse),
        label = "deneb-breathing-alpha",
    )
    return this.graphicsLayer {
        scaleX = scale
        scaleY = scale
        this.alpha = alpha
    }
}
