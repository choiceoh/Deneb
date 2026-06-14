package ai.deneb.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

// Design refresh (2026-06): the grouped-inset surface idiom that replaces the flat
// hairline rows. A [DenebGroup] is a rounded container with a faint monochrome wash
// that visually groups a set of [DenebListRow]s (iOS/Toss-style). It stays calm — the
// fill is a low-alpha onBackground wash (AMOLED-safe), separators are inset hairlines,
// and the only color is the restrained accent on a selected row. See Theme.kt accent
// doctrine + native-design-system.md.

/** Faint group fill — onBackground wash, lifted slightly off pure black on OLED. */
@Composable
private fun denebGroupFill(): Color {
    val cs = MaterialTheme.colorScheme
    return cs.onBackground.copy(alpha = if (cs.background.luminance() < 0.5f) 0.055f else 0.04f)
}

/**
 * The grouped-card surface — rounded corners + the faint monochrome wash. Shared by
 * [DenebGroup] and the settings-tab `SettingsCard` so every grouped surface reads
 * identically. Note: it keeps a faint wash even on OLED (the design-refresh idiom),
 * distinct from the older `denebAdaptiveCard*` outline-on-OLED callout style.
 */
@Composable
fun Modifier.denebGroupSurface(shape: Shape = RoundedCornerShape(16.dp)): Modifier = clip(shape).background(denebGroupFill())

/**
 * A grouped inset card. Optionally preceded by a [label] (tracked-caps section header).
 * Wrap a run of [DenebListRow]s; set `divider = false` on the last so no hairline shows
 * against the rounded bottom edge.
 */
@Composable
fun DenebGroup(
    modifier: Modifier = Modifier,
    label: String? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
        if (label != null) DenebSectionLabel(label)
        Column(
            Modifier
                .fillMaxWidth()
                .denebGroupSurface(),
            content = content,
        )
    }
}

/**
 * A row inside a [DenebGroup]: a leading mono icon, a title (+ optional subtitle), and a
 * trailing chevron (or custom [trailing]). A [selected] row tints its icon + title with
 * the interactive accent (`primary`). Each row draws an inset bottom hairline unless
 * [divider] is false (use on the last row of a group).
 */
@Composable
fun DenebListRow(
    title: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
    subtitle: String? = null,
    selected: Boolean = false,
    divider: Boolean = true,
    chevron: Boolean = true,
    trailing: (@Composable () -> Unit)? = null,
) {
    val hairline = denebHairline()
    val accent = MaterialTheme.colorScheme.primary
    val insetPx = with(androidx.compose.ui.platform.LocalDensity.current) { 52.dp.toPx() }
    Row(
        modifier = modifier
            .fillMaxWidth()
            .denebPressable(onClick = onClick)
            .handCursor()
            .drawBehind {
                if (divider) {
                    val stroke = 1.dp.toPx()
                    val y = size.height - stroke / 2f
                    drawLine(hairline, Offset(insetPx, y), Offset(size.width, y), strokeWidth = stroke)
                }
            }
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (icon != null) {
            Icon(
                imageVector = icon,
                contentDescription = null,
                tint = if (selected) accent else denebHint(),
                modifier = Modifier.size(22.dp),
            )
            Spacer(Modifier.width(14.dp))
        }
        Column(Modifier.weight(1f)) {
            Text(
                text = title,
                style = DenebType.rowTitleStrong,
                color = if (selected) accent else MaterialTheme.colorScheme.onBackground,
            )
            if (subtitle != null) {
                Text(text = subtitle, style = DenebType.rowSubtitle, color = denebHint())
            }
        }
        when {
            trailing != null -> {
                Spacer(Modifier.width(8.dp))
                trailing()
            }

            chevron -> {
                Spacer(Modifier.width(8.dp))
                Icon(
                    imageVector = Icons.AutoMirrored.Filled.KeyboardArrowRight,
                    contentDescription = null,
                    tint = denebHint(),
                    modifier = Modifier.size(20.dp),
                )
            }
        }
    }
}
