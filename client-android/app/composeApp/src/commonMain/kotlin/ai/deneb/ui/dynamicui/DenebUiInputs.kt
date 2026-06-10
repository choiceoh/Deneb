@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.dynamicui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.indication
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Map
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.ripple
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateMap
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import ai.deneb.ui.DenebOutlinedTextField
import ai.deneb.ui.components.DenebChip
import ai.deneb.ui.denebBreathing
import ai.deneb.ui.handCursor
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.bot_message_copy_content_description
import org.jetbrains.compose.resources.stringResource

/**
 * Interactive form components of the deneb-ui renderer: button / text input /
 * checkbox / select / switch / slider / radio group / chip group. They read and
 * write the shared formState owned by DenebUiRenderer.kt.
 */

@Composable
internal fun RenderButton(
    node: ButtonNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
) {
    val uriHandler = LocalUriHandler.current
    val clipboardManager = LocalClipboardManager.current
    var clicked by remember { mutableStateOf(false) }
    LaunchedEffect(isInteractive) {
        if (isInteractive) clicked = false
    }
    val frozen = LocalFrozenSubmission.current
    val isPressedSnapshot = !isInteractive && frozen?.pressedEvent != null && run {
        val action = node.action as? CallbackAction ?: return@run false
        action.event == frozen.pressedEvent && collectFormData(action, formState) == frozen.values
    }
    val showPulse = (clicked && !isInteractive) || (isPressedSnapshot && frozen.isPending)
    val enabled = isInteractive && (node.enabled != false)
    val onClick: () -> Unit = {
        try {
            when (val action = node.action) {
                is CallbackAction -> {
                    val data = collectFormData(action, formState)
                    clicked = true
                    onCallback(action.event, data)
                }

                is ToggleAction -> {
                    toggleState[action.targetId] = !(toggleState[action.targetId] ?: true)
                }

                is OpenUrlAction -> {
                    uriHandler.openUri(action.url)
                }

                is CopyToClipboardAction -> {
                    clipboardManager.setText(AnnotatedString(action.text))
                }

                null -> {}
            }
        } catch (_: Exception) {
            // Prevent crashes from action handlers
        }
    }

    val buttonModifier = Modifier.handCursor().then(pulseModifier(showPulse))
    if (node.action is CopyToClipboardAction) {
        IconButton(onClick = onClick, enabled = enabled, modifier = buttonModifier) {
            Icon(
                imageVector = Icons.Filled.ContentCopy,
                contentDescription = stringResource(Res.string.bot_message_copy_content_description),
            )
        }
        return
    }
    val labelContent: @Composable () -> Unit = { Text(node.label) }
    if (isPressedSnapshot) {
        // The pressed button in a frozen snapshot uses primary colors so it stands out
        // against the greyed-out disabled siblings. `enabled=false` prevents clicks; the
        // override on disabled colors bypasses Material's auto-faded disabled appearance.
        val pressedColors = ButtonDefaults.buttonColors(
            disabledContainerColor = MaterialTheme.colorScheme.primary,
            disabledContentColor = MaterialTheme.colorScheme.onPrimary,
        )
        Button(
            onClick = {},
            enabled = false,
            colors = pressedColors,
            modifier = buttonModifier,
        ) { labelContent() }
        return
    }
    when (node.variant) {
        ButtonVariant.OUTLINED -> OutlinedButton(onClick = onClick, enabled = enabled, modifier = buttonModifier) { labelContent() }
        ButtonVariant.TEXT -> TextButton(onClick = onClick, enabled = enabled, modifier = buttonModifier) { labelContent() }
        ButtonVariant.TONAL -> FilledTonalButton(onClick = onClick, enabled = enabled, modifier = buttonModifier) { labelContent() }
        ButtonVariant.FILLED, null -> Button(onClick = onClick, enabled = enabled, modifier = buttonModifier) { labelContent() }
    }
}

@Composable
private fun pulseModifier(active: Boolean): Modifier {
    if (!active) return Modifier
    // Shares the app-wide breathing cadence (waiting dot, stop button) so a live
    // dynamic-UI button pulses in sync with the rest of the chrome.
    return Modifier.denebBreathing(minScale = 0.96f, minAlpha = 0.55f)
}

@Composable
internal fun RenderTextInput(
    node: TextInputNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
) {
    DenebOutlinedTextField(
        value = formState[node.id] ?: "",
        onValueChange = { formState[node.id] = it },
        label = node.label?.let { { Text(it) } },
        placeholder = node.placeholder?.let { { Text(it) } },
        enabled = isInteractive,
        singleLine = node.multiline != true,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
internal fun RenderCheckbox(
    node: CheckboxNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
) {
    val checked = formState[node.id]?.toBooleanStrictOrNull() ?: false
    val toggle = { formState[node.id] = (!checked).toString() }
    val interactionSource = remember { MutableInteractionSource() }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .handCursor()
            .then(
                if (isInteractive) {
                    Modifier.clickable(
                        interactionSource = interactionSource,
                        indication = null,
                        onClick = toggle,
                    )
                } else {
                    Modifier
                },
            ),
    ) {
        Checkbox(
            checked = checked,
            onCheckedChange = null,
            enabled = isInteractive,
            modifier = Modifier.indication(
                interactionSource = interactionSource,
                indication = ripple(bounded = false, radius = 20.dp),
            ),
            interactionSource = interactionSource,
        )
        Text(node.label, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(start = 8.dp))
    }
}

@OptIn(ExperimentalMaterial3Api::class)

@Composable
internal fun RenderSelect(
    node: SelectNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
) {
    var expanded by remember { mutableStateOf(false) }
    val selected = formState[node.id] ?: ""

    ExposedDropdownMenuBox(
        expanded = expanded,
        onExpandedChange = { if (isInteractive) expanded = it },
    ) {
        OutlinedTextField(
            value = selected,
            onValueChange = {},
            readOnly = true,
            label = node.label?.let { { Text(it) } },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded) },
            enabled = isInteractive,
            shape = RoundedCornerShape(12.dp),
            modifier = Modifier.fillMaxWidth().menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable).handCursor(),
        )
        ExposedDropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            for (option in node.options) {
                DropdownMenuItem(
                    text = { Text(option) },
                    modifier = Modifier.handCursor(),
                    onClick = {
                        formState[node.id] = option
                        expanded = false
                    },
                )
            }
        }
    }
}

@Composable
internal fun RenderSwitch(
    node: SwitchNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
) {
    val checked = formState[node.id]?.toBooleanStrictOrNull() ?: false
    val toggle = { formState[node.id] = (!checked).toString() }
    val interactionSource = remember { MutableInteractionSource() }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .handCursor()
            .then(
                if (isInteractive) {
                    Modifier.clickable(
                        interactionSource = interactionSource,
                        indication = null,
                        onClick = toggle,
                    )
                } else {
                    Modifier
                },
            ),
    ) {
        Text(
            text = node.label,
            style = MaterialTheme.typography.bodyLarge,
            modifier = Modifier.weight(1f),
        )
        Switch(
            checked = checked,
            onCheckedChange = null,
            enabled = isInteractive,
            interactionSource = interactionSource,
        )
    }
}

@Composable
internal fun RenderSlider(
    node: SliderNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
) {
    val min = node.min ?: 0f
    val max = node.max ?: 100f
    val step = node.step
    val currentValue = formState[node.id]?.toFloatOrNull() ?: (node.value ?: min)

    Column(Modifier.fillMaxWidth()) {
        if (node.label != null) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(node.label, style = MaterialTheme.typography.bodyLarge)
                Text(
                    text = formatSliderValue(currentValue, step),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
        }
        val steps = if (step != null && step > 0) {
            ((max - min) / step).toInt() - 1
        } else {
            0
        }
        Slider(
            value = currentValue.coerceIn(min, max),
            onValueChange = { formState[node.id] = formatSliderValue(it, step) },
            valueRange = min..max,
            steps = steps.coerceAtLeast(0),
            enabled = isInteractive,
            modifier = Modifier.fillMaxWidth()
                .handCursor(),
            colors = SliderDefaults.colors(
                thumbColor = MaterialTheme.colorScheme.primary,
                activeTrackColor = MaterialTheme.colorScheme.primary,
                inactiveTrackColor = MaterialTheme.colorScheme.surfaceVariant,
                activeTickColor = Color.Transparent,
                inactiveTickColor = Color.Transparent,
            ),
            thumb = {
                Box(
                    modifier = Modifier
                        .size(20.dp)
                        .background(MaterialTheme.colorScheme.primary, RoundedCornerShape(50)),
                )
            },
            track = { sliderState ->
                SliderDefaults.Track(
                    sliderState = sliderState,
                    colors = SliderDefaults.colors(
                        activeTrackColor = MaterialTheme.colorScheme.primary,
                        inactiveTrackColor = MaterialTheme.colorScheme.surfaceVariant,
                    ),
                    drawStopIndicator = null,
                    drawTick = { _, _ -> },
                )
            },
        )
    }
}

internal fun formatSliderValue(value: Float, step: Float?): String {
    if (step != null && step > 0) {
        val rounded = kotlin.math.round(value / step) * step
        if (rounded == rounded.toLong().toFloat()) {
            return rounded.toLong().toString()
        }
        // Determine decimal places from step (e.g. step=0.1 → 1 decimal)
        val stepStr = step.toString()
        val decimals = stepStr.substringAfter('.', "").trimEnd('0').length.coerceIn(1, 6)
        var factor = 1f
        repeat(decimals) { factor *= 10f }
        return (kotlin.math.round(rounded * factor) / factor).toString()
    }
    return if (value == value.toLong().toFloat()) {
        value.toLong().toString()
    } else {
        val rounded = kotlin.math.round(value * 100.0f) / 100.0f
        rounded.toString()
    }
}

@Composable
internal fun RenderRadioGroup(
    node: RadioGroupNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
) {
    val selected = formState[node.id] ?: ""
    Column(
        Modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        if (node.label != null) {
            Text(
                text = node.label,
                style = MaterialTheme.typography.titleSmall,
                modifier = Modifier.padding(bottom = 4.dp),
            )
        }
        for (option in node.options) {
            key(option) {
                val interactionSource = remember { MutableInteractionSource() }
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .fillMaxWidth()
                        .handCursor()
                        .then(
                            if (isInteractive) {
                                Modifier.clickable(
                                    interactionSource = interactionSource,
                                    indication = null,
                                    onClick = { formState[node.id] = option },
                                )
                            } else {
                                Modifier
                            },
                        ),
                ) {
                    RadioButton(
                        selected = selected == option,
                        onClick = null,
                        enabled = isInteractive,
                        modifier = Modifier.indication(
                            interactionSource = interactionSource,
                            indication = ripple(bounded = false, radius = 20.dp),
                        ),
                        interactionSource = interactionSource,
                    )
                    Text(
                        text = option,
                        style = MaterialTheme.typography.bodyLarge,
                        modifier = Modifier.padding(start = 8.dp),
                    )
                }
            }
        }
    }
}

@Composable
internal fun RenderChipGroup(
    node: ChipGroupNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
) {
    val isDisplayOnly = node.selection == "none"
    val isMulti = node.selection == "multi"

    FlowRow(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.fillMaxWidth(),
    ) {
        for (chip in node.chips) {
            val value = chip.value.ifEmpty { chip.label }
            key(value) {
                if (isDisplayOnly) {
                    DenebChip { Text(chip.label) }
                } else {
                    val isSelected by remember {
                        derivedStateOf {
                            val csv = formState[node.id] ?: ""
                            csv.split(",").contains(value)
                        }
                    }
                    DenebChip(
                        selected = isSelected,
                        onClick = {
                            if (!isInteractive) return@DenebChip
                            val current = (formState[node.id] ?: "").split(",").filter { it.isNotEmpty() }.toSet()
                            val newSelection = if (isMulti) {
                                if (isSelected) current - value else current + value
                            } else {
                                if (isSelected) emptySet() else setOf(value)
                            }
                            formState[node.id] = newSelection.joinToString(",")
                        },
                        enabled = isInteractive,
                    ) {
                        Text(chip.label)
                    }
                }
            }
        }
    }
}
