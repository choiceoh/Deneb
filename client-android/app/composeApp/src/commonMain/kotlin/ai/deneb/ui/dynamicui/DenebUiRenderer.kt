@file:OptIn(ExperimentalMaterial3Api::class)

package ai.deneb.ui.dynamicui

import ai.deneb.ui.denebAdaptiveCardBorder
import ai.deneb.ui.denebAdaptiveCardColors
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.material.icons.filled.Map
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateMap
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.unit.dp
import deneb.composeapp.generated.resources.Res
import deneb.composeapp.generated.resources.deneb_ui_render_failed
import kotlinx.collections.immutable.ImmutableList
import org.jetbrains.compose.resources.stringResource

val LocalPreviewImages = staticCompositionLocalOf<Map<String, ImageBitmap>> { emptyMap() }

/**
 * A frozen snapshot of a user's deneb-ui submission: the values they submitted, plus the
 * event of the button they pressed. Matching a button uses event + collected form data
 * rather than event alone (multiple buttons often share an event but carry distinct
 * per-button data payloads, e.g. a quiz with one event and different `choice` values).
 * `isPending` is a transient UI flag — true while the AI is still answering this submission;
 * the pressed button pulses to signal the in-flight request.
 */
@Immutable
data class FrozenSubmission(
    val values: Map<String, String> = emptyMap(),
    val pressedEvent: String? = null,
    val isPending: Boolean = false,
)

internal val LocalFrozenSubmission = compositionLocalOf<FrozenSubmission?> { null }

/**
 * Form-validation context for one rendered deneb-ui tree: which input ids are
 * required, and which currently show a "필수" error. Buttons consult it before
 * firing a callback (blank required inputs block the submit and get flagged);
 * inputs clear their own flag on edit. CompositionLocal so the many Render*
 * signatures stay untouched.
 */
internal class UiFormValidation(
    val requiredIds: Set<String>,
    val errors: SnapshotStateMap<String, Boolean>,
) {
    /** Required ids among [ids] whose current form value is blank. */
    fun missingFrom(ids: List<String>, formState: Map<String, String>): List<String> = ids.filter { it in requiredIds && formState[it].isNullOrBlank() }
}

internal val LocalUiFormValidation = compositionLocalOf<UiFormValidation?> { null }

@Composable
fun DenebUiRenderer(
    node: DenebUiNode,
    isInteractive: Boolean,
    onCallback: (event: String, data: Map<String, String>) -> Unit,
    modifier: Modifier = Modifier,
    wrapInCard: Boolean = true,
    frozen: FrozenSubmission? = null,
) {
    val formState = remember { mutableStateMapOf<String, String>() }
    val toggleState = remember { mutableStateMapOf<String, Boolean>() }
    val validationErrors = remember { mutableStateMapOf<String, Boolean>() }
    val validation = remember(node) { UiFormValidation(collectRequiredIds(node), validationErrors) }
    var hasError by remember { mutableStateOf(false) }

    LaunchedEffect(node, frozen?.values) {
        try {
            initializeFormState(node, formState)
            frozen?.values?.let { formState.putAll(it) }
        } catch (_: Exception) {
            hasError = true
        }
    }

    if (hasError) {
        Text(
            text = stringResource(Res.string.deneb_ui_render_failed),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.error,
            modifier = modifier,
        )
        return
    }

    CompositionLocalProvider(
        LocalFrozenSubmission provides frozen,
        LocalUiFormValidation provides validation,
    ) {
        if (wrapInCard) {
            Card(
                modifier = modifier.fillMaxWidth().wrapContentHeight(),
                colors = denebAdaptiveCardColors(),
                border = denebAdaptiveCardBorder(),
            ) {
                Column(Modifier.padding(12.dp).wrapContentHeight()) {
                    RenderNode(
                        node = node,
                        isInteractive = isInteractive,
                        formState = formState,
                        toggleState = toggleState,
                        onCallback = safeCallback(onCallback),
                    )
                }
            }
        } else {
            CompositionLocalProvider(LocalContentColor provides MaterialTheme.colorScheme.onBackground) {
                Column(modifier = modifier.fillMaxWidth().wrapContentHeight()) {
                    RenderNode(
                        node = node,
                        isInteractive = isInteractive,
                        formState = formState,
                        toggleState = toggleState,
                        onCallback = safeCallback(onCallback),
                    )
                }
            }
        }
    }
}

private fun safeCallback(
    onCallback: (String, Map<String, String>) -> Unit,
): (String, Map<String, String>) -> Unit = { event, data ->
    try {
        onCallback(event, data)
    } catch (_: Exception) {
        // Silently handle callback errors to prevent crashes
    }
}

private const val MAX_DEPTH = 10

@Composable
internal fun RenderNode(
    node: DenebUiNode,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
    depth: Int = 0,
) {
    if (depth > MAX_DEPTH) return

    val nodeId = node.id
    if (nodeId != null && toggleState[nodeId] == false) return

    when (node) {
        is ColumnNode -> RenderColumn(node, isInteractive, formState, toggleState, onCallback, depth)
        is RowNode -> RenderRow(node, isInteractive, formState, toggleState, onCallback, depth)
        is CardNode -> RenderCard(node, isInteractive, formState, toggleState, onCallback, depth)
        is TextNode -> RenderText(node)
        is MarkdownNode -> RenderMarkdown(node, isInteractive, onCallback)
        is ButtonNode -> RenderButton(node, isInteractive, formState, toggleState, onCallback)
        is TextInputNode -> RenderTextInput(node, isInteractive, formState)
        is DateInputNode -> RenderDateInput(node, isInteractive, formState)
        is TimeInputNode -> RenderTimeInput(node, isInteractive, formState)
        is CheckboxNode -> RenderCheckbox(node, isInteractive, formState)
        is SelectNode -> RenderSelect(node, isInteractive, formState)
        is ImageNode -> RenderImage(node)
        is TableNode -> RenderTable(node)
        is ListNode -> RenderList(node, isInteractive, formState, toggleState, onCallback, depth)
        is DividerNode -> HorizontalDivider(Modifier.padding(vertical = 4.dp))
        is SwitchNode -> RenderSwitch(node, isInteractive, formState)
        is SliderNode -> RenderSlider(node, isInteractive, formState)
        is RadioGroupNode -> RenderRadioGroup(node, isInteractive, formState)
        is ProgressNode -> RenderProgress(node)
        is CountdownNode -> RenderCountdown(node, isInteractive, formState, toggleState, onCallback)
        is AlertNode -> RenderAlert(node)
        is ChipGroupNode -> RenderChipGroup(node, isInteractive, formState)
        is IconNode -> RenderIcon(node)
        is CodeNode -> RenderCode(node)
        is BoxNode -> RenderBox(node, isInteractive, formState, toggleState, onCallback, depth)
        is TabsNode -> RenderTabs(node, isInteractive, formState, toggleState, onCallback, depth)
        is AccordionNode -> RenderAccordion(node, isInteractive, formState, toggleState, onCallback, depth)
        is QuoteNode -> RenderQuote(node)
        is BadgeNode -> RenderBadge(node)
        is StatNode -> RenderStat(node)
        is AvatarNode -> RenderAvatar(node)
        is ChartNode -> RenderChart(node)
    }
}

@Composable
internal fun RenderChildren(
    children: ImmutableList<DenebUiNode>,
    isInteractive: Boolean,
    formState: SnapshotStateMap<String, String>,
    toggleState: SnapshotStateMap<String, Boolean>,
    onCallback: (String, Map<String, String>) -> Unit,
    depth: Int,
) {
    for (child in children) {
        RenderNode(child, isInteractive, formState, toggleState, onCallback, depth + 1)
    }
}

private fun initializeFormState(node: DenebUiNode, formState: MutableMap<String, String>) {
    when (node) {
        is TextInputNode -> node.value?.let { if (node.id !in formState) formState[node.id] = it }

        is DateInputNode -> node.value?.let { if (node.id !in formState) formState[node.id] = it }

        is TimeInputNode -> node.value?.let { if (node.id !in formState) formState[node.id] = it }

        is CheckboxNode -> if (node.id !in formState) formState[node.id] = (node.checked ?: false).toString()

        is SelectNode -> node.selected?.let { if (node.id !in formState) formState[node.id] = it }

        is SwitchNode -> if (node.id !in formState) formState[node.id] = (node.checked ?: false).toString()

        is SliderNode -> if (node.id !in formState) formState[node.id] = formatSliderValue(node.value ?: node.min ?: 0f, node.step)

        is RadioGroupNode -> node.selected?.let { if (node.id !in formState) formState[node.id] = it }

        is ChipGroupNode -> if (node.selection != "none" && node.id !in formState) {
            formState[node.id] = ""
        }

        is ColumnNode -> node.children.forEach { initializeFormState(it, formState) }

        is RowNode -> node.children.forEach { initializeFormState(it, formState) }

        is CardNode -> node.children.forEach { initializeFormState(it, formState) }

        is ListNode -> node.items.forEach { initializeFormState(it, formState) }

        is BoxNode -> node.children.forEach { initializeFormState(it, formState) }

        is TabsNode -> node.tabs.forEach { tab -> tab.children.forEach { initializeFormState(it, formState) } }

        is AccordionNode -> node.children.forEach { initializeFormState(it, formState) }

        else -> {}
    }
}

/** Walk the tree and collect ids of inputs flagged `required:true`. */
private fun collectRequiredIds(node: DenebUiNode, into: MutableSet<String> = mutableSetOf()): Set<String> {
    when (node) {
        is TextInputNode -> if (node.required == true) into.add(node.id)
        is DateInputNode -> if (node.required == true) into.add(node.id)
        is TimeInputNode -> if (node.required == true) into.add(node.id)
        is SelectNode -> if (node.required == true) into.add(node.id)
        is RadioGroupNode -> if (node.required == true) into.add(node.id)
        is ChipGroupNode -> if (node.required == true && node.selection != "none") into.add(node.id)
        is ColumnNode -> node.children.forEach { collectRequiredIds(it, into) }
        is RowNode -> node.children.forEach { collectRequiredIds(it, into) }
        is CardNode -> node.children.forEach { collectRequiredIds(it, into) }
        is ListNode -> node.items.forEach { collectRequiredIds(it, into) }
        is BoxNode -> node.children.forEach { collectRequiredIds(it, into) }
        is TabsNode -> node.tabs.forEach { tab -> tab.children.forEach { collectRequiredIds(it, into) } }
        is AccordionNode -> node.children.forEach { collectRequiredIds(it, into) }
        else -> {}
    }
    return into
}

internal fun collectFormData(action: CallbackAction, formState: Map<String, String>): Map<String, String> {
    val collected = mutableMapOf<String, String>()
    action.dataAsStrings?.let { collected.putAll(it) }
    action.collectFrom?.forEach { inputId ->
        formState[inputId]?.let { collected[inputId] = it }
    }
    return collected
}
