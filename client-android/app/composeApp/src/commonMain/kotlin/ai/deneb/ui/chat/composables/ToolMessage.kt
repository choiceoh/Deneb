package ai.deneb.ui.chat.composables

import ai.deneb.ui.denebSpatialSpring
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.animateContentSize
import androidx.compose.animation.core.FastOutSlowInEasing
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
import androidx.compose.foundation.layout.height
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
import androidx.compose.ui.graphics.TransformOrigin
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
        SlimeIndicator(color = dotColor)
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

// SlimeIndicator is the 답변-중 motion: a cute sky-blue jelly that hops with a
// squash-and-stretch — it crouches (squashes wide), springs up tall, then
// squashes again on landing (Disney squash & stretch), with a glossy highlight so
// it reads as a jelly, not a plain ball. transformOrigin sits at the base so it
// deforms from where it "lands". Pure animation, no state.
private const val slimePeriodMs = 1000

@Composable
private fun SlimeIndicator(color: Color, modifier: Modifier = Modifier) {
    val transition = rememberInfiniteTransition(label = "slime")
    val hopPx = with(LocalDensity.current) { 6.dp.toPx() }
    // hop: 0 (ground) → 1 (apex) → 0 (land), with a brief crouch and a rest pause.
    val hop by transition.animateFloat(
        initialValue = 0f,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(
            animation = keyframes {
                durationMillis = slimePeriodMs
                0f at 0
                0f at 160 using FastOutSlowInEasing
                1f at 440 using FastOutSlowInEasing
                0f at 700 using FastOutSlowInEasing
                0f at slimePeriodMs
            },
        ),
        label = "slime-hop",
    )
    // squash: + = wide & flat (crouch/land), − = tall & thin (launch).
    val squash by transition.animateFloat(
        initialValue = 0f,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(
            animation = keyframes {
                durationMillis = slimePeriodMs
                0f at 0
                0.30f at 160 using FastOutSlowInEasing
                -0.22f at 430 using FastOutSlowInEasing
                0.30f at 700 using FastOutSlowInEasing
                0f at 880 using FastOutSlowInEasing
                0f at slimePeriodMs
            },
        ),
        label = "slime-squash",
    )
    // Reserve hop headroom (22.dp) so the jelly never clips; it rests at the base.
    Box(modifier.height(22.dp).width(16.dp), contentAlignment = Alignment.BottomCenter) {
        Box(
            modifier = Modifier
                .size(16.dp)
                .graphicsLayer {
                    translationY = -hop * hopPx
                    scaleX = 1f + squash * 0.5f
                    scaleY = 1f - squash * 0.5f
                    transformOrigin = TransformOrigin(0.5f, 1f)
                }
                .background(color, CircleShape),
        ) {
            // glossy highlight (top-left) → a cute jelly, not a flat dot.
            Box(
                modifier = Modifier
                    .padding(start = 4.dp, top = 3.dp)
                    .size(5.dp)
                    .background(Color.White.copy(alpha = 0.5f), CircleShape),
            )
        }
    }
}
