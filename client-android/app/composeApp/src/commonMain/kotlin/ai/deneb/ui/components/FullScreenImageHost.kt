package ai.deneb.ui.components

import ai.deneb.PlatformBackHandler
import ai.deneb.ui.chat.composables.FullScreenAsyncImageViewerOverlay
import ai.deneb.ui.chat.composables.FullScreenImageViewerOverlay
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap

// (bitmap, pngBytes?) — the bitmap is what renders; the optional original encoded
// bytes back the viewer's save/share actions. Callers that have the source bytes
// (image attachments) pass them; those that don't pass null and the viewer hides
// the export buttons.
val LocalShowFullScreenImage = staticCompositionLocalOf<(ImageBitmap, ByteArray?) -> Unit> { { _, _ -> } }

// Model-backed variant (a Coil model, typically a URL string) for images we don't have
// decoded bytes for — markdown `![](url)` images. The overlay loads it via AsyncImage and
// offers zoom/pan/dismiss, but no save/share (no original bytes). A null model is a no-op.
val LocalShowFullScreenImageModel = staticCompositionLocalOf<(Any?) -> Unit> { { _ -> } }

@Composable
fun FullScreenImageHost(content: @Composable () -> Unit) {
    var image by remember { mutableStateOf<ImageBitmap?>(null) }
    var bytes by remember { mutableStateOf<ByteArray?>(null) }
    var model by remember { mutableStateOf<Any?>(null) }
    val show = remember {
        { bitmap: ImageBitmap, png: ByteArray? ->
            image = bitmap
            bytes = png
        }
    }
    val showModel = remember { { m: Any? -> model = m } }
    val dismiss = remember {
        {
            image = null
            bytes = null
            model = null
        }
    }

    Box(Modifier.fillMaxSize()) {
        CompositionLocalProvider(
            LocalShowFullScreenImage provides show,
            LocalShowFullScreenImageModel provides showModel,
        ) {
            content()
        }
        image?.let { bmp ->
            FullScreenImageViewerOverlay(bitmap = bmp, pngBytes = bytes, onDismiss = dismiss)
            PlatformBackHandler(enabled = true, onBack = dismiss)
        }
        model?.let { m ->
            FullScreenAsyncImageViewerOverlay(model = m, onDismiss = dismiss)
            PlatformBackHandler(enabled = true, onBack = dismiss)
        }
    }
}
