package ai.deneb.ui.chat.composables

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class WorkFeedSourcePresentationTest {
    @Test
    fun groupwareApprovalUsesDistinctCheckedDocumentSemantics() {
        val presentation = workFeedSourcePresentation("groupware-approval")

        assertEquals(WorkFeedSourceIcon.APPROVAL, presentation.icon)
        assertEquals("전자결재", presentation.label)
    }

    @Test
    fun unknownSourceKeepsGenericDocumentFallback() {
        val presentation = workFeedSourcePresentation("future-source")

        assertEquals(WorkFeedSourceIcon.DOCUMENT, presentation.icon)
        assertNull(presentation.label)
    }

    @Test
    fun autonomousProducersGetDistinctGlyphs() {
        // The gateway sends 17 sources; each autonomous producer must be told
        // apart by shape (the color channel stays reserved for urgency).
        val cases = mapOf(
            "dream" to (WorkFeedSourceIcon.DREAM to "드림"),
            "dream-digest" to (WorkFeedSourceIcon.DIGEST to "기억 다이제스트"),
            "self-correction" to (WorkFeedSourceIcon.TRUST to "자기교정"),
            "deal_question" to (WorkFeedSourceIcon.QUESTION to "질문"),
            "proactive" to (WorkFeedSourceIcon.REPORT to "리포트"),
            "meeting_report" to (WorkFeedSourceIcon.MEETING to "회의"),
            "groupware-board" to (WorkFeedSourceIcon.BOARD to "공지"),
            "kb-interview" to (WorkFeedSourceIcon.INTERVIEW to "인터뷰"),
            "heartbeat" to (WorkFeedSourceIcon.HEARTBEAT to "하트비트"),
            "system_log" to (WorkFeedSourceIcon.LOG to "로그"),
        )
        cases.forEach { (source, expected) ->
            val presentation = workFeedSourcePresentation(source)
            assertEquals(expected.first, presentation.icon, "icon for $source")
            assertEquals(expected.second, presentation.label, "label for $source")
        }
        // Genesis lanes share one glyph — same producer family, different stages.
        listOf("genesis-meta", "genesis-evolve-verdict", "genesis-ladder").forEach { source ->
            val presentation = workFeedSourcePresentation(source)
            assertEquals(WorkFeedSourceIcon.GENESIS, presentation.icon, "icon for $source")
            assertEquals("자가개선", presentation.label, "label for $source")
        }
    }
}
