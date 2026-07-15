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

    private fun uiFieldContractCases(): List<() -> Unit> = listOf(
        {
            val node = assertIs<ColumnNode>(
                parse("""{"type":"column","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<ColumnNode>(
                parse("""{"type":"column","id":null}"""),
            )

            assertEquals(ColumnNode().id, node.id)
        },
        {
            val node = assertIs<ColumnNode>(
                parse("""{"type":"column","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
            )

            assertEquals(1, node.children.size)
            val child = assertIs<TextNode>(node.children.single())
            assertEquals("child-id", child.id)
            assertEquals("child-value", child.value)
        },
        {
            val node = assertIs<ColumnNode>(
                parse("""{"type":"column","children":null}"""),
            )

            assertEquals(ColumnNode().children, node.children)
        },
        {
            val node = assertIs<RowNode>(
                parse("""{"type":"row","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<RowNode>(
                parse("""{"type":"row","id":null}"""),
            )

            assertEquals(RowNode().id, node.id)
        },
        {
            val node = assertIs<RowNode>(
                parse("""{"type":"row","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
            )

            assertEquals(1, node.children.size)
            val child = assertIs<TextNode>(node.children.single())
            assertEquals("child-id", child.id)
            assertEquals("child-value", child.value)
        },
        {
            val node = assertIs<RowNode>(
                parse("""{"type":"row","children":null}"""),
            )

            assertEquals(RowNode().children, node.children)
        },
        {
            val node = assertIs<CardNode>(
                parse("""{"type":"card","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<CardNode>(
                parse("""{"type":"card","id":null}"""),
            )

            assertEquals(CardNode().id, node.id)
        },
        {
            val node = assertIs<CardNode>(
                parse("""{"type":"card","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
            )

            assertEquals(1, node.children.size)
            val child = assertIs<TextNode>(node.children.single())
            assertEquals("child-id", child.id)
            assertEquals("child-value", child.value)
        },
        {
            val node = assertIs<CardNode>(
                parse("""{"type":"card","children":null}"""),
            )

            assertEquals(CardNode().children, node.children)
        },
        {
            val node = assertIs<DividerNode>(
                parse("""{"type":"divider","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<DividerNode>(
                parse("""{"type":"divider","id":null}"""),
            )

            assertEquals(DividerNode().id, node.id)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","id":null}"""),
            )

            assertEquals(TextNode().id, node.id)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","value":"value-한글 /?#"}"""),
            )

            assertEquals("value-한글 /?#", node.value)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","value":null}"""),
            )

            assertEquals(TextNode().value, node.value)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","style":"headline"}"""),
            )

            assertEquals(TextNodeStyle.HEADLINE, node.style)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","style":null}"""),
            )

            assertEquals(TextNode().style, node.style)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","bold":"yes"}"""),
            )

            assertEquals(true, node.bold)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","bold":null}"""),
            )

            assertEquals(TextNode().bold, node.bold)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","italic":"yes"}"""),
            )

            assertEquals(true, node.italic)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","italic":null}"""),
            )

            assertEquals(TextNode().italic, node.italic)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","color":"color-한글 /?#"}"""),
            )

            assertEquals("color-한글 /?#", node.color)
        },
        {
            val node = assertIs<TextNode>(
                parse("""{"type":"text","color":null}"""),
            )

            assertEquals(TextNode().color, node.color)
        },
        {
            val node = assertIs<MarkdownNode>(
                parse("""{"type":"markdown","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<MarkdownNode>(
                parse("""{"type":"markdown","id":null}"""),
            )

            assertEquals(MarkdownNode().id, node.id)
        },
        {
            val node = assertIs<MarkdownNode>(
                parse("""{"type":"markdown","value":"value-한글 /?#"}"""),
            )

            assertEquals("value-한글 /?#", node.value)
        },
        {
            val node = assertIs<MarkdownNode>(
                parse("""{"type":"markdown","value":null}"""),
            )

            assertEquals(MarkdownNode().value, node.value)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","id":null}"""),
            )

            assertEquals(ImageNode().id, node.id)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","url":"url-한글 /?#"}"""),
            )

            assertEquals("url-한글 /?#", node.url)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","url":null}"""),
            )

            assertEquals(ImageNode().url, node.url)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","alt":"alt-한글 /?#"}"""),
            )

            assertEquals("alt-한글 /?#", node.alt)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","alt":null}"""),
            )

            assertEquals(ImageNode().alt, node.alt)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","height":"17"}"""),
            )

            assertEquals(17, node.height)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","height":null}"""),
            )

            assertEquals(ImageNode().height, node.height)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","aspectRatio":"12.5"}"""),
            )

            assertEquals(12.5f, node.aspectRatio)
        },
        {
            val node = assertIs<ImageNode>(
                parse("""{"type":"image","aspectRatio":null}"""),
            )

            assertEquals(ImageNode().aspectRatio, node.aspectRatio)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","id":null}"""),
            )

            assertEquals(ButtonNode().id, node.id)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","label":null}"""),
            )

            assertEquals(ButtonNode().label, node.label)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","action":{"type":"callback","event":"submit","data":{"enabled":true,"count":7},"collectFrom":["input-a","input-b"]}}"""),
            )

            val action = assertIs<CallbackAction>(node.action)
            assertEquals("submit", action.event)
            assertEquals(mapOf("enabled" to "true", "count" to "7"), action.dataAsStrings)
            assertEquals(listOf("input-a", "input-b"), action.collectFrom)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","action":null}"""),
            )

            assertEquals(ButtonNode().action, node.action)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","variant":"tonal"}"""),
            )

            assertEquals(ButtonVariant.TONAL, node.variant)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","variant":null}"""),
            )

            assertEquals(ButtonNode().variant, node.variant)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","enabled":"yes"}"""),
            )

            assertEquals(true, node.enabled)
        },
        {
            val node = assertIs<ButtonNode>(
                parse("""{"type":"button","enabled":null}"""),
            )

            assertEquals(ButtonNode().enabled, node.enabled)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","id":null}"""),
            )

            assertEquals(TextInputNode().id, node.id)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","label":null}"""),
            )

            assertEquals(TextInputNode().label, node.label)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","placeholder":"placeholder-한글 /?#"}"""),
            )

            assertEquals("placeholder-한글 /?#", node.placeholder)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","placeholder":null}"""),
            )

            assertEquals(TextInputNode().placeholder, node.placeholder)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","value":"value-한글 /?#"}"""),
            )

            assertEquals("value-한글 /?#", node.value)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","value":null}"""),
            )

            assertEquals(TextInputNode().value, node.value)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","multiline":"yes"}"""),
            )

            assertEquals(true, node.multiline)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","multiline":null}"""),
            )

            assertEquals(TextInputNode().multiline, node.multiline)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","keyboard":"keyboard-한글 /?#"}"""),
            )

            assertEquals("keyboard-한글 /?#", node.keyboard)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","keyboard":null}"""),
            )

            assertEquals(TextInputNode().keyboard, node.keyboard)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","required":"yes"}"""),
            )

            assertEquals(true, node.required)
        },
        {
            val node = assertIs<TextInputNode>(
                parse("""{"type":"text_input","required":null}"""),
            )

            assertEquals(TextInputNode().required, node.required)
        },
        {
            val node = assertIs<DateInputNode>(
                parse("""{"type":"date_input","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<DateInputNode>(
                parse("""{"type":"date_input","id":null}"""),
            )

            assertEquals(DateInputNode().id, node.id)
        },
        {
            val node = assertIs<DateInputNode>(
                parse("""{"type":"date_input","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<DateInputNode>(
                parse("""{"type":"date_input","label":null}"""),
            )

            assertEquals(DateInputNode().label, node.label)
        },
        {
            val node = assertIs<DateInputNode>(
                parse("""{"type":"date_input","value":"value-한글 /?#"}"""),
            )

            assertEquals("value-한글 /?#", node.value)
        },
        {
            val node = assertIs<DateInputNode>(
                parse("""{"type":"date_input","value":null}"""),
            )

            assertEquals(DateInputNode().value, node.value)
        },
        {
            val node = assertIs<DateInputNode>(
                parse("""{"type":"date_input","required":"yes"}"""),
            )

            assertEquals(true, node.required)
        },
        {
            val node = assertIs<DateInputNode>(
                parse("""{"type":"date_input","required":null}"""),
            )

            assertEquals(DateInputNode().required, node.required)
        },
        {
            val node = assertIs<TimeInputNode>(
                parse("""{"type":"time_input","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<TimeInputNode>(
                parse("""{"type":"time_input","id":null}"""),
            )

            assertEquals(TimeInputNode().id, node.id)
        },
        {
            val node = assertIs<TimeInputNode>(
                parse("""{"type":"time_input","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<TimeInputNode>(
                parse("""{"type":"time_input","label":null}"""),
            )

            assertEquals(TimeInputNode().label, node.label)
        },
        {
            val node = assertIs<TimeInputNode>(
                parse("""{"type":"time_input","value":"value-한글 /?#"}"""),
            )

            assertEquals("value-한글 /?#", node.value)
        },
        {
            val node = assertIs<TimeInputNode>(
                parse("""{"type":"time_input","value":null}"""),
            )

            assertEquals(TimeInputNode().value, node.value)
        },
        {
            val node = assertIs<TimeInputNode>(
                parse("""{"type":"time_input","required":"yes"}"""),
            )

            assertEquals(true, node.required)
        },
        {
            val node = assertIs<TimeInputNode>(
                parse("""{"type":"time_input","required":null}"""),
            )

            assertEquals(TimeInputNode().required, node.required)
        },
        {
            val node = assertIs<CheckboxNode>(
                parse("""{"type":"checkbox","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<CheckboxNode>(
                parse("""{"type":"checkbox","id":null}"""),
            )

            assertEquals(CheckboxNode().id, node.id)
        },
        {
            val node = assertIs<CheckboxNode>(
                parse("""{"type":"checkbox","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<CheckboxNode>(
                parse("""{"type":"checkbox","label":null}"""),
            )

            assertEquals(CheckboxNode().label, node.label)
        },
        {
            val node = assertIs<CheckboxNode>(
                parse("""{"type":"checkbox","checked":"yes"}"""),
            )

            assertEquals(true, node.checked)
        },
        {
            val node = assertIs<CheckboxNode>(
                parse("""{"type":"checkbox","checked":null}"""),
            )

            assertEquals(CheckboxNode().checked, node.checked)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","id":null}"""),
            )

            assertEquals(SelectNode().id, node.id)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","label":null}"""),
            )

            assertEquals(SelectNode().label, node.label)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","options":["one","둘"]}"""),
            )

            assertEquals(listOf("one", "둘"), node.options.toList())
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","options":null}"""),
            )

            assertEquals(SelectNode().options, node.options)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","selected":"selected-한글 /?#"}"""),
            )

            assertEquals("selected-한글 /?#", node.selected)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","selected":null}"""),
            )

            assertEquals(SelectNode().selected, node.selected)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","placeholder":"placeholder-한글 /?#"}"""),
            )

            assertEquals("placeholder-한글 /?#", node.placeholder)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","placeholder":null}"""),
            )

            assertEquals(SelectNode().placeholder, node.placeholder)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","required":"yes"}"""),
            )

            assertEquals(true, node.required)
        },
        {
            val node = assertIs<SelectNode>(
                parse("""{"type":"select","required":null}"""),
            )

            assertEquals(SelectNode().required, node.required)
        },
        {
            val node = assertIs<SwitchNode>(
                parse("""{"type":"switch","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<SwitchNode>(
                parse("""{"type":"switch","id":null}"""),
            )

            assertEquals(SwitchNode().id, node.id)
        },
        {
            val node = assertIs<SwitchNode>(
                parse("""{"type":"switch","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<SwitchNode>(
                parse("""{"type":"switch","label":null}"""),
            )

            assertEquals(SwitchNode().label, node.label)
        },
        {
            val node = assertIs<SwitchNode>(
                parse("""{"type":"switch","checked":"yes"}"""),
            )

            assertEquals(true, node.checked)
        },
        {
            val node = assertIs<SwitchNode>(
                parse("""{"type":"switch","checked":null}"""),
            )

            assertEquals(SwitchNode().checked, node.checked)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","id":null}"""),
            )

            assertEquals(SliderNode().id, node.id)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","label":null}"""),
            )

            assertEquals(SliderNode().label, node.label)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","value":"12.5"}"""),
            )

            assertEquals(12.5f, node.value)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","value":null}"""),
            )

            assertEquals(SliderNode().value, node.value)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","min":"12.5"}"""),
            )

            assertEquals(12.5f, node.min)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","min":null}"""),
            )

            assertEquals(SliderNode().min, node.min)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","max":"12.5"}"""),
            )

            assertEquals(12.5f, node.max)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","max":null}"""),
            )

            assertEquals(SliderNode().max, node.max)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","step":"12.5"}"""),
            )

            assertEquals(12.5f, node.step)
        },
        {
            val node = assertIs<SliderNode>(
                parse("""{"type":"slider","step":null}"""),
            )

            assertEquals(SliderNode().step, node.step)
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","id":null}"""),
            )

            assertEquals(RadioGroupNode().id, node.id)
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","label":null}"""),
            )

            assertEquals(RadioGroupNode().label, node.label)
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","options":["one","둘"]}"""),
            )

            assertEquals(listOf("one", "둘"), node.options.toList())
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","options":null}"""),
            )

            assertEquals(RadioGroupNode().options, node.options)
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","selected":"selected-한글 /?#"}"""),
            )

            assertEquals("selected-한글 /?#", node.selected)
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","selected":null}"""),
            )

            assertEquals(RadioGroupNode().selected, node.selected)
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","required":"yes"}"""),
            )

            assertEquals(true, node.required)
        },
        {
            val node = assertIs<RadioGroupNode>(
                parse("""{"type":"radio_group","required":null}"""),
            )

            assertEquals(RadioGroupNode().required, node.required)
        },
        {
            val node = assertIs<ProgressNode>(
                parse("""{"type":"progress","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<ProgressNode>(
                parse("""{"type":"progress","id":null}"""),
            )

            assertEquals(ProgressNode().id, node.id)
        },
        {
            val node = assertIs<ProgressNode>(
                parse("""{"type":"progress","value":"12.5"}"""),
            )

            assertEquals(12.5f, node.value)
        },
        {
            val node = assertIs<ProgressNode>(
                parse("""{"type":"progress","value":null}"""),
            )

            assertEquals(ProgressNode().value, node.value)
        },
        {
            val node = assertIs<ProgressNode>(
                parse("""{"type":"progress","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<ProgressNode>(
                parse("""{"type":"progress","label":null}"""),
            )

            assertEquals(ProgressNode().label, node.label)
        },
        {
            val node = assertIs<AlertNode>(
                parse("""{"type":"alert","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<AlertNode>(
                parse("""{"type":"alert","id":null}"""),
            )

            assertEquals(AlertNode().id, node.id)
        },
        {
            val node = assertIs<AlertNode>(
                parse("""{"type":"alert","message":"message-한글 /?#"}"""),
            )

            assertEquals("message-한글 /?#", node.message)
        },
        {
            val node = assertIs<AlertNode>(
                parse("""{"type":"alert","message":null}"""),
            )

            assertEquals(AlertNode().message, node.message)
        },
        {
            val node = assertIs<AlertNode>(
                parse("""{"type":"alert","title":"title-한글 /?#"}"""),
            )

            assertEquals("title-한글 /?#", node.title)
        },
        {
            val node = assertIs<AlertNode>(
                parse("""{"type":"alert","title":null}"""),
            )

            assertEquals(AlertNode().title, node.title)
        },
        {
            val node = assertIs<AlertNode>(
                parse("""{"type":"alert","severity":"warning"}"""),
            )

            assertEquals(AlertSeverity.WARNING, node.severity)
        },
        {
            val node = assertIs<AlertNode>(
                parse("""{"type":"alert","severity":null}"""),
            )

            assertEquals(AlertNode().severity, node.severity)
        },
        {
            val node = assertIs<CountdownNode>(
                parse("""{"type":"countdown","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<CountdownNode>(
                parse("""{"type":"countdown","id":null}"""),
            )

            assertEquals(CountdownNode().id, node.id)
        },
        {
            val node = assertIs<CountdownNode>(
                parse("""{"type":"countdown","seconds":"17"}"""),
            )

            assertEquals(17, node.seconds)
        },
        {
            val node = assertIs<CountdownNode>(
                parse("""{"type":"countdown","seconds":null}"""),
            )

            assertEquals(CountdownNode().seconds, node.seconds)
        },
        {
            val node = assertIs<CountdownNode>(
                parse("""{"type":"countdown","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<CountdownNode>(
                parse("""{"type":"countdown","label":null}"""),
            )

            assertEquals(CountdownNode().label, node.label)
        },
        {
            val node = assertIs<CountdownNode>(
                parse("""{"type":"countdown","action":{"type":"callback","event":"submit","data":{"enabled":true,"count":7},"collectFrom":["input-a","input-b"]}}"""),
            )

            val action = assertIs<CallbackAction>(node.action)
            assertEquals("submit", action.event)
            assertEquals(mapOf("enabled" to "true", "count" to "7"), action.dataAsStrings)
            assertEquals(listOf("input-a", "input-b"), action.collectFrom)
        },
        {
            val node = assertIs<CountdownNode>(
                parse("""{"type":"countdown","action":null}"""),
            )

            assertEquals(CountdownNode().action, node.action)
        },
        {
            val node = assertIs<ChipGroupNode>(
                parse("""{"type":"chip_group","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<ChipGroupNode>(
                parse("""{"type":"chip_group","id":null}"""),
            )

            assertEquals(ChipGroupNode().id, node.id)
        },
        {
            val node = assertIs<ChipGroupNode>(
                parse("""{"type":"chip_group","chips":[{"label":"A","value":"a"},"B"]}"""),
            )

            assertEquals(listOf(ChipItem("A", "a"), ChipItem("B", "B")), node.chips.toList())
        },
        {
            val node = assertIs<ChipGroupNode>(
                parse("""{"type":"chip_group","chips":null}"""),
            )

            assertEquals(ChipGroupNode().chips, node.chips)
        },
        {
            val node = assertIs<ChipGroupNode>(
                parse("""{"type":"chip_group","selection":"selection-한글 /?#"}"""),
            )

            assertEquals("selection-한글 /?#", node.selection)
        },
        {
            val node = assertIs<ChipGroupNode>(
                parse("""{"type":"chip_group","selection":null}"""),
            )

            assertEquals(ChipGroupNode().selection, node.selection)
        },
        {
            val node = assertIs<ChipGroupNode>(
                parse("""{"type":"chip_group","required":"yes"}"""),
            )

            assertEquals(true, node.required)
        },
        {
            val node = assertIs<ChipGroupNode>(
                parse("""{"type":"chip_group","required":null}"""),
            )

            assertEquals(ChipGroupNode().required, node.required)
        },
        {
            val node = assertIs<IconNode>(
                parse("""{"type":"icon","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<IconNode>(
                parse("""{"type":"icon","id":null}"""),
            )

            assertEquals(IconNode().id, node.id)
        },
        {
            val node = assertIs<IconNode>(
                parse("""{"type":"icon","name":"name-한글 /?#"}"""),
            )

            assertEquals("name-한글 /?#", node.name)
        },
        {
            val node = assertIs<IconNode>(
                parse("""{"type":"icon","name":null}"""),
            )

            assertEquals(IconNode().name, node.name)
        },
        {
            val node = assertIs<IconNode>(
                parse("""{"type":"icon","size":"17"}"""),
            )

            assertEquals(17, node.size)
        },
        {
            val node = assertIs<IconNode>(
                parse("""{"type":"icon","size":null}"""),
            )

            assertEquals(IconNode().size, node.size)
        },
        {
            val node = assertIs<IconNode>(
                parse("""{"type":"icon","color":"color-한글 /?#"}"""),
            )

            assertEquals("color-한글 /?#", node.color)
        },
        {
            val node = assertIs<IconNode>(
                parse("""{"type":"icon","color":null}"""),
            )

            assertEquals(IconNode().color, node.color)
        },
        {
            val node = assertIs<CodeNode>(
                parse("""{"type":"code","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<CodeNode>(
                parse("""{"type":"code","id":null}"""),
            )

            assertEquals(CodeNode().id, node.id)
        },
        {
            val node = assertIs<CodeNode>(
                parse("""{"type":"code","code":"code-한글 /?#"}"""),
            )

            assertEquals("code-한글 /?#", node.code)
        },
        {
            val node = assertIs<CodeNode>(
                parse("""{"type":"code","code":null}"""),
            )

            assertEquals(CodeNode().code, node.code)
        },
        {
            val node = assertIs<CodeNode>(
                parse("""{"type":"code","language":"language-한글 /?#"}"""),
            )

            assertEquals("language-한글 /?#", node.language)
        },
        {
            val node = assertIs<CodeNode>(
                parse("""{"type":"code","language":null}"""),
            )

            assertEquals(CodeNode().language, node.language)
        },
        {
            val node = assertIs<BoxNode>(
                parse("""{"type":"box","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<BoxNode>(
                parse("""{"type":"box","id":null}"""),
            )

            assertEquals(BoxNode().id, node.id)
        },
        {
            val node = assertIs<BoxNode>(
                parse("""{"type":"box","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
            )

            assertEquals(1, node.children.size)
            val child = assertIs<TextNode>(node.children.single())
            assertEquals("child-id", child.id)
            assertEquals("child-value", child.value)
        },
        {
            val node = assertIs<BoxNode>(
                parse("""{"type":"box","children":null}"""),
            )

            assertEquals(BoxNode().children, node.children)
        },
        {
            val node = assertIs<BoxNode>(
                parse("""{"type":"box","contentAlignment":"contentAlignment-한글 /?#"}"""),
            )

            assertEquals("contentAlignment-한글 /?#", node.contentAlignment)
        },
        {
            val node = assertIs<BoxNode>(
                parse("""{"type":"box","contentAlignment":null}"""),
            )

            assertEquals(BoxNode().contentAlignment, node.contentAlignment)
        },
        {
            val node = assertIs<TabsNode>(
                parse("""{"type":"tabs","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<TabsNode>(
                parse("""{"type":"tabs","id":null}"""),
            )

            assertEquals(TabsNode().id, node.id)
        },
        {
            val node = assertIs<TabsNode>(
                parse("""{"type":"tabs","tabs":[{"label":"Tab A","children":[{"type":"text","value":"tab child"}]}]}"""),
            )

            assertEquals(1, node.tabs.size)
            assertEquals("Tab A", node.tabs.single().label)
            assertEquals("tab child", assertIs<TextNode>(node.tabs.single().children.single()).value)
        },
        {
            val node = assertIs<TabsNode>(
                parse("""{"type":"tabs","tabs":null}"""),
            )

            assertEquals(TabsNode().tabs, node.tabs)
        },
        {
            val node = assertIs<TabsNode>(
                parse("""{"type":"tabs","selectedIndex":"17"}"""),
            )

            assertEquals(17, node.selectedIndex)
        },
        {
            val node = assertIs<TabsNode>(
                parse("""{"type":"tabs","selectedIndex":null}"""),
            )

            assertEquals(TabsNode().selectedIndex, node.selectedIndex)
        },
        {
            val node = assertIs<AccordionNode>(
                parse("""{"type":"accordion","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<AccordionNode>(
                parse("""{"type":"accordion","id":null}"""),
            )

            assertEquals(AccordionNode().id, node.id)
        },
        {
            val node = assertIs<AccordionNode>(
                parse("""{"type":"accordion","title":"title-한글 /?#"}"""),
            )

            assertEquals("title-한글 /?#", node.title)
        },
        {
            val node = assertIs<AccordionNode>(
                parse("""{"type":"accordion","title":null}"""),
            )

            assertEquals(AccordionNode().title, node.title)
        },
        {
            val node = assertIs<AccordionNode>(
                parse("""{"type":"accordion","children":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
            )

            assertEquals(1, node.children.size)
            val child = assertIs<TextNode>(node.children.single())
            assertEquals("child-id", child.id)
            assertEquals("child-value", child.value)
        },
        {
            val node = assertIs<AccordionNode>(
                parse("""{"type":"accordion","children":null}"""),
            )

            assertEquals(AccordionNode().children, node.children)
        },
        {
            val node = assertIs<AccordionNode>(
                parse("""{"type":"accordion","expanded":"yes"}"""),
            )

            assertEquals(true, node.expanded)
        },
        {
            val node = assertIs<AccordionNode>(
                parse("""{"type":"accordion","expanded":null}"""),
            )

            assertEquals(AccordionNode().expanded, node.expanded)
        },
        {
            val node = assertIs<QuoteNode>(
                parse("""{"type":"quote","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<QuoteNode>(
                parse("""{"type":"quote","id":null}"""),
            )

            assertEquals(QuoteNode().id, node.id)
        },
        {
            val node = assertIs<QuoteNode>(
                parse("""{"type":"quote","text":"text-한글 /?#"}"""),
            )

            assertEquals("text-한글 /?#", node.text)
        },
        {
            val node = assertIs<QuoteNode>(
                parse("""{"type":"quote","text":null}"""),
            )

            assertEquals(QuoteNode().text, node.text)
        },
        {
            val node = assertIs<QuoteNode>(
                parse("""{"type":"quote","source":"source-한글 /?#"}"""),
            )

            assertEquals("source-한글 /?#", node.source)
        },
        {
            val node = assertIs<QuoteNode>(
                parse("""{"type":"quote","source":null}"""),
            )

            assertEquals(QuoteNode().source, node.source)
        },
        {
            val node = assertIs<BadgeNode>(
                parse("""{"type":"badge","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<BadgeNode>(
                parse("""{"type":"badge","id":null}"""),
            )

            assertEquals(BadgeNode().id, node.id)
        },
        {
            val node = assertIs<BadgeNode>(
                parse("""{"type":"badge","value":"value-한글 /?#"}"""),
            )

            assertEquals("value-한글 /?#", node.value)
        },
        {
            val node = assertIs<BadgeNode>(
                parse("""{"type":"badge","value":null}"""),
            )

            assertEquals(BadgeNode().value, node.value)
        },
        {
            val node = assertIs<BadgeNode>(
                parse("""{"type":"badge","color":"color-한글 /?#"}"""),
            )

            assertEquals("color-한글 /?#", node.color)
        },
        {
            val node = assertIs<BadgeNode>(
                parse("""{"type":"badge","color":null}"""),
            )

            assertEquals(BadgeNode().color, node.color)
        },
        {
            val node = assertIs<StatNode>(
                parse("""{"type":"stat","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<StatNode>(
                parse("""{"type":"stat","id":null}"""),
            )

            assertEquals(StatNode().id, node.id)
        },
        {
            val node = assertIs<StatNode>(
                parse("""{"type":"stat","value":"value-한글 /?#"}"""),
            )

            assertEquals("value-한글 /?#", node.value)
        },
        {
            val node = assertIs<StatNode>(
                parse("""{"type":"stat","value":null}"""),
            )

            assertEquals(StatNode().value, node.value)
        },
        {
            val node = assertIs<StatNode>(
                parse("""{"type":"stat","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<StatNode>(
                parse("""{"type":"stat","label":null}"""),
            )

            assertEquals(StatNode().label, node.label)
        },
        {
            val node = assertIs<StatNode>(
                parse("""{"type":"stat","description":"description-한글 /?#"}"""),
            )

            assertEquals("description-한글 /?#", node.description)
        },
        {
            val node = assertIs<StatNode>(
                parse("""{"type":"stat","description":null}"""),
            )

            assertEquals(StatNode().description, node.description)
        },
        {
            val node = assertIs<AvatarNode>(
                parse("""{"type":"avatar","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<AvatarNode>(
                parse("""{"type":"avatar","id":null}"""),
            )

            assertEquals(AvatarNode().id, node.id)
        },
        {
            val node = assertIs<AvatarNode>(
                parse("""{"type":"avatar","name":"name-한글 /?#"}"""),
            )

            assertEquals("name-한글 /?#", node.name)
        },
        {
            val node = assertIs<AvatarNode>(
                parse("""{"type":"avatar","name":null}"""),
            )

            assertEquals(AvatarNode().name, node.name)
        },
        {
            val node = assertIs<AvatarNode>(
                parse("""{"type":"avatar","imageUrl":"imageUrl-한글 /?#"}"""),
            )

            assertEquals("imageUrl-한글 /?#", node.imageUrl)
        },
        {
            val node = assertIs<AvatarNode>(
                parse("""{"type":"avatar","imageUrl":null}"""),
            )

            assertEquals(AvatarNode().imageUrl, node.imageUrl)
        },
        {
            val node = assertIs<AvatarNode>(
                parse("""{"type":"avatar","size":"17"}"""),
            )

            assertEquals(17, node.size)
        },
        {
            val node = assertIs<AvatarNode>(
                parse("""{"type":"avatar","size":null}"""),
            )

            assertEquals(AvatarNode().size, node.size)
        },
        {
            val node = assertIs<ListNode>(
                parse("""{"type":"list","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<ListNode>(
                parse("""{"type":"list","id":null}"""),
            )

            assertEquals(ListNode().id, node.id)
        },
        {
            val node = assertIs<ListNode>(
                parse("""{"type":"list","items":[{"type":"text","id":"child-id","value":"child-value"}]}"""),
            )

            assertEquals(1, node.items.size)
            val child = assertIs<TextNode>(node.items.single())
            assertEquals("child-id", child.id)
            assertEquals("child-value", child.value)
        },
        {
            val node = assertIs<ListNode>(
                parse("""{"type":"list","items":null}"""),
            )

            assertEquals(ListNode().items, node.items)
        },
        {
            val node = assertIs<ListNode>(
                parse("""{"type":"list","ordered":"yes"}"""),
            )

            assertEquals(true, node.ordered)
        },
        {
            val node = assertIs<ListNode>(
                parse("""{"type":"list","ordered":null}"""),
            )

            assertEquals(ListNode().ordered, node.ordered)
        },
        {
            val node = assertIs<TableNode>(
                parse("""{"type":"table","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<TableNode>(
                parse("""{"type":"table","id":null}"""),
            )

            assertEquals(TableNode().id, node.id)
        },
        {
            val node = assertIs<TableNode>(
                parse("""{"type":"table","headers":["one","둘"]}"""),
            )

            assertEquals(listOf("one", "둘"), node.headers.toList())
        },
        {
            val node = assertIs<TableNode>(
                parse("""{"type":"table","headers":null}"""),
            )

            assertEquals(TableNode().headers, node.headers)
        },
        {
            val node = assertIs<TableNode>(
                parse("""{"type":"table","rows":[["r1c1","r1c2"],{"a":"r2c1","b":"r2c2"},"r3"]}"""),
            )

            assertEquals(
                listOf(listOf("r1c1", "r1c2"), listOf("r2c1", "r2c2"), listOf("r3")),
                node.rows.map { it.toList() },
            )
        },
        {
            val node = assertIs<TableNode>(
                parse("""{"type":"table","rows":null}"""),
            )

            assertEquals(TableNode().rows, node.rows)
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","id":"id-한글 /?#"}"""),
            )

            assertEquals("id-한글 /?#", node.id)
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","id":null}"""),
            )

            assertEquals(ChartNode().id, node.id)
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","chartType":"chartType-한글 /?#"}"""),
            )

            assertEquals("chartType-한글 /?#", node.chartType)
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","chartType":null}"""),
            )

            assertEquals(ChartNode().chartType, node.chartType)
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","labels":["one","둘"]}"""),
            )

            assertEquals(listOf("one", "둘"), node.labels.toList())
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","labels":null}"""),
            )

            assertEquals(ChartNode().labels, node.labels)
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","values":[1,"2.5","bad"]}"""),
            )

            assertEquals(listOf(1f, 2.5f), node.values.toList())
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","values":null}"""),
            )

            assertEquals(ChartNode().values, node.values)
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","label":"label-한글 /?#"}"""),
            )

            assertEquals("label-한글 /?#", node.label)
        },
        {
            val node = assertIs<ChartNode>(
                parse("""{"type":"chart","label":null}"""),
            )

            assertEquals(ChartNode().label, node.label)
        },
    )

    @Test
    fun uiNodesMapNullAndWireNonDefaultFieldsWhenParsed() {
        uiFieldContractCases().forEach { it() }
    }
}
