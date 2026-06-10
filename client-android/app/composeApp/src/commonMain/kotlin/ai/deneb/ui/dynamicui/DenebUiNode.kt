package ai.deneb.ui.dynamicui

import androidx.compose.runtime.Immutable
import kotlinx.collections.immutable.ImmutableList
import kotlinx.collections.immutable.persistentListOf
import kotlinx.serialization.Contextual
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Immutable
@Serializable
sealed interface DenebUiNode {
    val id: String?
}

// --- Layout nodes ---

@Immutable
@Serializable
@SerialName("column")
data class ColumnNode(
    override val id: String? = null,
    @Contextual val children: ImmutableList<DenebUiNode> = persistentListOf(),
) : DenebUiNode

@Immutable
@Serializable
@SerialName("row")
data class RowNode(
    override val id: String? = null,
    @Contextual val children: ImmutableList<DenebUiNode> = persistentListOf(),
) : DenebUiNode

@Immutable
@Serializable
@SerialName("card")
data class CardNode(
    override val id: String? = null,
    @Contextual val children: ImmutableList<DenebUiNode> = persistentListOf(),
) : DenebUiNode

@Immutable
@Serializable
@SerialName("divider")
data class DividerNode(
    override val id: String? = null,
) : DenebUiNode

// --- Content nodes ---

@Immutable
@Serializable
@SerialName("text")
data class TextNode(
    override val id: String? = null,
    val value: String = "",
    val style: TextNodeStyle? = null,
    val bold: Boolean? = null,
    val italic: Boolean? = null,
    val color: String? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("image")
data class ImageNode(
    override val id: String? = null,
    val url: String = "",
    val alt: String? = null,
    val height: Int? = null,
    val aspectRatio: Float? = null,
) : DenebUiNode

// --- Interactive nodes ---

@Immutable
@Serializable
@SerialName("button")
data class ButtonNode(
    override val id: String? = null,
    val label: String = "",
    val action: UiAction? = null,
    val variant: ButtonVariant? = null,
    val enabled: Boolean? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("text_input")
data class TextInputNode(
    override val id: String = "",
    val label: String? = null,
    val placeholder: String? = null,
    val value: String? = null,
    val multiline: Boolean? = null,
    /** Soft keyboard hint: "text" (default), "number", "decimal", "email", "phone", "url". */
    val keyboard: String? = null,
    /** Submit-blocking required flag — buttons refuse to fire while this input is blank. */
    val required: Boolean? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("date_input")
data class DateInputNode(
    override val id: String = "",
    val label: String? = null,
    /** ISO date, e.g. "2026-06-15". Opens a Material date picker. */
    val value: String? = null,
    /** Submit-blocking required flag — buttons refuse to fire while this input is blank. */
    val required: Boolean? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("time_input")
data class TimeInputNode(
    override val id: String = "",
    val label: String? = null,
    /** 24h "HH:mm", e.g. "14:30". Opens a Material time picker. */
    val value: String? = null,
    /** Submit-blocking required flag — buttons refuse to fire while this input is blank. */
    val required: Boolean? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("checkbox")
data class CheckboxNode(
    override val id: String = "",
    val label: String = "",
    val checked: Boolean? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("select")
data class SelectNode(
    override val id: String = "",
    val label: String? = null,
    @Contextual val options: ImmutableList<String> = persistentListOf(),
    val selected: String? = null,
    /** Hint shown while nothing is selected. */
    val placeholder: String? = null,
    /** Submit-blocking required flag — buttons refuse to fire while nothing is selected. */
    val required: Boolean? = null,
) : DenebUiNode

// --- Interactive nodes (additional) ---

@Immutable
@Serializable
@SerialName("switch")
data class SwitchNode(
    override val id: String = "",
    val label: String = "",
    val checked: Boolean? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("slider")
data class SliderNode(
    override val id: String = "",
    val label: String? = null,
    val value: Float? = null,
    val min: Float? = null,
    val max: Float? = null,
    val step: Float? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("radio_group")
data class RadioGroupNode(
    override val id: String = "",
    val label: String? = null,
    @Contextual val options: ImmutableList<String> = persistentListOf(),
    val selected: String? = null,
    /** Submit-blocking required flag — buttons refuse to fire while nothing is selected. */
    val required: Boolean? = null,
) : DenebUiNode

// --- Feedback nodes ---

@Immutable
@Serializable
@SerialName("progress")
data class ProgressNode(
    override val id: String? = null,
    val value: Float? = null,
    val label: String? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("alert")
data class AlertNode(
    override val id: String? = null,
    val message: String = "",
    val title: String? = null,
    val severity: AlertSeverity? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("countdown")
data class CountdownNode(
    override val id: String? = null,
    val seconds: Int = 0,
    val label: String? = null,
    val action: UiAction? = null,
) : DenebUiNode

// --- Selection nodes ---

@Immutable
@Serializable
@SerialName("chip_group")
data class ChipGroupNode(
    override val id: String = "",
    @Contextual val chips: ImmutableList<ChipItem> = persistentListOf(),
    /** "single" (default), "multi", or "none" for display-only tags. */
    val selection: String = "single",
    /** Submit-blocking required flag — buttons refuse to fire while nothing is selected. */
    val required: Boolean? = null,
) : DenebUiNode

@Immutable
@Serializable
data class ChipItem(
    val label: String = "",
    val value: String = "",
)

// --- Content nodes (additional) ---

@Immutable
@Serializable
@SerialName("icon")
data class IconNode(
    override val id: String? = null,
    val name: String = "",
    val size: Int? = null,
    val color: String? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("code")
data class CodeNode(
    override val id: String? = null,
    val code: String = "",
    val language: String? = null,
) : DenebUiNode

// --- Layout nodes (additional) ---

@Immutable
@Serializable
@SerialName("box")
data class BoxNode(
    override val id: String? = null,
    @Contextual val children: ImmutableList<DenebUiNode> = persistentListOf(),
    val contentAlignment: String? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("tabs")
data class TabsNode(
    override val id: String? = null,
    @Contextual val tabs: ImmutableList<TabItem> = persistentListOf(),
    val selectedIndex: Int? = null,
) : DenebUiNode

@Immutable
@Serializable
data class TabItem(
    val label: String = "",
    @Contextual val children: ImmutableList<DenebUiNode> = persistentListOf(),
)

@Immutable
@Serializable
@SerialName("accordion")
data class AccordionNode(
    override val id: String? = null,
    val title: String = "",
    @Contextual val children: ImmutableList<DenebUiNode> = persistentListOf(),
    val expanded: Boolean? = null,
) : DenebUiNode

// --- Display nodes ---

@Immutable
@Serializable
@SerialName("quote")
data class QuoteNode(
    override val id: String? = null,
    val text: String = "",
    val source: String? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("badge")
data class BadgeNode(
    override val id: String? = null,
    val value: String = "",
    val color: String? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("stat")
data class StatNode(
    override val id: String? = null,
    val value: String = "",
    val label: String = "",
    val description: String? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("avatar")
data class AvatarNode(
    override val id: String? = null,
    val name: String? = null,
    val imageUrl: String? = null,
    val size: Int? = null,
) : DenebUiNode

// --- Data display nodes ---

@Immutable
@Serializable
@SerialName("list")
data class ListNode(
    override val id: String? = null,
    @Contextual val items: ImmutableList<DenebUiNode> = persistentListOf(),
    val ordered: Boolean? = null,
) : DenebUiNode

@Immutable
@Serializable
@SerialName("table")
data class TableNode(
    override val id: String? = null,
    @Contextual val headers: ImmutableList<String> = persistentListOf(),
    @Contextual val rows: ImmutableList<@Contextual ImmutableList<String>> = persistentListOf(),
) : DenebUiNode

@Immutable
@Serializable
@SerialName("chart")
data class ChartNode(
    override val id: String? = null,
    /** "bar" (default) or "line". */
    val chartType: String = "bar",
    @Contextual val labels: ImmutableList<String> = persistentListOf(),
    @Contextual val values: ImmutableList<Float> = persistentListOf(),
    val label: String? = null,
) : DenebUiNode

// --- Enums ---

@Serializable
enum class TextNodeStyle {
    @SerialName("headline")
    HEADLINE,

    @SerialName("title")
    TITLE,

    @SerialName("body")
    BODY,

    @SerialName("caption")
    CAPTION,
}

@Serializable
enum class ButtonVariant {
    @SerialName("filled")
    FILLED,

    @SerialName("outlined")
    OUTLINED,

    @SerialName("text")
    TEXT,

    @SerialName("tonal")
    TONAL,
}

@Serializable
enum class AlertSeverity {
    @SerialName("info")
    INFO,

    @SerialName("success")
    SUCCESS,

    @SerialName("warning")
    WARNING,

    @SerialName("error")
    ERROR,
}
