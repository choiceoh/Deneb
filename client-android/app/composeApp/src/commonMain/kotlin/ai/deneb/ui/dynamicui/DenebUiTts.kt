package ai.deneb.ui.dynamicui

import ai.deneb.ui.markdown.parseMarkdown
import ai.deneb.ui.markdown.toSpeakableText

/**
 * TTS-friendly text for a message that may contain deneb-ui fences. Routed through the unified
 * markdown parser: formatting is stripped, code blocks dropped, and deneb-ui blocks walked for
 * their human-readable labels (titles, alerts, chips, table cells) so the user hears what the
 * form says, not the JSON behind it.
 */
fun String.toSpeakableText(): String = parseMarkdown(this).toSpeakableText()

internal fun DenebUiNode.collectSpeakableText(): String {
    val parts = mutableListOf<String>()
    walk(parts)
    return parts.asSequence().filter { it.isNotBlank() }.joinToString(". ")
}

private fun DenebUiNode.walk(parts: MutableList<String>) {
    when (this) {
        is TextNode -> parts += value

        is ButtonNode -> parts += label

        is TextInputNode -> (
            value?.takeIf { it.isNotBlank() }
                ?: label?.takeIf { it.isNotBlank() }
                ?: placeholder?.takeIf { it.isNotBlank() }
            )?.let { parts += it }

        is DateInputNode -> (value?.takeIf { it.isNotBlank() } ?: label?.takeIf { it.isNotBlank() })?.let { parts += it }

        is TimeInputNode -> (value?.takeIf { it.isNotBlank() } ?: label?.takeIf { it.isNotBlank() })?.let { parts += it }

        is CheckboxNode -> parts += label

        is SwitchNode -> parts += label

        is SliderNode -> label?.let { parts += it }

        is SelectNode -> {
            label?.let { parts += it }
            selected?.let { parts += it }
        }

        is RadioGroupNode -> {
            label?.let { parts += it }
            selected?.let { parts += it }
        }

        is ChipGroupNode -> chips.forEach { parts += it.label }

        is ProgressNode -> label?.let { parts += it }

        is CountdownNode -> label?.let { parts += it }

        is AlertNode -> {
            title?.takeIf { it.isNotBlank() }?.let { parts += it }
            parts += message
        }

        is QuoteNode -> {
            parts += text
            source?.let { parts += it }
        }

        is BadgeNode -> parts += value

        is StatNode -> {
            parts += label
            parts += value
            description?.let { parts += it }
        }

        is AvatarNode -> name?.let { parts += it }

        is ImageNode -> alt?.let { parts += it }

        // Re-enter the markdown pipeline so formatting is stripped the same way
        // a plain assistant message is (code blocks dropped, markers removed).
        is MarkdownNode -> parts += parseMarkdown(value).toSpeakableText()

        is AccordionNode -> {
            parts += title
            children.forEach { it.walk(parts) }
        }

        is ColumnNode -> children.forEach { it.walk(parts) }

        is RowNode -> children.forEach { it.walk(parts) }

        is CardNode -> children.forEach { it.walk(parts) }

        is BoxNode -> children.forEach { it.walk(parts) }

        is ListNode -> items.forEach { it.walk(parts) }

        is TabsNode -> tabs.forEach { tab ->
            parts += tab.label
            tab.children.forEach { it.walk(parts) }
        }

        is TableNode -> {
            headers.filter { it.isNotBlank() }.takeIf { it.isNotEmpty() }?.let { parts += it.joinToString(", ") }
            rows.forEach { row ->
                row.filter { it.isNotBlank() }.takeIf { it.isNotEmpty() }?.let { parts += it.joinToString(", ") }
            }
        }

        is CodeNode -> Unit

        is IconNode -> Unit

        is DividerNode -> Unit

        is ChartNode -> {
            label?.let { parts += it }
            labels.filter { it.isNotBlank() }.takeIf { it.isNotEmpty() }?.let { parts += it.joinToString(", ") }
        }
    }
}
