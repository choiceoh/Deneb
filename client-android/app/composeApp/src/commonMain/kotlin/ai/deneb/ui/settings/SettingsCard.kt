package ai.deneb.ui.settings

import ai.deneb.ui.denebGroupSurface
import ai.deneb.ui.handCursor
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * Deneb-styled card used to group settings rows in [DenebConfigScreen]'s sub-tabs.
 *
 * Design refresh (2026-06): backed by the shared [denebGroupSurface] (rounded inset
 * card + faint monochrome wash) so the settings sub-tabs read identically to the
 * settings hub's `DenebGroup` cards — one grouped-card surface across the whole
 * settings tree (replaces the older Material `Card` + `denebAdaptiveCard*`).
 */
@Composable
internal fun SettingsCard(
    modifier: Modifier = Modifier,
    innerPadding: Boolean = true,
    onClick: (() -> Unit)? = null,
    content: @Composable () -> Unit,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .denebGroupSurface()
            .then(if (onClick != null) Modifier.clickable(onClick = onClick).handCursor() else Modifier)
            .then(if (innerPadding) Modifier.padding(16.dp) else Modifier),
    ) {
        content()
    }
}
