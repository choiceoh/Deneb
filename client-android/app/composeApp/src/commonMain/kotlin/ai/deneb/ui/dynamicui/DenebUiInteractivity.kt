package ai.deneb.ui.dynamicui

/**
 * True when the tree contains any node a user could act on (buttons, inputs,
 * selections, or an auto-firing countdown). Progressive streaming render uses
 * this to decide between painting a partial card live (display-only trees —
 * letters, reports) and holding a placeholder (a half-built form must not
 * accept taps mid-stream).
 */
fun DenebUiNode.hasInteractiveNode(): Boolean = when (this) {
    is ButtonNode, is TextInputNode, is DateInputNode, is TimeInputNode,
    is CheckboxNode, is SelectNode, is SwitchNode, is SliderNode,
    is RadioGroupNode, is ChipGroupNode, is CountdownNode,
    -> true

    is ColumnNode -> children.any { it.hasInteractiveNode() }

    is RowNode -> children.any { it.hasInteractiveNode() }

    is CardNode -> children.any { it.hasInteractiveNode() }

    is BoxNode -> children.any { it.hasInteractiveNode() }

    is AccordionNode -> children.any { it.hasInteractiveNode() }

    is ListNode -> items.any { it.hasInteractiveNode() }

    is TabsNode -> tabs.any { tab -> tab.children.any { it.hasInteractiveNode() } }

    else -> false
}
