package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHint
import ai.deneb.ui.handCursor
import androidx.compose.foundation.background
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp

/**
 * Deneb's single-choice option row — the form sibling of [ai.deneb.ui.DenebPivotRow].
 *
 * It used to wrap Material's SegmentedButton: an outlined stadium split into cells
 * with a tinted fill on the chosen one. On an OLED page that box was the loudest
 * thing in the form and read as a pill (operator, 2026-09-20: "이상하게 못생긴
 * 알약모양"). The choice is now carried by TYPE: every option is a row-size word;
 * the chosen one is ink and SemiBold with a 2dp accent underline the width of the
 * word, the rest are dimmed. That is the grammar the engine screen's model tabs
 * already use and the operator picked there ("배경 없이 이름에 밑줄"). Material stays
 * underneath as semantics only (selectableGroup + Role.RadioButton).
 *
 * Distinct from the pivot on purpose: a pivot switches what the page shows
 * (title-size, brightness alone); a segmented row picks a value inside a form
 * (row-size, and the underline marks the value). Same ink/dim voice, one extra mark.
 */
@Composable
fun DenebSegmentedRow(
    modifier: Modifier = Modifier,
    content: @Composable RowScope.() -> Unit,
) {
    Row(
        modifier = modifier.selectableGroup(),
        horizontalArrangement = Arrangement.spacedBy(20.dp),
        verticalAlignment = Alignment.Bottom,
        content = content,
    )
}

/**
 * One option of a [DenebSegmentedRow]. [label] is a plain `Text`; the segment sets
 * its style and colour, so call sites pass words, not styling.
 */
@Composable
fun DenebSegment(
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    label: @Composable () -> Unit,
) {
    val cs = MaterialTheme.colorScheme
    val base = if (selected) cs.onBackground else denebHint()
    val color = if (enabled) base else base.copy(alpha = base.alpha * DisabledAlpha)
    Column(
        modifier
            .width(IntrinsicSize.Max)
            .selectable(
                selected = selected,
                enabled = enabled,
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
                role = Role.RadioButton,
                onClick = onClick,
            )
            .handCursor()
            .padding(top = 6.dp),
    ) {
        ProvideTextStyle(if (selected) DenebType.rowTitleStrong else DenebType.rowTitle) {
            CompositionLocalProvider(LocalContentColor provides color) { label() }
        }
        Spacer(Modifier.height(6.dp))
        // The underline takes the word's own width (IntrinsicSize.Max above) and is
        // the only mark: no ripple, no box — the line moving is the feedback.
        Box(
            Modifier
                .fillMaxWidth()
                .height(2.dp)
                .background(if (selected) cs.primary else Color.Transparent, RoundedCornerShape(1.dp)),
        )
    }
}

private const val DisabledAlpha = 0.38f
