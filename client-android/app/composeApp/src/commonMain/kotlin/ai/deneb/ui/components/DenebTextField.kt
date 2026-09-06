package ai.deneb.ui.components

import ai.deneb.ui.DenebType
import ai.deneb.ui.denebHairline
import ai.deneb.ui.denebHint
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp

/**
 * Deneb's text input: a muted label above, the value in house body type, and one
 * hairline beneath that turns cool-primary on focus and error-red on error. No box,
 * no fill — the same flat idiom as the cron editor's fields and
 * [DenebUnderlineSearchField], so a form reads like the rest of the Deneb surface
 * instead of a stack of Material outlines.
 *
 * The parameter list mirrors Material's `OutlinedTextField` (the control it replaced
 * app-wide, 2026-09-06), so a call site migrates by changing the name: [label],
 * [placeholder], [supportingText] and the icon slots keep their composable shape.
 * The control is a foundation `BasicTextField`; nothing Material is drawn.
 *
 * Read-only pickers (dropdowns) pass [readOnly] and a [trailingIcon]; the whole
 * field is their anchor, so the menu opens under the hairline.
 */
@Composable
fun DenebTextField(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    readOnly: Boolean = false,
    label: @Composable (() -> Unit)? = null,
    placeholder: @Composable (() -> Unit)? = null,
    leadingIcon: @Composable (() -> Unit)? = null,
    trailingIcon: @Composable (() -> Unit)? = null,
    visualTransformation: VisualTransformation = VisualTransformation.None,
    singleLine: Boolean = false,
    minLines: Int = 1,
    maxLines: Int = if (singleLine) 1 else Int.MAX_VALUE,
    isError: Boolean = false,
    supportingText: @Composable (() -> Unit)? = null,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    keyboardActions: KeyboardActions = KeyboardActions.Default,
    textStyle: TextStyle = DenebType.body,
) {
    val cs = MaterialTheme.colorScheme
    var focused by remember { mutableStateOf(false) }
    val hint = denebHint()
    val labelColor = when {
        isError -> cs.error
        focused -> cs.primary
        !enabled -> hint.copy(alpha = 0.5f)
        else -> hint
    }
    val line = when {
        isError -> cs.error
        focused -> cs.primary
        !enabled -> denebHairline().copy(alpha = 0.5f)
        else -> denebHairline()
    }
    val valueColor = if (enabled) cs.onBackground else hint

    Column(modifier) {
        if (label != null) {
            CompositionLocalProvider(LocalContentColor provides labelColor) {
                ProvideTextStyle(DenebType.meta) { label() }
            }
            Spacer(Modifier.height(8.dp))
        }
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            modifier = Modifier
                .fillMaxWidth()
                .onFocusChanged { focused = it.isFocused }
                .padding(top = 2.dp, bottom = 10.dp),
            enabled = enabled,
            readOnly = readOnly,
            textStyle = textStyle.copy(color = valueColor),
            keyboardOptions = keyboardOptions,
            keyboardActions = keyboardActions,
            singleLine = singleLine,
            minLines = minLines,
            maxLines = maxLines,
            visualTransformation = visualTransformation,
            cursorBrush = SolidColor(cs.primary),
            decorationBox = { inner ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (leadingIcon != null) {
                        CompositionLocalProvider(LocalContentColor provides hint) { leadingIcon() }
                        Spacer(Modifier.width(10.dp))
                    }
                    Box(Modifier.weight(1f)) {
                        if (value.isEmpty() && placeholder != null) {
                            CompositionLocalProvider(LocalContentColor provides hint) {
                                ProvideTextStyle(textStyle) { placeholder() }
                            }
                        }
                        inner()
                    }
                    if (trailingIcon != null) {
                        Spacer(Modifier.width(8.dp))
                        CompositionLocalProvider(LocalContentColor provides hint) { trailingIcon() }
                    }
                }
            },
        )
        Box(
            Modifier
                .fillMaxWidth()
                .height(if (focused && !isError) 1.5.dp else 1.dp)
                .background(line),
        )
        if (supportingText != null) {
            Spacer(Modifier.height(6.dp))
            CompositionLocalProvider(LocalContentColor provides if (isError) cs.error else hint) {
                ProvideTextStyle(DenebType.meta) { supportingText() }
            }
        }
    }
}
