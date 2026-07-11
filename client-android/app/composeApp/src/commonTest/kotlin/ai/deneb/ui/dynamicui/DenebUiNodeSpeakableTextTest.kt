package ai.deneb.ui.dynamicui

import kotlinx.collections.immutable.persistentListOf
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class DenebUiNodeSpeakableTextTest {

    @Test
    fun basicContentAndActionNodesSpeakHumanLabelsInOrder() {
        val root = ColumnNode(
            children = persistentListOf(
                TextNode(value = "Title"),
                ButtonNode(label = "Continue"),
                CheckboxNode(label = "Accept terms"),
                SwitchNode(label = "Notifications"),
                SliderNode(label = "Volume"),
                ProgressNode(label = "Uploading"),
                CountdownNode(label = "Starts soon"),
                BadgeNode(value = "New"),
                AvatarNode(name = "Kim"),
                ImageNode(alt = "Project diagram"),
            ),
        )

        assertEquals(
            "Title. Continue. Accept terms. Notifications. Volume. Uploading. Starts soon. New. Kim. Project diagram",
            root.collectSpeakableText(),
        )
    }

    @Test
    fun textInputPrefersValueThenLabelThenPlaceholderIgnoringBlankValues() {
        assertEquals("Alice", TextInputNode(value = "Alice", label = "Name", placeholder = "Enter name").collectSpeakableText())
        assertEquals("Name", TextInputNode(value = "", label = "Name", placeholder = "Enter name").collectSpeakableText())
        assertEquals("Enter name", TextInputNode(value = "  ", label = "", placeholder = "Enter name").collectSpeakableText())
        assertEquals("", TextInputNode(value = null, label = null, placeholder = null).collectSpeakableText())
    }

    @Test
    fun dateAndTimeInputsFallBackToLabelsWhenValuesAreBlank() {
        assertEquals("2026-07-11", DateInputNode(value = "2026-07-11", label = "Due date").collectSpeakableText())
        assertEquals("Due date", DateInputNode(value = "", label = "Due date").collectSpeakableText())
        assertEquals("14:30", TimeInputNode(value = "14:30", label = "Start time").collectSpeakableText())
        assertEquals("Start time", TimeInputNode(value = "   ", label = "Start time").collectSpeakableText())
    }

    @Test
    fun selectionNodesSpeakLabelsSelectionsAndAllChipLabels() {
        val root = ColumnNode(
            children = persistentListOf(
                SelectNode(label = "Region", selected = "Seoul"),
                RadioGroupNode(label = "Priority", selected = "High"),
                ChipGroupNode(
                    chips = persistentListOf(
                        ChipItem(label = "Fast", value = "fast"),
                        ChipItem(label = "Safe", value = "safe"),
                    ),
                ),
            ),
        )

        assertEquals("Region. Seoul. Priority. High. Fast. Safe", root.collectSpeakableText())
    }

    @Test
    fun blankSelectionsAreOmittedWhileControlLabelsRemain() {
        val root = ColumnNode(
            children = persistentListOf(
                SelectNode(label = "Region", selected = ""),
                RadioGroupNode(label = "Priority", selected = "   "),
                ChipGroupNode(
                    chips = persistentListOf(
                        ChipItem(label = "", value = "empty"),
                        ChipItem(label = "Safe", value = "safe"),
                    ),
                ),
            ),
        )

        assertEquals("Region. Priority. Safe", root.collectSpeakableText())
    }

    @Test
    fun alertsQuotesAndStatsIncludeTheirSupportingContext() {
        val root = RowNode(
            children = persistentListOf(
                AlertNode(title = "Warning", message = "Check the amount"),
                QuoteNode(text = "Stay curious", source = "Operator"),
                StatNode(label = "Revenue", value = "₩10M", description = "Up 20 percent"),
            ),
        )

        assertEquals(
            "Warning. Check the amount. Stay curious. Operator. Revenue. ₩10M. Up 20 percent",
            root.collectSpeakableText(),
        )
    }

    @Test
    fun blankOptionalFieldsAreFilteredWithoutExtraSeparators() {
        val root = ColumnNode(
            children = persistentListOf(
                TextNode(value = ""),
                AlertNode(title = "   ", message = "Message"),
                QuoteNode(text = "Quote", source = ""),
                AvatarNode(name = " "),
                ImageNode(alt = ""),
                TextNode(value = "End"),
            ),
        )

        assertEquals("Message. Quote. End", root.collectSpeakableText())
    }

    @Test
    fun nestedLayoutsPreserveDepthFirstVisualOrder() {
        val root = CardNode(
            children = persistentListOf(
                TextNode(value = "Card start"),
                BoxNode(
                    children = persistentListOf(
                        RowNode(children = persistentListOf(TextNode(value = "Row one"), TextNode(value = "Row two"))),
                    ),
                ),
                ListNode(items = persistentListOf(TextNode(value = "Item one"), TextNode(value = "Item two"))),
                TextNode(value = "Card end"),
            ),
        )

        assertEquals("Card start. Row one. Row two. Item one. Item two. Card end", root.collectSpeakableText())
    }

    @Test
    fun tabsAndAccordionSpeakTitlesBeforeTheirChildren() {
        val root = TabsNode(
            tabs = persistentListOf(
                TabItem(
                    label = "Overview",
                    children = persistentListOf(TextNode(value = "Summary")),
                ),
                TabItem(
                    label = "Details",
                    children = persistentListOf(
                        AccordionNode(
                            title = "Risks",
                            children = persistentListOf(TextNode(value = "Weather delay")),
                        ),
                    ),
                ),
            ),
        )

        assertEquals("Overview. Summary. Details. Risks. Weather delay", root.collectSpeakableText())
    }

    @Test
    fun tableAndChartSpeakLabelsNotNumericImplementationDetails() {
        val root = ColumnNode(
            children = persistentListOf(
                TableNode(
                    headers = persistentListOf("Project", "Status"),
                    rows = persistentListOf(
                        persistentListOf("Alpha", "On track"),
                        persistentListOf("Beta", "At risk"),
                    ),
                ),
                ChartNode(
                    label = "Monthly output",
                    labels = persistentListOf("January", "February"),
                    values = persistentListOf(10f, 20f),
                ),
            ),
        )

        val spoken = root.collectSpeakableText()
        assertEquals(
            "Project, Status. Alpha, On track. Beta, At risk. Monthly output. January, February",
            spoken,
        )
        assertFalse("10" in spoken)
        assertFalse("20" in spoken)
    }

    @Test
    fun blankTableAndChartLabelsDoNotProducePunctuationNoise() {
        val root = ColumnNode(
            children = persistentListOf(
                TableNode(
                    headers = persistentListOf("", "Project", " "),
                    rows = persistentListOf(
                        persistentListOf("", "Alpha", ""),
                        persistentListOf("", "", ""),
                    ),
                ),
                ChartNode(
                    label = "",
                    labels = persistentListOf("", "January", " "),
                    values = persistentListOf(0f, 1f, 2f),
                ),
            ),
        )

        assertEquals("Project. Alpha. January", root.collectSpeakableText())
    }

    @Test
    fun markdownNodeUsesSameSpeakableMarkdownPipeline() {
        val node = MarkdownNode(value = "**Bold** text\n\n```kotlin\nval secret = 1\n```")

        val spoken = node.collectSpeakableText()

        assertTrue("Bold text" in spoken)
        assertFalse("secret" in spoken)
        assertFalse("**" in spoken)
    }

    @Test
    fun codeIconAndDividerNodesAreSilent() {
        val root = ColumnNode(
            children = persistentListOf(
                CodeNode(code = "rm -rf /", language = "sh"),
                IconNode(name = "warning"),
                DividerNode(),
            ),
        )

        assertEquals("", root.collectSpeakableText())
    }
}
