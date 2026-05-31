package com.inspiredandroid.kai.ui.components

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
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
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * Shimmer placeholder fill — a gently pulsing tint, used for skeleton loaders
 * while a screen's data is in flight. Pulsing alpha (not a sweeping gradient)
 * keeps it cheap and renders the same on every platform.
 */
@Composable
fun Modifier.shimmer(shape: Shape = RoundedCornerShape(8.dp)): Modifier {
    val transition = rememberInfiniteTransition(label = "shimmer")
    val alpha by transition.animateFloat(
        initialValue = 0.10f,
        targetValue = 0.28f,
        animationSpec = infiniteRepeatable(tween(850, easing = FastOutSlowInEasing), RepeatMode.Reverse),
        label = "shimmer-alpha",
    )
    return clip(shape).background(MaterialTheme.colorScheme.onSurface.copy(alpha = alpha))
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
