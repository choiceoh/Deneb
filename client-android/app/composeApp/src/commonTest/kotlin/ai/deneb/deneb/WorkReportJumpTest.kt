package ai.deneb.deneb

import ai.deneb.ui.chat.History
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class WorkReportJumpTest {

    private fun msg(role: History.Role, ts: Long, content: String = "x") =
        History(role = role, content = content, timestampMs = ts)

    // --- indexOfMirroredReport ------------------------------------------------

    @Test
    fun matchesNearestAssistantMessage() {
        val history = listOf(
            msg(History.Role.ASSISTANT, 1_000_000),
            msg(History.Role.USER, 1_500_000),
            msg(History.Role.ASSISTANT, 2_000_000),
            msg(History.Role.ASSISTANT, 3_000_000),
        )
        // Card stamped a few ms after the 2_000_000 mirror — the real-world gap.
        assertEquals(2, indexOfMirroredReport(history, 2_000_012))
    }

    @Test
    fun ignoresUserRowsAndMissingTimestamps() {
        val history = listOf(
            // USER row sits exactly on the card stamp; must never match.
            msg(History.Role.USER, 2_000_000),
            // Assistant row without a recorded timestamp (live streaming row).
            msg(History.Role.ASSISTANT, 0),
            msg(History.Role.ASSISTANT, 2_000_500),
        )
        assertEquals(2, indexOfMirroredReport(history, 2_000_000))
    }

    @Test
    fun rejectsWhenNothingWithinTolerance() {
        val history = listOf(msg(History.Role.ASSISTANT, 1_000_000))
        assertEquals(-1, indexOfMirroredReport(history, 1_000_000 + MIRRORED_REPORT_TOLERANCE_MS + 1))
    }

    @Test
    fun rejectsUnstampedCard() {
        val history = listOf(msg(History.Role.ASSISTANT, 1_000_000))
        assertEquals(-1, indexOfMirroredReport(history, 0))
    }

    // --- expandCollapsedReportFence -------------------------------------------

    // The canonical server shape from denebui.CollapsedReportFence: a single-line
    // accordion JSON between deneb-ui fences.
    private val serverFence =
        "```deneb-ui\n" +
            """{"type":"accordion","title":"📬 메일 분석","children":[{"type":"markdown","value":"본문\n둘째 줄"}]}""" +
            "\n```"

    @Test
    fun expandsCollapsedAccordion() {
        val out = expandCollapsedReportFence(serverFence)
        assertTrue(out.startsWith("```deneb-ui\n"), "fence opener must survive: $out")
        assertTrue(out.endsWith("\n```"), "fence closer must survive: $out")
        assertTrue("\"expanded\":true" in out, "expanded flag missing: $out")
        assertTrue("\"title\":\"📬 메일 분석\"" in out, "title must survive: $out")
        assertTrue("둘째 줄" in out, "body must survive: $out")
    }

    @Test
    fun leavesPlainProseUntouched() {
        val prose = "오늘의 모닝레터\n\n- 일정 3건"
        assertEquals(prose, expandCollapsedReportFence(prose))
    }

    @Test
    fun leavesNonAccordionFenceUntouched() {
        val fence = "```deneb-ui\n{\"type\":\"card\",\"title\":\"x\"}\n```"
        assertEquals(fence, expandCollapsedReportFence(fence))
    }

    @Test
    fun leavesMalformedJsonUntouched() {
        val fence = "```deneb-ui\n{\"type\":\"accordion\",\n```"
        assertEquals(fence, expandCollapsedReportFence(fence))
    }

    @Test
    fun leavesUnclosedFenceUntouched() {
        val fence = "```deneb-ui\n{\"type\":\"accordion\"}"
        assertEquals(fence, expandCollapsedReportFence(fence))
    }
}
