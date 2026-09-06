package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import ai.deneb.ui.handCursor
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonColors
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * Deneb's four button roles on the Material button substrate.
 *
 * Material stays underneath — ripple, disabled states, focus, semantics — and only
 * the presentation changes: one corner radius shared with [DenebChip], the house
 * button type ([DenebType.button]) instead of Material's label style, tighter
 * padding, and colours drawn from the two-accent doctrine rather than the tonal
 * defaults. Every parameter mirrors the Material call it wraps, so a call site
 * migrates by changing the name.
 *
 * Roles, by weight:
 *  - [DenebButton]         filled, cool primary — the one CTA on a screen.
 *  - [DenebTonalButton]    soft fill — a secondary action that still needs a shape.
 *  - [DenebOutlinedButton] hairline box — an alternative to the CTA, or a picker.
 *  - [DenebTextButton]     bare label — dialog decisions, inline "다시 시도".
 */
private val denebButtonShape = RoundedCornerShape(10.dp)
private val denebButtonMinHeight = 44.dp
private val denebButtonPadding = PaddingValues(horizontal = 20.dp, vertical = 10.dp)
private val denebTextButtonPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp)

@Composable
fun DenebButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    colors: ButtonColors? = null,
    contentPadding: PaddingValues = denebButtonPadding,
    content: @Composable RowScope.() -> Unit,
) {
    Button(
        onClick = onClick,
        modifier = modifier.defaultMinSize(minHeight = denebButtonMinHeight).handCursor(),
        enabled = enabled,
        shape = denebButtonShape,
        colors = colors ?: ButtonDefaults.buttonColors(
            containerColor = MaterialTheme.colorScheme.primary,
            contentColor = MaterialTheme.colorScheme.onPrimary,
        ),
        contentPadding = contentPadding,
    ) {
        ProvideTextStyle(DenebType.button) { content() }
    }
}

@Composable
fun DenebTonalButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    contentPadding: PaddingValues = denebButtonPadding,
    content: @Composable RowScope.() -> Unit,
) {
    Button(
        onClick = onClick,
        modifier = modifier.defaultMinSize(minHeight = denebButtonMinHeight).handCursor(),
        enabled = enabled,
        shape = denebButtonShape,
        colors = ButtonDefaults.buttonColors(
            containerColor = MaterialTheme.colorScheme.secondaryContainer,
            contentColor = MaterialTheme.colorScheme.onSecondaryContainer,
        ),
        contentPadding = contentPadding,
    ) {
        ProvideTextStyle(DenebType.button) { content() }
    }
}

@Composable
fun DenebOutlinedButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    contentPadding: PaddingValues = denebButtonPadding,
    content: @Composable RowScope.() -> Unit,
) {
    val outline = MaterialTheme.colorScheme.outline
    OutlinedButton(
        onClick = onClick,
        modifier = modifier.defaultMinSize(minHeight = denebButtonMinHeight).handCursor(),
        enabled = enabled,
        shape = denebButtonShape,
        colors = ButtonDefaults.outlinedButtonColors(contentColor = MaterialTheme.colorScheme.onSurface),
        border = BorderStroke(1.dp, if (enabled) outline else outline.copy(alpha = 0.38f)),
        contentPadding = contentPadding,
    ) {
        ProvideTextStyle(DenebType.button) { content() }
    }
}

@Composable
fun DenebTextButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    contentPadding: PaddingValues = denebTextButtonPadding,
    content: @Composable RowScope.() -> Unit,
) {
    TextButton(
        onClick = onClick,
        modifier = modifier.handCursor(),
        enabled = enabled,
        shape = denebButtonShape,
        colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.primary),
        contentPadding = contentPadding,
    ) {
        ProvideTextStyle(DenebType.button) { content() }
    }
}
