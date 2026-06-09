package ai.deneb.ui.chat.composables

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import ai.deneb.ui.components.rememberHaptics
import ai.deneb.ui.handCursor
import org.jetbrains.compose.resources.DrawableResource
import org.jetbrains.compose.resources.painterResource

@Composable
internal fun SmallIconButton(
    iconResource: DrawableResource,
    contentDescription: String? = null,
    onClick: () -> Unit,
) {
    SmallIconButtonBox(onClick) {
        Icon(
            modifier = Modifier.size(18.dp),
            painter = painterResource(iconResource),
            contentDescription = contentDescription,
            tint = MaterialTheme.colorScheme.onBackground,
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
            modifier = Modifier.size(18.dp),
            imageVector = imageVector,
            contentDescription = contentDescription,
            tint = MaterialTheme.colorScheme.onBackground,
        )
    }
}

@Composable
private fun SmallIconButtonBox(onClick: () -> Unit, content: @Composable () -> Unit) {
    val haptics = rememberHaptics()
    Box(
        modifier = Modifier.size(48.dp).clip(CircleShape).handCursor().clickable { haptics.tap(); onClick() },
        contentAlignment = Alignment.Center,
    ) { content() }
}
