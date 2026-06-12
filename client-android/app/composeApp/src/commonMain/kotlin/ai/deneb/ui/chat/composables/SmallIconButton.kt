package ai.deneb.ui.chat.composables

import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import org.jetbrains.compose.resources.DrawableResource
import org.jetbrains.compose.resources.painterResource

// Per-message meta actions (TTS/copy/regenerate). These sit under every bot
// reply, so they render in the hint tone at glyph size — present when sought,
// invisible when reading.
@Composable
internal fun SmallIconButton(
    iconResource: DrawableResource,
    contentDescription: String? = null,
    onClick: () -> Unit,
) {
    SmallIconButtonBox(onClick) {
        Icon(
            modifier = Modifier.size(14.dp),
            painter = painterResource(iconResource),
            contentDescription = contentDescription,
            tint = denebHint(),
        )
    }
}

@Composable
internal fun SmallIconButton(
    imageVector: ImageVector,
    contentDescription: String? = null,
    onClick: () -> Unit,
) {
    SmallIconButtonBox(onClick) {
        Icon(
            modifier = Modifier.size(14.dp),
            imageVector = imageVector,
            contentDescription = contentDescription,
            tint = denebHint(),
        )
    }
}

@Composable
private fun SmallIconButtonBox(onClick: () -> Unit, content: @Composable () -> Unit) {
    val haptics = rememberHaptics()
    Box(
        modifier = Modifier.size(36.dp).clip(CircleShape).handCursor().clickable {
            haptics.tap()
            onClick()
        },
        contentAlignment = Alignment.Center,
    ) { content() }
}
