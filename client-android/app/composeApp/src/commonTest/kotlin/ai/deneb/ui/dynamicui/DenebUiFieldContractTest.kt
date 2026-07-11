package ai.deneb.ui.dynamicui

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs

/**
 * Field-level wiring contracts for every dynamic UI node.
 *
 * LLM-produced UI is intentionally tolerant: booleans and numbers may arrive as
 * strings, collections may use shorthand shapes, and absent/null values must land
 * on safe constructor defaults. Each field is tested independently so adding a
 * node or rewiring one builder cannot silently drop an otherwise valid property.
 */
class DenebUiFieldContractTest {
    private fun parse(json: String): DenebUiNode {
        val result = DenebUiParser.parseUiBlockBody(json)
        return assertIs<DenebUiParser.UiBlockResult.Ui>(result).node
    }

    @Test
    fun columnNodeWiresNonDefaultId() {
        val node = assertIs<ColumnNode>(
            parse("""{"type":"column","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun columnNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<ColumnNode>(
            parse("""{"type":"column","id":null}"""),
        )

        assertEquals(ColumnNode().id, node.id)
    }

    @Test
    fun columnNodeWiresNonDefaultChildren() {
        val node = assertIs<ColumnNode>(
            parse("""{"type":"column","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
        )

        assertEquals(1, node.children.size)
        val child = assertIs<TextNode>(node.children.single())
        assertEquals("child-id", child.id)
        assertEquals("child-value", child.value)
    }

    @Test
    fun columnNodeMapsExplicitNullChildrenToSafeDefault() {
        val node = assertIs<ColumnNode>(
            parse("""{"type":"column","children":null}"""),
        )

        assertEquals(ColumnNode().children, node.children)
    }

    @Test
    fun rowNodeWiresNonDefaultId() {
        val node = assertIs<RowNode>(
            parse("""{"type":"row","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun rowNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<RowNode>(
            parse("""{"type":"row","id":null}"""),
        )

        assertEquals(RowNode().id, node.id)
    }

    @Test
    fun rowNodeWiresNonDefaultChildren() {
        val node = assertIs<RowNode>(
            parse("""{"type":"row","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
        )

        assertEquals(1, node.children.size)
        val child = assertIs<TextNode>(node.children.single())
        assertEquals("child-id", child.id)
        assertEquals("child-value", child.value)
    }

    @Test
    fun rowNodeMapsExplicitNullChildrenToSafeDefault() {
        val node = assertIs<RowNode>(
            parse("""{"type":"row","children":null}"""),
        )

        assertEquals(RowNode().children, node.children)
    }

    @Test
    fun cardNodeWiresNonDefaultId() {
        val node = assertIs<CardNode>(
            parse("""{"type":"card","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun cardNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<CardNode>(
            parse("""{"type":"card","id":null}"""),
        )

        assertEquals(CardNode().id, node.id)
    }

    @Test
    fun cardNodeWiresNonDefaultChildren() {
        val node = assertIs<CardNode>(
            parse("""{"type":"card","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
        )

        assertEquals(1, node.children.size)
        val child = assertIs<TextNode>(node.children.single())
        assertEquals("child-id", child.id)
        assertEquals("child-value", child.value)
    }

    @Test
    fun cardNodeMapsExplicitNullChildrenToSafeDefault() {
        val node = assertIs<CardNode>(
            parse("""{"type":"card","children":null}"""),
        )

        assertEquals(CardNode().children, node.children)
    }

    @Test
    fun dividerNodeWiresNonDefaultId() {
        val node = assertIs<DividerNode>(
            parse("""{"type":"divider","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun dividerNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<DividerNode>(
            parse("""{"type":"divider","id":null}"""),
        )

        assertEquals(DividerNode().id, node.id)
    }

    @Test
    fun textNodeWiresNonDefaultId() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun textNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","id":null}"""),
        )

        assertEquals(TextNode().id, node.id)
    }

    @Test
    fun textNodeWiresNonDefaultValue() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","value":"value-한글 /?#"}"""),
        )

        assertEquals("value-한글 /?#", node.value)
    }

    @Test
    fun textNodeMapsExplicitNullValueToSafeDefault() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","value":null}"""),
        )

        assertEquals(TextNode().value, node.value)
    }

    @Test
    fun textNodeWiresNonDefaultStyle() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","style":"headline"}"""),
        )

        assertEquals(TextNodeStyle.HEADLINE, node.style)
    }

    @Test
    fun textNodeMapsExplicitNullStyleToSafeDefault() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","style":null}"""),
        )

        assertEquals(TextNode().style, node.style)
    }

    @Test
    fun textNodeWiresNonDefaultBold() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","bold":"yes"}"""),
        )

        assertEquals(true, node.bold)
    }

    @Test
    fun textNodeMapsExplicitNullBoldToSafeDefault() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","bold":null}"""),
        )

        assertEquals(TextNode().bold, node.bold)
    }

    @Test
    fun textNodeWiresNonDefaultItalic() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","italic":"yes"}"""),
        )

        assertEquals(true, node.italic)
    }

    @Test
    fun textNodeMapsExplicitNullItalicToSafeDefault() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","italic":null}"""),
        )

        assertEquals(TextNode().italic, node.italic)
    }

    @Test
    fun textNodeWiresNonDefaultColor() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","color":"color-한글 /?#"}"""),
        )

        assertEquals("color-한글 /?#", node.color)
    }

    @Test
    fun textNodeMapsExplicitNullColorToSafeDefault() {
        val node = assertIs<TextNode>(
            parse("""{"type":"text","color":null}"""),
        )

        assertEquals(TextNode().color, node.color)
    }

    @Test
    fun markdownNodeWiresNonDefaultId() {
        val node = assertIs<MarkdownNode>(
            parse("""{"type":"markdown","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun markdownNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<MarkdownNode>(
            parse("""{"type":"markdown","id":null}"""),
        )

        assertEquals(MarkdownNode().id, node.id)
    }

    @Test
    fun markdownNodeWiresNonDefaultValue() {
        val node = assertIs<MarkdownNode>(
            parse("""{"type":"markdown","value":"value-한글 /?#"}"""),
        )

        assertEquals("value-한글 /?#", node.value)
    }

    @Test
    fun markdownNodeMapsExplicitNullValueToSafeDefault() {
        val node = assertIs<MarkdownNode>(
            parse("""{"type":"markdown","value":null}"""),
        )

        assertEquals(MarkdownNode().value, node.value)
    }

    @Test
    fun imageNodeWiresNonDefaultId() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun imageNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","id":null}"""),
        )

        assertEquals(ImageNode().id, node.id)
    }

    @Test
    fun imageNodeWiresNonDefaultUrl() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","url":"url-한글 /?#"}"""),
        )

        assertEquals("url-한글 /?#", node.url)
    }

    @Test
    fun imageNodeMapsExplicitNullUrlToSafeDefault() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","url":null}"""),
        )

        assertEquals(ImageNode().url, node.url)
    }

    @Test
    fun imageNodeWiresNonDefaultAlt() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","alt":"alt-한글 /?#"}"""),
        )

        assertEquals("alt-한글 /?#", node.alt)
    }

    @Test
    fun imageNodeMapsExplicitNullAltToSafeDefault() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","alt":null}"""),
        )

        assertEquals(ImageNode().alt, node.alt)
    }

    @Test
    fun imageNodeWiresNonDefaultHeight() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","height":"17"}"""),
        )

        assertEquals(17, node.height)
    }

    @Test
    fun imageNodeMapsExplicitNullHeightToSafeDefault() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","height":null}"""),
        )

        assertEquals(ImageNode().height, node.height)
    }

    @Test
    fun imageNodeWiresNonDefaultAspectRatio() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","aspectRatio":"12.5"}"""),
        )

        assertEquals(12.5f, node.aspectRatio)
    }

    @Test
    fun imageNodeMapsExplicitNullAspectRatioToSafeDefault() {
        val node = assertIs<ImageNode>(
            parse("""{"type":"image","aspectRatio":null}"""),
        )

        assertEquals(ImageNode().aspectRatio, node.aspectRatio)
    }

    @Test
    fun buttonNodeWiresNonDefaultId() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun buttonNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","id":null}"""),
        )

        assertEquals(ButtonNode().id, node.id)
    }

    @Test
    fun buttonNodeWiresNonDefaultLabel() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun buttonNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","label":null}"""),
        )

        assertEquals(ButtonNode().label, node.label)
    }

    @Test
    fun buttonNodeWiresNonDefaultAction() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","action":{"type":"callback","event":"submit","data":{"enabled":true,"count":7},"collectFrom":["input-a","input-b"]}}"""),
        )

        val action = assertIs<CallbackAction>(node.action)
        assertEquals("submit", action.event)
        assertEquals(mapOf("enabled" to "true", "count" to "7"), action.dataAsStrings)
        assertEquals(listOf("input-a", "input-b"), action.collectFrom)
    }

    @Test
    fun buttonNodeMapsExplicitNullActionToSafeDefault() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","action":null}"""),
        )

        assertEquals(ButtonNode().action, node.action)
    }

    @Test
    fun buttonNodeWiresNonDefaultVariant() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","variant":"tonal"}"""),
        )

        assertEquals(ButtonVariant.TONAL, node.variant)
    }

    @Test
    fun buttonNodeMapsExplicitNullVariantToSafeDefault() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","variant":null}"""),
        )

        assertEquals(ButtonNode().variant, node.variant)
    }

    @Test
    fun buttonNodeWiresNonDefaultEnabled() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","enabled":"yes"}"""),
        )

        assertEquals(true, node.enabled)
    }

    @Test
    fun buttonNodeMapsExplicitNullEnabledToSafeDefault() {
        val node = assertIs<ButtonNode>(
            parse("""{"type":"button","enabled":null}"""),
        )

        assertEquals(ButtonNode().enabled, node.enabled)
    }

    @Test
    fun textInputNodeWiresNonDefaultId() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun textInputNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","id":null}"""),
        )

        assertEquals(TextInputNode().id, node.id)
    }

    @Test
    fun textInputNodeWiresNonDefaultLabel() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun textInputNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","label":null}"""),
        )

        assertEquals(TextInputNode().label, node.label)
    }

    @Test
    fun textInputNodeWiresNonDefaultPlaceholder() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","placeholder":"placeholder-한글 /?#"}"""),
        )

        assertEquals("placeholder-한글 /?#", node.placeholder)
    }

    @Test
    fun textInputNodeMapsExplicitNullPlaceholderToSafeDefault() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","placeholder":null}"""),
        )

        assertEquals(TextInputNode().placeholder, node.placeholder)
    }

    @Test
    fun textInputNodeWiresNonDefaultValue() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","value":"value-한글 /?#"}"""),
        )

        assertEquals("value-한글 /?#", node.value)
    }

    @Test
    fun textInputNodeMapsExplicitNullValueToSafeDefault() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","value":null}"""),
        )

        assertEquals(TextInputNode().value, node.value)
    }

    @Test
    fun textInputNodeWiresNonDefaultMultiline() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","multiline":"yes"}"""),
        )

        assertEquals(true, node.multiline)
    }

    @Test
    fun textInputNodeMapsExplicitNullMultilineToSafeDefault() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","multiline":null}"""),
        )

        assertEquals(TextInputNode().multiline, node.multiline)
    }

    @Test
    fun textInputNodeWiresNonDefaultKeyboard() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","keyboard":"keyboard-한글 /?#"}"""),
        )

        assertEquals("keyboard-한글 /?#", node.keyboard)
    }

    @Test
    fun textInputNodeMapsExplicitNullKeyboardToSafeDefault() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","keyboard":null}"""),
        )

        assertEquals(TextInputNode().keyboard, node.keyboard)
    }

    @Test
    fun textInputNodeWiresNonDefaultRequired() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","required":"yes"}"""),
        )

        assertEquals(true, node.required)
    }

    @Test
    fun textInputNodeMapsExplicitNullRequiredToSafeDefault() {
        val node = assertIs<TextInputNode>(
            parse("""{"type":"text_input","required":null}"""),
        )

        assertEquals(TextInputNode().required, node.required)
    }

    @Test
    fun dateInputNodeWiresNonDefaultId() {
        val node = assertIs<DateInputNode>(
            parse("""{"type":"date_input","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun dateInputNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<DateInputNode>(
            parse("""{"type":"date_input","id":null}"""),
        )

        assertEquals(DateInputNode().id, node.id)
    }

    @Test
    fun dateInputNodeWiresNonDefaultLabel() {
        val node = assertIs<DateInputNode>(
            parse("""{"type":"date_input","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun dateInputNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<DateInputNode>(
            parse("""{"type":"date_input","label":null}"""),
        )

        assertEquals(DateInputNode().label, node.label)
    }

    @Test
    fun dateInputNodeWiresNonDefaultValue() {
        val node = assertIs<DateInputNode>(
            parse("""{"type":"date_input","value":"value-한글 /?#"}"""),
        )

        assertEquals("value-한글 /?#", node.value)
    }

    @Test
    fun dateInputNodeMapsExplicitNullValueToSafeDefault() {
        val node = assertIs<DateInputNode>(
            parse("""{"type":"date_input","value":null}"""),
        )

        assertEquals(DateInputNode().value, node.value)
    }

    @Test
    fun dateInputNodeWiresNonDefaultRequired() {
        val node = assertIs<DateInputNode>(
            parse("""{"type":"date_input","required":"yes"}"""),
        )

        assertEquals(true, node.required)
    }

    @Test
    fun dateInputNodeMapsExplicitNullRequiredToSafeDefault() {
        val node = assertIs<DateInputNode>(
            parse("""{"type":"date_input","required":null}"""),
        )

        assertEquals(DateInputNode().required, node.required)
    }

    @Test
    fun timeInputNodeWiresNonDefaultId() {
        val node = assertIs<TimeInputNode>(
            parse("""{"type":"time_input","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun timeInputNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<TimeInputNode>(
            parse("""{"type":"time_input","id":null}"""),
        )

        assertEquals(TimeInputNode().id, node.id)
    }

    @Test
    fun timeInputNodeWiresNonDefaultLabel() {
        val node = assertIs<TimeInputNode>(
            parse("""{"type":"time_input","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun timeInputNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<TimeInputNode>(
            parse("""{"type":"time_input","label":null}"""),
        )

        assertEquals(TimeInputNode().label, node.label)
    }

    @Test
    fun timeInputNodeWiresNonDefaultValue() {
        val node = assertIs<TimeInputNode>(
            parse("""{"type":"time_input","value":"value-한글 /?#"}"""),
        )

        assertEquals("value-한글 /?#", node.value)
    }

    @Test
    fun timeInputNodeMapsExplicitNullValueToSafeDefault() {
        val node = assertIs<TimeInputNode>(
            parse("""{"type":"time_input","value":null}"""),
        )

        assertEquals(TimeInputNode().value, node.value)
    }

    @Test
    fun timeInputNodeWiresNonDefaultRequired() {
        val node = assertIs<TimeInputNode>(
            parse("""{"type":"time_input","required":"yes"}"""),
        )

        assertEquals(true, node.required)
    }

    @Test
    fun timeInputNodeMapsExplicitNullRequiredToSafeDefault() {
        val node = assertIs<TimeInputNode>(
            parse("""{"type":"time_input","required":null}"""),
        )

        assertEquals(TimeInputNode().required, node.required)
    }

    @Test
    fun checkboxNodeWiresNonDefaultId() {
        val node = assertIs<CheckboxNode>(
            parse("""{"type":"checkbox","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun checkboxNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<CheckboxNode>(
            parse("""{"type":"checkbox","id":null}"""),
        )

        assertEquals(CheckboxNode().id, node.id)
    }

    @Test
    fun checkboxNodeWiresNonDefaultLabel() {
        val node = assertIs<CheckboxNode>(
            parse("""{"type":"checkbox","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun checkboxNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<CheckboxNode>(
            parse("""{"type":"checkbox","label":null}"""),
        )

        assertEquals(CheckboxNode().label, node.label)
    }

    @Test
    fun checkboxNodeWiresNonDefaultChecked() {
        val node = assertIs<CheckboxNode>(
            parse("""{"type":"checkbox","checked":"yes"}"""),
        )

        assertEquals(true, node.checked)
    }

    @Test
    fun checkboxNodeMapsExplicitNullCheckedToSafeDefault() {
        val node = assertIs<CheckboxNode>(
            parse("""{"type":"checkbox","checked":null}"""),
        )

        assertEquals(CheckboxNode().checked, node.checked)
    }

    @Test
    fun selectNodeWiresNonDefaultId() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun selectNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","id":null}"""),
        )

        assertEquals(SelectNode().id, node.id)
    }

    @Test
    fun selectNodeWiresNonDefaultLabel() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun selectNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","label":null}"""),
        )

        assertEquals(SelectNode().label, node.label)
    }

    @Test
    fun selectNodeWiresNonDefaultOptions() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","options":["one","둘"]}"""),
        )

        assertEquals(listOf("one", "둘"), node.options.toList())
    }

    @Test
    fun selectNodeMapsExplicitNullOptionsToSafeDefault() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","options":null}"""),
        )

        assertEquals(SelectNode().options, node.options)
    }

    @Test
    fun selectNodeWiresNonDefaultSelected() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","selected":"selected-한글 /?#"}"""),
        )

        assertEquals("selected-한글 /?#", node.selected)
    }

    @Test
    fun selectNodeMapsExplicitNullSelectedToSafeDefault() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","selected":null}"""),
        )

        assertEquals(SelectNode().selected, node.selected)
    }

    @Test
    fun selectNodeWiresNonDefaultPlaceholder() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","placeholder":"placeholder-한글 /?#"}"""),
        )

        assertEquals("placeholder-한글 /?#", node.placeholder)
    }

    @Test
    fun selectNodeMapsExplicitNullPlaceholderToSafeDefault() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","placeholder":null}"""),
        )

        assertEquals(SelectNode().placeholder, node.placeholder)
    }

    @Test
    fun selectNodeWiresNonDefaultRequired() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","required":"yes"}"""),
        )

        assertEquals(true, node.required)
    }

    @Test
    fun selectNodeMapsExplicitNullRequiredToSafeDefault() {
        val node = assertIs<SelectNode>(
            parse("""{"type":"select","required":null}"""),
        )

        assertEquals(SelectNode().required, node.required)
    }

    @Test
    fun switchNodeWiresNonDefaultId() {
        val node = assertIs<SwitchNode>(
            parse("""{"type":"switch","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun switchNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<SwitchNode>(
            parse("""{"type":"switch","id":null}"""),
        )

        assertEquals(SwitchNode().id, node.id)
    }

    @Test
    fun switchNodeWiresNonDefaultLabel() {
        val node = assertIs<SwitchNode>(
            parse("""{"type":"switch","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun switchNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<SwitchNode>(
            parse("""{"type":"switch","label":null}"""),
        )

        assertEquals(SwitchNode().label, node.label)
    }

    @Test
    fun switchNodeWiresNonDefaultChecked() {
        val node = assertIs<SwitchNode>(
            parse("""{"type":"switch","checked":"yes"}"""),
        )

        assertEquals(true, node.checked)
    }

    @Test
    fun switchNodeMapsExplicitNullCheckedToSafeDefault() {
        val node = assertIs<SwitchNode>(
            parse("""{"type":"switch","checked":null}"""),
        )

        assertEquals(SwitchNode().checked, node.checked)
    }

    @Test
    fun sliderNodeWiresNonDefaultId() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun sliderNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","id":null}"""),
        )

        assertEquals(SliderNode().id, node.id)
    }

    @Test
    fun sliderNodeWiresNonDefaultLabel() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun sliderNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","label":null}"""),
        )

        assertEquals(SliderNode().label, node.label)
    }

    @Test
    fun sliderNodeWiresNonDefaultValue() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","value":"12.5"}"""),
        )

        assertEquals(12.5f, node.value)
    }

    @Test
    fun sliderNodeMapsExplicitNullValueToSafeDefault() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","value":null}"""),
        )

        assertEquals(SliderNode().value, node.value)
    }

    @Test
    fun sliderNodeWiresNonDefaultMin() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","min":"12.5"}"""),
        )

        assertEquals(12.5f, node.min)
    }

    @Test
    fun sliderNodeMapsExplicitNullMinToSafeDefault() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","min":null}"""),
        )

        assertEquals(SliderNode().min, node.min)
    }

    @Test
    fun sliderNodeWiresNonDefaultMax() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","max":"12.5"}"""),
        )

        assertEquals(12.5f, node.max)
    }

    @Test
    fun sliderNodeMapsExplicitNullMaxToSafeDefault() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","max":null}"""),
        )

        assertEquals(SliderNode().max, node.max)
    }

    @Test
    fun sliderNodeWiresNonDefaultStep() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","step":"12.5"}"""),
        )

        assertEquals(12.5f, node.step)
    }

    @Test
    fun sliderNodeMapsExplicitNullStepToSafeDefault() {
        val node = assertIs<SliderNode>(
            parse("""{"type":"slider","step":null}"""),
        )

        assertEquals(SliderNode().step, node.step)
    }

    @Test
    fun radioGroupNodeWiresNonDefaultId() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun radioGroupNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","id":null}"""),
        )

        assertEquals(RadioGroupNode().id, node.id)
    }

    @Test
    fun radioGroupNodeWiresNonDefaultLabel() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun radioGroupNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","label":null}"""),
        )

        assertEquals(RadioGroupNode().label, node.label)
    }

    @Test
    fun radioGroupNodeWiresNonDefaultOptions() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","options":["one","둘"]}"""),
        )

        assertEquals(listOf("one", "둘"), node.options.toList())
    }

    @Test
    fun radioGroupNodeMapsExplicitNullOptionsToSafeDefault() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","options":null}"""),
        )

        assertEquals(RadioGroupNode().options, node.options)
    }

    @Test
    fun radioGroupNodeWiresNonDefaultSelected() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","selected":"selected-한글 /?#"}"""),
        )

        assertEquals("selected-한글 /?#", node.selected)
    }

    @Test
    fun radioGroupNodeMapsExplicitNullSelectedToSafeDefault() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","selected":null}"""),
        )

        assertEquals(RadioGroupNode().selected, node.selected)
    }

    @Test
    fun radioGroupNodeWiresNonDefaultRequired() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","required":"yes"}"""),
        )

        assertEquals(true, node.required)
    }

    @Test
    fun radioGroupNodeMapsExplicitNullRequiredToSafeDefault() {
        val node = assertIs<RadioGroupNode>(
            parse("""{"type":"radio_group","required":null}"""),
        )

        assertEquals(RadioGroupNode().required, node.required)
    }

    @Test
    fun progressNodeWiresNonDefaultId() {
        val node = assertIs<ProgressNode>(
            parse("""{"type":"progress","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun progressNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<ProgressNode>(
            parse("""{"type":"progress","id":null}"""),
        )

        assertEquals(ProgressNode().id, node.id)
    }

    @Test
    fun progressNodeWiresNonDefaultValue() {
        val node = assertIs<ProgressNode>(
            parse("""{"type":"progress","value":"12.5"}"""),
        )

        assertEquals(12.5f, node.value)
    }

    @Test
    fun progressNodeMapsExplicitNullValueToSafeDefault() {
        val node = assertIs<ProgressNode>(
            parse("""{"type":"progress","value":null}"""),
        )

        assertEquals(ProgressNode().value, node.value)
    }

    @Test
    fun progressNodeWiresNonDefaultLabel() {
        val node = assertIs<ProgressNode>(
            parse("""{"type":"progress","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun progressNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<ProgressNode>(
            parse("""{"type":"progress","label":null}"""),
        )

        assertEquals(ProgressNode().label, node.label)
    }

    @Test
    fun alertNodeWiresNonDefaultId() {
        val node = assertIs<AlertNode>(
            parse("""{"type":"alert","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun alertNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<AlertNode>(
            parse("""{"type":"alert","id":null}"""),
        )

        assertEquals(AlertNode().id, node.id)
    }

    @Test
    fun alertNodeWiresNonDefaultMessage() {
        val node = assertIs<AlertNode>(
            parse("""{"type":"alert","message":"message-한글 /?#"}"""),
        )

        assertEquals("message-한글 /?#", node.message)
    }

    @Test
    fun alertNodeMapsExplicitNullMessageToSafeDefault() {
        val node = assertIs<AlertNode>(
            parse("""{"type":"alert","message":null}"""),
        )

        assertEquals(AlertNode().message, node.message)
    }

    @Test
    fun alertNodeWiresNonDefaultTitle() {
        val node = assertIs<AlertNode>(
            parse("""{"type":"alert","title":"title-한글 /?#"}"""),
        )

        assertEquals("title-한글 /?#", node.title)
    }

    @Test
    fun alertNodeMapsExplicitNullTitleToSafeDefault() {
        val node = assertIs<AlertNode>(
            parse("""{"type":"alert","title":null}"""),
        )

        assertEquals(AlertNode().title, node.title)
    }

    @Test
    fun alertNodeWiresNonDefaultSeverity() {
        val node = assertIs<AlertNode>(
            parse("""{"type":"alert","severity":"warning"}"""),
        )

        assertEquals(AlertSeverity.WARNING, node.severity)
    }

    @Test
    fun alertNodeMapsExplicitNullSeverityToSafeDefault() {
        val node = assertIs<AlertNode>(
            parse("""{"type":"alert","severity":null}"""),
        )

        assertEquals(AlertNode().severity, node.severity)
    }

    @Test
    fun countdownNodeWiresNonDefaultId() {
        val node = assertIs<CountdownNode>(
            parse("""{"type":"countdown","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun countdownNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<CountdownNode>(
            parse("""{"type":"countdown","id":null}"""),
        )

        assertEquals(CountdownNode().id, node.id)
    }

    @Test
    fun countdownNodeWiresNonDefaultSeconds() {
        val node = assertIs<CountdownNode>(
            parse("""{"type":"countdown","seconds":"17"}"""),
        )

        assertEquals(17, node.seconds)
    }

    @Test
    fun countdownNodeMapsExplicitNullSecondsToSafeDefault() {
        val node = assertIs<CountdownNode>(
            parse("""{"type":"countdown","seconds":null}"""),
        )

        assertEquals(CountdownNode().seconds, node.seconds)
    }

    @Test
    fun countdownNodeWiresNonDefaultLabel() {
        val node = assertIs<CountdownNode>(
            parse("""{"type":"countdown","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun countdownNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<CountdownNode>(
            parse("""{"type":"countdown","label":null}"""),
        )

        assertEquals(CountdownNode().label, node.label)
    }

    @Test
    fun countdownNodeWiresNonDefaultAction() {
        val node = assertIs<CountdownNode>(
            parse("""{"type":"countdown","action":{"type":"callback","event":"submit","data":{"enabled":true,"count":7},"collectFrom":["input-a","input-b"]}}"""),
        )

        val action = assertIs<CallbackAction>(node.action)
        assertEquals("submit", action.event)
        assertEquals(mapOf("enabled" to "true", "count" to "7"), action.dataAsStrings)
        assertEquals(listOf("input-a", "input-b"), action.collectFrom)
    }

    @Test
    fun countdownNodeMapsExplicitNullActionToSafeDefault() {
        val node = assertIs<CountdownNode>(
            parse("""{"type":"countdown","action":null}"""),
        )

        assertEquals(CountdownNode().action, node.action)
    }

    @Test
    fun chipGroupNodeWiresNonDefaultId() {
        val node = assertIs<ChipGroupNode>(
            parse("""{"type":"chip_group","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun chipGroupNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<ChipGroupNode>(
            parse("""{"type":"chip_group","id":null}"""),
        )

        assertEquals(ChipGroupNode().id, node.id)
    }

    @Test
    fun chipGroupNodeWiresNonDefaultChips() {
        val node = assertIs<ChipGroupNode>(
            parse("""{"type":"chip_group","chips":[{"label":"A","value":"a"},"B"]}"""),
        )

        assertEquals(listOf(ChipItem("A", "a"), ChipItem("B", "B")), node.chips.toList())
    }

    @Test
    fun chipGroupNodeMapsExplicitNullChipsToSafeDefault() {
        val node = assertIs<ChipGroupNode>(
            parse("""{"type":"chip_group","chips":null}"""),
        )

        assertEquals(ChipGroupNode().chips, node.chips)
    }

    @Test
    fun chipGroupNodeWiresNonDefaultSelection() {
        val node = assertIs<ChipGroupNode>(
            parse("""{"type":"chip_group","selection":"selection-한글 /?#"}"""),
        )

        assertEquals("selection-한글 /?#", node.selection)
    }

    @Test
    fun chipGroupNodeMapsExplicitNullSelectionToSafeDefault() {
        val node = assertIs<ChipGroupNode>(
            parse("""{"type":"chip_group","selection":null}"""),
        )

        assertEquals(ChipGroupNode().selection, node.selection)
    }

    @Test
    fun chipGroupNodeWiresNonDefaultRequired() {
        val node = assertIs<ChipGroupNode>(
            parse("""{"type":"chip_group","required":"yes"}"""),
        )

        assertEquals(true, node.required)
    }

    @Test
    fun chipGroupNodeMapsExplicitNullRequiredToSafeDefault() {
        val node = assertIs<ChipGroupNode>(
            parse("""{"type":"chip_group","required":null}"""),
        )

        assertEquals(ChipGroupNode().required, node.required)
    }

    @Test
    fun iconNodeWiresNonDefaultId() {
        val node = assertIs<IconNode>(
            parse("""{"type":"icon","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun iconNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<IconNode>(
            parse("""{"type":"icon","id":null}"""),
        )

        assertEquals(IconNode().id, node.id)
    }

    @Test
    fun iconNodeWiresNonDefaultName() {
        val node = assertIs<IconNode>(
            parse("""{"type":"icon","name":"name-한글 /?#"}"""),
        )

        assertEquals("name-한글 /?#", node.name)
    }

    @Test
    fun iconNodeMapsExplicitNullNameToSafeDefault() {
        val node = assertIs<IconNode>(
            parse("""{"type":"icon","name":null}"""),
        )

        assertEquals(IconNode().name, node.name)
    }

    @Test
    fun iconNodeWiresNonDefaultSize() {
        val node = assertIs<IconNode>(
            parse("""{"type":"icon","size":"17"}"""),
        )

        assertEquals(17, node.size)
    }

    @Test
    fun iconNodeMapsExplicitNullSizeToSafeDefault() {
        val node = assertIs<IconNode>(
            parse("""{"type":"icon","size":null}"""),
        )

        assertEquals(IconNode().size, node.size)
    }

    @Test
    fun iconNodeWiresNonDefaultColor() {
        val node = assertIs<IconNode>(
            parse("""{"type":"icon","color":"color-한글 /?#"}"""),
        )

        assertEquals("color-한글 /?#", node.color)
    }

    @Test
    fun iconNodeMapsExplicitNullColorToSafeDefault() {
        val node = assertIs<IconNode>(
            parse("""{"type":"icon","color":null}"""),
        )

        assertEquals(IconNode().color, node.color)
    }

    @Test
    fun codeNodeWiresNonDefaultId() {
        val node = assertIs<CodeNode>(
            parse("""{"type":"code","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun codeNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<CodeNode>(
            parse("""{"type":"code","id":null}"""),
        )

        assertEquals(CodeNode().id, node.id)
    }

    @Test
    fun codeNodeWiresNonDefaultCode() {
        val node = assertIs<CodeNode>(
            parse("""{"type":"code","code":"code-한글 /?#"}"""),
        )

        assertEquals("code-한글 /?#", node.code)
    }

    @Test
    fun codeNodeMapsExplicitNullCodeToSafeDefault() {
        val node = assertIs<CodeNode>(
            parse("""{"type":"code","code":null}"""),
        )

        assertEquals(CodeNode().code, node.code)
    }

    @Test
    fun codeNodeWiresNonDefaultLanguage() {
        val node = assertIs<CodeNode>(
            parse("""{"type":"code","language":"language-한글 /?#"}"""),
        )

        assertEquals("language-한글 /?#", node.language)
    }

    @Test
    fun codeNodeMapsExplicitNullLanguageToSafeDefault() {
        val node = assertIs<CodeNode>(
            parse("""{"type":"code","language":null}"""),
        )

        assertEquals(CodeNode().language, node.language)
    }

    @Test
    fun boxNodeWiresNonDefaultId() {
        val node = assertIs<BoxNode>(
            parse("""{"type":"box","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun boxNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<BoxNode>(
            parse("""{"type":"box","id":null}"""),
        )

        assertEquals(BoxNode().id, node.id)
    }

    @Test
    fun boxNodeWiresNonDefaultChildren() {
        val node = assertIs<BoxNode>(
            parse("""{"type":"box","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
        )

        assertEquals(1, node.children.size)
        val child = assertIs<TextNode>(node.children.single())
        assertEquals("child-id", child.id)
        assertEquals("child-value", child.value)
    }

    @Test
    fun boxNodeMapsExplicitNullChildrenToSafeDefault() {
        val node = assertIs<BoxNode>(
            parse("""{"type":"box","children":null}"""),
        )

        assertEquals(BoxNode().children, node.children)
    }

    @Test
    fun boxNodeWiresNonDefaultContentAlignment() {
        val node = assertIs<BoxNode>(
            parse("""{"type":"box","contentAlignment":"contentAlignment-한글 /?#"}"""),
        )

        assertEquals("contentAlignment-한글 /?#", node.contentAlignment)
    }

    @Test
    fun boxNodeMapsExplicitNullContentAlignmentToSafeDefault() {
        val node = assertIs<BoxNode>(
            parse("""{"type":"box","contentAlignment":null}"""),
        )

        assertEquals(BoxNode().contentAlignment, node.contentAlignment)
    }

    @Test
    fun tabsNodeWiresNonDefaultId() {
        val node = assertIs<TabsNode>(
            parse("""{"type":"tabs","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun tabsNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<TabsNode>(
            parse("""{"type":"tabs","id":null}"""),
        )

        assertEquals(TabsNode().id, node.id)
    }

    @Test
    fun tabsNodeWiresNonDefaultTabs() {
        val node = assertIs<TabsNode>(
            parse("""{"type":"tabs","tabs":[{"label":"Tab A","children":[{"type":"text","value":"tab child"}]}]}"""),
        )

        assertEquals(1, node.tabs.size)
        assertEquals("Tab A", node.tabs.single().label)
        assertEquals("tab child", assertIs<TextNode>(node.tabs.single().children.single()).value)
    }

    @Test
    fun tabsNodeMapsExplicitNullTabsToSafeDefault() {
        val node = assertIs<TabsNode>(
            parse("""{"type":"tabs","tabs":null}"""),
        )

        assertEquals(TabsNode().tabs, node.tabs)
    }

    @Test
    fun tabsNodeWiresNonDefaultSelectedIndex() {
        val node = assertIs<TabsNode>(
            parse("""{"type":"tabs","selectedIndex":"17"}"""),
        )

        assertEquals(17, node.selectedIndex)
    }

    @Test
    fun tabsNodeMapsExplicitNullSelectedIndexToSafeDefault() {
        val node = assertIs<TabsNode>(
            parse("""{"type":"tabs","selectedIndex":null}"""),
        )

        assertEquals(TabsNode().selectedIndex, node.selectedIndex)
    }

    @Test
    fun accordionNodeWiresNonDefaultId() {
        val node = assertIs<AccordionNode>(
            parse("""{"type":"accordion","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun accordionNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<AccordionNode>(
            parse("""{"type":"accordion","id":null}"""),
        )

        assertEquals(AccordionNode().id, node.id)
    }

    @Test
    fun accordionNodeWiresNonDefaultTitle() {
        val node = assertIs<AccordionNode>(
            parse("""{"type":"accordion","title":"title-한글 /?#"}"""),
        )

        assertEquals("title-한글 /?#", node.title)
    }

    @Test
    fun accordionNodeMapsExplicitNullTitleToSafeDefault() {
        val node = assertIs<AccordionNode>(
            parse("""{"type":"accordion","title":null}"""),
        )

        assertEquals(AccordionNode().title, node.title)
    }

    @Test
    fun accordionNodeWiresNonDefaultChildren() {
        val node = assertIs<AccordionNode>(
            parse("""{"type":"accordion","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
        )

        assertEquals(1, node.children.size)
        val child = assertIs<TextNode>(node.children.single())
        assertEquals("child-id", child.id)
        assertEquals("child-value", child.value)
    }

    @Test
    fun accordionNodeMapsExplicitNullChildrenToSafeDefault() {
        val node = assertIs<AccordionNode>(
            parse("""{"type":"accordion","children":null}"""),
        )

        assertEquals(AccordionNode().children, node.children)
    }

    @Test
    fun accordionNodeWiresNonDefaultExpanded() {
        val node = assertIs<AccordionNode>(
            parse("""{"type":"accordion","expanded":"yes"}"""),
        )

        assertEquals(true, node.expanded)
    }

    @Test
    fun accordionNodeMapsExplicitNullExpandedToSafeDefault() {
        val node = assertIs<AccordionNode>(
            parse("""{"type":"accordion","expanded":null}"""),
        )

        assertEquals(AccordionNode().expanded, node.expanded)
    }

    @Test
    fun quoteNodeWiresNonDefaultId() {
        val node = assertIs<QuoteNode>(
            parse("""{"type":"quote","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun quoteNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<QuoteNode>(
            parse("""{"type":"quote","id":null}"""),
        )

        assertEquals(QuoteNode().id, node.id)
    }

    @Test
    fun quoteNodeWiresNonDefaultText() {
        val node = assertIs<QuoteNode>(
            parse("""{"type":"quote","text":"text-한글 /?#"}"""),
        )

        assertEquals("text-한글 /?#", node.text)
    }

    @Test
    fun quoteNodeMapsExplicitNullTextToSafeDefault() {
        val node = assertIs<QuoteNode>(
            parse("""{"type":"quote","text":null}"""),
        )

        assertEquals(QuoteNode().text, node.text)
    }

    @Test
    fun quoteNodeWiresNonDefaultSource() {
        val node = assertIs<QuoteNode>(
            parse("""{"type":"quote","source":"source-한글 /?#"}"""),
        )

        assertEquals("source-한글 /?#", node.source)
    }

    @Test
    fun quoteNodeMapsExplicitNullSourceToSafeDefault() {
        val node = assertIs<QuoteNode>(
            parse("""{"type":"quote","source":null}"""),
        )

        assertEquals(QuoteNode().source, node.source)
    }

    @Test
    fun badgeNodeWiresNonDefaultId() {
        val node = assertIs<BadgeNode>(
            parse("""{"type":"badge","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun badgeNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<BadgeNode>(
            parse("""{"type":"badge","id":null}"""),
        )

        assertEquals(BadgeNode().id, node.id)
    }

    @Test
    fun badgeNodeWiresNonDefaultValue() {
        val node = assertIs<BadgeNode>(
            parse("""{"type":"badge","value":"value-한글 /?#"}"""),
        )

        assertEquals("value-한글 /?#", node.value)
    }

    @Test
    fun badgeNodeMapsExplicitNullValueToSafeDefault() {
        val node = assertIs<BadgeNode>(
            parse("""{"type":"badge","value":null}"""),
        )

        assertEquals(BadgeNode().value, node.value)
    }

    @Test
    fun badgeNodeWiresNonDefaultColor() {
        val node = assertIs<BadgeNode>(
            parse("""{"type":"badge","color":"color-한글 /?#"}"""),
        )

        assertEquals("color-한글 /?#", node.color)
    }

    @Test
    fun badgeNodeMapsExplicitNullColorToSafeDefault() {
        val node = assertIs<BadgeNode>(
            parse("""{"type":"badge","color":null}"""),
        )

        assertEquals(BadgeNode().color, node.color)
    }

    @Test
    fun statNodeWiresNonDefaultId() {
        val node = assertIs<StatNode>(
            parse("""{"type":"stat","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun statNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<StatNode>(
            parse("""{"type":"stat","id":null}"""),
        )

        assertEquals(StatNode().id, node.id)
    }

    @Test
    fun statNodeWiresNonDefaultValue() {
        val node = assertIs<StatNode>(
            parse("""{"type":"stat","value":"value-한글 /?#"}"""),
        )

        assertEquals("value-한글 /?#", node.value)
    }

    @Test
    fun statNodeMapsExplicitNullValueToSafeDefault() {
        val node = assertIs<StatNode>(
            parse("""{"type":"stat","value":null}"""),
        )

        assertEquals(StatNode().value, node.value)
    }

    @Test
    fun statNodeWiresNonDefaultLabel() {
        val node = assertIs<StatNode>(
            parse("""{"type":"stat","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun statNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<StatNode>(
            parse("""{"type":"stat","label":null}"""),
        )

        assertEquals(StatNode().label, node.label)
    }

    @Test
    fun statNodeWiresNonDefaultDescription() {
        val node = assertIs<StatNode>(
            parse("""{"type":"stat","description":"description-한글 /?#"}"""),
        )

        assertEquals("description-한글 /?#", node.description)
    }

    @Test
    fun statNodeMapsExplicitNullDescriptionToSafeDefault() {
        val node = assertIs<StatNode>(
            parse("""{"type":"stat","description":null}"""),
        )

        assertEquals(StatNode().description, node.description)
    }

    @Test
    fun avatarNodeWiresNonDefaultId() {
        val node = assertIs<AvatarNode>(
            parse("""{"type":"avatar","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun avatarNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<AvatarNode>(
            parse("""{"type":"avatar","id":null}"""),
        )

        assertEquals(AvatarNode().id, node.id)
    }

    @Test
    fun avatarNodeWiresNonDefaultName() {
        val node = assertIs<AvatarNode>(
            parse("""{"type":"avatar","name":"name-한글 /?#"}"""),
        )

        assertEquals("name-한글 /?#", node.name)
    }

    @Test
    fun avatarNodeMapsExplicitNullNameToSafeDefault() {
        val node = assertIs<AvatarNode>(
            parse("""{"type":"avatar","name":null}"""),
        )

        assertEquals(AvatarNode().name, node.name)
    }

    @Test
    fun avatarNodeWiresNonDefaultImageUrl() {
        val node = assertIs<AvatarNode>(
            parse("""{"type":"avatar","imageUrl":"imageUrl-한글 /?#"}"""),
        )

        assertEquals("imageUrl-한글 /?#", node.imageUrl)
    }

    @Test
    fun avatarNodeMapsExplicitNullImageUrlToSafeDefault() {
        val node = assertIs<AvatarNode>(
            parse("""{"type":"avatar","imageUrl":null}"""),
        )

        assertEquals(AvatarNode().imageUrl, node.imageUrl)
    }

    @Test
    fun avatarNodeWiresNonDefaultSize() {
        val node = assertIs<AvatarNode>(
            parse("""{"type":"avatar","size":"17"}"""),
        )

        assertEquals(17, node.size)
    }

    @Test
    fun avatarNodeMapsExplicitNullSizeToSafeDefault() {
        val node = assertIs<AvatarNode>(
            parse("""{"type":"avatar","size":null}"""),
        )

        assertEquals(AvatarNode().size, node.size)
    }

    @Test
    fun listNodeWiresNonDefaultId() {
        val node = assertIs<ListNode>(
            parse("""{"type":"list","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun listNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<ListNode>(
            parse("""{"type":"list","id":null}"""),
        )

        assertEquals(ListNode().id, node.id)
    }

    @Test
    fun listNodeWiresNonDefaultItems() {
        val node = assertIs<ListNode>(
            parse("""{"type":"list","items":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
        )

        assertEquals(1, node.items.size)
        val child = assertIs<TextNode>(node.items.single())
        assertEquals("child-id", child.id)
        assertEquals("child-value", child.value)
    }

    @Test
    fun listNodeMapsExplicitNullItemsToSafeDefault() {
        val node = assertIs<ListNode>(
            parse("""{"type":"list","items":null}"""),
        )

        assertEquals(ListNode().items, node.items)
    }

    @Test
    fun listNodeWiresNonDefaultOrdered() {
        val node = assertIs<ListNode>(
            parse("""{"type":"list","ordered":"yes"}"""),
        )

        assertEquals(true, node.ordered)
    }

    @Test
    fun listNodeMapsExplicitNullOrderedToSafeDefault() {
        val node = assertIs<ListNode>(
            parse("""{"type":"list","ordered":null}"""),
        )

        assertEquals(ListNode().ordered, node.ordered)
    }

    @Test
    fun tableNodeWiresNonDefaultId() {
        val node = assertIs<TableNode>(
            parse("""{"type":"table","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun tableNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<TableNode>(
            parse("""{"type":"table","id":null}"""),
        )

        assertEquals(TableNode().id, node.id)
    }

    @Test
    fun tableNodeWiresNonDefaultHeaders() {
        val node = assertIs<TableNode>(
            parse("""{"type":"table","headers":["one","둘"]}"""),
        )

        assertEquals(listOf("one", "둘"), node.headers.toList())
    }

    @Test
    fun tableNodeMapsExplicitNullHeadersToSafeDefault() {
        val node = assertIs<TableNode>(
            parse("""{"type":"table","headers":null}"""),
        )

        assertEquals(TableNode().headers, node.headers)
    }

    @Test
    fun tableNodeWiresNonDefaultRows() {
        val node = assertIs<TableNode>(
            parse("""{"type":"table","rows":[["r1c1","r1c2"],{"a":"r2c1","b":"r2c2"},"r3"]}"""),
        )

        assertEquals(
            listOf(listOf("r1c1", "r1c2"), listOf("r2c1", "r2c2"), listOf("r3")),
            node.rows.map { it.toList() },
        )
    }

    @Test
    fun tableNodeMapsExplicitNullRowsToSafeDefault() {
        val node = assertIs<TableNode>(
            parse("""{"type":"table","rows":null}"""),
        )

        assertEquals(TableNode().rows, node.rows)
    }

    @Test
    fun chartNodeWiresNonDefaultId() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","id":"id-한글 /?#"}"""),
        )

        assertEquals("id-한글 /?#", node.id)
    }

    @Test
    fun chartNodeMapsExplicitNullIdToSafeDefault() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","id":null}"""),
        )

        assertEquals(ChartNode().id, node.id)
    }

    @Test
    fun chartNodeWiresNonDefaultChartType() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","chartType":"chartType-한글 /?#"}"""),
        )

        assertEquals("chartType-한글 /?#", node.chartType)
    }

    @Test
    fun chartNodeMapsExplicitNullChartTypeToSafeDefault() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","chartType":null}"""),
        )

        assertEquals(ChartNode().chartType, node.chartType)
    }

    @Test
    fun chartNodeWiresNonDefaultLabels() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","labels":["one","둘"]}"""),
        )

        assertEquals(listOf("one", "둘"), node.labels.toList())
    }

    @Test
    fun chartNodeMapsExplicitNullLabelsToSafeDefault() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","labels":null}"""),
        )

        assertEquals(ChartNode().labels, node.labels)
    }

    @Test
    fun chartNodeWiresNonDefaultValues() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","values":[1,"2.5","bad"]}"""),
        )

        assertEquals(listOf(1f, 2.5f), node.values.toList())
    }

    @Test
    fun chartNodeMapsExplicitNullValuesToSafeDefault() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","values":null}"""),
        )

        assertEquals(ChartNode().values, node.values)
    }

    @Test
    fun chartNodeWiresNonDefaultLabel() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","label":"label-한글 /?#"}"""),
        )

        assertEquals("label-한글 /?#", node.label)
    }

    @Test
    fun chartNodeMapsExplicitNullLabelToSafeDefault() {
        val node = assertIs<ChartNode>(
            parse("""{"type":"chart","label":null}"""),
        )

        assertEquals(ChartNode().label, node.label)
    }
}
