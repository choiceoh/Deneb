package ai.deneb.ui.chat.composables

import ai.deneb.saveFileToDevice
import ai.deneb.shareImageToApps
import ai.deneb.ui.handCursor
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Download
import androidx.compose.material.icons.filled.Share
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.image_viewer_close
import deneb.composeapp.generated.resources.image_viewer_save
import deneb.composeapp.generated.resources.image_viewer_share
import kotlinx.coroutines.launch
import org.jetbrains.compose.resources.stringResource

/**
 * Fullscreen image overlay. Rendered at the App root (not in a Dialog) so it
 * covers the entire Activity content including status / navigation bar areas.
 * Use via [ai.deneb.ui.components.FullScreenImageHost] +
 * [ai.deneb.ui.components.LocalShowFullScreenImage].
 */
@Composable
internal fun FullScreenImageViewerOverlay(
    bitmap: ImageBitmap,
    pngBytes: ByteArray?,
    onDismiss: () -> Unit,
) {
    var scale by remember { mutableStateOf(1f) }
    var offset by remember { mutableStateOf(Offset.Zero) }
    val scope = rememberCoroutineScope()

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(Color.Black.copy(alpha = 0.95f))
            .pointerInput(Unit) {
                // Single-tap on the backdrop dismisses, matching the old Dialog UX.
                detectTapGestures(onTap = { onDismiss() })
            },
    ) {
        Image(
            bitmap = bitmap,
            contentDescription = null,
            contentScale = ContentScale.Fit,
            modifier = Modifier
                .fillMaxSize()
                .graphicsLayer(
                    scaleX = scale,
                    scaleY = scale,
                    translationX = offset.x,
                    translationY = offset.y,
                )
                .pointerInput(Unit) {
                    detectTransformGestures { _, pan, zoom, _ ->
                        scale = (scale * zoom).coerceIn(1f, 5f)
                        offset = if (scale > 1f) offset + pan else Offset.Zero
                    }
                }
                .pointerInput(Unit) {
                    detectTapGestures(
                        onDoubleTap = {
                            if (scale > 1f) {
                                scale = 1f
                                offset = Offset.Zero
                            } else {
                                scale = 2.5f
                            }
                        },
                    )
                },
        )
        IconButton(
            onClick = onDismiss,
            modifier = Modifier
                .align(Alignment.TopEnd)
                .statusBarsPadding()
                .padding(8.dp)
                .clip(CircleShape)
                .background(Color.Black.copy(alpha = 0.4f))
                .handCursor(),
        ) {
            Icon(
                imageVector = Icons.Default.Close,
                contentDescription = stringResource(Res.string.image_viewer_close),
                tint = Color.White,
            )
        }

        // Export actions, shown only when the original encoded bytes are available
        // (image attachments pass them; a bitmap-only caller hides these): save to a
        // file via the platform save dialog, or hand off to the OS share sheet.
        val bytes = pngBytes
        if (bytes != null) {
            Row(
                modifier = Modifier
                    .align(Alignment.TopStart)
                    .statusBarsPadding()
                    .padding(8.dp),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                IconButton(
                    onClick = { scope.launch { saveFileToDevice(bytes, "deneb-image", "png") } },
                    modifier = Modifier
                        .clip(CircleShape)
                        .background(Color.Black.copy(alpha = 0.4f))
                        .handCursor(),
                ) {
                    Icon(
                        imageVector = Icons.Default.Download,
                        contentDescription = stringResource(Res.string.image_viewer_save),
                        tint = Color.White,
                    )
                }
                IconButton(
                    onClick = { scope.launch { shareImageToApps(bytes, "deneb-image", "png") } },
                    modifier = Modifier
                        .clip(CircleShape)
                        .background(Color.Black.copy(alpha = 0.4f))
                        .handCursor(),
                ) {
                    Icon(
                        imageVector = Icons.Default.Share,
                        contentDescription = stringResource(Res.string.image_viewer_share),
                        tint = Color.White,
                    )
                }
            }
        }
    }
}
