package com.inspiredandroid.kai.ui.components

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * Shimmer placeholder fill — a soft base tint with a highlight band sweeping
 * left→right across it, the loading idiom users read instantly as "content is
 * coming". The sweep is a single horizontal-gradient rect redrawn in the draw
 * phase (the progress State is read inside [drawWithCache]'s draw lambda, so it
 * repaints without recomposing), which keeps it cheap and identical on every
 * platform. Replaces the older flat alpha-pulse, which read as "broken/disabled"
 * more than "loading".
 */
@Composable
fun Modifier.shimmer(shape: Shape = RoundedCornerShape(8.dp)): Modifier {
    val transition = rememberInfiniteTransition(label = "shimmer")
    val progress = transition.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(1300, easing = LinearEasing), RepeatMode.Restart),
        label = "shimmer-sweep",
    )
    val tint = MaterialTheme.colorScheme.onSurface
    return clip(shape).drawWithCache {
        // Half-width highlight band that travels from fully off the left edge to
        // fully off the right, so the bright streak enters and exits cleanly.
        val band = size.width * 0.45f
        val travel = size.width + band * 2f
        onDrawBehind {
            drawRect(color = tint.copy(alpha = 0.07f))
            val center = -band + progress.value * travel
            drawRect(
                brush = Brush.horizontalGradient(
                    colors = listOf(Color.Transparent, tint.copy(alpha = 0.16f), Color.Transparent),
                    startX = center - band,
                    endX = center + band,
                ),
            )
        }
    }
}

/** A single shimmering text-line placeholder. */
@Composable
fun SkeletonLine(modifier: Modifier = Modifier, widthFraction: Float = 1f, height: Dp = 14.dp) {
    Box(modifier.fillMaxWidth(widthFraction).height(height).shimmer())
}

/**
 * A list of skeleton rows (avatar + two lines) shown while a list screen loads,
 * so content fades in instead of popping into a blank screen.
 */
@Composable
fun SkeletonList(modifier: Modifier = Modifier, rows: Int = 7, showAvatar: Boolean = true) {
    Column(
        modifier = modifier.fillMaxWidth().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(18.dp),
    ) {
        repeat(rows) {
            Row(
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (showAvatar) {
                    Box(Modifier.size(40.dp).shimmer(CircleShape))
                }
                Column(
                    modifier = Modifier.weight(1f),
                    verticalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    SkeletonLine(widthFraction = 0.65f)
                    SkeletonLine(widthFraction = 0.4f, height = 12.dp)
                }
            }
        }
    }
}
