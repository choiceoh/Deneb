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
private const val BACKDROP_HUE_SPREAD = 110f // degrees of hue across the width
private const val BACKDROP_PEAK_ALPHA = 0.62f
private const val BACKDROP_HEIGHT_FRACTION = 0.9f // deepest curtain reach
private const val BACKDROP_HEIGHT_MIN = 0.6f // shallowest curtain reach
private const val BACKDROP_START_HUE = 240f // blue
private const val BACKDROP_BREATH_S = 3.5f // brightness breath period (seconds)
private const val BACKDROP_SHAPE_S = 3.2f // curtain-ripple period (seconds), fallback
private const val BACKDROP_SLICE_DP = 10f // fallback column width

// Keep the backdrop on this long AFTER generation ends, so a fast reply (챗봇 answers
// quickly) still shows it instead of a flicker, and it lingers into the answer's first
// moments before fading out.
private const val BACKDROP_HOLD_MS = 600L

// A long linear clock (seconds) that resets when generation starts, so uTime gives the
// shader real elapsed seconds and the hue starts at blue. ~10 min is far longer than
// any generating window, so the wrap is never seen.
private const val BACKDROP_CLOCK_MAX_S = 600f

// Shared GPU shader source (AGSL on Android / SkSL on Skia desktop+iOS — same Skia
// shading language, same entry point and uniforms). It draws the whole aurora:
// top-down fade, a hue band across the width, an undulating curtain edge, upward ray
// flow, and a brightness breath — all from uTime/uIntensity/uResolution. The platform
// `auroraShaderBrush` factory feeds these uniforms; where unsupported it returns null
// and [drawAuroraSlices] paints a (flatter, no-ray) Compose fallback.
internal const val AURORA_SHADER_SRC = """
uniform float2 uResolution;
uniform float uTime;
uniform float uIntensity;

float hash(float2 p) { return fract(sin(dot(p, float2(127.1, 311.7))) * 43758.5453); }

float noise(float2 p) {
    float2 i = floor(p);
    float2 f = fract(p);
    float2 u = f * f * (3.0 - 2.0 * f);
    float a = hash(i);
    float b = hash(i + float2(1.0, 0.0));
    float c = hash(i + float2(0.0, 1.0));
    float d = hash(i + float2(1.0, 1.0));
    return mix(mix(a, b, u.x), mix(c, d, u.x), u.y);
}

float fbm(float2 p) {
    float v = 0.0;
    float amp = 0.5;
    for (int k = 0; k < 4; k++) {
        v += amp * noise(p);
        p *= 2.0;
        amp *= 0.5;
    }
    return v;
}

half3 hsv2rgb(float h, float s, float v) {
    float hp = fract(h / 360.0);
    float3 p = abs(fract(float3(hp, hp, hp) + float3(0.0, 2.0 / 3.0, 1.0 / 3.0)) * 6.0 - 3.0);
    float3 rgb = v * mix(float3(1.0, 1.0, 1.0), clamp(p - 1.0, 0.0, 1.0), s);
    return half3(rgb);
}

half4 main(float2 fragCoord) {
    float2 uv = fragCoord / uResolution; // uv.y: 0 top .. 1 bottom
    float t = uTime;

    float baseHue = 240.0 - (t / 6.0) * 360.0;
    float hue = baseHue + uv.x * 110.0;

    // Undulating curtain lower edge per column; bright at the top (uv.y=0), fading to
    // 0 at the edge. (smoothstep needs edge0 < edge1, hence 1.0 - smoothstep(0, edge).)
    float edge = mix(0.6, 0.9, fbm(float2(uv.x * 2.5, t * 0.25)));
    float vert = 1.0 - smoothstep(0.0, edge, uv.y);

    // Vertical rays streaming upward over time.
    float rays = 0.6 + 0.4 * fbm(float2(uv.x * 6.0, uv.y * 3.0 - t * 0.8));

    float breath = 0.85 + 0.15 * (0.5 + 0.5 * sin(t * 6.2831853 / 3.5));
    float peak = 0.62 * uIntensity * breath;
    float alpha = peak * vert * rays;

    half3 rgb = hsv2rgb(hue, 0.72, 0.95);
    return half4(rgb * alpha, alpha); // premultiplied
}
"""

/**
 * Platform GPU-shader draw. Paints the full [width]x[height] aurora for
 * [timeSeconds]/[intensity] directly into the current [DrawScope] and returns true; or
 * returns false (drawing nothing) when GPU shaders are unavailable (Android < 13,
 * iOS/wasm here), in which case the caller falls back to [drawAuroraSlices].
 * Implementations cache the compiled shader and only update uniforms per frame.
 */
expect fun DrawScope.drawAuroraShader(
    width: Float,
    height: Float,
    timeSeconds: Float,
    intensity: Float,
): Boolean

/**
 * The "generating" backdrop: while [active] (a reply is being thought up, before its
 * text starts rendering) a soft aurora glows from the top of the screen — a hue band
 * across the width, an undulating curtain edge, and (on GPU-shader platforms) vertical
 * rays streaming upward — fading to black toward the bottom. Drawn BEHIND the content
 * (then [drawContent]), so the top bar, chat, and input bar sit over it.
 *
 * It holds for a short [BACKDROP_HOLD_MS] after generation ends (so a fast reply still
 * shows it instead of a flicker) then dissolves gently, lingering into the answer's
 * first moments. The hue cycles (~6s) and the whole thing breathes, so it reads alive.
 *
 * Rendering: a shared Skia shader ([AURORA_SHADER_SRC]) via [auroraShaderBrush] on
 * Android (AGSL, API 33+) and desktop (SkSL); elsewhere a Compose slice fallback
 * ([drawAuroraSlices]) — flatter and without rays but the same color/curtain feel.
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
            val t = clock.value
            if (!drawAuroraShader(size.width, size.height, t, a)) {
                drawAuroraSlices(t, a)
            }
        }
        drawContent()
    }
}

/**
 * Compose fallback used when no GPU shader is available: paints the aurora as thin
 * vertical columns — a hue band across the width and an undulating curtain edge — from
 * the same [timeSeconds] clock. Flatter than the shader (no per-pixel ray flow) but the
 * same palette, curtain, breath, and hue cycle.
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
