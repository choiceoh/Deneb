package ai.deneb.ui

import ai.deneb.ui.chat.composables.denebBottomBarRoutes
import androidx.compose.animation.AnimatedContentScope
import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.AnimatedVisibilityScope
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.ExperimentalSharedTransitionApi
import androidx.compose.animation.SharedTransitionScope
import androidx.compose.animation.core.SpringSpec
import androidx.compose.animation.core.VisibilityThreshold
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.IntOffset
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable

/**
 * Screen-to-screen navigation motion. Two grammars, chosen by the same rule that
 * shows/hides the bottom bar, so chrome and motion always agree on what a move *is*:
 *
 *  - **Lateral** (both routes keep the bottom bar): sibling sections — a calm, fast
 *    crossfade. Tabs don't slide; sliding would imply hierarchy that isn't there.
 *  - **Drill-in/out** (the bar hides or returns): parent→child — the incoming screen
 *    slides in from the end on a critically-damped spring while the parent recedes
 *    a quarter-width behind it (iOS push parallax); pop reverses both.
 *
 * The slide spring is critically damped (no overshoot): a full-screen pane that
 * overshoots exposes the surface behind its trailing edge for a frame.
 */
private fun screenSlideSpring(): SpringSpec<IntOffset> = spring(dampingRatio = 1f, stiffness = 380f, visibilityThreshold = IntOffset.VisibilityThreshold)

/** How far the receding parent slides behind an incoming child (1/N of its width). */
private const val ParallaxFraction = 4

private val AnimatedContentTransitionScope<NavBackStackEntry>.isLateral: Boolean
    get() = initialState.destination.route in denebBottomBarRoutes &&
        targetState.destination.route in denebBottomBarRoutes

fun AnimatedContentTransitionScope<NavBackStackEntry>.denebNavEnter(): EnterTransition = if (isLateral) {
    fadeIn(tween(DenebMotion.DurationFast, easing = DenebMotion.emphasizedDecelerate))
} else {
    slideIntoContainer(AnimatedContentTransitionScope.SlideDirection.Start, screenSlideSpring())
}

fun AnimatedContentTransitionScope<NavBackStackEntry>.denebNavExit(): ExitTransition = if (isLateral) {
    fadeOut(tween(DenebMotion.DurationQuick, easing = DenebMotion.emphasizedAccelerate))
} else {
    slideOutOfContainer(
        AnimatedContentTransitionScope.SlideDirection.Start,
        screenSlideSpring(),
        targetOffset = { it / ParallaxFraction },
    )
}

fun AnimatedContentTransitionScope<NavBackStackEntry>.denebNavPopEnter(): EnterTransition = if (isLateral) {
    fadeIn(tween(DenebMotion.DurationFast, easing = DenebMotion.emphasizedDecelerate))
} else {
    slideIntoContainer(
        AnimatedContentTransitionScope.SlideDirection.End,
        screenSlideSpring(),
        initialOffset = { it / ParallaxFraction },
    )
}

fun AnimatedContentTransitionScope<NavBackStackEntry>.denebNavPopExit(): ExitTransition = if (isLateral) {
    fadeOut(tween(DenebMotion.DurationQuick, easing = DenebMotion.emphasizedAccelerate))
} else {
    slideOutOfContainer(AnimatedContentTransitionScope.SlideDirection.End, screenSlideSpring())
}

// ---- Shared-element rail ----
// App.kt provides the SharedTransitionScope once (around the NavHost) and each
// destination provides its AnimatedContentScope via [denebComposable]. Screens then
// opt individual composables into a shared-bounds morph with [denebSharedBounds] —
// which is a NO-OP wherever either scope is absent (renderPreviews, the desktop
// split-view pane, dialogs), so previewable bodies never need scope plumbing.

val LocalSharedTransitionScope = staticCompositionLocalOf<SharedTransitionScope?> { null }
val LocalNavAnimatedVisibilityScope = staticCompositionLocalOf<AnimatedVisibilityScope?> { null }

/** `composable<T>` that publishes its animation scope for [denebSharedBounds] participants. */
inline fun <reified T : Any> NavGraphBuilder.denebComposable(
    noinline content: @Composable AnimatedContentScope.(NavBackStackEntry) -> Unit,
) {
    composable<T> { entry ->
        CompositionLocalProvider(LocalNavAnimatedVisibilityScope provides this) {
            content(entry)
        }
    }
}

/**
 * Morph this composable's bounds to its counterpart (same [key]) on the other side of a
 * navigation. Key convention: `"surface-field-id"` (e.g. `"mail-subject-42"`). Text of
 * different styles scales toward the destination bounds, start-aligned.
 */
@OptIn(ExperimentalSharedTransitionApi::class)
@Composable
fun Modifier.denebSharedBounds(key: String): Modifier {
    val sharedScope = LocalSharedTransitionScope.current ?: return this
    val animatedScope = LocalNavAnimatedVisibilityScope.current ?: return this
    return with(sharedScope) {
        this@denebSharedBounds.sharedBounds(
            rememberSharedContentState(key),
            animatedVisibilityScope = animatedScope,
            resizeMode = SharedTransitionScope.ResizeMode.scaleToBounds(ContentScale.FillWidth, Alignment.CenterStart),
        )
    }
}
