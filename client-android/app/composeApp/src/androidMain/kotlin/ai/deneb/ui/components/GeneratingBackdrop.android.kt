package ai.deneb.ui.components

import android.graphics.RuntimeShader
import android.os.Build
import androidx.annotation.RequiresApi
import androidx.compose.ui.graphics.ShaderBrush
import androidx.compose.ui.graphics.drawscope.DrawScope

// AGSL RuntimeShader is API 33+ (Android 13). Below that — and on any compile failure —
// return false so the caller falls back to the Compose slice draw.
actual fun DrawScope.drawAuroraShader(
    width: Float,
    height: Float,
    timeSeconds: Float,
    intensity: Float,
): Boolean {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU || width <= 0f || height <= 0f) return false
    return drawAgsl(width, height, timeSeconds, intensity)
}

// Compiled once, reused; only the uniforms change per frame.
@RequiresApi(Build.VERSION_CODES.TIRAMISU)
private var auroraShader: RuntimeShader? = null

@RequiresApi(Build.VERSION_CODES.TIRAMISU)
private fun DrawScope.drawAgsl(width: Float, height: Float, timeSeconds: Float, intensity: Float): Boolean {
    val shader = auroraShader
        ?: runCatching { RuntimeShader(AURORA_SHADER_SRC) }.getOrNull()?.also { auroraShader = it }
        ?: return false
    shader.setFloatUniform("uResolution", width, height)
    shader.setFloatUniform("uTime", timeSeconds)
    shader.setFloatUniform("uIntensity", intensity)
    drawRect(brush = ShaderBrush(shader))
    return true
}
