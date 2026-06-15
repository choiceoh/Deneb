package ai.deneb.ui.components

import androidx.compose.ui.graphics.drawscope.DrawScope

// iOS uses the Compose slice fallback (a runtime-shader path isn't wired here). Return
// false so [generatingBackdrop] paints [drawAuroraSlices].
actual fun DrawScope.drawAuroraShader(
    width: Float,
    height: Float,
    timeSeconds: Float,
    intensity: Float,
): Boolean = false
