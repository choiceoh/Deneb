package ai.deneb.ui.components

import androidx.compose.ui.graphics.drawscope.DrawScope

// Desktop uses the Compose slice fallback rather than a Skia RuntimeEffect: Compose
// Desktop frequently runs the SOFTWARE Skia backend (e.g. headless/no-GPU hosts, our
// render harness), which silently no-ops runtime-effect shaders — that would leave a
// blank backdrop with no fallback. The slice draw always renders. (The shader source
// itself is verified by AuroraShaderTest, and ships live on Android's GPU path.)
actual fun DrawScope.drawAuroraShader(
    width: Float,
    height: Float,
    timeSeconds: Float,
    intensity: Float,
): Boolean = false
