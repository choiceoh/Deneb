package ai.deneb.ui.settings

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import ai.deneb.ui.denebAdaptiveCardBorder
import ai.deneb.ui.denebAdaptiveCardColors
import ai.deneb.ui.handCursor

/**
 * Deneb-styled card used to group settings rows. The Kai settings screen that
 * originally hosted it was removed (unreachable), but the live [DenebConfigScreen]
 * still uses this card, so it was extracted here on its own.
 */
@Composable
internal fun SettingsCard(
    modifier: Modifier = Modifier,
    innerPadding: Boolean = true,
    onClick: (() -> Unit)? = null,
    content: @Composable () -> Unit,
) {
    Card(
        modifier = modifier,
        colors = denebAdaptiveCardColors(),
        border = denebAdaptiveCardBorder(),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .then(if (onClick != null) Modifier.clickable(onClick = onClick).handCursor() else Modifier)
                .then(if (innerPadding) Modifier.padding(16.dp) else Modifier),
        ) {
            content()
        }
    }
}
