package ai.deneb.ui

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.zIndex

/**
 * The always-alive tab pane: every bottom-bar tab stays composed for the whole app
 * session, so switching tabs is a crossfade between already-built screens — no
 * screen rebuild, no re-run of `LaunchedEffect(Unit)` entry fetches, and every
 * `remember` (search text, month pager, scroll, drawers) survives the switch.
 *
 * Mechanics: all tabs are measured every pass (keeps lazy lists and their item
 * compositions warm), but a hidden tab is *not placed* — it is not drawn, receives
 * no pointer input, and drops out of the semantics tree. The lateral crossfade
 * reuses the bar-keeping motion grammar (DenebNavMotion): incoming fades in over
 * DurationFast/decelerate on top, outgoing fades out under DurationQuick/accelerate.
 *
 * What liveness does NOT gate for hidden tabs: their coroutines and state
 * collection keep running (that is the point — a background tab stays current).
 * What screens must gate themselves: system-back interception. A hidden tab's
 * `PlatformBackHandler` stays registered, so any handler inside a tab screen must
 * AND its `enabled` with [LocalLiveTabActive].
 */
val LocalLiveTabActive = compositionLocalOf { true }

/** One live tab: [route] is its stable identity (bottom-bar route string). */
class LiveTab(val route: String, val content: @Composable () -> Unit)

@Composable
fun LiveTabPane(
    selectedRoute: String,
    tabs: List<LiveTab>,
    modifier: Modifier = Modifier,
) {
    // A hidden tab's focused field would keep the IME up and swallow key events —
    // drop focus the moment the selection changes.
    val focusManager = LocalFocusManager.current
    LaunchedEffect(selectedRoute) { focusManager.clearFocus() }
    Box(modifier) {
        tabs.forEach { tab ->
            key(tab.route) {
                val active = tab.route == selectedRoute
                val alpha by animateFloatAsState(
                    targetValue = if (active) 1f else 0f,
                    animationSpec = tween(
                        durationMillis = if (active) DenebMotion.DurationFast else DenebMotion.DurationQuick,
                        easing = if (active) DenebMotion.emphasizedDecelerate else DenebMotion.emphasizedAccelerate,
                    ),
                    label = "liveTabAlpha",
                )
                Box(
                    Modifier
                        .fillMaxSize()
                        .zIndex(if (active) 1f else 0f)
                        .graphicsLayer { this.alpha = alpha }
                        // Measure always; place only while visible. The placement
                        // lambda reads the animated alpha, so the fading-out tab
                        // stays placed until the crossfade lands, then detaches.
                        .layout { measurable, constraints ->
                            val placeable = measurable.measure(constraints)
                            layout(placeable.width, placeable.height) {
                                if (active || alpha > 0.01f) placeable.place(0, 0)
                            }
                        },
                ) {
                    CompositionLocalProvider(LocalLiveTabActive provides active) { tab.content() }
                }
            }
        }
    }
}
