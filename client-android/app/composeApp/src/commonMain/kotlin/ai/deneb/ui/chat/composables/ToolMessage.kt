package ai.deneb.ui.chat.composables

import ai.deneb.ui.denebSpatialSpring
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.animateContentSize
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.StartOffset
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.keyframes
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.tools_count_more
import deneb.composeapp.generated.resources.waiting_brewing
import deneb.composeapp.generated.resources.waiting_content_description
import deneb.composeapp.generated.resources.waiting_elapsed_min_sec
import deneb.composeapp.generated.resources.waiting_elapsed_sec
import deneb.composeapp.generated.resources.waiting_thinking
import deneb.composeapp.generated.resources.waiting_working
import kotlinx.collections.immutable.ImmutableList
import kotlinx.coroutines.delay
import org.jetbrains.compose.resources.stringResource
import kotlin.time.Duration.Companion.seconds
import kotlin.time.TimeSource

// Keep the chip quiet this long before showing the elapsed-time suffix: quick
// turns stay minimal, while multi-minute tool calls read as alive, not hung.
private const val ELAPSED_SHOW_AFTER_SEC = 10

@Composable
internal fun toolSummaryText(
    executingTools: ImmutableList<Pair<String, String>>,
): String? = when {
    executingTools.isEmpty() -> null

    executingTools.size == 1 -> executingTools.first().second

    // "메일 확인 중 외 1" — lead with the first concrete label instead of a
    // bare count, so parallel tools still narrate something meaningful.
    else -> stringResource(Res.string.tools_count_more, executingTools.first().second, executingTools.size - 1)
}

@Composable
internal fun WaitingResponseRow(
    executingTools: ImmutableList<Pair<String, String>>,
    isStatusOnly: Boolean = false,
    statusText: String? = null,
    // When provided, the elapsed display anchors to the turn's actual start
    // (survives this row briefly leaving composition, e.g. the deneb-ui
    // pending stretch); otherwise it anchors to first composition.
    turnStart: TimeSource.Monotonic.ValueTimeMark? = null,
) {
    val summary = statusText ?: toolSummaryText(executingTools)
    val effectiveStatusOnly = isStatusOnly || statusText != null
    // Surface the live status to screen readers too — "응답 대기 중 — 메일 확인 중".
    val waitingCdBase = stringResource(Res.string.waiting_content_description)
    val waitingCd = if (summary != null) "$waitingCdBase — $summary" else waitingCdBase

    // Elapsed time since the turn started (see turnStart above).
    val anchor = remember(turnStart) { turnStart ?: TimeSource.Monotonic.markNow() }
    var elapsedSec by remember { mutableIntStateOf(0) }
    LaunchedEffect(anchor) {
        while (true) {
            elapsedSec = anchor.elapsedNow().inWholeSeconds.toInt()
            delay(1.seconds)
        }
    }
    val elapsedLabel = when {
        elapsedSec < ELAPSED_SHOW_AFTER_SEC -> null
        elapsedSec >= 60 -> stringResource(Res.string.waiting_elapsed_min_sec, elapsedSec / 60, elapsedSec % 60)
        else -> stringResource(Res.string.waiting_elapsed_sec, elapsedSec)
    }

    // No chip: a transparent, inline pulsing dot + status text — the way modern
    // chat apps show "thinking". The old surfaceVariant fill read as a gray slab
    // (and needed an OLED special-case); dropping it lets the status sit flush on
    // the message-content margin (16.dp), animating its width as the text grows.
    Row(
        modifier = Modifier
            .padding(horizontal = 16.dp, vertical = 10.dp)
            .animateContentSize(animationSpec = denebSpatialSpring())
            .clipToBounds()
            .semantics { contentDescription = waitingCd },
        verticalAlignment = Alignment.CenterVertically,
    ) {
        PulsingStatusIndicator(
            toolSummary = summary,
            isStatusOnly = effectiveStatusOnly,
            dotColor = MaterialTheme.colorScheme.primary, // sky-blue cool accent — a touch of life
            textColor = MaterialTheme.colorScheme.onSurfaceVariant,
            textStyle = MaterialTheme.typography.bodyMedium,
        )
        if (elapsedLabel != null) {
            Text(
                text = " · $elapsedLabel",
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 1,
            )
        }
    }
}

@Composable
internal fun PulsingStatusIndicator(
    toolSummary: String?,
    dotColor: Color,
    textColor: Color,
    textStyle: TextStyle,
    modifier: Modifier = Modifier,
    isStatusOnly: Boolean = false,
) {
    val waitingTexts = remember {
        listOf(
            Res.string.waiting_thinking,
            Res.string.waiting_working,
            Res.string.waiting_brewing,
        )
    }
    var index by remember { mutableIntStateOf(0) }
    LaunchedEffect(Unit) {
        while (true) {
            delay(3.seconds)
            index = (index + 1) % waitingTexts.size
        }
    }

    Row(
        modifier = modifier,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        TypingDots(color = dotColor)
        Spacer(Modifier.width(8.dp))
        if (isStatusOnly && toolSummary != null) {
            Text(
                text = toolSummary,
                color = textColor,
                style = textStyle,
                textAlign = TextAlign.Center,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        } else {
            AnimatedContent(
                targetState = index,
                transitionSpec = {
                    (fadeIn(tween(300)) togetherWith fadeOut(tween(300)))
                        .using(SizeTransform(clip = false) { _, _ -> tween(300) })
                },
            ) { targetIndex ->
                Text(
                    text = stringResource(waitingTexts[targetIndex]),
                    color = textColor,
                    style = textStyle,
                )
            }
            if (toolSummary != null) {
                Text(
                    text = " · $toolSummary",
                    color = textColor,
                    style = textStyle,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

// TypingDots is the 답변-중 motion: three small dots bounce in a staggered wave
// (the Gemini/Grok "typing" idiom), each brightening at the top of its hop —
// livelier and more refined than one big pulsing dot. Pure animation, no state;
// the color is the caller's (sky-blue cool accent for the waiting row).
private const val typingDotCount = 3
private const val typingDotStaggerMs = 140
private const val typingDotPeriodMs = 760

@Composable
private fun TypingDots(color: Color, modifier: Modifier = Modifier) {
    val transition = rememberInfiniteTransition(label = "typing-dots")
    val bouncePx = with(LocalDensity.current) { 5.dp.toPx() }
    Row(modifier, verticalAlignment = Alignment.CenterVertically) {
        repeat(typingDotCount) { i ->
            // t runs 0 → 1 (peak) → 0 each period; staggered so the dots ripple.
            val t by transition.animateFloat(
                initialValue = 0f,
                targetValue = 0f,
                animationSpec = infiniteRepeatable(
                    animation = keyframes {
                        durationMillis = typingDotPeriodMs
                        0f at 0
                        1f at 200 using FastOutSlowInEasing
                        0f at 440 using FastOutSlowInEasing
                        0f at typingDotPeriodMs
                    },
                    initialStartOffset = StartOffset(i * typingDotStaggerMs),
                ),
                label = "typing-dot-$i",
            )
            Box(
                modifier = Modifier
                    .padding(horizontal = 1.5.dp)
                    .size(6.dp)
                    .graphicsLayer {
                        translationY = -t * bouncePx
                        alpha = 0.45f + 0.55f * t
                    }
                    .background(color, CircleShape),
            )
        }
    }
}
