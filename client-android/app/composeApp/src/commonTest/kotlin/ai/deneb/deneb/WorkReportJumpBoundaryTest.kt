package ai.deneb.deneb

import ai.deneb.data.SharedJson
import ai.deneb.ui.chat.History
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class WorkReportJumpBoundaryTest {

    private fun row(role: History.Role, timestamp: Long, content: String = "content") = History(
        role = role,
        content = content,
        timestampMs = timestamp,
    )

    private fun assistant(timestamp: Long, content: String = "content") = row(History.Role.ASSISTANT, timestamp, content)

    private fun fence(payload: String, suffix: String = ""): String = buildString {
        append("```deneb-ui\n")
        append(payload)
        append("\n```")
        append(suffix)
    }

    private fun expandedPayload(content: String) = SharedJson.parseToJsonElement(
        content.lines().drop(1).takeWhile { it.trim() != "```" }.joinToString("\n"),
    ).jsonObject

    @Test
    fun emptyHistoryHasNoMirror() {
        assertEquals(-1, indexOfMirroredReport(emptyList(), 1))
    }

    @Test
    fun nonPositiveCardTimestampsAreAlwaysRejected() {
        val history = listOf(assistant(1))

        for (timestamp in listOf(Long.MIN_VALUE, -1L, 0L)) {
            assertEquals(-1, indexOfMirroredReport(history, timestamp), timestamp.toString())
        }
    }

    @Test
    fun exactAssistantTimestampMatches() {
        assertEquals(0, indexOfMirroredReport(listOf(assistant(1_000)), 1_000))
    }

    @Test
    fun userToolAndExecutingRowsNeverMatchEvenAtExactTimestamp() {
        val history = listOf(
            row(History.Role.USER, 1_000),
            row(History.Role.TOOL, 1_000),
            row(History.Role.TOOL_EXECUTING, 1_000),
        )

        assertEquals(-1, indexOfMirroredReport(history, 1_000))
    }

    @Test
    fun assistantRowsWithNonPositiveTimestampsAreIgnored() {
        val history = listOf(assistant(Long.MIN_VALUE), assistant(-1), assistant(0))

        assertEquals(-1, indexOfMirroredReport(history, 1))
    }

    @Test
    fun cardCanLandAfterMirrorWithinTolerance() {
        val mirror = 1_000_000L

        assertEquals(0, indexOfMirroredReport(listOf(assistant(mirror)), mirror + 50_000))
    }

    @Test
    fun cardCanLandBeforeMirrorWithinTolerance() {
        val card = 1_000_000L

        assertEquals(0, indexOfMirroredReport(listOf(assistant(card + 50_000)), card))
    }

    @Test
    fun toleranceBoundaryIsInclusiveOnBothSides() {
        val card = 1_000_000L

        assertEquals(0, indexOfMirroredReport(listOf(assistant(card - MIRRORED_REPORT_TOLERANCE_MS)), card))
        assertEquals(0, indexOfMirroredReport(listOf(assistant(card + MIRRORED_REPORT_TOLERANCE_MS)), card))
    }

    @Test
    fun oneMillisecondOutsideToleranceIsRejectedOnBothSides() {
        val card = 1_000_000L

        assertEquals(-1, indexOfMirroredReport(listOf(assistant(card - MIRRORED_REPORT_TOLERANCE_MS - 1)), card))
        assertEquals(-1, indexOfMirroredReport(listOf(assistant(card + MIRRORED_REPORT_TOLERANCE_MS + 1)), card))
    }

    @Test
    fun nearestEligibleAssistantWinsRegardlessOfListOrder() {
        val history = listOf(assistant(1_050), assistant(999), assistant(1_010), assistant(900))

        assertEquals(1, indexOfMirroredReport(history, 1_000))
    }

    @Test
    fun equalDistanceTieKeepsFirstAssistantInTranscriptOrder() {
        val history = listOf(assistant(900, "first"), assistant(1_100, "second"))

        assertEquals(0, indexOfMirroredReport(history, 1_000))
    }

    @Test
    fun duplicateExactTimestampsKeepFirstRow() {
        val history = listOf(assistant(1_000, "first"), assistant(1_000, "second"))

        assertEquals(0, indexOfMirroredReport(history, 1_000))
    }

    @Test
    fun ignoredExactUserDoesNotPreventNearbyAssistantMatch() {
        val history = listOf(row(History.Role.USER, 1_000), assistant(1_001))

        assertEquals(1, indexOfMirroredReport(history, 1_000))
    }

    @Test
    fun maximumPositiveTimestampsDoNotOverflowDelta() {
        val history = listOf(assistant(Long.MAX_VALUE), assistant(1))

        assertEquals(0, indexOfMirroredReport(history, Long.MAX_VALUE - 1))
        assertEquals(1, indexOfMirroredReport(history, 1))
    }

    @Test
    fun extremePositiveRangeStillRejectsFarMirror() {
        val history = listOf(assistant(Long.MAX_VALUE))

        assertEquals(-1, indexOfMirroredReport(history, 1))
    }

    @Test
    fun largeHistoryReturnsAbsoluteIndexNotAssistantSubsequenceIndex() {
        val history = buildList {
            repeat(1_000) { add(row(History.Role.USER, it + 1L)) }
            add(assistant(50_000))
        }

        assertEquals(1_000, indexOfMirroredReport(history, 50_001))
    }

    @Test
    fun plainTextAndEmptyContentPassThroughByIdentityContent() {
        for (content in listOf("", "plain", "오늘의 보고서\n\n본문", "```markdown\ntext\n```")) {
            assertEquals(content, expandCollapsedReportFence(content))
        }
    }

    @Test
    fun fenceMustStartOnFirstLine() {
        val content = "prefix\n" + fence("""{"type":"accordion"}""")

        assertEquals(content, expandCollapsedReportFence(content))
    }

    @Test
    fun openerLanguageIsCaseSensitive() {
        for (opener in listOf("```Deneb-ui", "```DENEB-UI", "```denebui", "```json")) {
            val content = "$opener\n{\"type\":\"accordion\"}\n```"
            assertEquals(content, expandCollapsedReportFence(content))
        }
    }

    @Test
    fun surroundingWhitespaceOnCanonicalOpenerIsAcceptedAndPreserved() {
        val content = "  ```deneb-ui  \n{\"type\":\"accordion\"}\n```"

        val expanded = expandCollapsedReportFence(content)

        assertTrue(expanded.startsWith("  ```deneb-ui  \n"))
        assertTrue("\"expanded\":true" in expanded)
    }

    @Test
    fun missingClosingFencePassesThrough() {
        val content = "```deneb-ui\n{\"type\":\"accordion\"}"

        assertEquals(content, expandCollapsedReportFence(content))
    }

    @Test
    fun tooShortFencePassesThrough() {
        assertEquals("```deneb-ui\n```", expandCollapsedReportFence("```deneb-ui\n```"))
    }

    @Test
    fun malformedJsonPassesThrough() {
        for (payload in listOf("", "{", "not-json", "null")) {
            val content = fence(payload)
            assertEquals(content, expandCollapsedReportFence(content), payload)
        }
    }

    @Test
    fun jsonArrayRootPassesThrough() {
        val content = fence("""[{"type":"accordion"}]""")

        assertEquals(content, expandCollapsedReportFence(content))
    }

    @Test
    fun missingOrNonAccordionTypePassesThrough() {
        for (payload in listOf("{}", "{\"title\":\"x\"}", "{\"type\":\"card\"}", "{\"type\":\"Accordion\"}")) {
            val content = fence(payload)
            assertEquals(content, expandCollapsedReportFence(content), payload)
        }
    }

    @Test
    fun maliciousObjectTypePassesThroughInsteadOfThrowing() {
        val content = fence("""{"type":{"nested":"accordion"},"title":"x"}""")

        assertEquals(content, expandCollapsedReportFence(content))
    }

    @Test
    fun maliciousArrayAndBooleanTypesPassThrough() {
        for (payload in listOf("{\"type\":[\"accordion\"]}", "{\"type\":true}", "{\"type\":7}", "{\"type\":null}")) {
            val content = fence(payload)
            assertEquals(content, expandCollapsedReportFence(content), payload)
        }
    }

    @Test
    fun accordionWithoutExpandedFieldGetsBooleanTrue() {
        val expanded = expandCollapsedReportFence(fence("""{"type":"accordion","title":"x"}"""))

        assertTrue(expandedPayload(expanded)["expanded"]!!.jsonPrimitive.boolean)
    }

    @Test
    fun falseExpandedFieldIsOverwrittenTrue() {
        val expanded = expandCollapsedReportFence(fence("""{"type":"accordion","expanded":false}"""))

        assertTrue(expandedPayload(expanded)["expanded"]!!.jsonPrimitive.boolean)
    }

    @Test
    fun malformedStringExpandedFieldIsReplacedByBoolean() {
        val expanded = expandCollapsedReportFence(fence("""{"type":"accordion","expanded":"false"}"""))
        val value = expandedPayload(expanded)["expanded"]!!

        assertTrue(value.jsonPrimitive.boolean)
        assertFalse(value.toString().startsWith("\""))
    }

    @Test
    fun existingTrueExpansionIsIdempotent() {
        val once = expandCollapsedReportFence(fence("""{"type":"accordion","expanded":true,"title":"x"}"""))
        val twice = expandCollapsedReportFence(once)

        assertEquals(once, twice)
    }

    @Test
    fun nestedChildrenAndUnknownFieldsSurviveRewrite() {
        val content = fence(
            """{"type":"accordion","title":"보고서","future":{"x":1},"children":[{"type":"markdown","value":"본문"}]}""",
        )

        val payload = expandedPayload(expandCollapsedReportFence(content))

        assertEquals("보고서", payload["title"]?.jsonPrimitive?.content)
        assertEquals(1, payload["future"]?.jsonObject?.get("x")?.jsonPrimitive?.content?.toInt())
        assertEquals("본문", payload["children"]?.toString()?.let { if ("본문" in it) "본문" else "" })
    }

    @Test
    fun multilinePrettyPrintedPayloadIsAccepted() {
        val content = fence(
            """
            {
              "type": "accordion",
              "title": "Pretty",
              "children": []
            }
            """.trimIndent(),
        )

        val expanded = expandCollapsedReportFence(content)

        assertTrue("\"expanded\":true" in expanded)
        assertTrue("\"title\":\"Pretty\"" in expanded)
    }

    @Test
    fun suffixAfterClosingFenceIsPreservedExactly() {
        val suffix = "\n\nTrailing prose\n- item"
        val content = fence("""{"type":"accordion"}""", suffix)

        assertTrue(expandCollapsedReportFence(content).endsWith(suffix))
    }

    @Test
    fun firstClosingFenceEndsPayloadAndLaterFenceRemainsSuffix() {
        val suffix = "\n```json\n{\"other\":true}\n```"
        val content = fence("""{"type":"accordion"}""", suffix)

        val expanded = expandCollapsedReportFence(content)

        assertTrue(expanded.endsWith(suffix))
        assertTrue("\"expanded\":true" in expanded.substringBefore(suffix))
    }

    @Test
    fun whitespaceAroundClosingFenceIsAcceptedAndPreserved() {
        val content = "```deneb-ui\n{\"type\":\"accordion\"}\n  ```  \ntrailing"

        val expanded = expandCollapsedReportFence(content)

        assertTrue("\n  ```  \ntrailing" in expanded)
    }

    @Test
    fun crlfPayloadIsParsedWithoutLeakingCarriageReturnsIntoJson() {
        val content = "```deneb-ui\r\n{\"type\":\"accordion\",\"title\":\"x\"}\r\n```"

        val expanded = expandCollapsedReportFence(content)

        assertTrue("\"expanded\":true" in expanded)
        assertTrue("\"title\":\"x\"" in expanded)
    }

    @Test
    fun veryLargeAccordionBodyRewritesWithoutTruncation() {
        val body = "한글🚀".repeat(50_000)
        val content = fence("""{"type":"accordion","body":"$body"}""")

        val expanded = expandCollapsedReportFence(content)

        assertEquals(body, expandedPayload(expanded)["body"]?.jsonPrimitive?.content)
    }
}
