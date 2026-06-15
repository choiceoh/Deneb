package ai.deneb.ui.chat.composables

import ai.deneb.ui.denebSpatialSpring
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.animateContentSize
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.keyframes
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
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
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Fill
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
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
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin
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
        StarIndicator(color = dotColor)
        // 6dp not 8: the glow box carries ~3dp of transparent ring on the text
        // side, so a tighter spacer keeps the visual gap to the text the same.
        Spacer(Modifier.width(6.dp))
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

// StarIndicator is the 답변-중 motion: a four-point sparkle (✦) — a glint of
// starlight, Deneb being a blue star — that BURSTS: it pops bright and swells,
// then settles dim and small before the next wink. A sparkle's thin concave rays
// carry the twinkle/burst far better than a chunky five-point star.
//
// The fill is not flat: it's a gradient *derived from the caller's accent* — the
// accent hue in the middle, fanned out into a wide pair of analogous neighbours
// (cyan-side and indigo-side of sky-blue) so the rays catch light like a faceted
// gem. Two slow loops keep that colour alive in real time without ever leaving
// the blue family: the gradient *axis rotates* continuously (the sheen flows
// across the glyph) and the hue band *centre wanders* back and forth. Behind the
// star sits a soft radial glow that swells and brightens on each burst, so the
// blue star reads as luminous rather than a flat glyph. Pure animation, no state.
private const val sparklePeriodMs = 1300
private const val hueRotationPeriodMs = 6000 // gradient-axis sweep → flowing sheen
private const val hueDriftPeriodMs = 5200 // band-centre breathing

// analogousSpreadDeg = how wide the gradient fans into neighbouring hues — large
// enough that the cyan↔indigo difference reads at 16dp, still inside the blue
// family (no rainbow). hueDriftDeg = how far the whole band slowly wanders so the
// colour keeps shifting in real time.
private const val analogousSpreadDeg = 104f
private const val hueDriftDeg = 30f

// The star core fills this fraction of the box; the ring left over is breathing
// room for the glow to bloom into.
private const val starFillFraction = 0.72f

@Composable
private fun StarIndicator(color: Color, modifier: Modifier = Modifier) {
    val transition = rememberInfiniteTransition(label = "sparkle")
    // 0 (dim, small) → quick bright swollen pop → settle dim → rest, repeat.
    val twinkle by transition.animateFloat(
        initialValue = 0f,
        targetValue = 0f,
        animationSpec = infiniteRepeatable(
            animation = keyframes {
                durationMillis = sparklePeriodMs
                0f at 0
                1f at 260 using FastOutSlowInEasing
                0.12f at 760 using FastOutSlowInEasing
                0f at 1100
                0f at sparklePeriodMs
            },
        ),
        label = "twinkle",
    )
    // Continuous (non-reversing) rotation of the gradient axis: every point on the
    // glyph smoothly cycles through the analogous band → the colour flows in real
    // time. degrees 0→360 on a loop.
    val sweep by transition.animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(
            animation = tween(hueRotationPeriodMs, easing = LinearEasing),
        ),
        label = "sweep",
    )
    // Slow side-to-side wander of the band centre — reversing (not wrapping) so the
    // colour breathes through neighbouring hues and never goes full-spectrum.
    val hueDrift by transition.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(hueDriftPeriodMs, easing = LinearEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "hueDrift",
    )

    // Decompose the caller's accent into HSV so the gradient is *derived from the
    // current colour*: analogous hues to either side, same saturation/character.
    val hsv = remember(color) { rgbToHsv(color) }
    val baseHue = hsv[0]
    val sat = hsv[1]
    val value = hsv[2]

    Canvas(
        modifier
            // 22dp box, but the star core only fills ~0.72 of it (starFillFraction)
            // — the ring around it is room for the glow to bloom into. The star
            // itself stays ~16dp, roughly its previous size.
            .size(22.dp)
            .graphicsLayer {
                // Stay clearly present at rest (≈0.6) so the colour reads even
                // between winks; the pop flares it bright and a touch larger.
                alpha = 0.6f + 0.4f * twinkle
                val s = 0.85f + 0.25f * twinkle
                scaleX = s
                scaleY = s
            },
    ) {
        val centerPt = Offset(size.width / 2f, size.height / 2f)
        // Hue band centred on the accent, slid slowly by the drift; shared by the
        // glow and the star so they stay the same colour. Read inside draw scope
        // → redraws each frame, no recomposition.
        val center = baseHue + (hueDrift - 0.5f) * hueDriftDeg

        // Glow first, behind the star: a soft radial bloom in the accent hue,
        // desaturated + brightened toward "light", that swells and brightens with
        // each burst → the blue star glows instead of reading as a flat glyph.
        val glowRadius = size.minDimension / 2f * (0.80f + 0.28f * twinkle)
        val glowColor = hueColor(center, sat * 0.45f, (value * 1.25f).coerceAtMost(1f))
        val glowCoreAlpha = 0.42f + 0.40f * twinkle
        // 3-stop eased falloff (hot core → quick mid → 0) instead of a linear
        // 2-stop ramp: a softer, more natural bloom and finer bands, so the alpha
        // gradient doesn't ring/banded on 8-bit OLED panels.
        drawCircle(
            brush = Brush.radialGradient(
                0.0f to glowColor.copy(alpha = glowCoreAlpha),
                0.45f to glowColor.copy(alpha = glowCoreAlpha * 0.40f),
                1.0f to glowColor.copy(alpha = 0f),
                center = centerPt,
                radius = glowRadius,
            ),
            radius = glowRadius,
            center = centerPt,
        )

        // Star core with the live hue-flowing gradient, fanned wide into its two
        // analogous neighbours; axis rotates with `sweep`.
        val path = denebSparklePath(size, starFillFraction)
        val brush = Brush.linearGradient(
            colors = listOf(
                hueColor(center - analogousSpreadDeg / 2f, sat * 0.82f, (value * 1.1f).coerceAtMost(1f)),
                hueColor(center, sat, value),
                hueColor(center + analogousSpreadDeg / 2f, (sat * 1.1f).coerceAtMost(1f), value * 0.84f),
            ),
            start = rotatedAxisPoint(size, sweep, far = false),
            end = rotatedAxisPoint(size, sweep, far = true),
        )
        // Fill plus a thin round stroke softens the ray tips (the brand's rounded
        // feel) without fattening the thin sparkle.
        drawPath(path, brush, style = Fill)
        drawPath(
            path,
            brush,
            style = Stroke(width = size.minDimension * 0.06f, join = StrokeJoin.Round, cap = StrokeCap.Round),
        )
    }
}

// rotatedAxisPoint returns one end of a gradient axis through the glyph centre at
// [angleDeg]; far=false is the near end, far=true the opposite end. Rotating the
// axis sweeps the gradient so the colours flow across the sparkle.
private fun rotatedAxisPoint(size: Size, angleDeg: Float, far: Boolean): Offset {
    val rad = angleDeg / 180f * PI.toFloat()
    val half = size.maxDimension / 2f
    val cx = size.width / 2f
    val cy = size.height / 2f
    val dx = cos(rad) * half
    val dy = sin(rad) * half
    return if (far) Offset(cx + dx, cy + dy) else Offset(cx - dx, cy - dy)
}

// hueColor wraps a hue into [0,360) and builds an HSV colour, clamping sat/value,
// so the gradient stops stay analogous to the caller's accent.
private fun hueColor(hue: Float, saturation: Float, value: Float): Color {
    val h = ((hue % 360f) + 360f) % 360f
    return Color.hsv(h, saturation.coerceIn(0f, 1f), value.coerceIn(0f, 1f))
}

// rgbToHsv converts a Color to [hue°, saturation, value] so the sparkle gradient
// can be derived from whatever accent the caller passes (adapts to dark/light).
private fun rgbToHsv(color: Color): FloatArray {
    val r = color.red
    val g = color.green
    val b = color.blue
    val max = maxOf(r, g, b)
    val min = minOf(r, g, b)
    val delta = max - min
    val hue = when {
        delta == 0f -> 0f
        max == r -> 60f * ((((g - b) / delta) % 6f + 6f) % 6f)
        max == g -> 60f * (((b - r) / delta) + 2f)
        else -> 60f * (((r - g) / delta) + 4f)
    }
    val sat = if (max == 0f) 0f else delta / max
    return floatArrayOf(hue, sat, max)
}

// denebSparklePath builds a centred four-point sparkle (✦) whose tips reach
// [fill] of the box half-extent (leaving a ring for the glow): four tips
// (N/E/S/W) joined by quadratic curves whose controls sit close to the centre,
// so the rays read thin and concave like a glint of light.
private fun denebSparklePath(size: Size, fill: Float): Path {
    val cx = size.width / 2f
    val cy = size.height / 2f
    val outer = size.minDimension / 2f * fill
    val d = outer * 0.12f // side-curve control offset on the diagonals → thin rays
    val path = Path()
    path.moveTo(cx, cy - outer)
    path.quadraticBezierTo(cx + d, cy - d, cx + outer, cy)
    path.quadraticBezierTo(cx + d, cy + d, cx, cy + outer)
    path.quadraticBezierTo(cx - d, cy + d, cx - outer, cy)
    path.quadraticBezierTo(cx - d, cy - d, cx, cy - outer)
    path.close()
    return path
}
