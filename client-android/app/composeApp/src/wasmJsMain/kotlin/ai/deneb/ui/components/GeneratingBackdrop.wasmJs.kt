package ai.deneb.ui.components

import androidx.compose.ui.graphics.drawscope.DrawScope

// wasmJs uses the Compose slice fallback. Return false so [generatingBackdrop] paints
// [drawAuroraSlices].
actual fun DrawScope.drawAuroraShader(
    width: Float,
    height: Float,
    timeSeconds: Float,
    intensity: Float,
): Boolean = false
