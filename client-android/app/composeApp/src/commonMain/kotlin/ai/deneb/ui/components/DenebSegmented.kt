package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import ai.deneb.ui.handCursor
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.SingleChoiceSegmentedButtonRowScope
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

/**
 * Deneb's single-choice segmented control on Material's substrate.
 *
 * Material keeps the mutually-exclusive selection semantics and the a11y role;
 * the presentation is ours. Two things made the stock control read as another
 * app's: the ✓ checkmark Material draws inside the selected segment (its most
 * recognisable signature, and pure decoration here — the fill already says
 * "selected"), and the pill shape. The mark is gone, the corners match
 * [DenebChip] and the buttons, and the selected fill is the same soft
 * secondary container a selected chip uses, so one vocabulary covers every
 * "pick one of these" surface.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DenebSegmentedRow(
    modifier: Modifier = Modifier,
    content: @Composable SingleChoiceSegmentedButtonRowScope.() -> Unit,
) {
    SingleChoiceSegmentedButtonRow(modifier = modifier, content = content)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SingleChoiceSegmentedButtonRowScope.DenebSegment(
    selected: Boolean,
    onClick: () -> Unit,
    index: Int,
    count: Int,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    label: @Composable () -> Unit,
) {
    val cs = MaterialTheme.colorScheme
    SegmentedButton(
        selected = selected,
        onClick = onClick,
        shape = SegmentedButtonDefaults.itemShape(index = index, count = count, baseShape = RoundedCornerShape(10.dp)),
        modifier = modifier.handCursor(),
        enabled = enabled,
        colors = SegmentedButtonDefaults.colors(
            activeContainerColor = cs.secondaryContainer,
            activeContentColor = cs.onSecondaryContainer,
            activeBorderColor = cs.outline,
            inactiveContainerColor = Color.Transparent,
            inactiveContentColor = cs.onSurfaceVariant,
            inactiveBorderColor = cs.outline,
            disabledActiveContainerColor = cs.secondaryContainer.copy(alpha = 0.38f),
            disabledActiveContentColor = cs.onSecondaryContainer.copy(alpha = 0.38f),
            disabledActiveBorderColor = cs.outline.copy(alpha = 0.38f),
            disabledInactiveContainerColor = Color.Transparent,
            disabledInactiveContentColor = cs.onSurfaceVariant.copy(alpha = 0.38f),
            disabledInactiveBorderColor = cs.outline.copy(alpha = 0.38f),
        ),
        // No checkmark: the fill carries the state, and the mark is what made the
        // control read as Material rather than as ours.
        icon = {},
        label = { ProvideTextStyle(DenebType.rowSubtitle) { label() } },
    )
}
